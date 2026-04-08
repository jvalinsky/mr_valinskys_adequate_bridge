package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bots"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	legacyhandlers "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/handlers"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/network"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
)

const (
	defaultBridgedRoomPeerSyncInterval = 30 * time.Second
	bridgedRoomPeerConnectRetry        = 2 * time.Second
)

type bridgedRoomPeerSessionManager interface {
	Start(ctx context.Context)
	Reconcile(ctx context.Context) error
	Stop() error
}

type bridgedRoomPeerAccountLister interface {
	GetAllBridgedAccounts(ctx context.Context) ([]db.BridgedAccount, error)
}

type bridgedRoomPeerManagerConfig struct {
	AccountLister bridgedRoomPeerAccountLister
	RoomRuntime   *room.Runtime
	Store         *feedlog.StoreImpl
	BotSeed       string
	AppKey        string
	SyncInterval  time.Duration
}

type bridgedRoomPeerSession struct {
	did    string
	feed   refs.FeedRef
	key    *keys.KeyPair
	cancel context.CancelFunc
	done   chan struct{}
}

// BridgedRoomPeerManager keeps one room-connected peer session per active bridged DID.
type BridgedRoomPeerManager struct {
	cfg    bridgedRoomPeerManagerConfig
	logger *log.Logger

	keyManager *bots.Manager

	mu       sync.Mutex
	sessions map[string]*bridgedRoomPeerSession
	started  bool
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	ensureMemberFn func(context.Context, refs.FeedRef) error
	runSessionFn   func(context.Context, *bridgedRoomPeerSession)
}

func newBridgedRoomPeerManager(cfg bridgedRoomPeerManagerConfig, logger *log.Logger) (*BridgedRoomPeerManager, error) {
	logger = logutil.Ensure(logger)
	if cfg.AccountLister == nil {
		return nil, fmt.Errorf("bridged peer manager: account lister is required")
	}
	if strings.TrimSpace(cfg.BotSeed) == "" {
		return nil, fmt.Errorf("bridged peer manager: bot seed is required")
	}
	if cfg.SyncInterval <= 0 {
		cfg.SyncInterval = defaultBridgedRoomPeerSyncInterval
	}

	m := &BridgedRoomPeerManager{
		cfg:        cfg,
		logger:     logger,
		keyManager: bots.NewManager([]byte(cfg.BotSeed), nil, nil, nil),
		sessions:   make(map[string]*bridgedRoomPeerSession),
	}
	m.ensureMemberFn = m.ensureRoomMember
	m.runSessionFn = m.runSession
	return m, nil
}

func (m *BridgedRoomPeerManager) Start(parent context.Context) {
	if m == nil {
		return
	}

	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.ctx, m.cancel = context.WithCancel(parent)
	m.started = true
	m.mu.Unlock()

	m.wg.Add(1)
	go m.reconcileLoop()
}

func (m *BridgedRoomPeerManager) Stop() error {
	if m == nil {
		return nil
	}

	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = false
	cancel := m.cancel
	sessions := make([]*bridgedRoomPeerSession, 0, len(m.sessions))
	for did, sess := range m.sessions {
		sessions = append(sessions, sess)
		delete(m.sessions, did)
	}
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		if sess.cancel != nil {
			sess.cancel()
		}
		<-sess.done
	}

	m.wg.Wait()
	return nil
}

func (m *BridgedRoomPeerManager) Reconcile(ctx context.Context) error {
	if m == nil {
		return nil
	}

	accounts, err := m.cfg.AccountLister.GetAllBridgedAccounts(ctx)
	if err != nil {
		return fmt.Errorf("list bridged accounts: %w", err)
	}

	active := make(map[string]db.BridgedAccount, len(accounts))
	for _, account := range accounts {
		if !account.Active {
			continue
		}
		did := strings.TrimSpace(account.ATDID)
		if did == "" {
			continue
		}
		active[did] = account
	}

	for did, account := range active {
		kp, err := m.keyManager.GetKeyPair(did)
		if err != nil {
			m.logger.Printf("event=bridged_room_peer_key_derive_failed did=%s err=%v", did, err)
			continue
		}

		expectedFeed := kp.FeedRef()
		mappedFeed, err := refs.ParseFeedRef(strings.TrimSpace(account.SSBFeedID))
		if err != nil {
			m.logger.Printf("event=bridged_room_peer_invalid_feed did=%s feed=%q err=%v", did, account.SSBFeedID, err)
			continue
		}
		if !mappedFeed.Equal(expectedFeed) {
			m.logger.Printf(
				"event=bridged_room_peer_feed_mismatch did=%s mapped=%s derived=%s",
				did,
				mappedFeed.String(),
				expectedFeed.String(),
			)
			continue
		}

		if err := m.ensureMemberFn(ctx, expectedFeed); err != nil {
			m.logger.Printf("event=bridged_room_peer_member_ensure_failed did=%s feed=%s err=%v", did, expectedFeed.String(), err)
			continue
		}
		m.ensureSession(did, expectedFeed, kp)
	}

	var stale []string
	m.mu.Lock()
	for did := range m.sessions {
		if _, ok := active[did]; !ok {
			stale = append(stale, did)
		}
	}
	m.mu.Unlock()

	for _, did := range stale {
		m.stopSession(did)
	}

	return nil
}

