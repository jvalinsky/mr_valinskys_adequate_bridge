# 019 README Section: Quick Start & CLI

## Quick Start

```bash
# Build
go build -o bridge-cli ./cmd/bridge-cli

# Add bridged accounts
./bridge-cli --db bridge.sqlite --bot-seed <seed> account add did:plc:example123

# Start the bridge (firehose + Room2 + admin UI)
./bridge-cli --db bridge.sqlite --bot-seed <seed> --relay-url wss://bsky.network \
    start --repo-path .ssb-bridge --firehose-enable --room-enable \
    --room-listen-addr 127.0.0.1:9898 --room-http-listen-addr 127.0.0.1:9876
```

See `docs/runbook.md` for full operational procedures.

## CLI Commands

| Command | Description |
|---------|-------------|
| `account list` | List all bridged AT DIDs with activation status |
| `account add <did>` | Register a new DID for bridging |
| `account remove <did>` | Deactivate a bridged account |
| `stats` | Show bridge statistics (message counts, failures, account summaries) |
| `start` | Start firehose consumer + SSB publisher + Room2 server + admin UI |
| `backfill` | Replay historical records for DIDs via `sync.getRepo` |
| `retry-failures` | Retry failed unpublished bridge messages |
| `serve-ui` | Run only the admin web UI (read-only dashboard mode) |

### Global Flags

| Flag | Description |
|------|-------------|
| `--db` | Path to SQLite database |
| `--relay-url` | ATProto subscribeRepos endpoint |
| `--bot-seed` | Seed for deterministic DID → SSB feed derivation |
