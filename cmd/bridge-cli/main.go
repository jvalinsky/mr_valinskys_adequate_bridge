// Bridge-cli manages and runs the ATProto-to-SSB bridge.
//
// It provides account management, runtime orchestration, backfill, retry, and admin UI commands.
package main

import (
	"log"
	"os"
	"strings"
	"time"

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
							return runAccountList(c.Context, dbPath)
						},
					},
					{
						Name:      "add",
						Usage:     "Add a new bridged account",
						ArgsUsage: "<did>",
						Action: func(c *cli.Context) error {
							return runAccountAdd(c.Context, dbPath, botSeed, c.Args().First())
						},
					},
					{
						Name:      "remove",
						Usage:     "Deactivate a bridged account",
						ArgsUsage: "<did>",
						Action: func(c *cli.Context) error {
							return runAccountRemove(c.Context, dbPath, c.Args().First())
						},
					},
				},
			},
			{
				Name:  "stats",
				Usage: "Show bridge statistics",
				Action: func(c *cli.Context) error {
					return runStats(c.Context, dbPath)
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
					&cli.StringFlag{
						Name:    "plc-url",
						Value:   "https://plc.directory",
						EnvVars: []string{"BRIDGE_PLC_URL"},
						Usage:   "ATProto PLC directory URL (local/test stacks only)",
					},
					&cli.BoolFlag{
						Name:    "atproto-insecure",
						EnvVars: []string{"BRIDGE_ATPROTO_INSECURE"},
						Usage:   "disable TLS verification for all ATProto/XRPC connections (local/test stacks only)",
					},
				},
				Action: runStart,
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
					&cli.StringFlag{
						Name:    "plc-url",
						Value:   "https://plc.directory",
						EnvVars: []string{"BRIDGE_PLC_URL"},
						Usage:   "ATProto PLC directory URL (local/test stacks only)",
					},
					&cli.BoolFlag{
						Name:    "atproto-insecure",
						EnvVars: []string{"BRIDGE_ATPROTO_INSECURE"},
						Usage:   "disable TLS verification for all ATProto/XRPC connections (local/test stacks only)",
					},
				},
				Action: runBackfill,
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
				Action: runRetryFailures,
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
						Name:  "pds-password",
						Usage: "PDS password for manual posting",
					},
					&cli.StringFlag{
						Name:  "repo-path",
						Usage: "shared SSB repo path for blob serving fallback",
					},
					&cli.StringFlag{
						Name:  "room-repo-path",
						Usage: "room repo path for room admin workspace data (default: <repo-path>/room)",
					},
					&cli.StringFlag{
						Name:  "room-http-base-url",
						Usage: "optional room HTTP base URL for health/status verification (for example http://127.0.0.1:8976)",
					},
					&cli.BoolFlag{
						Name:    "atproto-insecure",
						Usage: "disable TLS verification for ATProto/XRPC connections (local/test stacks only)",
					},
				},
				Action: runServeUI,
			},
			{
				Name:  "mcp",
				Usage: "Run MCP (Model Context Protocol) servers for AI assistant integration",
				Subcommands: []*cli.Command{
					{
						Name:  "bridge-ops",
						Usage: "Run the bridge operations MCP server (status, accounts, messages, failures, retry)",
						Action: func(c *cli.Context) error {
							return runMCPBridgeOps(dbPath, botSeed)
						},
					},
					{
						Name:  "ssb",
						Usage: "Run the SSB node MCP server (feeds, blobs, peers, replication, room management)",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:  "repo-path",
								Value: ".ssb-bridge",
								Usage: "SSB repo path",
							},
						},
						Action: func(c *cli.Context) error {
							repoPath := c.String("repo-path")
							if strings.TrimSpace(repoPath) == "" {
								repoPath = ".ssb-bridge"
							}
							return runMCPSSB(dbPath, repoPath, botSeed)
						},
					},
					{
						Name:  "atproto",
						Usage: "Run the ATProto client MCP server (resolve, profiles, records, tracking)",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "plc-url",
								Value:   "https://plc.directory",
								EnvVars: []string{"BRIDGE_PLC_URL"},
								Usage:   "ATProto PLC directory URL",
							},
							&cli.StringFlag{
								Name:  "appview-url",
								Value: "https://public.api.bsky.app",
								Usage: "ATProto AppView URL for XRPC queries",
							},
							&cli.BoolFlag{
								Name:    "atproto-insecure",
								EnvVars: []string{"BRIDGE_ATPROTO_INSECURE"},
								Usage:   "disable TLS verification for ATProto/XRPC connections",
							},
						},
						Action: func(c *cli.Context) error {
							return runMCPATProto(
								dbPath,
								c.String("plc-url"),
								c.String("appview-url"),
								c.Bool("atproto-insecure"),
							)
						},
					},
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