func (m *BridgedRoomPeerManager) reconcileLoop() {
	defer m.wg.Done()

	ctx := m.managerContext()
	_ = m.Reconcile(ctx)

	ticker := time.NewTicker(m.cfg.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.Reconcile(ctx); err != nil {
				m.logger.Printf("event=bridged_room_peer_reconcile_failed err=%v", err)
			}
		}
	}
}

func (m *BridgedRoomPeerManager) managerContext() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil {
		return context.Background()
	}
	return m.ctx
}

func (m *BridgedRoomPeerManager) ensureRoomMember(ctx context.Context, feed refs.FeedRef) error {
	if m.cfg.RoomRuntime == nil || m.cfg.RoomRuntime.RoomDB() == nil {
		return fmt.Errorf("room runtime unavailable")
	}

	if _, err := m.cfg.RoomRuntime.RoomDB().Members().GetByFeed(ctx, feed); err == nil {
		return nil
	}
	return m.cfg.RoomRuntime.AddMember(ctx, feed, roomdb.RoleMember)
}

func (m *BridgedRoomPeerManager) ensureSession(did string, feed refs.FeedRef, key *keys.KeyPair) {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	if _, ok := m.sessions[did]; ok {
		m.mu.Unlock()
		return
	}
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	sess := &bridgedRoomPeerSession{
		did:    did,
		feed:   feed,
		key:    key,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	m.sessions[did] = sess
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer close(sess.done)
		m.runSessionFn(sessionCtx, sess)
	}()
}

func (m *BridgedRoomPeerManager) stopSession(did string) {
	m.mu.Lock()
	sess, ok := m.sessions[did]
	if ok {
		delete(m.sessions, did)
	}
	m.mu.Unlock()
	if !ok || sess == nil {
		return
	}
	if sess.cancel != nil {
		sess.cancel()
	}
	<-sess.done
}

func (m *BridgedRoomPeerManager) runSession(ctx context.Context, sess *bridgedRoomPeerSession) {
	if m.cfg.RoomRuntime == nil {
		return
	}
	if m.cfg.Store == nil {
		return
	}

	handler := newBridgedPeerSessionHandler(sess.feed, sess.key, m.cfg.Store, m.logger)
	client := network.NewClient(network.Options{
		KeyPair: sess.key,
		AppKey:  m.cfg.AppKey,
	})

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		peer, err := client.Connect(ctx, m.cfg.RoomRuntime.Addr(), m.cfg.RoomRuntime.RoomFeed().PubKey(), handler)
		if err != nil {
			m.logger.Printf("event=bridged_room_peer_connect_failed did=%s feed=%s err=%v", sess.did, sess.feed.String(), err)
			if !waitForRetry(ctx, bridgedRoomPeerConnectRetry) {
				return
			}
			continue
		}

		rpc := peer.RPC()
		if rpc == nil {
			_ = peer.Conn.Close()
			if !waitForRetry(ctx, bridgedRoomPeerConnectRetry) {
				return
			}
			continue
		}

		if err := announceRoomPeer(ctx, rpc); err != nil {
			m.logger.Printf("event=bridged_room_peer_announce_failed did=%s feed=%s err=%v", sess.did, sess.feed.String(), err)
			_ = peer.Conn.Close()
			if !waitForRetry(ctx, bridgedRoomPeerConnectRetry) {
				return
			}
			continue
		}

		m.logger.Printf("event=bridged_room_peer_session_started did=%s feed=%s room=%s", sess.did, sess.feed.String(), m.cfg.RoomRuntime.Addr())

		waitCh := rpc.Wait()
		ticker := time.NewTicker(m.cfg.SyncInterval)
		connected := true
		for connected {
			select {
			case <-ctx.Done():
				ticker.Stop()
				leaveCtx, leaveCancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = leaveRoomPeer(leaveCtx, rpc)
				leaveCancel()
				_ = peer.Conn.Close()
				return
			case <-waitCh:
				connected = false
			case <-ticker.C:
				if err := announceRoomPeer(ctx, rpc); err != nil {
					m.logger.Printf("event=bridged_room_peer_reannounce_failed did=%s feed=%s err=%v", sess.did, sess.feed.String(), err)
					connected = false
				}
			}
		}
		ticker.Stop()
		_ = peer.Conn.Close()
		if !waitForRetry(ctx, bridgedRoomPeerConnectRetry) {
			return
		}
	}
}

