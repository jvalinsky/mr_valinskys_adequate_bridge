package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/atindex"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/backfill"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/blobbridge"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bridge"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/firehose"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/metrics"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/publishqueue"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssbruntime"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/handlers"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/xrpc"
	"github.com/mark3labs/mcp-go/server"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type BridgeApp struct {
	db              *db.DB
	ssbRuntime      *ssbruntime.Runtime
	publisher       *publishqueue.WorkerPublisher
	processor       *bridge.Processor
	indexer         *atindex.Service
	firehose        *firehose.Client
	room            *room.Runtime
	bridgedPeers    bridgedRoomPeerSessionManager
	logger          *log.Logger
	mcpServer       *server.MCPServer
	followerTracker *bridge.FollowerTracker
	metricsSrv      *http.Server

	cfg AppConfig
}

type AppConfig struct {
	DBPath              string
	RepoPath            string
	BotSeed             string
	HMACKey             *[32]byte
	AppKey              string
	SSBListenAddr       string
	PublishWorkers      int
	FirehoseEnable      bool
	RelayURL            string
	XRPCReadHost        string
	RoomEnable          bool
	RoomListenAddr      string
	RoomHTTPAddr        string
	RoomMode            string
	RoomDomain          string
	RoomTLSCert         string
	RoomTLSKey          string
	PLCURL              string
	AtprotoInsecure     bool
	MCPListenAddr       string
	MetricsListenAddr   string
	MaxMsgsPerDIDPerMin int
	BridgedPeerSyncIntv time.Duration
}

func (a *BridgeApp) MCPServer() *server.MCPServer {
	return a.mcpServer
}

func NewBridgeApp(cfg AppConfig, logger *log.Logger) *BridgeApp {
	return &BridgeApp{
		cfg:    cfg,
		logger: logger,
	}
}

