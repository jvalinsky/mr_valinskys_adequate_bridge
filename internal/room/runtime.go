// Package room provides an embedded SSB Room server using our internal SSB implementation.
package room

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb/sqlite"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomstate"
)

const (
	defaultMUXRPCListenAddr = "127.0.0.1:8989"
	defaultHTTPListenAddr   = "127.0.0.1:8976"
	defaultRoomRepoPath     = ".ssb-room"
	defaultRoomMode         = "community"
)

type Config struct {
	ListenAddr            string
	HTTPListenAddr        string
	RepoPath              string
	Mode                  string
	HTTPSDomain           string
	KeyPair               *keys.KeyPair
	BridgeAccountLister   ActiveBridgeAccountLister
	BridgeAccountDetailer ActiveBridgeAccountDetailer
}

type Runtime struct {
	logger     *log.Logger
	cfg        Config
	muxrpcAddr string
	httpAddr   string

	keyPair *keys.KeyPair
	roomDB  *sqlite.DB
	state   *roomstate.Manager

	httpServer     *http.Server
	httpListener   net.Listener
	muxrpcListener net.Listener

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	closeOnce  sync.Once
	closeErr   error
	shutdownCh chan struct{}
}

func Start(parentCtx context.Context, cfg Config, logger *log.Logger) (*Runtime, error) {
	logger = logutil.Ensure(logger)

	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(parentCtx)

	if cfg.KeyPair == nil {
		kp, err := keys.Generate()
		if err != nil {
			cancel()
			return nil, fmt.Errorf("room: failed to generate key pair: %w", err)
		}
		cfg.KeyPair = kp
	}

	if err := os.MkdirAll(filepath.Join(cfg.RepoPath), 0700); err != nil {
		cancel()
		return nil, fmt.Errorf("room: failed to create repo directory: %w", err)
	}

	roomDB, err := sqlite.Open(filepath.Join(cfg.RepoPath, "room.sqlite"))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("room: failed to open room db: %w", err)
	}

	privacyMode := roomdb.ParsePrivacyMode(strings.ToLower(strings.TrimSpace(cfg.Mode)))
	if err := roomDB.RoomConfig().SetPrivacyMode(ctx, privacyMode); err != nil {
		roomDB.Close()
		cancel()
		return nil, fmt.Errorf("room: failed to set privacy mode: %w", err)
	}

	state := roomstate.NewManager()

	feedRef := cfg.KeyPair.FeedRef()
	roomSrv := newRoomServer(&feedRef, roomDB, state)

	handlerMux := &muxrpc.Manifest{}
	registerRoomHandlers(handlerMux, roomSrv)

	httpListener, err := net.Listen("tcp", cfg.HTTPListenAddr)
	if err != nil {
		roomDB.Close()
		cancel()
		return nil, fmt.Errorf("room: listen http: %w", err)
	}

	muxrpcListener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		httpListener.Close()
		roomDB.Close()
		cancel()
		return nil, fmt.Errorf("room: listen muxrpc: %w", err)
	}

	muxHandler := newServeMux(ctx, roomDB, state, cfg.KeyPair)
	httpServer := &http.Server{
		Handler:           newBridgeRoomHandler(muxHandler, roomDB.RoomConfig(), cfg.BridgeAccountLister, cfg.BridgeAccountDetailer),
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      3 * time.Minute,
		IdleTimeout:       3 * time.Minute,
	}

	rt := &Runtime{
		logger:         logger,
		cfg:            cfg,
		keyPair:        cfg.KeyPair,
		roomDB:         roomDB,
		state:          state,
		httpServer:     httpServer,
		httpListener:   httpListener,
		muxrpcListener: muxrpcListener,
		ctx:            ctx,
		cancel:         cancel,
		muxrpcAddr:     cfg.ListenAddr,
		httpAddr:       httpListener.Addr().String(),
		shutdownCh:     make(chan struct{}),
	}

	rt.wg.Add(2)
	go rt.serveHTTP()
	go rt.serveMUXRPC(ctx, rt.shutdownCh, muxrpcListener)

	go func() {
		<-ctx.Done()
		_ = rt.Close()
	}()

	rt.logger.Printf("event=room_runtime_started muxrpc_addr=%s http_addr=%s mode=%s", rt.muxrpcAddr, rt.httpAddr, strings.ToLower(cfg.Mode))
	return rt, nil
}

func (r *Runtime) Addr() string {
	if r == nil {
		return ""
	}
	return r.muxrpcAddr
}

func (r *Runtime) HTTPAddr() string {
	if r == nil {
		return ""
	}
	return r.httpAddr
}

func (r *Runtime) RoomFeed() refs.FeedRef {
	if r == nil || r.keyPair == nil {
		return refs.FeedRef{}
	}
	return r.keyPair.FeedRef()
}

func (r *Runtime) AddMember(ctx context.Context, feed refs.FeedRef, role roomdb.Role) error {
	if r == nil || r.roomDB == nil {
		return fmt.Errorf("room runtime not started")
	}
	_, err := r.roomDB.Members().Add(ctx, feed, role)
	return err
}

