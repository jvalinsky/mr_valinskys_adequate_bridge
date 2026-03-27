package room

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	kitlog "go.mindeco.de/log"

	"github.com/ssbc/go-ssb-room/v2/roomdb"
	roomsqlite "github.com/ssbc/go-ssb-room/v2/roomdb/sqlite"
	"github.com/ssbc/go-ssb-room/v2/roomsrv"
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
}

type Runtime struct {
	logger *log.Logger
	cfg    Config

	roomServer   *roomsrv.Server
	roomDB       *roomsqlite.Database
	httpServer   *http.Server
	httpListener net.Listener

	cancel context.CancelFunc
	wg     sync.WaitGroup

	closeOnce sync.Once
	closeErr  error
}

func Start(parentCtx context.Context, cfg Config, logger *log.Logger) (*Runtime, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	if err := ensureRepoPath(cfg.RepoPath); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(parentCtx)

	rdb, err := roomsqlite.Open(repoAdapter{basePath: cfg.RepoPath})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open roomdb sqlite: %w", err)
	}

	mode := roomdb.ParsePrivacyMode(strings.ToLower(strings.TrimSpace(cfg.Mode)))
	if mode == roomdb.ModeUnknown {
		_ = rdb.Close()
		cancel()
		return nil, fmt.Errorf("invalid room mode %q", cfg.Mode)
	}
	if err := rdb.Config.SetPrivacyMode(ctx, mode); err != nil {
		_ = rdb.Close()
		cancel()
		return nil, fmt.Errorf("set room privacy mode: %w", err)
	}

	netInfo, err := buildServerEndpointDetails(cfg)
	if err != nil {
		_ = rdb.Close()
		cancel()
		return nil, err
	}

	roomLogger := kitlog.NewLogfmtLogger(kitlog.NewSyncWriter(logger.Writer()))
	options := []roomsrv.Option{
		roomsrv.WithContext(ctx),
		roomsrv.WithRepoPath(cfg.RepoPath),
		roomsrv.WithUNIXSocket(false),
		roomsrv.WithLogger(roomLogger),
	}

	srv, err := newRoomServer(rdb, netInfo, options)
	if err != nil {
		_ = rdb.Close()
		cancel()
		return nil, err
	}

	httpListener, err := net.Listen("tcp", cfg.HTTPListenAddr)
	if err != nil {
		srv.Shutdown()
		_ = srv.Close()
		_ = rdb.Close()
		cancel()
		return nil, fmt.Errorf("listen room http interface: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("room2 runtime ready"))
	})

	httpServer := &http.Server{
		Handler:           srv.Network.WebsockHandler(mux),
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      3 * time.Minute,
		IdleTimeout:       3 * time.Minute,
	}

	rt := &Runtime{
		logger:       logger,
		cfg:          cfg,
		roomServer:   srv,
		roomDB:       rdb,
		httpServer:   httpServer,
		httpListener: httpListener,
		cancel:       cancel,
	}

	rt.wg.Add(2)
	go rt.serveHTTP()
	go rt.serveMUXRPC(ctx)

	go func() {
		<-ctx.Done()
		_ = rt.Close()
	}()

	rt.logger.Printf("event=room_runtime_started muxrpc_addr=%s http_addr=%s mode=%s", rt.Addr(), rt.HTTPAddr(), strings.ToLower(cfg.Mode))
	return rt, nil
}

func (r *Runtime) Addr() string {
	if r == nil || r.roomServer == nil || r.roomServer.Network == nil {
		return ""
	}
	addr := r.roomServer.Network.GetListenAddr()
	if addr == nil {
		return ""
	}
	return addr.String()
}

