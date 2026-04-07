// SSB Client is a full-featured SSB client with web UI.
//
// It provides identity management, feed browsing, post composition,
// following/blocking, profile management, and Room2 integration.
package main

import (
	"log"
	"os"

	"github.com/urfave/cli/v2"
)

var (
	repoPath       string
	listenAddr     string
	httpListenAddr string
	appKey         string
	enableEBT      bool
	enableRoom     bool
	roomMode       string
	roomHTTPAddr   string
	initialPeers   string
	localLogOutput string
	logLevel       string
	logJSON        bool
)

const (
	defaultRepoPath   = ".ssb-client"
	defaultListenAddr = "127.0.0.1:8008"
	defaultHTTPListen = "127.0.0.1:8080"
)

func main() {
	app := &cli.App{
		Name:  "ssb-client",
		Usage: "A full-featured SSB client with web UI",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "repo-path",
				Value:       defaultRepoPath,
				Usage:       "path to the SSB repository",
				Destination: &repoPath,
			},
			&cli.StringFlag{
				Name:        "listen-addr",
				Value:       defaultListenAddr,
				Usage:       "SSB MUXRPC listen address",
				Destination: &listenAddr,
			},
			&cli.StringFlag{
				Name:        "http-listen-addr",
				Value:       defaultHTTPListen,
				Usage:       "HTTP web UI listen address",
				Destination: &httpListenAddr,
			},
			&cli.StringFlag{
				Name:        "app-key",
				Usage:       "SSB network identifier (hex or name; empty = standard SSB key)",
				Destination: &appKey,
			},
			&cli.BoolFlag{
				Name:        "enable-ebt",
				Value:       true,
				Usage:       "enable Epidemic Broadcast Trees replication",
				Destination: &enableEBT,
			},
			&cli.BoolFlag{
				Name:        "enable-room",
				Value:       true,
				Usage:       "enable Room2 client support",
				Destination: &enableRoom,
			},
			&cli.StringFlag{
				Name:        "room-mode",
				Value:       "community",
				Usage:       "Room2 mode when connecting to rooms: open|community|restricted",
				Destination: &roomMode,
			},
			&cli.StringFlag{
				Name:        "room-http-addr",
				Usage:       "HTTP address of the Room2 server to use for invite.consume (e.g., http://127.0.0.1:8976)",
				Destination: &roomHTTPAddr,
			},
			&cli.StringFlag{
				Name:        "initial-peers",
				Usage:       "path to JSON file containing initial peer list",
				Destination: &initialPeers,
			},
			&cli.StringFlag{
				Name:        "local-log-output",
				Value:       "text",
				Usage:       "local log output mode: text|none",
				Destination: &localLogOutput,
			},
			&cli.StringFlag{
				Name:        "log-level",
				Value:       "info",
				Usage:       "log level: debug|info|warn|error",
				Destination: &logLevel,
			},
			&cli.BoolFlag{
				Name:        "log-json",
				Usage:       "output logs as JSON",
				Destination: &logJSON,
			},
		},
		Commands: []*cli.Command{
			{
				Name:   "serve",
				Usage:  "Start the SSB client server with web UI + JSON API",
				Action: runServe,
			},
			{
				Name:  "identity",
				Usage: "Manage identity (offline — no server needed)",
				Subcommands: []*cli.Command{
					{
						Name:   "whoami",
						Usage:  "Display current identity",
						Action: runIdentityWhoami,
					},
					{
						Name:   "create",
						Usage:  "Create a new identity",
						Action: runIdentityCreate,
					},
					{
						Name:   "export",
						Usage:  "Export identity secret for backup",
						Action: runIdentityExport,
					},
					{
						Name:   "import",
						Usage:  "Import identity from backup",
						Action: runIdentityImport,
					},
				},
			},
			{
				Name:   "state",
				Usage:  "Show server state (JSON): identity, peers, feeds, sequences",
				Action: runState,
			},
			{
				Name:   "feeds",
				Usage:  "List all known feeds with sequence numbers (JSON)",
				Action: runFeeds,
			},
			{
				Name:   "feed",
				Usage:  "Show feed messages (JSON). Defaults to combined feed.",
				Action: runFeed,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "author", Usage: "filter by author feed ID (@xxx.ed25519)"},
					&cli.IntFlag{Name: "limit", Value: 50, Usage: "max messages to return"},
					&cli.StringFlag{Name: "type", Usage: "filter by message type (post, contact, about, ...)"},
				},
			},
			{
				Name:      "message",
				Usage:     "Get a single message by feed ID and sequence (JSON)",
				ArgsUsage: "<feedId> <sequence>",
				Action:    runMessage,
			},
			{
				Name:   "publish",
				Usage:  "Publish a message via the running server",
				Action: runPublish,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "type", Value: "post", Usage: "message type"},
					&cli.StringFlag{Name: "text", Usage: "text content (for post type)"},
					&cli.StringFlag{Name: "raw", Usage: "raw JSON content (overrides other flags)"},
					&cli.StringFlag{Name: "contact", Usage: "contact feed ID (for contact type)"},
					&cli.BoolFlag{Name: "following", Usage: "set following=true (for contact type)"},
					&cli.BoolFlag{Name: "blocking", Usage: "set blocking=true (for contact type)"},
				},
			},
			{
				Name:   "replication",
				Usage:  "Show EBT replication state matrix (JSON)",
				Action: runReplication,
			},
			{
				Name:  "peers",
				Usage: "Manage peers (hits running server)",
				Subcommands: []*cli.Command{
					{
						Name:   "list",
						Usage:  "List connected peers with bandwidth/latency stats (JSON)",
						Action: runPeersList,
					},
					{
						Name:      "add",
						Usage:     "Add and connect to a peer",
						ArgsUsage: "<address> <pubkey>",
						Action:    runPeersAdd,
					},
					{
						Name:      "connect",
						Usage:     "Connect to a peer",
						ArgsUsage: "<address> <pubkey>",
						Action:    runPeersConnect,
					},
				},
			},
			{
				Name:  "room",
				Usage: "Room command (SIP 6 auth)",
				Subcommands: []*cli.Command{
					{
						Name:      "login",
						Usage:     "Log in to a room using SSB HTTP Auth (SIP 6)",
						ArgsUsage: "<room-http-url>",
						Action:    runRoomLogin,
					},
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
