// Package room embeds and supervises the go-ssb-room runtime.
package room

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	roomadapter "github.com/ssbc/go-ssb-room/v2/bridgeadapter"
	"github.com/ssbc/go-ssb-room/v2/roomdb"
	kitlog "go.mindeco.de/log"
	refs "github.com/ssbc/go-ssb-refs"
)

const (
	defaultMUXRPCListenAddr = "127.0.0.1:8989"
	defaultHTTPListenAddr   = "127.0.0.1:8976"
	defaultRoomRepoPath     = ".ssb-room"
	defaultRoomMode         = "community"
)

// Config controls embedded go-ssb-room runtime setup.
type Config struct {
	ListenAddr     string
	HTTPListenAddr string
	RepoPath       string
	Mode           string
	HTTPSDomain    string
	BridgeAccounts ActiveBridgeAccountLister
}

// Runtime wraps the lifecycle of the embedded room adapter and HTTP server.
type Runtime struct {
	logger     *log.Logger
	cfg        Config
	muxrpcAddr string
	httpAddr   string

	roomAdapter  *roomadapter.Runtime
	httpServer   *http.Server
	httpListener net.Listener

	cancel context.CancelFunc
	wg     sync.WaitGroup

	closeOnce sync.Once
	closeErr  error
}

// Start boots the embedded room runtime and returns a managed Runtime.
func Start(parentCtx context.Context, cfg Config, logger *log.Logger) (*Runtime, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(parentCtx)

	mode := roomdb.ParsePrivacyMode(strings.ToLower(strings.TrimSpace(cfg.Mode)))
	roomLogger := kitlog.NewLogfmtLogger(kitlog.NewSyncWriter(logger.Writer()))
	adapter, err := roomadapter.Start(ctx, roomadapter.Config{
		RepoPath:          cfg.RepoPath,
		MUXRPCListenAddr:  cfg.ListenAddr,
		HTTPListenAddr:    cfg.HTTPListenAddr,
		HTTPSDomain:       cfg.HTTPSDomain,
		Mode:              mode,
		AliasesSubdomains: false,
	}, roomLogger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("start room adapter: %w", err)
	}

	stockHandler, err := adapter.HTTPHandler()
	if err != nil {
		_ = adapter.Close()
		cancel()
		return nil, fmt.Errorf("build room http handler: %w", err)
	}

	httpListener, err := net.Listen("tcp", cfg.HTTPListenAddr)
	if err != nil {
		_ = adapter.Close()
		cancel()
		return nil, fmt.Errorf("listen room http interface: %w", err)
	}

	httpServer := &http.Server{
		Handler:           adapter.Server.Network.WebsockHandler(newBridgeRoomHandler(stockHandler, adapter.RoomConfig(), cfg.BridgeAccounts)),
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      3 * time.Minute,
		IdleTimeout:       3 * time.Minute,
	}

	rt := &Runtime{
		logger:       logger,
		cfg:          cfg,
		roomAdapter:  adapter,
		httpServer:   httpServer,
		httpListener: httpListener,
		cancel:       cancel,
		muxrpcAddr:   cfg.ListenAddr,
		httpAddr:     httpListener.Addr().String(),
	}

	rt.wg.Add(2)
	go rt.serveHTTP()
	go rt.serveMUXRPC(ctx)

	go func() {
		<-ctx.Done()
		_ = rt.Close()
	}()

	rt.logger.Printf("event=room_runtime_started muxrpc_addr=%s http_addr=%s mode=%s", rt.muxrpcAddr, rt.httpAddr, strings.ToLower(cfg.Mode))
	return rt, nil
}

// Addr returns the muxrpc listen address.
func (r *Runtime) Addr() string {
	if r == nil {
		return ""
	}
	return r.muxrpcAddr
}

// HTTPAddr returns the HTTP listen address.
func (r *Runtime) HTTPAddr() string {
	if r == nil {
		return ""
	}
	return r.httpAddr
}

// RoomFeed returns the public feed of the embedded room.
func (r *Runtime) RoomFeed() refs.FeedRef {
	if r == nil || r.roomAdapter == nil {
		return refs.FeedRef{}
	}
	return r.roomAdapter.Server.Whoami()
}

// AddMember adds a member to the room.
func (r *Runtime) AddMember(ctx context.Context, feed refs.FeedRef, role roomdb.Role) error {
	if r == nil || r.roomAdapter == nil {
		return fmt.Errorf("room runtime not started")
	}
	_, err := r.roomAdapter.Server.Members.Add(ctx, feed, role)
	return err
}

// Close stops the room runtime and releases listeners and background loops.
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}

	r.closeOnce.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var errs []error
		if r.httpServer != nil {
			if err := r.httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs = append(errs, fmt.Errorf("shutdown room http server: %w", err))
			}
		}
		if r.httpListener != nil {
			if err := r.httpListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				errs = append(errs, fmt.Errorf("close room http listener: %w", err))
			}
		}
		if r.roomAdapter != nil {
			if err := r.roomAdapter.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close room adapter: %w", err))
			}
		}

		r.wg.Wait()
		r.closeErr = errors.Join(errs...)
		if r.closeErr == nil {
			r.logger.Printf("event=room_runtime_stopped muxrpc_addr=%s http_addr=%s", r.muxrpcAddr, r.httpAddr)
		}
	})

	return r.closeErr
}

func (r *Runtime) serveHTTP() {
	defer r.wg.Done()

	err := r.httpServer.Serve(r.httpListener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		r.logger.Printf("event=room_http_serve_error err=%v", err)
	}
}

func (r *Runtime) serveMUXRPC(ctx context.Context) {
	defer r.wg.Done()

	for {
		err := r.roomAdapter.Server.Network.Serve(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Printf("event=room_muxrpc_serve_error err=%v", err)
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		time.Sleep(1 * time.Second)
	}
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
	cfg.HTTPSDomain = strings.TrimSpace(cfg.HTTPSDomain)
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

	if cfg.requiresHTTPSDomain() && cfg.HTTPSDomain == "" {
		return fmt.Errorf("room-https-domain is required when room listeners are non-loopback")
	}

	return nil
}

func (cfg Config) requiresHTTPSDomain() bool {
	return !addrIsLoopback(cfg.ListenAddr) || !addrIsLoopback(cfg.HTTPListenAddr)
}

func addrIsLoopback(listenAddr string) bool {
	host, _, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return false
	}
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
