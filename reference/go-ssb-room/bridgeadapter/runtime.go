package bridgeadapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	refs "github.com/ssbc/go-ssb-refs"
	"github.com/ssbc/go-ssb-room/v2/internal/network"
	roomrepo "github.com/ssbc/go-ssb-room/v2/internal/repo"
	"github.com/ssbc/go-ssb-room/v2/internal/signinwithssb"
	"github.com/ssbc/go-ssb-room/v2/roomdb"
	roomsqlite "github.com/ssbc/go-ssb-room/v2/roomdb/sqlite"
	"github.com/ssbc/go-ssb-room/v2/roomsrv"
	"github.com/ssbc/go-ssb-room/v2/web/handlers"
	kitlog "go.mindeco.de/log"
)

// Config is a stable, typed constructor contract for embedded Room2 startup.
type Config struct {
	RepoPath          string
	MUXRPCListenAddr  string
	HTTPListenAddr    string
	HTTPSDomain       string
	Mode              roomdb.PrivacyMode
	AliasesSubdomains bool
}

// Runtime wraps resources initialized through the stable adapter layer.
type Runtime struct {
	Server *roomsrv.Server
	roomDB *roomsqlite.Database

	repo        roomrepo.Interface
	netInfo     network.ServerEndpointDetails
	authWithSSB *signinwithssb.SignalBridge
	logger      kitlog.Logger
}

func Start(ctx context.Context, cfg Config, logger kitlog.Logger) (*Runtime, error) {
	if strings.TrimSpace(cfg.RepoPath) == "" {
		return nil, fmt.Errorf("repo path must not be empty")
	}
	if strings.TrimSpace(cfg.MUXRPCListenAddr) == "" {
		return nil, fmt.Errorf("muxrpc listen address must not be empty")
	}
	if strings.TrimSpace(cfg.HTTPListenAddr) == "" {
		return nil, fmt.Errorf("http listen address must not be empty")
	}
	if cfg.Mode == roomdb.ModeUnknown {
		return nil, fmt.Errorf("room mode must be set")
	}

	if logger == nil {
		logger = kitlog.NewLogfmtLogger(io.Discard)
	}

	if err := os.MkdirAll(cfg.RepoPath, 0o700); err != nil {
		return nil, fmt.Errorf("create room repo path: %w", err)
	}

	r := roomrepo.New(cfg.RepoPath)
	rdb, err := roomsqlite.Open(r)
	if err != nil {
		return nil, fmt.Errorf("open room sqlite db: %w", err)
	}

	if err := rdb.Config.SetPrivacyMode(ctx, cfg.Mode); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("set room privacy mode: %w", err)
	}

	netInfo, err := buildEndpointDetails(cfg)
	if err != nil {
		_ = rdb.Close()
		return nil, err
	}

	siwssbBridge := signinwithssb.NewSignalBridge()

	opts := []roomsrv.Option{
		roomsrv.WithContext(ctx),
		roomsrv.WithRepoPath(cfg.RepoPath),
		roomsrv.WithUNIXSocket(false),
	}
	if logger != nil {
		opts = append(opts, roomsrv.WithLogger(logger))
	}

	srv, err := roomsrv.New(
		rdb.Members,
		rdb.DeniedKeys,
		rdb.Aliases,
		rdb.AuthWithSSB,
		siwssbBridge,
		rdb.Config,
		netInfo,
		opts...,
	)
	if err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("start room server: %w", err)
	}

	if err := ensureAdminMember(ctx, rdb.Members, srv.Whoami()); err != nil {
		_ = srv.Close()
		_ = rdb.Close()
		return nil, fmt.Errorf("ensure room admin member: %w", err)
	}

	return &Runtime{
		Server:      srv,
		roomDB:      rdb,
		repo:        r,
		netInfo:     netInfo,
		authWithSSB: siwssbBridge,
		logger:      logger,
	}, nil
}

func ensureAdminMember(ctx context.Context, members roomdb.MembersService, roomFeed refs.FeedRef) error {
	member, err := members.GetByFeed(ctx, roomFeed)
	if err != nil {
		if !errors.Is(err, roomdb.ErrNotFound) {
			return err
		}

		_, err = members.Add(ctx, roomFeed, roomdb.RoleAdmin)
		var alreadyAdded roomdb.ErrAlreadyAdded
		if err != nil && !errors.As(err, &alreadyAdded) {
			return err
		}
		return nil
	}

	if member.Role == roomdb.RoleAdmin {
		return nil
	}
	return members.SetRole(ctx, member.ID, roomdb.RoleAdmin)
}

func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}

	var firstErr error
	if r.Server != nil {
		r.Server.Shutdown()
		if err := r.Server.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if r.roomDB != nil {
		if err := r.roomDB.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// HTTPHandler constructs the stock room web handler backed by the embedded room runtime.
func (r *Runtime) HTTPHandler() (http.Handler, error) {
	if r == nil {
		return nil, fmt.Errorf("bridgeadapter runtime is nil")
	}
	if r.Server == nil {
		return nil, fmt.Errorf("bridgeadapter runtime server is nil")
	}
	if r.roomDB == nil {
		return nil, fmt.Errorf("bridgeadapter runtime room db is nil")
	}

	return handlers.New(
		r.logger,
		r.repo,
		r.netInfo,
		r.Server.StateManager,
		r.Server.Network,
		r.authWithSSB,
		handlers.Databases{
			Aliases:       r.roomDB.Aliases,
			AuthFallback:  r.roomDB.AuthFallback,
			AuthWithSSB:   r.roomDB.AuthWithSSB,
			Config:        r.roomDB.Config,
			DeniedKeys:    r.roomDB.DeniedKeys,
			Invites:       r.roomDB.Invites,
			Notices:       r.roomDB.Notices,
			Members:       r.roomDB.Members,
			PinnedNotices: r.roomDB.PinnedNotices,
		},
	)
}

// RoomConfig exposes the live room configuration store for request-time reads.
func (r *Runtime) RoomConfig() roomdb.RoomConfig {
	if r == nil || r.roomDB == nil {
		return nil
	}
	return r.roomDB.Config
}

func buildEndpointDetails(cfg Config) (network.ServerEndpointDetails, error) {
	host, portStr, err := net.SplitHostPort(cfg.HTTPListenAddr)
	if err != nil {
		return network.ServerEndpointDetails{}, fmt.Errorf("invalid room HTTP listen addr %q: %w", cfg.HTTPListenAddr, err)
	}
	port, err := net.LookupPort("tcp", portStr)
	if err != nil {
		return network.ServerEndpointDetails{}, fmt.Errorf("invalid room HTTP port in %q: %w", cfg.HTTPListenAddr, err)
	}

	domain := strings.TrimSpace(cfg.HTTPSDomain)
	devMode := false
	if domain == "" {
		domain = "localhost"
		devMode = true
	}
	if isLoopbackHost(host) && strings.TrimSpace(cfg.HTTPSDomain) == "" {
		devMode = true
	}

	return network.ServerEndpointDetails{
		ListenAddressMUXRPC:    cfg.MUXRPCListenAddr,
		Domain:                 domain,
		PortHTTPS:              uint(port),
		UseSubdomainForAliases: cfg.AliasesSubdomains,
		Development:            devMode,
	}, nil
}

func isLoopbackHost(host string) bool {
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
