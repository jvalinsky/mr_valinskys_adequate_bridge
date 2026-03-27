// Bridge-cli manages and runs the ATProto-to-SSB bridge.
//
// It provides account management, runtime orchestration, backfill, retry, and admin UI commands.
package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/xrpc"
	"github.com/go-chi/chi/v5"
	"github.com/mr_valinskys_adequate_bridge/internal/backfill"
	"github.com/mr_valinskys_adequate_bridge/internal/blobbridge"
	"github.com/mr_valinskys_adequate_bridge/internal/bots"
	"github.com/mr_valinskys_adequate_bridge/internal/bridge"
	"github.com/mr_valinskys_adequate_bridge/internal/db"
	"github.com/mr_valinskys_adequate_bridge/internal/firehose"
	"github.com/mr_valinskys_adequate_bridge/internal/publishqueue"
	"github.com/mr_valinskys_adequate_bridge/internal/room"
	"github.com/mr_valinskys_adequate_bridge/internal/ssbruntime"
	"github.com/mr_valinskys_adequate_bridge/internal/web/handlers"
	websecurity "github.com/mr_valinskys_adequate_bridge/internal/web/security"
	"github.com/urfave/cli/v2"
)

var (
	dbPath   string
	relayURL string
	botSeed  string
)

const (
	bridgeRuntimeStatusKey        = "bridge_runtime_status"
	bridgeRuntimeStartedAtKey     = "bridge_runtime_started_at"
	bridgeRuntimeLastHeartbeatKey = "bridge_runtime_last_heartbeat_at"
	bridgeRuntimeStoppingAtKey    = "bridge_runtime_stopping_at"
	bridgeRuntimeStoppedAtKey     = "bridge_runtime_stopped_at"
	bridgeRuntimeLastErrorKey     = "bridge_runtime_last_error"
)