func (a *BridgeApp) Init(ctx context.Context) error {
	var err error
	a.db, err = db.Open(a.cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	a.mcpServer = server.NewMCPServer(
		"bridge-live",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	a.ssbRuntime, err = ssbruntime.Open(ctx, ssbruntime.Config{
		RepoPath:   a.cfg.RepoPath,
		ListenAddr: a.cfg.SSBListenAddr,
		MasterSeed: []byte(a.cfg.BotSeed),
		HMACKey:    a.cfg.HMACKey,
		AppKey:     a.cfg.AppKey,
		GossipDB:   a.db,
	}, a.logger)
	if err != nil {
		return fmt.Errorf("init ssb runtime: %w", err)
	}

	a.publisher = publishqueue.New(a.ssbRuntime, a.cfg.PublishWorkers, a.logger)

	xrpcHost, err := resolveLiveXRPCHost(a.cfg.XRPCReadHost)
	if err != nil {
		return err
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	if a.cfg.AtprotoInsecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	xrpcClient := &xrpc.Client{
		Host:   xrpcHost,
		Client: httpClient,
	}

	blobHostResolver, err := resolveLiveBlobHostResolver(a.cfg.XRPCReadHost, a.cfg.PLCURL, a.cfg.AtprotoInsecure)
	if err != nil {
		return err
	}

	blobBridge := blobbridge.NewWithResolver(a.db, a.ssbRuntime.BlobStore(), blobHostResolver, httpClient, a.logger)
	pdsResolver := backfill.DIDPDSResolver{
		PLCURL:     a.cfg.PLCURL,
		HTTPClient: httpClient,
	}
	a.indexer = atindex.New(a.db, pdsResolver, backfill.XRPCRepoFetcher{HTTPClient: httpClient}, a.cfg.RelayURL, a.logger)
	recordFetcher := bridge.NewPDSAwareRecordFetcher(pdsResolver, xrpcClient)

	dependencyResolver := bridge.NewATProtoDependencyResolver(
		a.db,
		a.logger,
		recordFetcher,
		func(ctx context.Context, atDID, atURI, atCID, collection string, recordJSON []byte) error {
			return a.processor.ProcessRecord(ctx, atDID, atURI, atCID, collection, recordJSON)
		},
	)

	a.processor = bridge.NewProcessor(
		a.db,
		a.logger,
		bridge.WithPublisher(a.publisher),
		bridge.WithBlobBridge(blobBridge),
		bridge.WithDependencyResolver(dependencyResolver),
		bridge.WithFeedResolver(a.ssbRuntime),
		bridge.WithMaxMessagesPerMinute(a.cfg.MaxMsgsPerDIDPerMin),
	)

	if a.cfg.FirehoseEnable {
		firehoseOpts := []firehose.ClientOption{}
		if cursor, ok, err := readFirehoseCursor(ctx, a.db); err == nil && ok {
			firehoseOpts = append(firehoseOpts, firehose.WithCursor(cursor))
		}
		firehoseOpts = append(firehoseOpts, firehose.WithConnectedCallback(func() {
			setBridgeStateBestEffort(ctx, a.db, "firehose_connected", "1", a.logger)
		}))
		a.firehose = firehose.NewClient(a.cfg.RelayURL, a.indexer, a.logger, firehoseOpts...)
	}

	if a.cfg.RoomEnable {
		a.room, err = room.Start(ctx, room.Config{
			ListenAddr:            a.cfg.RoomListenAddr,
			HTTPListenAddr:        a.cfg.RoomHTTPAddr,
			RepoPath:              filepath.Join(a.cfg.RepoPath, "room"),
			Mode:                  a.cfg.RoomMode,
			HTTPSDomain:           a.cfg.RoomDomain,
			TLSCertFile:           a.cfg.RoomTLSCert,
			TLSKeyFile:            a.cfg.RoomTLSKey,
			AppKey:                a.cfg.AppKey,
			BridgeAccountLister:   a.db,
			BridgeAccountDetailer: a.db,
			HandlerMux:            a.ssbRuntime.Node().HandlerMux(),
		}, a.logger)
		if err != nil {
			return fmt.Errorf("start room runtime: %w", err)
		}

		a.bridgedPeers, err = newBridgedRoomPeerManager(bridgedRoomPeerManagerConfig{
			AccountLister: a.db,
			RoomRuntime:   a.room,
			Store:         a.ssbRuntime.Node().Store(),
			BotSeed:       a.cfg.BotSeed,
			AppKey:        a.cfg.AppKey,
			SyncInterval:  a.cfg.BridgedPeerSyncIntv,
		}, a.logger)
		if err != nil {
			return fmt.Errorf("init bridged room peer manager: %w", err)
		}

		a.initFollowerTracker(ctx)
	}

	// Register all MCP tools against the live application instances.
	registerBridgeOpsTools(a.mcpServer, a.db, a.cfg.BotSeed)

	atprotoDeps := atprotoMCPDeps{
		database:   a.db,
		httpClient: httpClient,
		appviewURL: strings.TrimRight(xrpcHost, "/"),
		plcURL:     strings.TrimRight(a.cfg.PLCURL, "/"),
	}
	registerATProtoTools(a.mcpServer, atprotoDeps)

	ssbDeps := ssbMCPDeps{
		ssbRT:   a.ssbRuntime,
		roomOps: nil,
	}
	if a.cfg.RoomEnable {
		roomProvider, err := handlers.OpenSQLiteRoomOpsProvider(filepath.Join(a.cfg.RepoPath, "room"), "", roomdb.RoleAdmin, nil)
		if err == nil {
			ssbDeps.roomOps = roomProvider
		} else {
			a.logger.Printf("mcp: failed to open room ops provider: %v", err)
		}
	}
	registerSSBTools(a.mcpServer, ssbDeps)

	return nil
}

func (a *BridgeApp) initFollowerTracker(ctx context.Context) {
	if a.room == nil || a.ssbRuntime == nil {
		return
	}

	bridgeBotDID := ""
	bridgeBotSSBFeed := ""

	accounts, err := a.db.GetAllBridgedAccounts(ctx)
	if err == nil && len(accounts) > 0 {
		bridgeBotDID = accounts[0].ATDID
		bridgeBotSSBFeed = accounts[0].SSBFeedID
	}

	if bridgeBotDID == "" {
		a.logger.Printf("follower-tracker: no bridged accounts found, skipping")
		return
	}

	xrpcClient := &xrpc.Client{
		Host: strings.TrimRight(a.cfg.XRPCReadHost, "/"),
	}

	tracker := bridge.NewFollowerTracker(bridge.FollowerTrackerConfig{
		DB:            a.db,
		XRPCClient:    xrpcClient,
		BotDID:        bridgeBotDID,
		BotSSBFeed:    bridgeBotSSBFeed,
		DebounceDelay: 5 * time.Second,
		RateLimitDur:  60 * time.Second,
		MaxFollowsPer: 10,
	})

	a.room.SetAnnounceHook(func(feed refs.FeedRef) error {
		tracker.Announce(feed)
		return nil
	})
	tracker.Start(ctx, a.logger)
	a.followerTracker = tracker

	a.logger.Printf("follower-tracker: started for bot %s", bridgeBotDID)
}

func (a *BridgeApp) Start(ctx context.Context) error {
	if err := a.StartIndexerPipeline(ctx); err != nil {
		return err
	}

	if a.firehose != nil {
		go func() {
			err := a.firehose.RunWithReconnect(ctx, firehose.ReconnectConfig{
				InitialBackoff: 2 * time.Second,
				MaxBackoff:     60 * time.Second,
				Jitter:         750 * time.Millisecond,
			})
			if err != nil && !errors.Is(err, context.Canceled) {
				a.logger.Printf("firehose error: %v", err)
			}
		}()
	}

	if a.room != nil {
		go runRoomTunnelBootstrap(ctx, a.ssbRuntime, a.room, a.logger)
		if a.bridgedPeers != nil {
			a.bridgedPeers.Start(ctx)
			if err := a.bridgedPeers.Reconcile(ctx); err != nil {
				a.logger.Printf("event=bridged_room_peer_reconcile_failed err=%v", err)
			}
		}
	}

	go runRetryScheduler(ctx, a.processor, a.logger)
	go runDeferredResolverScheduler(ctx, a.processor, a.logger)
	go runDeferredExpiryScheduler(ctx, a.processor, a.logger)
	go runATProtoTrackScheduler(ctx, a.db, a.indexer, a.logger)
	go runRuntimeHeartbeatScheduler(ctx, a.db, a.logger, 10*time.Second)
	a.processor.StartRateLimiterCleanup(ctx, 5*time.Minute, 10*time.Minute)

	setBridgeStateBestEffort(ctx, a.db, bridgeRuntimeStatusKey, "live", a.logger)
	setBridgeStateBestEffort(ctx, a.db, bridgeRuntimeStartedAtKey, time.Now().UTC().Format(time.RFC3339), a.logger)

	if a.cfg.MetricsListenAddr != "" {
		a.startMetricsUpdater(ctx)
		go func() {
			if err := a.startMetricsServer(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				a.logger.Printf("metrics server error: %v", err)
			}
		}()
	}

	if a.cfg.MCPListenAddr != "" && a.mcpServer != nil {
		go func() {
			err := a.startMCPServer(ctx)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				a.logger.Printf("mcp server error: %v", err)
			}
		}()
	}

	return nil
}

func (a *BridgeApp) startMCPServer(ctx context.Context) error {
	mcpSSEServer := server.NewSSEServer(a.mcpServer, server.WithStaticBasePath("/api/mcp"))

	mux := http.NewServeMux()
	mux.Handle("/api/mcp/sse", mcpSSEServer.SSEHandler())
	mux.Handle("/api/mcp/message", mcpSSEServer.MessageHandler())

	srv := &http.Server{
		Addr:    a.cfg.MCPListenAddr,
		Handler: mux,
	}

	a.logger.Printf("event=mcp_server_start addr=%s", a.cfg.MCPListenAddr)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	return srv.ListenAndServe()
}

func (a *BridgeApp) startMetricsServer(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	a.metricsSrv = &http.Server{
		Addr:    a.cfg.MetricsListenAddr,
		Handler: mux,
	}

	a.logger.Printf("event=metrics_server_start addr=%s", a.cfg.MetricsListenAddr)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.metricsSrv.Shutdown(shutdownCtx)
	}()

	return a.metricsSrv.ListenAndServe()
}

func (a *BridgeApp) startMetricsUpdater(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.updateMetrics(ctx)
			}
		}
	}()
}