func newBridgedPeerSessionHandler(feed refs.FeedRef, key *keys.KeyPair, store *feedlog.StoreImpl, logger *log.Logger) muxrpc.Handler {
	historyHandler := &singleFeedHistoryHandler{
		feed:  feed,
		inner: legacyhandlers.NewHistoryStreamHandler(store),
	}

	inner := &muxrpc.HandlerMux{}
	inner.Register(muxrpc.Method{"whoami"}, legacyhandlers.NewWhoamiHandler(key))
	inner.Register(muxrpc.Method{"gossip", "ping"}, legacyhandlers.NewPingHandler())
	inner.Register(muxrpc.Method{"createHistoryStream"}, historyHandler)

	mux := &muxrpc.HandlerMux{}
	mux.Register(muxrpc.Method{"whoami"}, legacyhandlers.NewWhoamiHandler(key))
	mux.Register(muxrpc.Method{"gossip", "ping"}, legacyhandlers.NewPingHandler())
	mux.Register(muxrpc.Method{"createHistoryStream"}, historyHandler)
	mux.Register(muxrpc.Method{"ebt", "replicate"}, &noopDuplexHandler{
		method: muxrpc.Method{"ebt", "replicate"},
	})
	mux.Register(muxrpc.Method{"replicate", "upto"}, &emptySourceHandler{
		method: muxrpc.Method{"replicate", "upto"},
	})
	mux.Register(muxrpc.Method{"tunnel", "connect"}, &bridgedPeerTunnelConnectHandler{
		feed:   feed,
		inner:  inner,
		logger: logger,
	})
	return mux
}

type singleFeedHistoryHandler struct {
	feed  refs.FeedRef
	inner *legacyhandlers.HistoryStreamHandler
}

func (h *singleFeedHistoryHandler) Handled(m muxrpc.Method) bool {
	return len(m) == 1 && m[0] == "createHistoryStream"
}

func (h *singleFeedHistoryHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	requestedFeed, err := parseHistoryRequestFeed(req.RawArgs)
	if err != nil {
		req.CloseWithError(fmt.Errorf("createHistoryStream: parse args: %w", err))
		return
	}
	if !requestedFeed.Equal(h.feed) {
		req.CloseWithError(fmt.Errorf("createHistoryStream: feed %s not served by this peer", requestedFeed.String()))
		return
	}
	h.inner.HandleCall(ctx, req)
}

