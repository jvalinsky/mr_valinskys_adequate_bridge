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
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/xrpc"
	"github.com/go-chi/chi/v5"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/backfill"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/blobbridge"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bots"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bridge"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/firehose"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/publishqueue"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssbruntime"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/handlers"
	websecurity "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/security"
	"github.com/urfave/cli/v2"
)

var (
	dbPath         string
	relayURL       string
	botSeed        string
	otelEndpoint   string
	otelProtocol   string
	otelInsecure   bool
	otelService    string
	localLogOutput string
)

const (
	defaultRelayURL               = "wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos"
	defaultLiveReadXRPCHost       = "https://public.api.bsky.app"
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
				Value:       defaultRelayURL,
				Usage:       "ATProto subscribeRepos endpoint (firehose only)",
				Destination: &relayURL,
			},
			&cli.StringFlag{
				Name:        "bot-seed",
				Value:       "dev-insecure-seed-change-me",
				EnvVars:     []string{"BRIDGE_BOT_SEED"},
				Usage:       "seed used for deterministic AT DID -> SSB feed derivation",
				Destination: &botSeed,
			},
			&cli.StringFlag{
				Name:        "otel-logs-endpoint",
				Usage:       "OTLP logs endpoint; empty disables OTLP log export",
				Destination: &otelEndpoint,
			},
			&cli.StringFlag{
				Name:        "otel-logs-protocol",
				Value:       "grpc",
				Usage:       "OTLP logs protocol: grpc|http",
				Destination: &otelProtocol,
			},
			&cli.BoolFlag{
				Name:        "otel-logs-insecure",
				Usage:       "disable OTLP transport security for log export",
				Destination: &otelInsecure,
			},
			&cli.StringFlag{
				Name:        "otel-service-name",
				Usage:       "override OTel service.name resource attribute",
				Destination: &otelService,
			},
			&cli.StringFlag{
				Name:        "local-log-output",
				Value:       "text",
				Usage:       "local log output mode: text|none",
				Destination: &localLogOutput,
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
					&cli.StringFlag{
						Name:  "ssb-listen-addr",
						Value: ":8008",
						Usage: "SSB MUXRPC listen address for the bridge's internal sbot daemon",
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
						Usage: "optional ATProto read host for dependency record and blob fetches (defaults to AppView)",
					},
					&cli.StringFlag{
						Name:  "app-key",
						Usage: "SSB network identifier (hex or name; empty = standard SSB key)",
					},
					&cli.BoolFlag{
						Name:  "firehose-enable",
						Value: true,
						Usage: "enable ATProto firehose subscribeRepos ingestion loop",
					},
				},
				Action: func(c *cli.Context) error {
					logRuntime, err := newBridgeLogRuntime(c, "bridge-cli")
					if err != nil {
						return err
					}
					defer shutdownLogRuntime(logRuntime)

					database, err := db.Open(dbPath)
					if err != nil {
						return err
					}
					defer database.Close()

					bridgeLogger := logRuntime.Logger("bridge")
					firehoseLogger := logRuntime.Logger("firehose")
					roomLogger := logRuntime.Logger("room")

					hmacKey, err := parseHMACKey(c.String("hmac-key"))
					if err != nil {
						return err
					}

					repoPath, err := resolveSharedRepoPath(c)
					if err != nil {
						return err
					}

					ssbRuntime, err := ssbruntime.Open(c.Context, ssbruntime.Config{
						RepoPath:   repoPath,
						ListenAddr: c.String("ssb-listen-addr"),
						MasterSeed: []byte(botSeed),
						HMACKey:    hmacKey,
						AppKey:     c.String("app-key"),
					}, bridgeLogger)
					if err != nil {
						return fmt.Errorf("init ssb runtime: %w", err)
					}

					xrpcHost, err := resolveLiveXRPCHost(c.String("xrpc-host"))
					if err != nil {
						_ = ssbRuntime.Close()
						return err
					}
					xrpcClient := &xrpc.Client{Host: xrpcHost}

					workerPublisher := publishqueue.New(ssbRuntime, c.Int("publish-workers"), bridgeLogger)
					defer workerPublisher.Close()

					blobHostResolver, err := resolveLiveBlobHostResolver(c.String("xrpc-host"), c.IsSet("xrpc-host"))
					if err != nil {
						_ = ssbRuntime.Close()
						return err
					}

					blobBridge := blobbridge.NewWithResolver(database, ssbRuntime.BlobStore(), blobHostResolver, nil, bridgeLogger)
					pdsResolver := backfill.DIDPDSResolver{PLCURL: "https://plc.directory"}
					recordFetcher := bridge.NewPDSAwareRecordFetcher(pdsResolver, xrpcClient)
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

					firehoseEnabled := c.Bool("firehose-enable")
					firehoseOpts := []firehose.ClientOption{}
					if firehoseEnabled {
						if cursor, ok, err := readFirehoseCursor(c.Context, database); err != nil {
							_ = ssbRuntime.Close()
							return err
						} else if ok {
							firehoseOpts = append(firehoseOpts, firehose.WithCursor(cursor))
							bridgeLogger.Printf("event=cursor_resume seq=%d", cursor)
						}
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
							ListenAddr:            c.String("room-listen-addr"),
							HTTPListenAddr:        c.String("room-http-listen-addr"),
							RepoPath:              filepath.Join(repoPath, "room"),
							Mode:                  c.String("room-mode"),
							HTTPSDomain:           c.String("room-https-domain"),
							AppKey:                c.String("app-key"),
							BridgeAccountLister:   database,
							BridgeAccountDetailer: database,
							HandlerMux:            ssbRuntime.Node().HandlerMux(),
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

						go runRoomTunnelBootstrap(runCtx, ssbRuntime, roomRuntime, bridgeLogger)
					}

					var errCh <-chan error
					if firehoseEnabled {
						firehoseLogger.Printf("event=firehose_enabled relay_url=%s", relayURL)
						firehoseErrCh := make(chan error, 1)
						firehoseOpts = append(firehoseOpts, firehose.WithConnectedCallback(func() {
							setBridgeStateBestEffort(runCtx, database, "firehose_connected", "1", bridgeLogger)
						}))
						client := firehose.NewClient(relayURL, processor, firehoseLogger, firehoseOpts...)
						go func() {
							firehoseErrCh <- client.RunWithReconnect(runCtx, firehose.ReconnectConfig{
								InitialBackoff: 2 * time.Second,
								MaxBackoff:     60 * time.Second,
								Jitter:         750 * time.Millisecond,
							})
						}()
						errCh = firehoseErrCh
					} else {
						bridgeLogger.Printf("event=firehose_disabled")
					}

					go runRetryScheduler(runCtx, processor, bridgeLogger)
					go runDeferredResolverScheduler(runCtx, processor, bridgeLogger)
					go runRuntimeHeartbeatScheduler(runCtx, database, bridgeLogger, 10*time.Second)
					setBridgeStateBestEffort(runCtx, database, bridgeRuntimeStatusKey, "live", bridgeLogger)
					setBridgeStateBestEffort(runCtx, database, bridgeRuntimeLastHeartbeatKey, time.Now().UTC().Format(time.RFC3339), bridgeLogger)

					fmt.Println("Starting bridge engine...")
					var runErr error
					firehoseDone := !firehoseEnabled
					if firehoseEnabled {
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
					} else {
						<-ctx.Done()
						setBridgeStateBestEffort(context.Background(), database, bridgeRuntimeStatusKey, "stopping", bridgeLogger)
						setBridgeStateBestEffort(context.Background(), database, bridgeRuntimeStoppingAtKey, time.Now().UTC().Format(time.RFC3339), bridgeLogger)
						cancelRun()
					}

					if firehoseEnabled && !firehoseDone {
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
						Usage: "optional fixed PDS host override for sync.getRepo (mainly for local/test stacks)",
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
					logRuntime, err := newBridgeLogRuntime(c, "bridge-cli")
					if err != nil {
						return err
					}
					defer shutdownLogRuntime(logRuntime)

					database, err := db.Open(dbPath)
					if err != nil {
						return err
					}
					defer database.Close()

					bridgeLogger := logRuntime.Logger("bridge")

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

					ssbRuntime, err := ssbruntime.Open(c.Context, ssbruntime.Config{
						RepoPath:   repoPath,
						MasterSeed: []byte(botSeed),
						HMACKey:    hmacKey,
					}, bridgeLogger)
					if err != nil {
						return fmt.Errorf("init ssb runtime: %w", err)
					}
					defer ssbRuntime.Close()

					workerPublisher := publishqueue.New(ssbRuntime, c.Int("publish-workers"), bridgeLogger)
					defer workerPublisher.Close()

					liveReadHost, err := resolveLiveXRPCHost(c.String("xrpc-host"))
					if err != nil {
						return err
					}
					liveReadClient := &xrpc.Client{Host: liveReadHost}

					blobHostResolver, err := resolveLiveBlobHostResolver(c.String("xrpc-host"), c.IsSet("xrpc-host"))
					if err != nil {
						return err
					}

					blobBridge := blobbridge.NewWithResolver(database, ssbRuntime.BlobStore(), blobHostResolver, nil, bridgeLogger)
					retryPDSResolver := backfill.DIDPDSResolver{PLCURL: "https://plc.directory"}
					recordFetcher := bridge.NewPDSAwareRecordFetcher(retryPDSResolver, liveReadClient)
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

					hostResolver, err := resolveBackfillHostResolver(c.String("xrpc-host"))
					if err != nil {
						return err
					}
					repoFetcher := backfill.XRPCRepoFetcher{}

					total := backfill.Stats{}
					statusCounts := map[backfill.DIDStatus]int{}
					for _, did := range dids {
						result := backfill.RunForDID(c.Context, did, sinceFilter, processor, bridgeLogger, hostResolver, repoFetcher)
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
					logRuntime, err := newBridgeLogRuntime(c, "bridge-cli")
					if err != nil {
						return err
					}
					defer shutdownLogRuntime(logRuntime)

					database, err := db.Open(dbPath)
					if err != nil {
						return err
					}
					defer database.Close()

					bridgeLogger := logRuntime.Logger("bridge")

					hmacKey, err := parseHMACKey(c.String("hmac-key"))
					if err != nil {
						return err
					}

					repoPath, err := resolveSharedRepoPath(c)
					if err != nil {
						return err
					}

					ssbRuntime, err := ssbruntime.Open(c.Context, ssbruntime.Config{
						RepoPath:   repoPath,
						MasterSeed: []byte(botSeed),
						HMACKey:    hmacKey,
					}, bridgeLogger)
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
					&cli.StringFlag{
						Name:  "pds-host",
						Usage: "optional PDS host for manual posting (defaults to live AppView)",
					},
					&cli.StringFlag{
						Name:    "pds-password",
						Usage:   "password for PDS accounts when manual posting",
						EnvVars: []string{"BRIDGE_PDS_PASSWORD"},
					},
					&cli.StringFlag{
						Name:  "repo-path",
						Usage: "path to SSB repo for blob viewing",
					},
				},
				Action: func(c *cli.Context) error {
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

					var atpClient *handlers.PDSClient
					if c.String("pds-host") != "" && c.String("pds-password") != "" {
						host, err := resolveLiveXRPCHost(c.String("pds-host"))
						if err != nil {
							return err
						}
						atpClient = &handlers.PDSClient{
							Host:     host,
							Password: c.String("pds-password"),
						}
					}

					var blobStore handlers.BlobStore
					if c.String("repo-path") != "" {
						// We only need the blob store part of the runtime.
						// For now, let's open the full runtime as it's easier.
						ssbRuntime, err := ssbruntime.Open(c.Context, ssbruntime.Config{
							RepoPath:   c.String("repo-path"),
							MasterSeed: []byte(botSeed), // Use global bot seed
						}, logRuntime.Logger("ssb"))
						if err != nil {
							return fmt.Errorf("open ssb runtime for blobs: %w", err)
						}
						defer ssbRuntime.Close()
						blobStore = ssbRuntime.BlobStore()
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
					r := chi.NewRouter()
					r.Use(websecurity.RequestLogMiddleware(uiLogger))
					r.Use(websecurity.SecurityHeadersMiddleware(true))
					if authConfigured {
						r.Use(websecurity.BasicAuthMiddleware(authUser, authPass))
					}

					ui := handlers.NewUIHandler(database, uiLogger, atpClient, blobStore)
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

func newBridgeLogRuntime(c *cli.Context, defaultService string) (*logutil.Runtime, error) {
	serviceName := strings.TrimSpace(c.String("otel-service-name"))
	if serviceName == "" {
		serviceName = defaultService
	}
	return logutil.NewRuntime(logutil.Config{
		Endpoint:    c.String("otel-logs-endpoint"),
		Protocol:    c.String("otel-logs-protocol"),
		Insecure:    c.Bool("otel-logs-insecure"),
		ServiceName: serviceName,
		CommandName: c.Command.Name,
		LocalOutput: c.String("local-log-output"),
	})
}

func shutdownLogRuntime(rt *logutil.Runtime) {
	if rt == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = rt.Shutdown(ctx)
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

func resolveLiveXRPCHost(explicitHost string) (string, error) {
	if strings.TrimSpace(explicitHost) == "" {
		explicitHost = defaultLiveReadXRPCHost
	}
	return backfill.NormalizeServiceEndpoint(explicitHost)
}

func resolveLiveBlobHostResolver(explicitHost string, explicitlySet bool) (blobbridge.HostResolver, error) {
	if explicitlySet && strings.TrimSpace(explicitHost) != "" {
		host, err := backfill.NormalizeServiceEndpoint(explicitHost)
		if err != nil {
			return nil, err
		}
		return backfill.FixedHostResolver{Host: host}, nil
	}
	return backfill.DIDPDSResolver{}, nil
}

func resolveBackfillHostResolver(fixedHost string) (backfill.HostResolver, error) {
	if strings.TrimSpace(fixedHost) != "" {
		host, err := backfill.NormalizeServiceEndpoint(fixedHost)
		if err != nil {
			return nil, err
		}
		return backfill.FixedHostResolver{Host: host}, nil
	}
	return backfill.DIDPDSResolver{}, nil
}

func fallbackValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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

// runRoomTunnelBootstrap connects the bridge sbot to the embedded room server
// and periodically re-announces on the room tunnel. It polls for readiness
// instead of using fixed sleeps, and retries the full sequence on failure.
func runRoomTunnelBootstrap(ctx context.Context, ssbRT *ssbruntime.Runtime, roomRT *room.Runtime, logger *log.Logger) {
	const reannounceEvery = 30 * time.Second

	bridgeFeed := ssbRT.Node().KeyPair.FeedRef()
	sbotListenAddr := ssbRT.Node().ListenAddr()

	// Ensure bridge is a room admin so it can announce.
	if err := roomRT.AddMember(ctx, bridgeFeed, roomdb.RoleAdmin); err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			logger.Printf("event=room_add_member_failed err=%v", err)
		}
	}

	// Announce the bridge sbot directly in the room's peer state (in-process).
	roomRT.AnnouncePeer(bridgeFeed, sbotListenAddr)
	logger.Printf("event=room_tunnel_announce_success feed=%s addr=%s", bridgeFeed.Ref(), sbotListenAddr)

	// Re-announce periodically to stay visible.
	ticker := time.NewTicker(reannounceEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			roomRT.AnnouncePeer(bridgeFeed, sbotListenAddr)
		}
	}
}

func runRuntimeHeartbeatScheduler(ctx context.Context, database *db.DB, logger *log.Logger, interval time.Duration) {
	if database == nil {
		return
	}
	if logger == nil {
		logger = logutil.NewTextLogger("bridge")
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
		logger = logutil.NewTextLogger("bridge")
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
		logger = logutil.NewTextLogger("bridge")
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
		logger = logutil.NewTextLogger("bridge")
	}

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := processor.ResolveDeferredMessages(ctx, 500)
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
