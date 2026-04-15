package sbot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/blobs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/gossip"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/handlers"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/handlers/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/network"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/publisher"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/replication"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb/sqlite"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomstate"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
	"golang.org/x/crypto/ed25519"
)

type Options struct {
	RepoPath   string
	ListenAddr string
	KeyPair    *keys.KeyPair
	AppKey     string
	EnableEBT  bool
	Hops       int

	EnableRoom   bool
	RoomMode     string
	RoomHTTPAddr string

	GossipDB    gossip.Database
	DialTimeout time.Duration
}

type Sbot struct {
	ctx    context.Context
	cancel context.CancelFunc

	KeyPair *keys.KeyPair
	opts    Options

	store  *feedlog.StoreImpl
	ebt    *replication.EBTHandler
	state  *replication.StateMatrix
	blobs  *blobs.Store
	gossip *gossip.Manager

	netServer  *network.Server
	netClient  *network.Client
	manifest   *muxrpc.Manifest
	handlerMux *muxrpc.HandlerMux

	roomDB    *sqlite.DB
	roomState *roomstate.Manager
	roomSrv   *room.RoomServer

	Network         *SbotNetwork
	replicatedFeeds sync.Map

	mu     sync.RWMutex
	closed bool
}

func New(opts Options) (*Sbot, error) {
	if opts.AppKey == "" {
		opts.AppKey = ""
	}
	if opts.Hops == 0 {
		opts.Hops = 2
	}
	if opts.KeyPair == nil {
		secretPath := opts.RepoPath + "/secret"
		kp, err := keys.Load(secretPath)
		if err != nil {
			kp, err = keys.Generate()
			if err != nil {
				return nil, fmt.Errorf("sbot: failed to generate key pair: %w", err)
			}
			if saveErr := keys.Save(kp, secretPath); saveErr != nil {
				existingKP, loadErr := loadKeyPairWithRetry(secretPath, 5, 50*time.Millisecond)
				if loadErr != nil {
					return nil, fmt.Errorf("sbot: failed to save key pair: %v (reload existing secret failed: %w)", saveErr, loadErr)
				}
				kp = existingKP
			}
		}
		opts.KeyPair = kp
	}

	feedStore, err := feedlog.NewStore(feedlog.Config{
		DBPath:     opts.RepoPath + "/flume.sqlite",
		RepoPath:   opts.RepoPath,
		BlobSubdir: "blobs",
	})
	if err != nil {
		return nil, fmt.Errorf("sbot: failed to create store: %w", err)
	}

	feedStore.SetSignatureVerifier(&feedlog.DefaultSignatureVerifier{})

	selfRef := opts.KeyPair.FeedRef()
	stateMatrix, err := replication.NewStateMatrix(
		opts.RepoPath+"/ebt-state",
		&selfRef,
		feedStore,
	)
	if err != nil {
		return nil, fmt.Errorf("sbot: failed to create state matrix: %w", err)
	}

	if err := stateMatrix.InitializeFromFeedlog(); err != nil {
		return nil, fmt.Errorf("sbot: failed to initialize EBT state: %w", err)
	}

	feedManagerAdapter := NewFeedManagerAdapter(feedStore)
	feedReplicator := NewFeedReplicator(feedStore)
	ebtHandler := replication.NewEBTHandler(&selfRef, feedManagerAdapter, stateMatrix, feedReplicator)

	blobStore := blobs.NewStore(feedStore.Blobs())

	dialTimeout := opts.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 10 * time.Second
	}
	netServer, err := network.NewServer(network.Options{
		ListenAddr: opts.ListenAddr,
		KeyPair:    opts.KeyPair,
		AppKey:     opts.AppKey,
		Timeout:    dialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("sbot: failed to create server: %w", err)
	}

	netClient := network.NewClient(network.Options{
		KeyPair: opts.KeyPair,
		AppKey:  opts.AppKey,
		Timeout: dialTimeout,
	})

	manifest := newManifest(opts.EnableEBT, opts.EnableRoom, opts.RoomHTTPAddr != "")
	handlerMux := &muxrpc.HandlerMux{}

	registerHandlers(handlerMux, feedStore, ebtHandler, blobStore, opts.KeyPair)

	gossipMgr := gossip.NewManager(netClient, ebtHandler, handlerMux, opts.GossipDB, nil)

	var roomDB *sqlite.DB
	var roomState *roomstate.Manager
	var roomSrv *room.RoomServer

	if opts.EnableRoom {
		var err error
		roomDB, err = sqlite.Open(opts.RepoPath + "/room.sqlite")
		if err != nil {
			return nil, fmt.Errorf("sbot: failed to open room db: %w", err)
		}

		roomState = roomstate.NewManager()
		feedRef := opts.KeyPair.FeedRef()
		roomSrv = room.NewRoomServer(
			&feedRef,
			roomDB.Members(),
			roomDB.Aliases(),
			roomDB.Invites(),
			roomDB.DeniedKeys(),
			roomDB.RoomConfig(),
			roomState,
			"",
		)

		aliasHandler := room.NewAliasHandler(roomSrv)
		handlerMux.Register(muxrpc.Method{"room", "registerAlias"}, aliasHandler)
		handlerMux.Register(muxrpc.Method{"room", "revokeAlias"}, aliasHandler)
		handlerMux.Register(muxrpc.Method{"room", "listAliases"}, aliasHandler)
		handlerMux.Register(muxrpc.Method{"room", "members"}, aliasHandler)
		handlerMux.Register(muxrpc.Method{"room", "attendants"}, aliasHandler)
		handlerMux.Register(muxrpc.Method{"room", "metadata"}, aliasHandler)

		tunnelHandler := room.NewTunnelHandler(roomSrv, opts.KeyPair, opts.AppKey)
		handlerMux.Register(muxrpc.Method{"tunnel", "announce"}, tunnelHandler)
		handlerMux.Register(muxrpc.Method{"tunnel", "leave"}, tunnelHandler)
		handlerMux.Register(muxrpc.Method{"tunnel", "connect"}, room.NewClientTunnelConnectHandler(opts.KeyPair, secretstream.NewAppKey(opts.AppKey), handlerMux))
		handlerMux.Register(muxrpc.Method{"tunnel", "endpoints"}, tunnelHandler)
		handlerMux.Register(muxrpc.Method{"tunnel", "isRoom"}, tunnelHandler)
		handlerMux.Register(muxrpc.Method{"tunnel", "ping"}, tunnelHandler)

		if opts.RoomHTTPAddr != "" {
			inviteUseHandler := handlers.NewInviteUseHandler(opts.RoomHTTPAddr)
			handlerMux.Register(muxrpc.Method{"invite", "use"}, inviteUseHandler)
		}
	}

	net := &SbotNetwork{
		server: netServer,
		client: netClient,
	}

	return &Sbot{
		ctx:        context.Background(),
		KeyPair:    opts.KeyPair,
		opts:       opts,
		store:      feedStore,
		ebt:        ebtHandler,
		state:      stateMatrix,
		blobs:      blobStore,
		gossip:     gossipMgr,
		netServer:  netServer,
		netClient:  netClient,
		Network:    net,
		manifest:   manifest,
		handlerMux: handlerMux,
		roomDB:     roomDB,
		roomState:  roomState,
		roomSrv:    roomSrv,
	}, nil
}

