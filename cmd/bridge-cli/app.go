package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/bluesky-social/indigo/xrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/backfill"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/blobbridge"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bridge"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/firehose"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/publishqueue"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssbruntime"
)

type BridgeApp struct {
	db         *db.DB
	ssbRuntime *ssbruntime.Runtime
	publisher  *publishqueue.WorkerPublisher
	processor  *bridge.Processor
	firehose   *firehose.Client
	room       *room.Runtime
	logger     *log.Logger

	cfg AppConfig
}

type AppConfig struct {
	DBPath         string
	RepoPath       string
	BotSeed        string
	HMACKey        *[32]byte
	AppKey         string
	SSBListenAddr  string
	PublishWorkers int
	FirehoseEnable bool
	RelayURL       string
	XRPCReadHost   string
	RoomEnable     bool
	RoomListenAddr string
	RoomHTTPAddr   string
	RoomMode       string
	RoomDomain     string
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
	xrpcClient := &xrpc.Client{Host: xrpcHost}

	blobHostResolver, err := resolveLiveBlobHostResolver(a.cfg.XRPCReadHost, a.cfg.XRPCReadHost != "")
	if err != nil {
		return err
	}

	blobBridge := blobbridge.NewWithResolver(a.db, a.ssbRuntime.BlobStore(), blobHostResolver, nil, a.logger)
	pdsResolver := backfill.DIDPDSResolver{PLCURL: "https://plc.directory"}
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
	)

	if a.cfg.FirehoseEnable {
		firehoseOpts := []firehose.ClientOption{}
		if cursor, ok, err := readFirehoseCursor(ctx, a.db); err == nil && ok {
			firehoseOpts = append(firehoseOpts, firehose.WithCursor(cursor))
		}
		firehoseOpts = append(firehoseOpts, firehose.WithConnectedCallback(func() {
			setBridgeStateBestEffort(ctx, a.db, "firehose_connected", "1", a.logger)
		}))
		a.firehose = firehose.NewClient(a.cfg.RelayURL, a.processor, a.logger, firehoseOpts...)
	}

	if a.cfg.RoomEnable {
		a.room, err = room.Start(ctx, room.Config{
			ListenAddr:            a.cfg.RoomListenAddr,
			HTTPListenAddr:        a.cfg.RoomHTTPAddr,
			RepoPath:              filepath.Join(a.cfg.RepoPath, "room"),
			Mode:                  a.cfg.RoomMode,
			HTTPSDomain:           a.cfg.RoomDomain,
			AppKey:                a.cfg.AppKey,
			BridgeAccountLister:   a.db,
			BridgeAccountDetailer: a.db,
			HandlerMux:            a.ssbRuntime.Node().HandlerMux(),
		}, a.logger)
		if err != nil {
			return fmt.Errorf("start room runtime: %w", err)
		}
	}

	return nil
}

func (a *BridgeApp) Start(ctx context.Context) error {
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
	}

	go runRetryScheduler(ctx, a.processor, a.logger)
	go runDeferredResolverScheduler(ctx, a.processor, a.logger)
	go runAutoBackfillScheduler(ctx, a.db, a.processor, a.logger)
	go runRuntimeHeartbeatScheduler(ctx, a.db, a.logger, 10*time.Second)

	setBridgeStateBestEffort(ctx, a.db, bridgeRuntimeStatusKey, "live", a.logger)
	setBridgeStateBestEffort(ctx, a.db, bridgeRuntimeStartedAtKey, time.Now().UTC().Format(time.RFC3339), a.logger)

	return nil
}

func (a *BridgeApp) Stop() error {
	var errs []error

	if a.db != nil {
		setBridgeStateBestEffort(context.Background(), a.db, bridgeRuntimeStatusKey, "stopping", a.logger)
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

func (a *BridgeApp) SSB() *ssbruntime.Runtime {
	return a.ssbRuntime
}
