package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	muxhandlers "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/handlers"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/network"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/sbot"
)

const roomMemberIngestRetry = 2 * time.Second

type roomMemberIngestAccountLister interface {
	GetAllBridgedAccounts(ctx context.Context) ([]db.BridgedAccount, error)
}

type roomMemberIngestManagerConfig struct {
	AccountLister roomMemberIngestAccountLister
	RoomRuntime   *room.Runtime
	Sbot          *sbot.Sbot
	ReceiveLog    feedlog.Log
	Store         *feedlog.StoreImpl
	AppKey        string
}

type roomMemberIngestSession struct {
	feed   refs.FeedRef
	cancel context.CancelFunc
	done   chan struct{}
}

type roomMemberIngestManager struct {
	cfg       roomMemberIngestManagerConfig
	logger    *log.Logger
	netClient *network.Client

	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	started  bool
	sessions map[string]*roomMemberIngestSession
	wg       sync.WaitGroup
}

type roomHistoryEnvelope struct {
	Key       string                 `json:"key"`
	Value     roomHistorySignedValue `json:"value"`
	Timestamp float64                `json:"timestamp"`
}

type roomHistorySignedValue struct {
	Previous  *string         `json:"previous"`
	Author    string          `json:"author"`
	Sequence  int64           `json:"sequence"`
	Timestamp int64           `json:"timestamp"`
	Hash      string          `json:"hash"`
	Content   json.RawMessage `json:"content"`
	Signature string          `json:"signature"`
}

func newRoomMemberIngestManager(cfg roomMemberIngestManagerConfig, logger *log.Logger) (*roomMemberIngestManager, error) {
	logger = logutil.Ensure(logger)
	switch {
	case cfg.RoomRuntime == nil:
		return nil, fmt.Errorf("room member ingest: room runtime is required")
	case cfg.Sbot == nil:
		return nil, fmt.Errorf("room member ingest: sbot is required")
	case cfg.ReceiveLog == nil:
		return nil, fmt.Errorf("room member ingest: receive log is required")
	case cfg.Store == nil:
		return nil, fmt.Errorf("room member ingest: store is required")
	}

	return &roomMemberIngestManager{
		cfg:    cfg,
		logger: logger,
		netClient: network.NewClient(network.Options{
			KeyPair: cfg.Sbot.KeyPair,
			AppKey:  cfg.AppKey,
		}),
		sessions: make(map[string]*roomMemberIngestSession),
	}, nil
}

func (m *roomMemberIngestManager) Start(parent context.Context) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return
	}
	m.ctx, m.cancel = context.WithCancel(parent)
	m.started = true
}

func (m *roomMemberIngestManager) Stop() error {
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
	sessions := make([]*roomMemberIngestSession, 0, len(m.sessions))
	for key, sess := range m.sessions {
		sessions = append(sessions, sess)
		delete(m.sessions, key)
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

func (m *roomMemberIngestManager) Announce(feed refs.FeedRef) error {
	if m == nil {
		return nil
	}
	if m.shouldIgnoreFeed(feed) {
		return nil
	}
	m.ensureSession(feed)
	return nil
}

func (m *roomMemberIngestManager) shouldIgnoreFeed(feed refs.FeedRef) bool {
	if feed == (refs.FeedRef{}) {
		return true
	}
	if m.cfg.Sbot != nil && m.cfg.Sbot.KeyPair != nil && feed.Equal(m.cfg.Sbot.KeyPair.FeedRef()) {
		return true
	}
	if m.cfg.RoomRuntime != nil && feed.Equal(m.cfg.RoomRuntime.RoomFeed()) {
		return true
	}
	if m.cfg.AccountLister == nil {
		return false
	}

	accounts, err := m.cfg.AccountLister.GetAllBridgedAccounts(m.managerContext())
	if err != nil {
		m.logger.Printf("event=room_member_ingest_list_accounts_failed feed=%s err=%v", feed.String(), err)
		return false
	}
	for _, account := range accounts {
		if !account.Active {
			continue
		}
		accountFeed, err := refs.ParseFeedRef(strings.TrimSpace(account.SSBFeedID))
		if err == nil && accountFeed.Equal(feed) {
			return true
		}
	}
	return false
}

func (m *roomMemberIngestManager) managerContext() context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil {
		return context.Background()
	}
	return m.ctx
}

func (m *roomMemberIngestManager) ensureSession(feed refs.FeedRef) {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	key := feed.String()
	if _, ok := m.sessions[key]; ok {
		m.mu.Unlock()
		return
	}
	sessionCtx, cancel := context.WithCancel(m.ctx)
	sess := &roomMemberIngestSession{
		feed:   feed,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	m.sessions[key] = sess
	m.mu.Unlock()

	m.logger.Printf("event=room_member_ingest_session_started feed=%s", feed.String())
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer close(sess.done)
		m.runSession(sessionCtx, sess)
	}()
}

func (m *roomMemberIngestManager) runSession(ctx context.Context, sess *roomMemberIngestSession) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := m.streamFeed(ctx, sess.feed); err != nil && ctx.Err() == nil {
			m.logger.Printf("event=room_member_ingest_stream_failed feed=%s err=%v", sess.feed.String(), err)
		} else if ctx.Err() == nil {
			m.logger.Printf("event=room_member_ingest_stream_ended feed=%s", sess.feed.String())
		}
		if !waitForRetry(ctx, roomMemberIngestRetry) {
			return
		}
	}
}