func main() {
	app := &cli.App{
		Name:  "bridge-cli",
		Usage: "Manage the ATProto to SSB bridge",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "db",
				Value:       "bridge.sqlite",
				Usage:       "path to the sqlite database",
				Destination: &dbPath,
			},
			&cli.StringFlag{
				Name:        "relay-url",
				Value:       "wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos",
				Usage:       "ATProto subscribeRepos endpoint",
				Destination: &relayURL,
			},
			&cli.StringFlag{
				Name:        "bot-seed",
				Value:       "dev-insecure-seed-change-me",
				EnvVars:     []string{"BRIDGE_BOT_SEED"},
				Usage:       "seed used for deterministic AT DID -> SSB feed derivation",
				Destination: &botSeed,
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "account",
				Usage: "Manage bridged accounts",
				Subcommands: []*cli.Command{
					{
						Name:  "list",
						Usage: "List all bridged accounts",
						Action: func(c *cli.Context) error {
							database, err := db.Open(dbPath)
							if err != nil {
								return err
							}
							defer database.Close()

							accounts, err := database.GetAllBridgedAccounts(c.Context)
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
						},
					},
					{
						Name:      "add",
						Usage:     "Add a new bridged account",
						ArgsUsage: "<did>",
						Action: func(c *cli.Context) error {
							did := c.Args().First()
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

							if err := database.AddBridgedAccount(c.Context, acc); err != nil {
								return err
							}

							fmt.Printf("Added account %s\n", did)
							return nil
						},
					},
					{
						Name:      "remove",
						Usage:     "Deactivate a bridged account",
						ArgsUsage: "<did>",
						Action: func(c *cli.Context) error {
							did := c.Args().First()
							if did == "" {
								return fmt.Errorf("must provide a DID")
							}

							database, err := db.Open(dbPath)
							if err != nil {
								return err
							}
							defer database.Close()

							acc, err := database.GetBridgedAccount(c.Context, did)
							if err != nil {
								return err
							}
							if acc == nil {
								return fmt.Errorf("account not found")
							}

							acc.Active = false
							if err := database.AddBridgedAccount(c.Context, *acc); err != nil {
								return err
							}

							fmt.Printf("Deactivated account %s\n", did)
							return nil
						},
					},
				},
			},
			{
				Name:  "stats",
				Usage: "Show bridge statistics",
				Action: func(c *cli.Context) error {
					database, err := db.Open(dbPath)
					if err != nil {
						return err
					}
					defer database.Close()

					totalAccounts, err := database.CountBridgedAccounts(c.Context)
					if err != nil {
						return err
					}

					activeAccounts, err := database.CountActiveBridgedAccounts(c.Context)
					if err != nil {
						return err
					}

					totalMessages, err := database.CountMessages(c.Context)
					if err != nil {
						return err
					}

					publishedMessages, err := database.CountPublishedMessages(c.Context)
					if err != nil {
						return err
					}

					publishFailures, err := database.CountPublishFailures(c.Context)
					if err != nil {
						return err
					}
					deferredMessages, err := database.CountDeferredMessages(c.Context)
					if err != nil {
						return err
					}
					deletedMessages, err := database.CountDeletedMessages(c.Context)
					if err != nil {
						return err
					}

					totalBlobs, err := database.CountBlobs(c.Context)
					if err != nil {
						return err
					}

					cursorVal, _, err := database.GetBridgeState(c.Context, "firehose_seq")
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
				},
			},
			{
				Name:  "start",
				Usage: "Start the bridge engine",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "repo-path",
						Usage: "shared SSB repo path for bridge publishing and embedded room runtime (default .ssb-bridge)",
					},
					&cli.StringFlag{
						Name:  "ssb-repo-path",
						Usage: "deprecated: legacy bridge repo path alias (use --repo-path)",
					},
					&cli.StringFlag{
						Name:  "hmac-key",
						Usage: "optional 32-byte HMAC key (base64, hex, or raw) for SSB message signing",
					},
					&cli.IntFlag{
						Name:  "publish-workers",
						Value: 1,
						Usage: "publish worker count (default 1 keeps deterministic ordering)",
					},
					&cli.BoolFlag{
						Name:  "room-enable",
						Value: true,
						Usage: "start embedded room runtime alongside bridge processor",
					},
					&cli.StringFlag{
						Name:  "room-listen-addr",
						Value: "127.0.0.1:8989",
						Usage: "Room2 muxrpc listen address",
					},
					&cli.StringFlag{
						Name:  "room-http-listen-addr",
						Value: "127.0.0.1:8976",
						Usage: "Room2 HTTP interface listen address",
					},
					&cli.StringFlag{
						Name:  "room-repo-path",
						Usage: "deprecated: legacy room repo path alias (use --repo-path)",
					},
					&cli.StringFlag{
						Name:  "room-mode",
						Value: "community",
						Usage: "Room2 mode: open|community|restricted",
					},
					&cli.StringFlag{
						Name:  "room-https-domain",
						Usage: "Room2 HTTPS domain (required for non-loopback room exposure)",
					},
					&cli.StringFlag{
						Name:  "xrpc-host",
						Usage: "optional ATProto XRPC host for blob fetches (derived from relay-url when omitted)",
					},
				},
				Action: func(c *cli.Context) error {
					database, err := db.Open(dbPath)
					if err != nil {
						return err
					}
					defer database.Close()

					bridgeLogger := log.New(os.Stdout, "bridge: ", log.LstdFlags)
					firehoseLogger := log.New(os.Stdout, "firehose: ", log.LstdFlags)
					roomLogger := log.New(os.Stdout, "room: ", log.LstdFlags)

					hmacKey, err := parseHMACKey(c.String("hmac-key"))
					if err != nil {
						return err
					}

					repoPath, err := resolveSharedRepoPath(c)
					if err != nil {
						return err
					}

					ssbRuntime, err := ssbruntime.Open(repoPath, []byte(botSeed), hmacKey, bridgeLogger)
					if err != nil {
						return fmt.Errorf("init ssb runtime: %w", err)
					}

					xrpcHost, err := resolveXRPCHost(c.String("xrpc-host"), relayURL)
					if err != nil {
						_ = ssbRuntime.Close()
						return err
					}
					xrpcClient := &xrpc.Client{Host: xrpcHost}

					workerPublisher := publishqueue.New(ssbRuntime, c.Int("publish-workers"), bridgeLogger)
					defer workerPublisher.Close()

					blobBridge := blobbridge.New(database, ssbRuntime.BlobStore(), xrpcClient, bridgeLogger)
					recordFetcher := bridge.NewXRPCRecordFetcher(xrpcClient)
					var processor *bridge.Processor
					dependencyResolver := bridge.NewATProtoDependencyResolver(
						database,
						bridgeLogger,
						recordFetcher,
						func(ctx context.Context, atDID, atURI, atCID, collection string, recordJSON []byte) error {
							return processor.ProcessRecord(ctx, atDID, atURI, atCID, collection, recordJSON)
						},
					)
					processor = bridge.NewProcessor(
						database,
						bridgeLogger,
						bridge.WithPublisher(workerPublisher),
						bridge.WithBlobBridge(blobBridge),
						bridge.WithDependencyResolver(dependencyResolver),
						bridge.WithFeedResolver(ssbRuntime),
					)

					firehoseOpts := []firehose.ClientOption{}
					if cursor, ok, err := readFirehoseCursor(c.Context, database); err != nil {
						_ = ssbRuntime.Close()
						return err
					} else if ok {
						firehoseOpts = append(firehoseOpts, firehose.WithCursor(cursor))
						bridgeLogger.Printf("event=cursor_resume seq=%d", cursor)
					}

					ctx, stop := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
					defer stop()
					runCtx, cancelRun := context.WithCancel(ctx)
					defer cancelRun()
					startedAt := time.Now().UTC()
					setBridgeStateBestEffort(runCtx, database, bridgeRuntimeStatusKey, "starting", bridgeLogger)
					setBridgeStateBestEffort(runCtx, database, bridgeRuntimeStartedAtKey, startedAt.Format(time.RFC3339), bridgeLogger)
					setBridgeStateBestEffort(runCtx, database, bridgeRuntimeLastHeartbeatKey, startedAt.Format(time.RFC3339), bridgeLogger)
					setBridgeStateBestEffort(runCtx, database, bridgeRuntimeLastErrorKey, "", bridgeLogger)

					var roomRuntime *room.Runtime
					if c.Bool("room-enable") {
						roomRuntime, err = room.Start(runCtx, room.Config{
							ListenAddr:     c.String("room-listen-addr"),
							HTTPListenAddr: c.String("room-http-listen-addr"),
							RepoPath:       repoPath,
							Mode:           c.String("room-mode"),
							HTTPSDomain:    c.String("room-https-domain"),
						}, roomLogger)
						if err != nil {
							_ = ssbRuntime.Close()
							return fmt.Errorf("start room runtime: %w", err)
						}
						bridgeLogger.Printf(
							"event=room_enabled muxrpc_addr=%s http_addr=%s mode=%s",
							roomRuntime.Addr(),
							roomRuntime.HTTPAddr(),
							strings.ToLower(c.String("room-mode")),
						)
					}

					client := firehose.NewClient(relayURL, processor, firehoseLogger, firehoseOpts...)
					errCh := make(chan error, 1)
					go func() {
						errCh <- client.RunWithReconnect(runCtx, firehose.ReconnectConfig{
							InitialBackoff: 2 * time.Second,
							MaxBackoff:     60 * time.Second,
							Jitter:         750 * time.Millisecond,
						})
					}()

					go runRetryScheduler(runCtx, processor, bridgeLogger)
					go runDeferredResolverScheduler(runCtx, processor, bridgeLogger)
					go runRuntimeHeartbeatScheduler(runCtx, database, bridgeLogger, 10*time.Second)
					setBridgeStateBestEffort(runCtx, database, bridgeRuntimeStatusKey, "live", bridgeLogger)
					setBridgeStateBestEffort(runCtx, database, bridgeRuntimeLastHeartbeatKey, time.Now().UTC().Format(time.RFC3339), bridgeLogger)

					fmt.Println("Starting bridge engine...")
					var runErr error
					firehoseDone := false
					select {
					case <-ctx.Done():
						setBridgeStateBestEffort(context.Background(), database, bridgeRuntimeStatusKey, "stopping", bridgeLogger)
						setBridgeStateBestEffort(context.Background(), database, bridgeRuntimeStoppingAtKey, time.Now().UTC().Format(time.RFC3339), bridgeLogger)
						cancelRun()
					case err := <-errCh:
						firehoseDone = true
						if err != nil && !errors.Is(err, context.Canceled) {
							runErr = err
						}
						setBridgeStateBestEffort(context.Background(), database, bridgeRuntimeStatusKey, "stopping", bridgeLogger)
						setBridgeStateBestEffort(context.Background(), database, bridgeRuntimeStoppingAtKey, time.Now().UTC().Format(time.RFC3339), bridgeLogger)
						cancelRun()
					}

					if !firehoseDone {
						select {
						case err := <-errCh:
							if err != nil && !errors.Is(err, context.Canceled) && runErr == nil {
								runErr = err
							}
						case <-time.After(5 * time.Second):
							bridgeLogger.Printf("event=firehose_shutdown_timeout timeout=5s")
						}
					}

					var shutdownErr error
					if roomRuntime != nil {
						if err := roomRuntime.Close(); err != nil {
							shutdownErr = errors.Join(shutdownErr, fmt.Errorf("shutdown room runtime: %w", err))
						}
					}
					if err := ssbRuntime.Close(); err != nil {
						shutdownErr = errors.Join(shutdownErr, fmt.Errorf("close ssb runtime: %w", err))
					}
					setBridgeStateBestEffort(context.Background(), database, bridgeRuntimeStatusKey, "stopped", bridgeLogger)
					setBridgeStateBestEffort(context.Background(), database, bridgeRuntimeStoppedAtKey, time.Now().UTC().Format(time.RFC3339), bridgeLogger)
					if err := errors.Join(runErr, shutdownErr); err != nil {
						setBridgeStateBestEffort(context.Background(), database, bridgeRuntimeLastErrorKey, err.Error(), bridgeLogger)
					}

					return errors.Join(runErr, shutdownErr)
				},
			},
			{
				Name:  "backfill",
				Usage: "Backfill supported records for one or more DIDs using sync.getRepo",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "repo-path",
						Usage: "shared SSB repo path for publish/backfill (default .ssb-bridge)",
					},
					&cli.StringSliceFlag{
						Name:  "did",
						Usage: "DID to backfill (repeatable)",
					},
					&cli.BoolFlag{
						Name:  "active-accounts",
						Usage: "backfill all currently active bridged accounts from the local DB",
					},
					&cli.StringFlag{
						Name:  "since",
						Usage: "timestamp or sequence marker for filtering (timestamp filtering is applied when available)",
					},
					&cli.StringFlag{
						Name:  "xrpc-host",
						Usage: "optional ATProto XRPC host (derived from relay-url when omitted)",
					},
					&cli.StringFlag{
						Name:  "ssb-repo-path",
						Usage: "deprecated: legacy bridge repo path alias (use --repo-path)",
					},
					&cli.StringFlag{
						Name:  "hmac-key",
						Usage: "optional 32-byte HMAC key (base64, hex, or raw) for SSB message signing",
					},
					&cli.IntFlag{
						Name:  "publish-workers",
						Value: 1,
						Usage: "publish worker count (default 1 keeps deterministic ordering)",
					},
				},
				Action: func(c *cli.Context) error {
					database, err := db.Open(dbPath)
					if err != nil {
						return err
					}
					defer database.Close()

					bridgeLogger := log.New(os.Stdout, "bridge: ", log.LstdFlags)

					dids := append([]string{}, c.StringSlice("did")...)
					if c.Bool("active-accounts") {
						accounts, err := database.GetAllBridgedAccounts(c.Context)
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

					hmacKey, err := parseHMACKey(c.String("hmac-key"))
					if err != nil {
						return err
					}

					repoPath, err := resolveSharedRepoPath(c)
					if err != nil {
						return err
					}

					ssbRuntime, err := ssbruntime.Open(repoPath, []byte(botSeed), hmacKey, bridgeLogger)
					if err != nil {
						return fmt.Errorf("init ssb runtime: %w", err)
					}
					defer ssbRuntime.Close()

					xrpcHost, err := resolveXRPCHost(c.String("xrpc-host"), relayURL)
					if err != nil {
						return err
					}
					xrpcClient := &xrpc.Client{Host: xrpcHost}

					workerPublisher := publishqueue.New(ssbRuntime, c.Int("publish-workers"), bridgeLogger)
					defer workerPublisher.Close()

					blobBridge := blobbridge.New(database, ssbRuntime.BlobStore(), xrpcClient, bridgeLogger)
					recordFetcher := bridge.NewXRPCRecordFetcher(xrpcClient)
					var processor *bridge.Processor
					dependencyResolver := bridge.NewATProtoDependencyResolver(
						database,
						bridgeLogger,
						recordFetcher,
						func(ctx context.Context, atDID, atURI, atCID, collection string, recordJSON []byte) error {
							return processor.ProcessRecord(ctx, atDID, atURI, atCID, collection, recordJSON)
						},
					)
					processor = bridge.NewProcessor(
						database,
						bridgeLogger,
						bridge.WithPublisher(workerPublisher),
						bridge.WithBlobBridge(blobBridge),
						bridge.WithDependencyResolver(dependencyResolver),
						bridge.WithFeedResolver(ssbRuntime),
					)

					sinceFilter, err := backfill.ParseSince(c.String("since"))
					if err != nil {
						return err
					}

					total := backfill.Stats{}
					for _, did := range dids {
						stats, err := backfill.RunForDID(c.Context, xrpcClient, did, sinceFilter, processor, bridgeLogger)
						if err != nil {
							return err
						}
						total.Processed += stats.Processed
						total.Skipped += stats.Skipped
						total.Errors += stats.Errors
					}

					fmt.Printf("Backfill complete: dids=%d processed=%d skipped=%d errors=%d\n", len(dids), total.Processed, total.Skipped, total.Errors)
					return nil
				},
			},
			{
				Name:  "retry-failures",
				Usage: "Retry failed unpublished bridge messages",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "repo-path",
						Usage: "shared SSB repo path for publish/retry (default .ssb-bridge)",
					},
					&cli.IntFlag{
						Name:  "limit",
						Value: 100,
						Usage: "maximum number of candidate failures to inspect",
					},
					&cli.StringFlag{
						Name:  "did",
						Usage: "optional DID filter for retries",
					},
					&cli.IntFlag{
						Name:  "max-attempts",
						Value: 8,
						Usage: "maximum publish attempts before a record is excluded from retries",
					},
					&cli.DurationFlag{
						Name:  "base-backoff",
						Value: 5 * time.Second,
						Usage: "base retry backoff duration (doubles per attempt)",
					},
					&cli.StringFlag{
						Name:  "ssb-repo-path",
						Usage: "deprecated: legacy bridge repo path alias (use --repo-path)",
					},
					&cli.StringFlag{
						Name:  "hmac-key",
						Usage: "optional 32-byte HMAC key (base64, hex, or raw) for SSB message signing",
					},
					&cli.IntFlag{
						Name:  "publish-workers",
						Value: 1,
						Usage: "publish worker count",
					},
				},
				Action: func(c *cli.Context) error {
					database, err := db.Open(dbPath)
					if err != nil {
						return err
					}
					defer database.Close()

					bridgeLogger := log.New(os.Stdout, "bridge: ", log.LstdFlags)

					hmacKey, err := parseHMACKey(c.String("hmac-key"))
					if err != nil {
						return err
					}

					repoPath, err := resolveSharedRepoPath(c)
					if err != nil {
						return err
					}

					ssbRuntime, err := ssbruntime.Open(repoPath, []byte(botSeed), hmacKey, bridgeLogger)
					if err != nil {
						return fmt.Errorf("init ssb runtime: %w", err)
					}
					defer ssbRuntime.Close()

					workerPublisher := publishqueue.New(ssbRuntime, c.Int("publish-workers"), bridgeLogger)
					defer workerPublisher.Close()

					processor := bridge.NewProcessor(
						database,
						bridgeLogger,
						bridge.WithPublisher(workerPublisher),
					)

					result, err := processor.RetryFailedMessages(c.Context, bridge.RetryConfig{
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
				},
			},
			{
				Name:  "serve-ui",
				Usage: "Run the bridge admin web UI",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "listen-addr",
						Value: "127.0.0.1:8080",
						Usage: "listen address for the web admin UI",
					},
					&cli.StringFlag{
						Name:  "ui-auth-user",
						Usage: "HTTP Basic auth username for the admin UI",
					},
					&cli.StringFlag{
						Name:  "ui-auth-pass-env",
						Usage: "environment variable containing HTTP Basic auth password for the admin UI",
					},
				},
				Action: func(c *cli.Context) error {
					database, err := db.Open(dbPath)
					if err != nil {
						return err
					}
					defer database.Close()

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

					uiLogger := log.New(os.Stdout, "ui: ", log.LstdFlags)
					r := chi.NewRouter()
					r.Use(websecurity.RequestLogMiddleware(uiLogger))
					if authConfigured {
						r.Use(websecurity.BasicAuthMiddleware(authUser, authPass))
					}

					ui := handlers.NewUIHandler(database)
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
				},
			},
		},
	}

	if err := app.RunContext(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

// parseHMACKey parses a 32-byte key from base64, hex, or raw input.
func parseHMACKey(raw string) (*[32]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	decoders := []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		hex.DecodeString,
	}

	for _, decode := range decoders {
		b, err := decode(raw)
		if err != nil {
			continue
		}
		if len(b) == 32 {
			var key [32]byte
			copy(key[:], b)
			return &key, nil
		}
	}

	if len(raw) == 32 {
		var key [32]byte
		copy(key[:], []byte(raw))
		return &key, nil
	}

	return nil, fmt.Errorf("hmac key must decode to 32 bytes")
}