func (a *BridgeApp) updateMetrics(ctx context.Context) {
	if a.db != nil {
		if active, err := a.db.CountActiveBridgedAccounts(ctx); err == nil {
			metrics.ActiveAccounts.Set(float64(active))
		}
		if deferred, err := a.db.CountDeferredMessages(ctx); err == nil {
			metrics.DeferredBacklog.Set(float64(deferred))
		}
	}
	if a.indexer != nil {
		metrics.IndexerQueueDepth.Set(float64(a.indexer.QueueDepth()))
	}
	if info, err := os.Stat(a.cfg.DBPath); err == nil {
		metrics.DBSizeBytes.Set(float64(info.Size()))
	}
	if size, err := dirSize(filepath.Join(a.cfg.RepoPath, "blobs")); err == nil {
		metrics.BlobStoreSizeBytes.Set(float64(size))
	}
	if a.db != nil {
		if age, err := a.db.OldestDeferredAgeSeconds(ctx); err == nil {
			metrics.DeferredOldestAgeSeconds.Set(age)
		}
		if exhausted, err := a.db.CountExhaustedMessages(ctx, 8); err == nil {
			metrics.RetryExhaustedMessages.Set(float64(exhausted))
		}
	}
}

func dirSize(path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total, err
}

func (a *BridgeApp) StartIndexerPipeline(ctx context.Context) error {
	if a.indexer != nil {
		a.indexer.Start(ctx)
		if err := a.startIndexerConsumer(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (a *BridgeApp) Stop() error {
	var errs []error

	if a.db != nil {
		setBridgeStateBestEffort(context.Background(), a.db, bridgeRuntimeStatusKey, "stopping", a.logger)
	}

	if a.bridgedPeers != nil {
		if err := a.bridgedPeers.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("stop bridged room peers: %w", err))
		}
	}

	if a.room != nil {
		if err := a.room.Close(); err != nil {
			errs = append(errs, fmt.Errorf("stop room: %w", err))
		}
	}

	if a.publisher != nil {
		a.publisher.Close()
	}

	if a.ssbRuntime != nil {
		if err := a.ssbRuntime.Close(); err != nil {
			errs = append(errs, fmt.Errorf("stop ssb: %w", err))
		}
	}

	if a.db != nil {
		setBridgeStateBestEffort(context.Background(), a.db, bridgeRuntimeStatusKey, "stopped", a.logger)
		if err := a.db.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close db: %w", err))
		}
	}

	return errors.Join(errs...)
}

