package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/atindex"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/backfill"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bots"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bridge"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssbruntime"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/handlers"
	websecurity "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/security"
	"github.com/urfave/cli/v2"
)

type bridgeAppLifecycle interface {
	Init(ctx context.Context) error
	Start(ctx context.Context) error
	Stop() error
}

var newBridgeAppForRunStart = func(cfg AppConfig, logger *log.Logger) bridgeAppLifecycle {
	return NewBridgeApp(cfg, logger)
}

func runAccountList(ctx context.Context, dbPath string) error {
	database, err := db.Open(dbPath)
	if err != nil {
		return err
	}
	defer database.Close()

	accounts, err := database.GetAllBridgedAccounts(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Found %d accounts:\n", len(accounts))
	for _, acc := range accounts {
		status := "active"
		if !acc.Active {
			status = "inactive"
		}
		fmt.Printf("- %s (SSB: %s) [%s]\n", acc.ATDID, acc.SSBFeedID, status)
	}
	return nil
}

func runAccountAdd(ctx context.Context, dbPath, botSeed, did string) error {
	if did == "" {
		return fmt.Errorf("must provide a DID")
	}

	database, err := db.Open(dbPath)
	if err != nil {
		return err
	}
	defer database.Close()

	manager := bots.NewManager([]byte(botSeed), nil, nil, nil)
	feedRef, err := manager.GetFeedID(did)
	if err != nil {
		return fmt.Errorf("derive feed id: %w", err)
	}

	acc := db.BridgedAccount{
		ATDID:     did,
		SSBFeedID: feedRef.Ref(),
		Active:    true,
	}

	if err := database.AddBridgedAccount(ctx, acc); err != nil {
		return err
	}

	fmt.Printf("Added account %s\n", did)
	return nil
}

func runAccountRemove(ctx context.Context, dbPath, did string) error {
	if did == "" {
		return fmt.Errorf("must provide a DID")
	}

	database, err := db.Open(dbPath)
	if err != nil {
		return err
	}
	defer database.Close()

	acc, err := database.GetBridgedAccount(ctx, did)
	if err != nil {
		return err
	}
	if acc == nil {
		return fmt.Errorf("account not found")
	}

	acc.Active = false
	if err := database.AddBridgedAccount(ctx, *acc); err != nil {
		return err
	}

	fmt.Printf("Deactivated account %s\n", did)
	return nil
}

func runStats(ctx context.Context, dbPath string) error {
	database, err := db.Open(dbPath)
	if err != nil {
		return err
	}
	defer database.Close()

	totalAccounts, err := database.CountBridgedAccounts(ctx)
	if err != nil {
		return err
	}

	activeAccounts, err := database.CountActiveBridgedAccounts(ctx)
	if err != nil {
		return err
	}

	totalMessages, err := database.CountMessages(ctx)
	if err != nil {
		return err
	}

	publishedMessages, err := database.CountPublishedMessages(ctx)
	if err != nil {
		return err
	}

	publishFailures, err := database.CountPublishFailures(ctx)
	if err != nil {
		return err
	}
	deferredMessages, err := database.CountDeferredMessages(ctx)
	if err != nil {
		return err
	}
	deletedMessages, err := database.CountDeletedMessages(ctx)
	if err != nil {
		return err
	}

	totalBlobs, err := database.CountBlobs(ctx)
	if err != nil {
		return err
	}

	cursorVal, _, err := database.GetBridgeState(ctx, "firehose_seq")
	if err != nil {
		return err
	}
	replayCursor, _, err := database.GetBridgeState(ctx, "atproto_event_cursor")
	if err != nil {
		return err
	}
	source, err := database.GetATProtoSource(ctx, "default-relay")
	if err != nil {
		return err
	}
	eventHeadCursor, eventHeadOK, err := database.GetLatestATProtoEventCursor(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Bridge stats\n")
	fmt.Printf("- Accounts: %d total (%d active)\n", totalAccounts, activeAccounts)
	fmt.Printf("- Messages bridged: %d\n", totalMessages)
	fmt.Printf("- Messages published: %d\n", publishedMessages)
	fmt.Printf("- Publish failures: %d\n", publishFailures)
	fmt.Printf("- Messages deferred: %d\n", deferredMessages)
	fmt.Printf("- Messages deleted: %d\n", deletedMessages)
	fmt.Printf("- Blobs bridged: %d\n", totalBlobs)
	if replayCursor != "" {
		fmt.Printf("- Bridge replay cursor: %s\n", replayCursor)
	}
	if eventHeadOK {
		fmt.Printf("- ATProto event-log head: %d\n", eventHeadCursor)
	}
	if source != nil {
		fmt.Printf("- Relay source cursor: %d (%s)\n", source.LastSeq, fallbackValue(source.RelayURL, "-"))
	}
	if cursorVal != "" {
		fmt.Printf("- Legacy firehose cursor: %s\n", cursorVal)
	}
	return nil
}

func runStart(c *cli.Context) error {
	logRuntime, err := newBridgeLogRuntime(c, "bridge-cli")
	if err != nil {
		return err
	}
	defer shutdownLogRuntime(logRuntime)

	// Setup slog with runtime level control
	level := parseSlogLevel(c.String("log-level"))
	logRuntime.SetupDefaultSlogger(level)

	cfg, err := buildAppConfigFromCLI(c, appModeStart)
	if err != nil {
		return err
	}

	app := newBridgeAppForRunStart(cfg, logRuntime.Logger("bridge"))
	if err := app.Init(c.Context); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
	defer stop()

	return startBridgeAppLifecycle(ctx, app)
}

func runBackfill(c *cli.Context) error {
	logRuntime, err := newBridgeLogRuntime(c, "bridge-cli")
	if err != nil {
		return err
	}
	defer shutdownLogRuntime(logRuntime)

	cfg, err := buildAppConfigFromCLI(c, appModeBackfill)
	if err != nil {
		return err
	}

	app := NewBridgeApp(cfg, logRuntime.Logger("bridge"))
	if err := app.Init(c.Context); err != nil {
		return err
	}
	defer app.Stop()
	if err := app.StartIndexerPipeline(c.Context); err != nil {
		return err
	}

	dids := append([]string{}, c.StringSlice("did")...)
	if c.Bool("active-accounts") {
		accounts, err := app.DB().GetAllBridgedAccounts(c.Context)
		if err != nil {
			return err
		}
		for _, account := range accounts {
			if account.Active {
				dids = append(dids, account.ATDID)
			}
		}
	}
	dids = dedupeStrings(dids)
	if len(dids) == 0 {
		return fmt.Errorf("backfill requires at least one --did or --active-accounts")
	}

	sinceFilter, err := backfill.ParseSince(c.String("since"))
	if err != nil {
		return err
	}
	if sinceFilter.Raw != "" {
		fmt.Printf("Backfill note: --since is currently advisory under the queued atindex worker and is not applied to sync.getRepo snapshots in v1.\n")
	}

	stateCounts := map[string]int{}
	failed := 0
	var maxReplayCursor int64
	for _, did := range dids {
		if err := app.Indexer().RequestResync(c.Context, did, "cli_backfill"); err != nil {
			failed++
			fmt.Printf("Backfill did=%s status=error err=%v\n", did, err)
			continue
		}
		info, waitErr := waitForIndexedRepoState(c.Context, app.Indexer(), did, 5*time.Minute)
		if waitErr != nil {
			failed++
			fmt.Printf("Backfill did=%s status=error err=%v\n", did, waitErr)
			continue
		}
		stateCounts[info.SyncState]++
		if info.SyncState != atindex.StateSynced {
			failed++
		}
		if info.LastEventCursor != nil && *info.LastEventCursor > maxReplayCursor {
			maxReplayCursor = *info.LastEventCursor
		}
		fmt.Printf(
			"Backfill did=%s pds=%s status=%s generation=%d last_error=%s\n",
			did,
			fallbackValue(info.PDSURL, "-"),
			info.SyncState,
			info.Generation,
			fallbackValue(info.LastError, "-"),
		)
	}
	if failed == 0 && maxReplayCursor > 0 {
		if err := waitForReplayCursor(c.Context, app.DB(), maxReplayCursor, 5*time.Minute); err != nil {
			failed++
			fmt.Printf("Backfill replay status=error target_cursor=%d err=%v\n", maxReplayCursor, err)
		}
	}

	fmt.Printf(
		"Backfill summary: dids=%d pending=%d backfilling=%d synced=%d desynchronized=%d deleted=%d deactivated=%d takendown=%d suspended=%d error=%d failed=%d\n",
		len(dids),
		stateCounts[atindex.StatePending],
		stateCounts[atindex.StateBackfilling],
		stateCounts[atindex.StateSynced],
		stateCounts[atindex.StateDesynchronized],
		stateCounts[atindex.StateDeleted],
		stateCounts[atindex.StateDeactivated],
		stateCounts[atindex.StateTakendown],
		stateCounts[atindex.StateSuspended],
		stateCounts[atindex.StateError],
		failed,
	)
	if failed > 0 {
		return fmt.Errorf("backfill failed for %d did(s)", failed)
	}
	return nil
}

func runRetryFailures(c *cli.Context) error {
	logRuntime, err := newBridgeLogRuntime(c, "bridge-cli")
	if err != nil {
		return err
	}
	defer shutdownLogRuntime(logRuntime)

	cfg, err := buildAppConfigFromCLI(c, appModeRetry)
	if err != nil {
		return err
	}

	app := NewBridgeApp(cfg, logRuntime.Logger("bridge"))
	if err := app.Init(c.Context); err != nil {
		return err
	}
	defer app.Stop()

	result, err := app.Processor().RetryFailedMessages(c.Context, bridge.RetryConfig{
		Limit:       c.Int("limit"),
		ATDID:       c.String("did"),
		MaxAttempts: c.Int("max-attempts"),
		BaseBackoff: c.Duration("base-backoff"),
	})
	if err != nil {
		return err
	}

	fmt.Printf(
		"Retry complete: selected=%d attempted=%d published=%d failed=%d deferred=%d\n",
		result.Selected,
		result.Attempted,
		result.Published,
		result.Failed,
		result.Deferred,
	)
	return nil
}

func runServeUI(c *cli.Context) error {
	logRuntime, err := newBridgeLogRuntime(c, "bridge-ui")
	if err != nil {
		return err
	}
	defer shutdownLogRuntime(logRuntime)

	database, err := db.Open(dbPath)
	if err != nil {
		return err
	}
	defer database.Close()

	var atpClient handlers.PDSClientInterface
	if c.String("pds-host") != "" && c.String("pds-password") != "" {
		host, err := resolveLiveXRPCHost(c.String("pds-host"))
		if err != nil {
			return err
		}
		atpClient = &handlers.PDSClient{
			Host:     host,
			Password: c.String("pds-password"),
			Insecure: c.Bool("atproto-insecure"),
		}
	}

	listenAddr := c.String("listen-addr")
	authUser := strings.TrimSpace(c.String("ui-auth-user"))
	authPassEnv := strings.TrimSpace(c.String("ui-auth-pass-env"))
	authPass := ""
	if authPassEnv != "" {
		authPass = os.Getenv(authPassEnv)
		if strings.TrimSpace(authPass) == "" {
			return fmt.Errorf("ui auth password env %q is empty or unset", authPassEnv)
		}
	}

	if authUser == "" && authPassEnv != "" {
		return fmt.Errorf("--ui-auth-user is required when --ui-auth-pass-env is set")
	}
	if authUser != "" && authPassEnv == "" {
		return fmt.Errorf("--ui-auth-pass-env is required when --ui-auth-user is set")
	}

	authConfigured := authUser != "" && authPass != ""
	if websecurity.RequireAuthForBind(listenAddr) && !authConfigured {
		return fmt.Errorf("refusing to serve UI on non-loopback address %q without auth; configure --ui-auth-user and --ui-auth-pass-env", listenAddr)
	}

	uiLogger := logRuntime.Logger("ui")
	effectiveRepoPath := strings.TrimSpace(c.String("repo-path"))
	if effectiveRepoPath == "" {
		effectiveRepoPath = ".ssb-bridge"
	}
	roomRepoPath := strings.TrimSpace(c.String("room-repo-path"))
	if roomRepoPath == "" {
		roomRepoPath = filepath.Join(effectiveRepoPath, "room")
	}
	roomHTTPBaseURL := strings.TrimSpace(c.String("room-http-base-url"))

	httpClient := newATProtoHTTPClient(c.Bool("atproto-insecure"), c.Duration("http-timeout"))
	indexer := atindex.New(
		database,
		backfill.DIDPDSResolver{PLCURL: c.String("plc-url"), HTTPClient: httpClient},
		backfill.XRPCRepoFetcher{HTTPClient: httpClient},
		relayURL,
		uiLogger,
	)

	var ssbStatus handlers.SSBStatusProvider
	var blobStore handlers.BlobStore
	var ssbRuntime *ssbruntime.Runtime
	if repo := strings.TrimSpace(c.String("repo-path")); repo != "" {
		ssbRuntime, err = ssbruntime.Open(c.Context, ssbruntime.Config{
			RepoPath:   repo,
			MasterSeed: []byte(botSeed),
			GossipDB:   database,
		}, logRuntime.Logger("ssb"))

		// We always wrap the blob store in a composite that checks the filesystem
		// (RepoPath/blobs) if the runtime doesn't have it.
		var primaryStore handlers.BlobStore
		if err == nil {
			defer ssbRuntime.Close()
			primaryStore = ssbRuntime.BlobStore()
			ssbStatus = ssbRuntime
		} else {
			uiLogger.Printf("event=ssb_runtime_open_failed repo=%s err=%v acting_as_blob_only=true", repo, err)
		}
		blobStore = &compositeBlobStore{
			primary: primaryStore,
			fsPath:  filepath.Join(repo, "blobs"),
		}
	}

	var roomOps handlers.RoomOpsProvider
	roomProvider, err := handlers.OpenSQLiteRoomOpsProvider(roomRepoPath, roomHTTPBaseURL, roomdb.RoleAdmin, uiLogger)
	if err != nil {
		uiLogger.Printf("event=room_ops_provider_unavailable room_repo=%s err=%v", roomRepoPath, err)
	} else {
		roomOps = roomProvider
		defer roomOps.Close()
	}

	ctx, stop := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
	defer stop()
	indexer.Start(ctx)

	r := chi.NewRouter()
	r.Use(websecurity.RequestLogMiddleware(uiLogger))
	r.Use(websecurity.SecurityHeadersMiddleware(true))
	if authConfigured {
		r.Use(websecurity.BasicAuthMiddleware(authUser, authPass))
	}
	csrfConfig := websecurity.DefaultCSRFConfig()
	r.Use(websecurity.CSRFMiddleware(csrfConfig))

	manager := bots.NewManager([]byte(botSeed), nil, nil, nil)
	ui := handlers.NewUIHandler(database, uiLogger, atpClient, blobStore, ssbStatus, manager).WithATProto(database, indexer)
	if reverseCreds, err := bridge.LoadReverseCredentials(c.String("reverse-credentials-file")); err != nil {
		return err
	} else {
		reverseProcessor := bridge.NewReverseProcessor(bridge.ReverseProcessorConfig{
			DB:           database,
			BlobStore:    blobStore,
			BlobFetcher:  ssbRuntime,
			Writer:       &bridge.ATProtoReverseWriter{HTTPClient: httpClient, Insecure: c.Bool("atproto-insecure")},
			HostResolver: bridge.NewDefaultReverseHostResolver(c.String("plc-url"), httpClient),
			Logger:       uiLogger,
			Credentials:  reverseCreds,
			Enabled:      c.Bool("reverse-sync-enable"),
		})
		ui = ui.WithReverseSync(reverseProcessor)
	}
	if roomOps != nil {
		ui = ui.WithRoomOps(roomOps)
	}
	ui.Mount(r)

	server := &http.Server{
		Addr:    listenAddr,
		Handler: r,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	fmt.Printf(
		"Serving UI at http://%s (auth=%t room_repo=%s room_data=%t)\n",
		listenAddr,
		authConfigured,
		roomRepoPath,
		roomOps != nil,
	)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func startBridgeAppLifecycle(ctx context.Context, app bridgeAppLifecycle) error {
	if err := app.Start(ctx); err != nil {
		stopErr := app.Stop()
		if stopErr != nil {
			return errors.Join(err, stopErr)
		}
		return err
	}

	<-ctx.Done()
	return app.Stop()
}

func waitForIndexedRepoState(ctx context.Context, indexer *atindex.Service, did string, timeout time.Duration) (*db.ATProtoRepo, error) {
	if indexer == nil {
		return nil, fmt.Errorf("indexer not configured")
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		info, err := indexer.GetRepoInfo(deadlineCtx, did)
		if err != nil {
			return nil, err
		}
		if info != nil {
			switch info.SyncState {
			case atindex.StateSynced, atindex.StateDeleted, atindex.StateDeactivated, atindex.StateTakendown, atindex.StateSuspended, atindex.StateError:
				return info, nil
			}
		}

		select {
		case <-deadlineCtx.Done():
			return nil, deadlineCtx.Err()
		case <-ticker.C:
		}
	}
}

func waitForReplayCursor(ctx context.Context, database *db.DB, target int64, timeout time.Duration) error {
	if database == nil {
		return fmt.Errorf("database not configured")
	}
	if target <= 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		cursor, err := readATProtoEventCursor(deadlineCtx, database)
		if err != nil {
			return err
		}
		if cursor >= target {
			return nil
		}

		select {
		case <-deadlineCtx.Done():
			return deadlineCtx.Err()
		case <-ticker.C:
		}
	}
}