// resolveXRPCHost resolves the XRPC host from an explicit value or relay URL.
func resolveXRPCHost(explicitHost, relay string) (string, error) {
	if explicitHost != "" {
		return strings.TrimRight(explicitHost, "/"), nil
	}

	if relay == "" {
		relay = "wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos"
	}
	u, err := url.Parse(relay)
	if err != nil {
		return "", fmt.Errorf("parse relay URL %q: %w", relay, err)
	}

	scheme := "https"
	switch u.Scheme {
	case "ws":
		scheme = "http"
	case "wss":
		scheme = "https"
	case "http", "https":
		scheme = u.Scheme
	}
	if u.Host == "" {
		return "", fmt.Errorf("relay URL missing host: %q", relay)
	}
	return fmt.Sprintf("%s://%s", scheme, u.Host), nil
}

// readFirehoseCursor reads and parses the persisted firehose cursor sequence.
func readFirehoseCursor(ctx context.Context, database *db.DB) (int64, bool, error) {
	value, ok, err := database.GetBridgeState(ctx, "firehose_seq")
	if err != nil || !ok || strings.TrimSpace(value) == "" {
		return 0, ok, err
	}
	seq, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse firehose_seq state %q: %w", value, err)
	}
	return seq, true, nil
}

// dedupeStrings trims values, drops empties, and preserves first-seen order.
func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func resolveSharedRepoPath(c *cli.Context) (string, error) {
	const defaultRepoPath = ".ssb-bridge"

	repoPath := strings.TrimSpace(c.String("repo-path"))
	repoPathSet := c.IsSet("repo-path")

	legacyValues := make([]string, 0, 2)
	if c.IsSet("ssb-repo-path") {
		legacyValues = append(legacyValues, strings.TrimSpace(c.String("ssb-repo-path")))
	}
	if c.IsSet("room-repo-path") {
		legacyValues = append(legacyValues, strings.TrimSpace(c.String("room-repo-path")))
	}

	legacyValues = dedupeStrings(legacyValues)
	switch {
	case repoPathSet:
		for _, legacy := range legacyValues {
			if legacy != "" && legacy != repoPath {
				return "", fmt.Errorf("conflicting repo flags: --repo-path=%q conflicts with legacy repo path %q; use --repo-path only", repoPath, legacy)
			}
		}
	case len(legacyValues) > 0:
		repoPath = legacyValues[0]
		if len(legacyValues) > 1 {
			return "", fmt.Errorf("conflicting legacy repo flags: %q vs %q; use a single --repo-path value", legacyValues[0], legacyValues[1])
		}
	default:
		repoPath = defaultRepoPath
	}

	if strings.TrimSpace(repoPath) == "" {
		return "", fmt.Errorf("repo path must not be empty")
	}
	return repoPath, nil
}