func (m *roomMemberIngestManager) streamFeed(ctx context.Context, target refs.FeedRef) error {
	roomPeer, err := m.netClient.Connect(ctx, m.cfg.RoomRuntime.Addr(), m.cfg.RoomRuntime.RoomFeed().PubKey(), m.cfg.Sbot.HandlerMux())
	if err != nil {
		return fmt.Errorf("connect room: %w", err)
	}
	defer roomPeer.Conn.Close()
	m.logger.Printf("event=room_member_ingest_room_connected feed=%s room=%s", target.String(), m.cfg.RoomRuntime.Addr())

	roomRPC := roomPeer.RPC()
	if roomRPC == nil {
		return fmt.Errorf("room rpc unavailable")
	}

	tunnelSource, tunnelSink, err := roomRPC.Duplex(ctx, muxrpc.TypeBinary, muxrpc.Method{"tunnel", "connect"}, map[string]any{
		"portal": m.cfg.RoomRuntime.RoomFeed(),
		"target": target,
	})
	if err != nil {
		return fmt.Errorf("tunnel.connect %s: %w", target.String(), err)
	}
	m.logger.Printf("event=room_member_ingest_tunnel_connected feed=%s", target.String())

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	streamConn := muxrpc.NewByteStreamConn(streamCtx, tunnelSource, tunnelSink, roomPeer.Conn.RemoteAddr())
	endpoint := muxrpc.NewServer(streamCtx, streamConn, nil, nil)
	defer endpoint.Terminate()
	defer func() {
		_ = streamConn.Close()
		tunnelSource.Cancel(nil)
		_ = tunnelSink.Close()
	}()

	lastSeq, err := m.lastKnownSeq(target.String())
	if err != nil {
		return err
	}
	keys := true
	old := true
	args := muxhandlers.HistoryStreamArgs{
		ID:       target.String(),
		Sequence: lastSeq,
		Live:     true,
		Old:      &old,
		Keys:     &keys,
	}
	historySource, err := endpoint.Source(streamCtx, muxrpc.TypeJSON, muxrpc.Method{"createHistoryStream"}, args)
	if err != nil {
		return fmt.Errorf("open createHistoryStream for %s: %w", target.String(), err)
	}
	m.logger.Printf("event=room_member_ingest_history_opened feed=%s start_seq=%d", target.String(), lastSeq)

	for historySource.Next(streamCtx) {
		payload, err := historySource.Bytes()
		if err != nil {
			return fmt.Errorf("read createHistoryStream frame for %s: %w", target.String(), err)
		}
		if err := m.ingestHistoryFrame(target, payload); err != nil {
			return fmt.Errorf("ingest history frame for %s: %w", target.String(), err)
		}
	}
	if err := historySource.Err(); err != nil && err != io.EOF && ctx.Err() == nil {
		return fmt.Errorf("history stream ended for %s: %w", target.String(), err)
	}
	m.logger.Printf("event=room_member_ingest_history_closed feed=%s", target.String())
	return nil
}