func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}

	r.closeOnce.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}

		if r.shutdownCh != nil {
			close(r.shutdownCh)
		}

		var errs []error

		if r.httpListener != nil {
			if err := r.httpListener.Close(); err != nil && err != net.ErrClosed {
				errs = append(errs, fmt.Errorf("close room http listener: %w", err))
			}
		}

		if r.muxrpcListener != nil {
			if err := r.muxrpcListener.Close(); err != nil && err != net.ErrClosed {
				errs = append(errs, fmt.Errorf("close room muxrpc listener: %w", err))
			}
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if r.httpServer != nil {
			if err := r.httpServer.Shutdown(shutdownCtx); err != nil && err != http.ErrServerClosed {
				errs = append(errs, fmt.Errorf("shutdown room http server: %w", err))
			}
		}

		if r.roomDB != nil {
			if err := r.roomDB.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close room db: %w", err))
			}
		}

		r.wg.Wait()
		r.closeErr = joinErrors(errs)
		if r.closeErr == nil {
			r.logger.Printf("event=room_runtime_stopped muxrpc_addr=%s http_addr=%s", r.muxrpcAddr, r.httpAddr)
		}
	})

	return r.closeErr
}

func (r *Runtime) serveHTTP() {
	defer r.wg.Done()

	err := r.httpServer.Serve(r.httpListener)
	if err != nil && err != http.ErrServerClosed {
		r.logger.Printf("event=room_http_serve_error err=%v", err)
	}
}

func (r *Runtime) serveMUXRPC(ctx context.Context, shutdownCh <-chan struct{}, ln net.Listener) {
	defer r.wg.Done()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-shutdownCh:
				return
			default:
				continue
			}
		}

		go r.handleMUXRPCConn(ctx, conn)
	}
}

func (r *Runtime) handleMUXRPCConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	var secretConn muxrpc.Conn = &connWrapper{conn}
	muxrpc.NewServer(ctx, secretConn, nil, nil)

	<-ctx.Done()
}

type connWrapper struct {
	net.Conn
}

func (c *connWrapper) Read(p []byte) (n int, err error) {
	return c.Conn.Read(p)
}

func (c *connWrapper) Write(p []byte) (n int, err error) {
	return c.Conn.Write(p)
}

func (c *connWrapper) Close() error {
	return c.Conn.Close()
}

func (c *connWrapper) RemoteAddr() net.Addr {
	return c.Conn.RemoteAddr()
}

type roomServer struct {
	keyPair *refs.FeedRef
	db      *sqlite.DB
	state   *roomstate.Manager
}

func newRoomServer(keyPair *refs.FeedRef, db *sqlite.DB, state *roomstate.Manager) *roomServer {
	return &roomServer{
		keyPair: keyPair,
		db:      db,
		state:   state,
	}
}

func newServeMux(ctx context.Context, db *sqlite.DB, state *roomstate.Manager, keyPair *keys.KeyPair) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	inviteH := newInviteHandler(db.Invites(), db.RoomConfig(), keyPair, "")
	mux.HandleFunc("/create-invite", inviteH.handleCreateInvite)
	mux.HandleFunc("/join", inviteH.handleJoin)

	authH := newAuthHandler(db.AuthFallback())
	mux.HandleFunc("/login", authH.handleLogin)
	mux.HandleFunc("/reset-password", authH.handleResetPassword)

	return mux
}

func registerRoomHandlers(mux *muxrpc.Manifest, srv *roomServer) {
}

func (cfg Config) withDefaults() Config {
	if strings.TrimSpace(cfg.ListenAddr) == "" {
		cfg.ListenAddr = defaultMUXRPCListenAddr
	}
	if strings.TrimSpace(cfg.HTTPListenAddr) == "" {
		cfg.HTTPListenAddr = defaultHTTPListenAddr
	}
	if strings.TrimSpace(cfg.RepoPath) == "" {
		cfg.RepoPath = defaultRoomRepoPath
	}
	if strings.TrimSpace(cfg.Mode) == "" {
		cfg.Mode = defaultRoomMode
	}
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	return cfg
}

func (cfg Config) validate() error {
	if _, _, err := net.SplitHostPort(cfg.ListenAddr); err != nil {
		return fmt.Errorf("invalid room listen addr %q: %w", cfg.ListenAddr, err)
	}
	if _, _, err := net.SplitHostPort(cfg.HTTPListenAddr); err != nil {
		return fmt.Errorf("invalid room HTTP listen addr %q: %w", cfg.HTTPListenAddr, err)
	}

	mode := roomdb.ParsePrivacyMode(cfg.Mode)
	if mode == roomdb.ModeUnknown {
		return fmt.Errorf("room-mode must be one of open|community|restricted")
	}

	if cfg.HTTPSDomain == "" {
		host, _, err := net.SplitHostPort(cfg.ListenAddr)
		if err == nil {
			if host != "127.0.0.1" && host != "localhost" && host != "::1" {
				return fmt.Errorf("room-https-domain is required when binding to non-loopback address %q", cfg.ListenAddr)
			}
		}
	}

	return nil
}

func joinErrors(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return fmt.Errorf("multiple errors: %v", errs)
	}
}