func (r *Runtime) HTTPAddr() string {
	if r == nil || r.httpListener == nil {
		return ""
	}
	return r.httpListener.Addr().String()
}

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

		if r.roomServer != nil {
			r.roomServer.Shutdown()
			if err := r.roomServer.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close room server: %w", err))
			}
		}

		if r.roomDB != nil {
			if err := r.roomDB.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close roomdb: %w", err))
			}
		}

		r.wg.Wait()
		r.closeErr = errors.Join(errs...)
		if r.closeErr == nil {
			r.logger.Printf("event=room_runtime_stopped muxrpc_addr=%s http_addr=%s", r.Addr(), r.HTTPAddr())
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
		err := r.roomServer.Network.Serve(ctx)
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

func ensureRepoPath(repoPath string) error {
	if err := os.MkdirAll(repoPath, 0o700); err != nil {
		return fmt.Errorf("create room repo path: %w", err)
	}
	return nil
}

func buildServerEndpointDetails(cfg Config) (reflect.Value, error) {
	newFnType := reflect.TypeOf(roomsrv.New)
	if newFnType.NumIn() < 7 {
		return reflect.Value{}, fmt.Errorf("roomsrv.New signature changed: expected >=7 args, got %d", newFnType.NumIn())
	}

	netInfoType := newFnType.In(6)
	if netInfoType.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("roomsrv.New arg 7 is not a struct type")
	}

	host, portStr, err := net.SplitHostPort(cfg.HTTPListenAddr)
	if err != nil {
		return reflect.Value{}, fmt.Errorf("split room HTTP listen addr: %w", err)
	}
	port, err := net.LookupPort("tcp", portStr)
	if err != nil {
		return reflect.Value{}, fmt.Errorf("resolve room HTTP listen port: %w", err)
	}

	netInfo := reflect.New(netInfoType).Elem()
	if err := setStructField(netInfo, "ListenAddressMUXRPC", cfg.ListenAddr); err != nil {
		return reflect.Value{}, err
	}
	if err := setStructField(netInfo, "PortHTTPS", uint(port)); err != nil {
		return reflect.Value{}, err
	}
	if err := setStructField(netInfo, "UseSubdomainForAliases", false); err != nil {
		return reflect.Value{}, err
	}

	domain := cfg.HTTPSDomain
	development := false
	if domain == "" {
		domain = "localhost"
		development = true
	}
	if addrIsLoopback(net.JoinHostPort(host, portStr)) && cfg.HTTPSDomain == "" {
		development = true
	}

	if err := setStructField(netInfo, "Domain", domain); err != nil {
		return reflect.Value{}, err
	}
	if err := setStructField(netInfo, "Development", development); err != nil {
		return reflect.Value{}, err
	}

	return netInfo, nil
}

func setStructField(v reflect.Value, field string, value interface{}) error {
	f := v.FieldByName(field)
	if !f.IsValid() {
		return fmt.Errorf("room netInfo missing field %q", field)
	}
	if !f.CanSet() {
		return fmt.Errorf("room netInfo field %q is not settable", field)
	}

	val := reflect.ValueOf(value)
	if !val.Type().AssignableTo(f.Type()) {
		if val.Type().ConvertibleTo(f.Type()) {
			val = val.Convert(f.Type())
		} else {
			return fmt.Errorf("cannot assign %T to room netInfo field %q (%s)", value, field, f.Type())
		}
	}

	f.Set(val)
	return nil
}

func newRoomServer(roomDB *roomsqlite.Database, netInfo reflect.Value, options []roomsrv.Option) (*roomsrv.Server, error) {
	newFn := reflect.ValueOf(roomsrv.New)
	newFnType := newFn.Type()
	if newFnType.NumIn() < 8 {
		return nil, fmt.Errorf("roomsrv.New signature changed: expected >=8 args, got %d", newFnType.NumIn())
	}

	args := []reflect.Value{
		reflect.ValueOf(roomDB.Members),
		reflect.ValueOf(roomDB.DeniedKeys),
		reflect.ValueOf(roomDB.Aliases),
		reflect.ValueOf(roomDB.AuthWithSSB),
		reflect.Zero(newFnType.In(4)),
		reflect.ValueOf(roomDB.Config),
		netInfo,
	}
	for _, opt := range options {
		args = append(args, reflect.ValueOf(opt))
	}

	results := newFn.Call(args)
	if len(results) != 2 {
		return nil, fmt.Errorf("roomsrv.New returned unexpected result arity: %d", len(results))
	}

	if !results[1].IsNil() {
		return nil, fmt.Errorf("start room server: %w", results[1].Interface().(error))
	}

	srv, ok := results[0].Interface().(*roomsrv.Server)
	if !ok || srv == nil {
		return nil, fmt.Errorf("roomsrv.New did not return *roomsrv.Server")
	}

	return srv, nil
}

type repoAdapter struct {
	basePath string
}

func (r repoAdapter) GetPath(parts ...string) string {
	return filepath.Join(append([]string{r.basePath}, parts...)...)
}