func loadKeyPairWithRetry(path string, attempts int, delay time.Duration) (*keys.KeyPair, error) {
	if attempts <= 0 {
		attempts = 1
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		kp, err := keys.Load(path)
		if err == nil {
			return kp, nil
		}
		lastErr = err
		if i+1 < attempts {
			time.Sleep(delay)
		}
	}

	return nil, lastErr
}

func newManifest(enableEBT, enableRoom, enableInviteUse bool) *muxrpc.Manifest {
	m := muxrpc.NewManifest()

	m.RegisterSync("manifest")
	m.RegisterAsync("whoami")
	m.RegisterSource("createHistoryStream")
	m.RegisterAsync("gossip.ping")

	if enableEBT {
		m.RegisterDuplex("ebt.replicate")
	}

	m.RegisterSink("blobs.add")
	m.RegisterSource("blobs.get")
	m.RegisterAsync("blobs.has")
	m.RegisterAsync("blobs.size")
	m.RegisterAsync("blobs.want")
	m.RegisterSource("blobs.createWants")

	m.RegisterSource("replicate.upto")

	if enableRoom {
		m.RegisterSource("room.attendants")
		m.RegisterAsync("room.listAliases")
		m.RegisterAsync("room.registerAlias")
		m.RegisterAsync("room.revokeAlias")
		m.RegisterSource("room.members")
		m.RegisterAsync("room.metadata")

		m.RegisterSync("tunnel.announce")
		m.RegisterSync("tunnel.leave")
		m.RegisterDuplex("tunnel.connect")
		m.RegisterSource("tunnel.endpoints")
		m.RegisterAsync("tunnel.isRoom")
		m.RegisterSync("tunnel.ping")
		if enableInviteUse {
			m.RegisterAsync("invite.use")
		}
	}

	return m
}

