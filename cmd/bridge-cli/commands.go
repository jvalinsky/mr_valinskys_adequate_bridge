package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/backfill"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bots"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bridge"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssbruntime"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/handlers"
	websecurity "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/security"
	"github.com/urfave/cli/v2"
)

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

	fmt.Printf("Bridge stats\n")
	fmt.Printf("- Accounts: %d total (%d active)\n", totalAccounts, activeAccounts)
	fmt.Printf("- Messages bridged: %d\n", totalMessages)
	fmt.Printf("- Messages published: %d\n", publishedMessages)
	fmt.Printf("- Publish failures: %d\n", publishFailures)
	fmt.Printf("- Messages deferred: %d\n", deferredMessages)
	fmt.Printf("- Messages deleted: %d\n", deletedMessages)
	fmt.Printf("- Blobs bridged: %d\n", totalBlobs)
	if cursorVal != "" {
		fmt.Printf("- Firehose cursor: %s\n", cursorVal)
	}
	return nil
}

func runStart(c *cli.Context) error {
	logRuntime, err := newBridgeLogRuntime(c, "bridge-cli")
	if err != nil {
		return err
	}
	defer shutdownLogRuntime(logRuntime)

	hmacKey, err := parseHMACKey(c.String("hmac-key"))
	if err != nil {
		return err
	}

	repoPath, err := resolveSharedRepoPath(c)
	if err != nil {
		return err
	}

	cfg := AppConfig{
		DBPath:         dbPath,
		RepoPath:       repoPath,
		BotSeed:        botSeed,
		HMACKey:        hmacKey,
		AppKey:         c.String("app-key"),
		SSBListenAddr:  c.String("ssb-listen-addr"),
		PublishWorkers: c.Int("publish-workers"),
		FirehoseEnable: c.Bool("firehose-enable"),
		RelayURL:       relayURL,
		XRPCReadHost:   c.String("xrpc-host"),
		RoomEnable:     c.Bool("room-enable"),
		RoomListenAddr: c.String("room-listen-addr"),
		RoomHTTPAddr:   c.String("room-http-listen-addr"),
		RoomMode:       c.String("room-mode"),
		RoomDomain:     c.String("room-https-domain"),
		PLCURL:         c.String("plc-url"),
		AtprotoInsecure: c.Bool("atproto-insecure"),
	}

	app := NewBridgeApp(cfg, logRuntime.Logger("bridge"))
	if err := app.Init(c.Context); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Start(ctx); err != nil {
		return err
	}

	<-ctx.Done()
	return app.Stop()
}

func runBackfill(c *cli.Context) error {
	logRuntime, err := newBridgeLogRuntime(c, "bridge-cli")
	if err != nil {
		return err
	}
	defer shutdownLogRuntime(logRuntime)

	hmacKey, err := parseHMACKey(c.String("hmac-key"))
	if err != nil {
		return err
	}

	repoPath, err := resolveSharedRepoPath(c)
	if err != nil {
		return err
	}

	cfg := AppConfig{
		DBPath:         dbPath,
		RepoPath:       repoPath,
		BotSeed:        botSeed,
		HMACKey:        hmacKey,
		AppKey:         c.String("app-key"),
		PublishWorkers: c.Int("publish-workers"),
		XRPCReadHost:   c.String("xrpc-host"),
		PLCURL:         c.String("plc-url"),
		AtprotoInsecure: c.Bool("atproto-insecure"),
	}

	app := NewBridgeApp(cfg, logRuntime.Logger("bridge"))
	if err := app.Init(c.Context); err != nil {
		return err
	}
	defer app.Stop()

	bridgeLogger := logRuntime.Logger("bridge")

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

	hostResolver, err := resolveBackfillHostResolver(c.String("xrpc-host"), c.String("plc-url"), c.Bool("atproto-insecure"))
	if err != nil {
		return err
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	if c.Bool("atproto-insecure") {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	repoFetcher := backfill.XRPCRepoFetcher{HTTPClient: httpClient}

	total := backfill.Stats{}
	statusCounts := map[backfill.DIDStatus]int{}
	for _, did := range dids {
		result := backfill.RunForDID(c.Context, did, sinceFilter, app.Processor(), bridgeLogger, hostResolver, repoFetcher)
		statusCounts[result.Status]++
		total.Processed += result.Stats.Processed
		total.Skipped += result.Stats.Skipped
		total.Errors += result.Stats.Errors

		if result.Err != nil {
			fmt.Printf(
				"Backfill did=%s pds=%s status=%s processed=%d skipped=%d record_errors=%d err=%v\n",
				result.DID,
				fallbackValue(result.PDSHost, "-"),
				result.Status,
				result.Stats.Processed,
				result.Stats.Skipped,
				result.Stats.Errors,
				result.Err,
			)
			continue
		}

		fmt.Printf(
			"Backfill did=%s pds=%s status=%s processed=%d skipped=%d record_errors=%d\n",
			result.DID,
			fallbackValue(result.PDSHost, "-"),
			result.Status,
			result.Stats.Processed,
			result.Stats.Skipped,
			result.Stats.Errors,
		)
	}

	failedCount := len(dids) - statusCounts[backfill.StatusSuccess]
	fmt.Printf(
		"Backfill summary: dids=%d processed=%d skipped=%d record_errors=%d auth_required=%d not_found=%d malformed_did_doc=%d unsupported_did=%d transport_error=%d failed=%d\n",
		len(dids),
		total.Processed,
		total.Skipped,
		total.Errors,
		statusCounts[backfill.StatusAuthRequired],
		statusCounts[backfill.StatusNotFound],
		statusCounts[backfill.StatusMalformedDIDDoc],
		statusCounts[backfill.StatusUnsupportedDID],
		statusCounts[backfill.StatusTransportError],
		failedCount,
	)
	if failedCount > 0 {
		return fmt.Errorf("backfill failed for %d did(s)", failedCount)
	}
	return nil
}

func runRetryFailures(c *cli.Context) error {
	logRuntime, err := newBridgeLogRuntime(c, "bridge-cli")
	if err != nil {
		return err
	}
	defer shutdownLogRuntime(logRuntime)

	hmacKey, err := parseHMACKey(c.String("hmac-key"))
	if err != nil {
		return err
	}

	repoPath, err := resolveSharedRepoPath(c)
	if err != nil {
		return err
	}

	cfg := AppConfig{
		DBPath:         dbPath,
		RepoPath:       repoPath,
		BotSeed:        botSeed,
		HMACKey:        hmacKey,
		AppKey:         c.String("app-key"),
		PublishWorkers: c.Int("publish-workers"),
		PLCURL:         c.String("plc-url"),
		AtprotoInsecure: c.Bool("atproto-insecure"),
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
	var ssbStatus handlers.SSBStatusProvider
	var blobStore handlers.BlobStore
	if repo := c.String("repo-path"); repo != "" {
		ssbRuntime, err := ssbruntime.Open(c.Context, ssbruntime.Config{
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
	r := chi.NewRouter()
	r.Use(websecurity.RequestLogMiddleware(uiLogger))
	r.Use(websecurity.SecurityHeadersMiddleware(true))
	if authConfigured {
		r.Use(websecurity.BasicAuthMiddleware(authUser, authPass))
	}

	ui := handlers.NewUIHandler(database, uiLogger, atpClient, blobStore, ssbStatus)
	ui.Mount(r)

	server := &http.Server{
		Addr:    listenAddr,
		Handler: r,
	}

	ctx, stop := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	fmt.Printf("Serving UI at http://%s (auth=%t)\n", listenAddr, authConfigured)
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