func runRuntimeHeartbeatScheduler(ctx context.Context, database *db.DB, logger *log.Logger, interval time.Duration) {
	if database == nil {
		return
	}
	if logger == nil {
		logger = log.New(os.Stdout, "bridge: ", log.LstdFlags)
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().UTC().Format(time.RFC3339)
			setBridgeStateBestEffort(ctx, database, bridgeRuntimeStatusKey, "live", logger)
			setBridgeStateBestEffort(ctx, database, bridgeRuntimeLastHeartbeatKey, now, logger)
		}
	}
}

func setBridgeStateBestEffort(ctx context.Context, database *db.DB, key, value string, logger *log.Logger) {
	if database == nil || strings.TrimSpace(key) == "" {
		return
	}
	if logger == nil {
		logger = log.New(os.Stdout, "bridge: ", log.LstdFlags)
	}
	if err := database.SetBridgeState(ctx, key, value); err != nil {
		logger.Printf("event=bridge_state_persist_error key=%s err=%v", key, err)
	}
}

// runRetryScheduler periodically retries failed unpublished messages.
func runRetryScheduler(ctx context.Context, processor *bridge.Processor, logger *log.Logger) {
	if processor == nil {
		return
	}
	if logger == nil {
		logger = log.New(os.Stdout, "bridge: ", log.LstdFlags)
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := processor.RetryFailedMessages(ctx, bridge.RetryConfig{
				Limit:       100,
				MaxAttempts: 8,
				BaseBackoff: 5 * time.Second,
			})
			if err != nil {
				logger.Printf("event=retry_scheduler_error err=%v", err)
				continue
			}
			if result.Attempted > 0 || result.Deferred > 0 || result.Failed > 0 {
				logger.Printf(
					"event=retry_scheduler selected=%d attempted=%d published=%d failed=%d deferred=%d",
					result.Selected,
					result.Attempted,
					result.Published,
					result.Failed,
					result.Deferred,
				)
			}
		}
	}
}

func runDeferredResolverScheduler(ctx context.Context, processor *bridge.Processor, logger *log.Logger) {
	if processor == nil {
		return
	}
	if logger == nil {
		logger = log.New(os.Stdout, "bridge: ", log.LstdFlags)
	}

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := processor.ResolveDeferredMessages(ctx, 100)
			if err != nil {
				logger.Printf("event=deferred_scheduler_error err=%v", err)
				continue
			}
			if result.Attempted > 0 || result.Deferred > 0 || result.Failed > 0 || result.Published > 0 {
				logger.Printf(
					"event=deferred_scheduler selected=%d attempted=%d published=%d deferred=%d failed=%d",
					result.Selected,
					result.Attempted,
					result.Published,
					result.Deferred,
					result.Failed,
				)
			}
		}
	}
}