func registerHandlers(mux *muxrpc.HandlerMux, store *feedlog.StoreImpl, ebt *replication.EBTHandler, blobStore *blobs.Store, keyPair *keys.KeyPair) {
	whoamiHandler := handlers.NewWhoamiHandler(keyPair)
	mux.Register(muxrpc.Method{"whoami"}, whoamiHandler)

	pingHandler := handlers.NewPingHandler()
	mux.Register(muxrpc.Method{"gossip", "ping"}, pingHandler)

	historyHandler := handlers.NewHistoryStreamHandler(store)
	mux.Register(muxrpc.Method{"createHistoryStream"}, historyHandler)

	handlers.RegisterTangleHandler(mux, store)

	selfRef := keyPair.FeedRef()
	blobHandler := blobs.NewPlugin(&selfRef, blobStore.BlobStore(), blobStore.WantManager(), nil)
	mux.Register(muxrpc.Method{"blobs", "add"}, blobHandler)
	mux.Register(muxrpc.Method{"blobs", "get"}, blobHandler)
	mux.Register(muxrpc.Method{"blobs", "has"}, blobHandler)
	mux.Register(muxrpc.Method{"blobs", "size"}, blobHandler)
	mux.Register(muxrpc.Method{"blobs", "want"}, blobHandler)
	mux.Register(muxrpc.Method{"blobs", "createWants"}, blobHandler)

	ebtHandlerWrapper := NewEBTHandlerWrapper(ebt)
	mux.Register(muxrpc.Method{"ebt", "replicate"}, ebtHandlerWrapper)
}

func (s *Sbot) Serve() error {
	s.ctx, s.cancel = context.WithCancel(context.Background())

	go func() {
		if err := s.netServer.Serve(s.ctx, s.handlerMux); err != nil {
			fmt.Printf("sbot: server error: %v\n", err)
		}
	}()

	if s.gossip != nil {
		go s.gossip.Run(s.ctx)
	}

	<-s.ctx.Done()
	return s.ctx.Err()
}

