package sbot

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/blobs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/handlers"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/handlers/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/network"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/replication"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb/sqlite"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomstate"
)

type Options struct {
	RepoPath   string
	ListenAddr string
	KeyPair    *keys.KeyPair
	AppKey     string
	EnableEBT  bool
	Hops       int

	EnableRoom bool
	RoomMode   string
}

type Sbot struct {
	ctx context.Context

	KeyPair *keys.KeyPair
	opts    Options

	store *feedlog.StoreImpl
	ebt   *replication.EBTHandler
	state *replication.StateMatrix
	blobs *blobs.Store

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
		opts.AppKey = "tofu"
	}
	if opts.Hops == 0 {
		opts.Hops = 2
	}
	if opts.KeyPair == nil {
		kp, err := keys.Generate()
		if err != nil {
			return nil, fmt.Errorf("sbot: failed to generate key pair: %w", err)
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

	stateMatrix, err := replication.NewStateMatrix(
		opts.RepoPath+"/ebt-state",
		nil,
		feedStore,
	)
	if err != nil {
		return nil, fmt.Errorf("sbot: failed to create state matrix: %w", err)
	}

	ebtHandler := replication.NewEBTHandler(nil, nil, stateMatrix, nil)

	blobStore := blobs.NewStore(feedStore.Blobs())

	netServer, err := network.NewServer(network.Options{
		ListenAddr: opts.ListenAddr,
		KeyPair:    opts.KeyPair,
		AppKey:     opts.AppKey,
		Timeout:    30 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("sbot: failed to create server: %w", err)
	}

	netClient := network.NewClient(network.Options{
		KeyPair: opts.KeyPair,
		AppKey:  opts.AppKey,
	})

	manifest := newManifest(opts.EnableEBT, opts.EnableRoom)
	handlerMux := &muxrpc.HandlerMux{}

	registerHandlers(handlerMux, feedStore, ebtHandler, blobStore, opts.KeyPair)

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
		)

		aliasHandler := room.NewAliasHandler(roomSrv)
		handlerMux.Register(muxrpc.Method{"room", "registerAlias"}, aliasHandler)
		handlerMux.Register(muxrpc.Method{"room", "revokeAlias"}, aliasHandler)
		handlerMux.Register(muxrpc.Method{"room", "listAliases"}, aliasHandler)
		handlerMux.Register(muxrpc.Method{"room", "members"}, aliasHandler)
		handlerMux.Register(muxrpc.Method{"room", "attendants"}, aliasHandler)
		handlerMux.Register(muxrpc.Method{"room", "metadata"}, aliasHandler)

		tunnelHandler := room.NewTunnelHandler(roomSrv)
		handlerMux.Register(muxrpc.Method{"tunnel", "announce"}, tunnelHandler)
		handlerMux.Register(muxrpc.Method{"tunnel", "leave"}, tunnelHandler)
		handlerMux.Register(muxrpc.Method{"tunnel", "connect"}, tunnelHandler)
		handlerMux.Register(muxrpc.Method{"tunnel", "endpoints"}, tunnelHandler)
		handlerMux.Register(muxrpc.Method{"tunnel", "isRoom"}, tunnelHandler)
		handlerMux.Register(muxrpc.Method{"tunnel", "ping"}, tunnelHandler)
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
		netServer:  netServer,
		netClient:  netClient,
		Network:    net,
		manifest:   manifest,
		handlerMux: handlerMux,
	}, nil
}

func newManifest(enableEBT, enableRoom bool) *muxrpc.Manifest {
	m := muxrpc.NewManifest()

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
		m.RegisterAsync("room.attendants")
		m.RegisterAsync("room.listAliases")
		m.RegisterAsync("room.registerAlias")
		m.RegisterAsync("room.revokeAlias")
		m.RegisterAsync("room.members")
		m.RegisterSource("room.members")
		m.RegisterAsync("room.metadata")

		m.RegisterSync("tunnel.announce")
		m.RegisterSync("tunnel.leave")
		m.RegisterDuplex("tunnel.connect")
		m.RegisterSource("tunnel.endpoints")
		m.RegisterAsync("tunnel.isRoom")
		m.RegisterSync("tunnel.ping")

		m.RegisterAsync("invite.create")
		m.RegisterAsync("invite.use")
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

	selfRef := keyPair.FeedRef()
	blobHandler := blobs.NewPlugin(&selfRef, blobStore.BlobStore(), blobStore.WantManager(), nil)
	mux.Register(muxrpc.Method{"blobs", "add"}, blobHandler)
	mux.Register(muxrpc.Method{"blobs", "get"}, blobHandler)
	mux.Register(muxrpc.Method{"blobs", "has"}, blobHandler)
	mux.Register(muxrpc.Method{"blobs", "size"}, blobHandler)
	mux.Register(muxrpc.Method{"blobs", "want"}, blobHandler)
	mux.Register(muxrpc.Method{"blobs", "createWants"}, blobHandler)
}

func (s *Sbot) Serve() error {
	s.ctx, _ = context.WithCancel(context.Background())

	go func() {
		if err := s.netServer.Serve(s.ctx, s.handlerMux); err != nil {
			fmt.Printf("sbot: server error: %v\n", err)
		}
	}()

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

func (s *Sbot) Connect(ctx context.Context, addr string) (*network.Peer, error) {
	return s.netClient.Connect(ctx, addr)
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

func (s *Sbot) Store() *feedlog.StoreImpl {
	return s.store
}

func (s *Sbot) EBT() *replication.EBTHandler {
	return s.ebt
}

func (s *Sbot) StateMatrix() *replication.StateMatrix {
	return s.state
}

func (s *Sbot) BlobStore() *blobs.Store {
	return s.blobs
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

func (n *SbotNetwork) Connect(ctx context.Context, addr interface{}) error {
	var addrStr string
	switch a := addr.(type) {
	case string:
		addrStr = a
	case net.Addr:
		addrStr = a.String()
	default:
		return fmt.Errorf("unsupported addr type: %T", addr)
	}
	_, err := n.client.Connect(ctx, addrStr)
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
	switch f := feed.(type) {
	case refs.FeedRef:
		s.replicatedFeeds.Store(f.String(), f)
	case string:
		s.replicatedFeeds.Store(f, nil)
	}
}