func (a *BridgeApp) DB() *db.DB {
	return a.db
}

func (a *BridgeApp) Processor() *bridge.Processor {
	return a.processor
}

func (a *BridgeApp) Indexer() *atindex.Service {
	return a.indexer
}

func (a *BridgeApp) SSB() *ssbruntime.Runtime {
	return a.ssbRuntime
}

func (a *BridgeApp) Room() *room.Runtime {
	return a.room
}

func (a *BridgeApp) startIndexerConsumer(ctx context.Context) error {
	if a.indexer == nil || a.processor == nil {
		return nil
	}

	cursor, err := readATProtoEventCursor(ctx, a.db)
	if err != nil {
		return err
	}
	stream, err := a.indexer.Subscribe(ctx, cursor)
	if err != nil {
		return err
	}

	go func() {
		for note := range stream {
			if note.Kind != atindex.EventKindRecord || note.Record == nil {
				continue
			}
			if err := a.processor.HandleRecordEvent(ctx, *note.Record); err != nil {
				a.logger.Printf("event=atindex_consumer_error at_uri=%s action=%s err=%v", note.Record.ATURI, note.Record.Action, err)
				continue
			}
			setBridgeStateBestEffort(ctx, a.db, "atproto_event_cursor", fmt.Sprintf("%d", note.Cursor), a.logger)
		}
	}()
	return nil
}

func readATProtoEventCursor(ctx context.Context, database *db.DB) (int64, error) {
	if database == nil {
		return 0, nil
	}
	value, ok, err := database.GetBridgeState(ctx, "atproto_event_cursor")
	if err != nil || !ok || strings.TrimSpace(value) == "" {
		return 0, err
	}
	cursor, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse atproto_event_cursor %q: %w", value, err)
	}
	return cursor, nil
}