func (s *Sbot) Shutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	if s.cancel != nil {
		s.cancel()
	}

	var errs []error

	if s.netServer != nil {
		if err := s.netServer.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if s.store != nil {
		if err := s.store.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if s.state != nil {
		if err := s.state.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (s *Sbot) Connect(ctx context.Context, addr string, remote ed25519.PublicKey) (*network.Peer, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}

	peer, err := s.netClient.Connect(s.connectionContext(), addr, remote, s.handlerMux)
	if err != nil {
		return nil, err
	}
	s.netServer.AddPeer(peer)
	s.trackOutgoingPeer(peer)
	s.startPeerReplication(peer, addr)
	return peer, nil
}

func (s *Sbot) connectionContext() context.Context {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func (s *Sbot) trackOutgoingPeer(peer *network.Peer) {
	if s == nil || s.netServer == nil || peer == nil || peer.RPC() == nil {
		return
	}

	go func() {
		<-peer.RPC().Wait()
		s.netServer.RemovePeer(peer)
		_ = peer.Conn.Close()
	}()
}

func (s *Sbot) startPeerReplication(peer *network.Peer, addr string) {
	if s == nil || s.ebt == nil || peer == nil || peer.RPC() == nil {
		return
	}

	go func() {
		src, sink, err := peer.RPC().Duplex(s.connectionContext(), muxrpc.TypeJSON, muxrpc.Method{"ebt", "replicate"}, 3)
		if err != nil {
			fmt.Printf("sbot: failed to initiate ebt.replicate on %s: %v\n", addr, err)
			return
		}

		rx := muxrpc.NewByteSourceAdapter(src)
		tx := muxrpc.NewByteSinkWriter(sink)
		remoteFeed, _ := network.GetFeedRefFromAddr(peer.Conn.RemoteAddr())

		if err := s.ebt.HandleDuplex(s.connectionContext(), tx, rx, addr, remoteFeed); err != nil && err != context.Canceled {
			fmt.Printf("sbot: ebt replication error on %s: %v\n", addr, err)
		}
	}()
}

func (s *Sbot) Node() *Sbot {
	return s
}

func (s *Sbot) ListenAddr() string {
	return s.opts.ListenAddr
}

func (s *Sbot) Peers() []*network.Peer {
	return s.netServer.Peers()
}

func (s *Sbot) Publish(content []byte) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (s *Sbot) GetFeedSeq(author string) (int64, error) {
	log, err := s.store.Logs().Get(author)
	if err != nil {
		return -1, err
	}
	return log.Seq()
}

func (s *Sbot) GetMessage(author string, seq int64) ([]byte, error) {
	log, err := s.store.Logs().Get(author)
	if err != nil {
		return nil, err
	}

	msg, err := log.Get(seq)
	if err != nil {
		return nil, err
	}

	return msg.Value, nil
}

func (s *Sbot) Whoami() (string, error) {
	if s.KeyPair == nil {
		return "", fmt.Errorf("sbot: no key pair")
	}
	pub := s.KeyPair.Public()
	return fmt.Sprintf("@%s.ed25519", base64.StdEncoding.EncodeToString(pub[:])), nil
}

func (s *Sbot) AppKey() secretstream.AppKey {
	return secretstream.NewAppKey(s.opts.AppKey)
}

func (s *Sbot) Store() *feedlog.StoreImpl {
	return s.store
}

func (s *Sbot) EBT() *replication.EBTHandler {
	return s.ebt
}

func (s *Sbot) HandlerMux() *muxrpc.HandlerMux {
	return s.handlerMux
}

func (s *Sbot) Manifest() *muxrpc.Manifest {
	return s.manifest
}

func (s *Sbot) RoomDB() *sqlite.DB {
	return s.roomDB
}

func (s *Sbot) RoomState() *roomstate.Manager {
	return s.roomState
}

func (s *Sbot) RoomServer() *room.RoomServer {
	return s.roomSrv
}

func (s *Sbot) RoomEnabled() bool {
	return s.opts.EnableRoom
}

func (s *Sbot) RoomMode() string {
	return s.opts.RoomMode
}

func (s *Sbot) RoomHTTPAddr() string {
	return s.opts.RoomHTTPAddr
}

func (s *Sbot) StateMatrix() *replication.StateMatrix {
	return s.state
}

func (s *Sbot) Gossip() *gossip.Manager {
	return s.gossip
}

func (s *Sbot) NetServer() *network.Server {
	return s.netServer
}

func (s *Sbot) BlobStore() *blobs.Store {
	return s.blobs
}

func (s *Sbot) EnsureBlob(ctx context.Context, ref *refs.BlobRef) error {
	if s == nil || s.blobs == nil {
		return fmt.Errorf("sbot: blob store not initialized")
	}
	if ref == nil {
		return fmt.Errorf("sbot: blob ref is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	has, err := s.blobs.BlobStore().Has(ref.Hash())
	if err != nil {
		return fmt.Errorf("sbot: check blob %s: %w", ref.Ref(), err)
	}
	if has {
		return nil
	}

	if wantManager := s.blobs.WantManager(); wantManager != nil {
		_ = wantManager.Want(ref)
	}

	var lastErr error
	for _, peer := range s.Peers() {
		if peer == nil || peer.RPC() == nil {
			continue
		}
		if err := s.fetchBlobFromPeer(ctx, peer, ref); err == nil {
			if wantManager := s.blobs.WantManager(); wantManager != nil {
				_ = wantManager.CancelWant(ref)
			}
			return nil
		} else {
			lastErr = err
		}
	}

	if lastErr != nil {
		return fmt.Errorf("sbot: fetch blob %s: %w", ref.Ref(), lastErr)
	}
	return fmt.Errorf("sbot: no peers available for blob %s", ref.Ref())
}

func (s *Sbot) fetchBlobFromPeer(ctx context.Context, peer *network.Peer, ref *refs.BlobRef) error {
	if s == nil || s.blobs == nil {
		return fmt.Errorf("sbot: blob store not initialized")
	}
	if peer == nil || peer.RPC() == nil {
		return fmt.Errorf("sbot: peer RPC unavailable")
	}

	fetchCtx := ctx
	cancel := func() {}
	if _, hasDeadline := fetchCtx.Deadline(); !hasDeadline {
		fetchCtx, cancel = context.WithTimeout(fetchCtx, 10*time.Second)
	}
	defer cancel()

	src, err := peer.RPC().Source(fetchCtx, muxrpc.TypeBinary, muxrpc.Method{"blobs", "get"}, map[string]string{
		"hash": ref.Ref(),
	})
	if err != nil {
		return err
	}

	var payload bytes.Buffer
	for src.Next(fetchCtx) {
		chunk, err := src.Bytes()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if len(chunk) > 0 {
			if _, err := payload.Write(chunk); err != nil {
				return err
			}
		}
	}
	if err := src.Err(); err != nil {
		return err
	}
	if payload.Len() == 0 {
		return fmt.Errorf("empty blob response")
	}

	sum := sha256.Sum256(payload.Bytes())
	if !bytes.Equal(sum[:], ref.Hash()) {
		return fmt.Errorf("blob hash mismatch")
	}
	storedHash, err := s.blobs.BlobStore().Put(bytes.NewReader(payload.Bytes()))
	if err != nil {
		return err
	}
	if !bytes.Equal(storedHash, ref.Hash()) {
		return fmt.Errorf("stored blob hash mismatch")
	}
	return nil
}

func (s *Sbot) Publisher() (*publisher.Publisher, error) {
	var receiveLog feedlog.Log
	if s.store != nil {
		var err error
		receiveLog, err = s.store.ReceiveLog()
		if err != nil {
			return nil, fmt.Errorf("sbot: open receive log: %w", err)
		}
	}

	return publisher.New(
		s.KeyPair,
		receiveLog,
		s.store.Logs(),
		publisher.WithAfterPublish(func(feed refs.FeedRef, seq int64) {
			s.NotifyFeedSeq(&feed, seq)
		}),
	)
}

func (s *Sbot) SetMessageLogger(logger feedlog.MessageLogger) {
	s.store.SetMessageLogger(logger)
}

type Endpoint interface {
	Async(ctx context.Context, result interface{}, tipe interface{}, method interface{}) error
}

type SbotNetwork struct {
	server *network.Server
	client *network.Client
}

func (s *Sbot) GetNet() *SbotNetwork {
	return s.Network
}

func (n *SbotNetwork) Connect(ctx context.Context, addr string, remote ed25519.PublicKey) error {
	_, err := n.client.Connect(ctx, addr, remote, nil) // or some default handler
	return err
}

func (n *SbotNetwork) GetEndpointFor(feed interface{}) (Endpoint, bool) {
	var feedRef refs.FeedRef
	switch f := feed.(type) {
	case refs.FeedRef:
		feedRef = f
	case string:
		parsed, err := refs.ParseFeedRef(f)
		if err != nil {
			return nil, false
		}
		feedRef = *parsed
	default:
		return nil, false
	}
	for _, peer := range n.server.Peers() {
		if peer.ID.Equal(feedRef) {
			return &peerEndpoint{peer: peer}, true
		}
	}
	return nil, false
}

func (n *SbotNetwork) Peers() []*network.Peer {
	return n.server.Peers()
}

type peerEndpoint struct {
	peer *network.Peer
}

func (e *peerEndpoint) Async(ctx context.Context, result interface{}, tipe interface{}, method interface{}) error {
	return fmt.Errorf("not implemented")
}

func (s *Sbot) Replicate(feed interface{}) {
	var feedRef *refs.FeedRef
	switch f := feed.(type) {
	case refs.FeedRef:
		s.replicatedFeeds.Store(f.String(), f)
		feedRef = &f
	case *refs.FeedRef:
		if f == nil {
			return
		}
		s.replicatedFeeds.Store(f.String(), *f)
		feedRef = f
	case string:
		trimmed := strings.TrimSpace(f)
		if trimmed == "" {
			return
		}
		s.replicatedFeeds.Store(trimmed, nil)
		parsed, err := refs.ParseFeedRef(trimmed)
		if err == nil {
			feedRef = parsed
			s.replicatedFeeds.Store(parsed.String(), *parsed)
		}
	default:
		return
	}

	if feedRef == nil || s.state == nil {
		return
	}

	seq := int64(0)
	if s.store != nil {
		log, err := s.store.Logs().Get(feedRef.String())
		switch err {
		case nil:
			currentSeq, seqErr := log.Seq()
			if seqErr == nil && currentSeq > 0 {
				seq = currentSeq
			}
		case feedlog.ErrNotFound:
		default:
			return
		}
	}

	s.state.SetFeedSeq(feedRef, seq)
}

func (s *Sbot) NotifyFeedSeq(feed *refs.FeedRef, seq int64) {
	if s.state != nil {
		s.state.SetFeedSeq(feed, seq)
	}
}

func (s *Sbot) SetSignatureLogger(logger feedlog.SignatureLogger) {
	if s.store != nil {
		s.store.SetSignatureLogger(logger)
	}
}