func (h *singleFeedHistoryHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

func parseHistoryRequestFeed(raw json.RawMessage) (refs.FeedRef, error) {
	var args []json.RawMessage
	if err := json.Unmarshal(raw, &args); err != nil {
		return refs.FeedRef{}, fmt.Errorf("expected muxrpc args array")
	}
	if len(args) != 1 {
		return refs.FeedRef{}, fmt.Errorf("expected exactly one argument")
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args[0], &payload); err != nil {
		return refs.FeedRef{}, err
	}
	feed, err := refs.ParseFeedRef(strings.TrimSpace(payload.ID))
	if err != nil {
		return refs.FeedRef{}, err
	}
	return *feed, nil
}

type bridgedPeerTunnelConnectHandler struct {
	feed   refs.FeedRef
	inner  muxrpc.Handler
	logger *log.Logger
}

func (h *bridgedPeerTunnelConnectHandler) Handled(m muxrpc.Method) bool {
	return len(m) == 2 && m[0] == "tunnel" && m[1] == "connect"
}

func (h *bridgedPeerTunnelConnectHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "duplex" {
		req.CloseWithError(fmt.Errorf("tunnel.connect is duplex"))
		return
	}

	args, err := parseTunnelConnectArgs(req.RawArgs)
	if err != nil {
		req.CloseWithError(fmt.Errorf("tunnel.connect: parse args: %w", err))
		return
	}
	if args.Target != (refs.FeedRef{}) && !args.Target.Equal(h.feed) {
		req.CloseWithError(fmt.Errorf("tunnel.connect: target mismatch want=%s got=%s", h.feed.String(), args.Target.String()))
		return
	}

	source := req.Source()
	if source == nil {
		req.CloseWithError(fmt.Errorf("tunnel.connect: request source unavailable"))
		return
	}
	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("tunnel.connect: response sink unavailable: %w", err))
		return
	}

	serverConn, tunnelConn := net.Pipe()
	defer serverConn.Close()
	defer tunnelConn.Close()

	innerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	innerRPC := muxrpc.NewServer(innerCtx, serverConn, h.inner, nil)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		h.copySourceToConn(innerCtx, source, tunnelConn, cancel)
	}()
	go func() {
		defer wg.Done()
		h.copyConnToSink(innerCtx, tunnelConn, sink, cancel)
	}()

	go func() {
		<-innerCtx.Done()
		_ = tunnelConn.Close()
		_ = serverConn.Close()
	}()

	wg.Wait()
	cancel()
	_ = sink.Close()
	_ = req.Close()
	_ = innerRPC.Terminate()
}

func (h *bridgedPeerTunnelConnectHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

func (h *bridgedPeerTunnelConnectHandler) copySourceToConn(ctx context.Context, source *muxrpc.ByteSource, conn net.Conn, cancel context.CancelFunc) {
	defer cancel()
	for {
		if !source.Next(ctx) {
			return
		}
		payload, err := source.Bytes()
		if err != nil {
			return
		}
		if len(payload) == 0 {
			continue
		}
		if _, err := conn.Write(payload); err != nil {
			return
		}
	}
}

func (h *bridgedPeerTunnelConnectHandler) copyConnToSink(ctx context.Context, conn net.Conn, sink *muxrpc.ByteSink, cancel context.CancelFunc) {
	defer cancel()
	buf := make([]byte, 32*1024)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := conn.Read(buf)
		if n > 0 {
			if _, werr := sink.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			if h.logger != nil {
				h.logger.Printf("event=bridged_room_peer_tunnel_copy_read_failed feed=%s err=%v", h.feed.String(), err)
			}
			return
		}
	}
}

type noopDuplexHandler struct {
	method muxrpc.Method
}

func (h *noopDuplexHandler) Handled(m muxrpc.Method) bool {
	return m.String() == h.method.String()
}

func (h *noopDuplexHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "duplex" {
		req.CloseWithError(fmt.Errorf("%s is duplex", h.method.String()))
		return
	}

	if source := req.Source(); source != nil {
		source.Cancel(nil)
	}
	sink, err := req.ResponseSink()
	if err == nil && sink != nil {
		_ = sink.Close()
	}
	_ = req.Close()
}

func (h *noopDuplexHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

type emptySourceHandler struct {
	method muxrpc.Method
}

func (h *emptySourceHandler) Handled(m muxrpc.Method) bool {
	return m.String() == h.method.String()
}

func (h *emptySourceHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "source" {
		req.CloseWithError(fmt.Errorf("%s is source", h.method.String()))
		return
	}
	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("%s: response sink: %w", h.method.String(), err))
		return
	}
	_ = sink.Close()
	_ = req.Close()
}

func (h *emptySourceHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

type tunnelConnectArgs struct {
	Origin refs.FeedRef `json:"origin"`
	Portal refs.FeedRef `json:"portal"`
	Target refs.FeedRef `json:"target"`
}

func parseTunnelConnectArgs(raw json.RawMessage) (tunnelConnectArgs, error) {
	var args []json.RawMessage
	if err := json.Unmarshal(raw, &args); err != nil {
		return tunnelConnectArgs{}, fmt.Errorf("expected muxrpc args array")
	}
	if len(args) == 0 {
		return tunnelConnectArgs{}, nil
	}
	if len(args) != 1 {
		return tunnelConnectArgs{}, fmt.Errorf("expected exactly one argument")
	}
	var parsed tunnelConnectArgs
	if err := json.Unmarshal(args[0], &parsed); err != nil {
		return tunnelConnectArgs{}, err
	}
	return parsed, nil
}