func (m *roomMemberIngestManager) lastKnownSeq(author string) (int64, error) {
	log, err := m.cfg.Store.Logs().Get(author)
	if err == feedlog.ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get feed log for %s: %w", author, err)
	}
	seq, err := log.Seq()
	if err != nil || seq < 0 {
		return 0, err
	}
	return seq, nil
}

func (m *roomMemberIngestManager) ingestHistoryFrame(target refs.FeedRef, payload []byte) error {
	var env roomHistoryEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return fmt.Errorf("decode history envelope: %w", err)
	}

	sig, err := legacy.NewSignatureFromBase64([]byte(strings.TrimSpace(env.Value.Signature)))
	if err != nil {
		return fmt.Errorf("parse signature: %w", err)
	}
	author := strings.TrimSpace(env.Value.Author)
	if author == "" {
		return fmt.Errorf("history message missing author")
	}

	log, err := m.cfg.Store.Logs().Get(author)
	if err == feedlog.ErrNotFound {
		log, err = m.cfg.Store.Logs().Create(author)
	}
	if err != nil {
		return fmt.Errorf("open feed log for %s: %w", author, err)
	}

	currentSeq, err := log.Seq()
	if err != nil {
		return fmt.Errorf("read feed seq for %s: %w", author, err)
	}
	if env.Value.Sequence <= currentSeq {
		return nil
	}
	if currentSeq >= 0 && env.Value.Sequence != currentSeq+1 {
		return fmt.Errorf("history sequence gap for %s: have=%d got=%d", author, currentSeq, env.Value.Sequence)
	}

	metadata := &feedlog.Metadata{
		Author:    author,
		Sequence:  env.Value.Sequence,
		Timestamp: env.Value.Timestamp,
		Sig:       sig,
		Hash:      strings.TrimSpace(env.Key),
	}
	if env.Value.Previous != nil {
		metadata.Previous = strings.TrimSpace(*env.Value.Previous)
	}

	content := bytes.TrimSpace([]byte(env.Value.Content))
	if _, err := log.Append(content, metadata); err != nil {
		return fmt.Errorf("append feed log for %s: %w", author, err)
	}

	rawSigned, err := roomHistoryRawSignedMessage(env, sig)
	if err != nil {
		return fmt.Errorf("rebuild signed message for %s: %w", author, err)
	}
	if metadata.Hash == "" {
		msgRef, refErr := legacy.SignedMessageRefFromJSON(rawSigned)
		if refErr != nil {
			return fmt.Errorf("derive message ref for %s: %w", author, refErr)
		}
		metadata.Hash = msgRef.String()
	}
	if _, err := m.cfg.ReceiveLog.Append(rawSigned, metadata); err != nil {
		return fmt.Errorf("append receive log for %s: %w", author, err)
	}

	parsedFeed, err := refs.ParseFeedRef(author)
	if err == nil {
		m.cfg.Sbot.Replicate(*parsedFeed)
		m.cfg.Sbot.NotifyFeedSeq(parsedFeed, env.Value.Sequence)
	}
	m.logger.Printf("event=room_member_ingest_appended feed=%s seq=%d target=%s", author, env.Value.Sequence, target.String())
	return nil
}

func roomHistoryRawSignedMessage(env roomHistoryEnvelope, sig legacy.Signature) ([]byte, error) {
	author, err := refs.ParseFeedRef(strings.TrimSpace(env.Value.Author))
	if err != nil {
		return nil, fmt.Errorf("parse author: %w", err)
	}

	var previous *refs.MessageRef
	if env.Value.Previous != nil && strings.TrimSpace(*env.Value.Previous) != "" {
		previous, err = refs.ParseMessageRef(strings.TrimSpace(*env.Value.Previous))
		if err != nil {
			return nil, fmt.Errorf("parse previous: %w", err)
		}
	}

	contentBytes := bytes.TrimSpace([]byte(env.Value.Content))
	var content any
	if err := json.Unmarshal(contentBytes, &content); err != nil {
		content = string(contentBytes)
	}

	msg := &legacy.Message{
		Previous:  previous,
		Author:    *author,
		Sequence:  env.Value.Sequence,
		Timestamp: env.Value.Timestamp,
		Hash:      strings.TrimSpace(env.Value.Hash),
		Content:   content,
	}
	if msg.Hash == "" {
		msg.Hash = legacy.HashAlgorithm
	}
	return msg.MarshalWithSignature(sig)
}
