# Mr Valinsky's Adequate Bridge

An ATProto-to-SSB bridge with an embedded Room2 server.

## What It Does

- Ingests ATProto repository events from the Bluesky firehose (`subscribeRepos`)
- Maps supported record types to SSB message formats and publishes to SSB feeds
- Deterministically derives SSB bot identities from ATProto DIDs using a master seed
- Runs an integrated SSB Room2 server for peer discovery and message distribution
- Mirrors ATProto blobs (images, media) into the SSB blob store
- Provides an admin web UI for monitoring, triage, and operations

## Supported Record Types

| ATProto Collection | SSB Type | Description |
|----|----|----|
| `app.bsky.feed.post` | `post` | Text posts with mentions and links |
| `app.bsky.feed.like` | `like` | Likes referencing a subject record |
| `app.bsky.feed.repost` | `repost` | Reposts referencing a subject record |
| `app.bsky.graph.follow` | `contact` | Follow relationships |
| `app.bsky.graph.block` | `block_v2` | Block relationships |
| `app.bsky.actor.profile` | `about` | Profile name, description, avatar |

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

See [`docs/runbook.md`](docs/runbook.md) for full operational procedures.

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

Global flags: `--db` (SQLite path), `--relay-url` (firehose endpoint), `--bot-seed` (DID→SSB derivation seed). Run `bridge-cli --help` for full flag reference.

## Architecture

```
ATProto Firehose (subscribeRepos)
        │
        ▼
   ┌─────────┐     ┌────────┐     ┌────────────┐     ┌──────┐
   │ firehose │────▶│ bridge │────▶│ ssbruntime │────▶│  db  │
   └─────────┘     └────────┘     └────────────┘     └──────┘
                       │                 │
                       ▼                 │
                  ┌────────┐             │
                  │ mapper │             │
                  └────────┘             │
                       │                 ▼
                       ▼            ┌──────┐
                  ┌────────────┐    │ room │
                  │ blobbridge │    └──────┘
                  └────────────┘
```

The **firehose** package streams ATProto commits. The **bridge** processor coordinates record processing: it invokes the **mapper** to translate records into SSB payloads, the **blobbridge** to mirror media, and the **ssbruntime** to publish to SSB feeds. The **room** package runs an embedded Room2 server for SSB peer connectivity. All state is persisted to SQLite via the **db** package.

### Internal Packages

| Package | Description |
|---------|-------------|
| `bridge` | Coordinates firehose ingestion, mapping, publishing, and persistence |
| `db` | SQLite-backed persistence for bridge state and mappings |
| `ssbruntime` | Manages local SSB storage and publishing dependencies |
| `firehose` | Streams ATProto repository commits from subscribeRepos |
| `mapper` | Converts supported ATProto records into SSB-compatible payloads |
| `bots` | Manages deterministic SSB identities derived from ATProto DIDs |
| `blobbridge` | Mirrors ATProto blobs into the local SSB blob store |
| `backfill` | Replays supported records from ATProto repositories via sync.getRepo |
| `publishqueue` | Per-DID publish ordering with bounded worker parallelism |
| `room` | Embeds and supervises the go-ssb-room runtime |
| `web/handlers` | HTTP routes for the bridge admin UI |
| `web/templates` | HTML views for the bridge admin UI |
| `web/security` | HTTP middleware for admin UI exposure hardening |
| `presentation` | Formats bridge data for human-readable CLI and UI output |
| `livee2e` | End-to-end tests against a live ATProto relay and Room2 instance |
| `smoke` | Deterministic integration tests for the bridge pipeline |

## Development

Requires Go 1.25.5+.

```bash
# Run all tests
go test ./...

# Deterministic smoke test
./scripts/smoke_bridge.sh

# Local E2E (requires Docker for ATProto stack)
./scripts/local_atproto_up.sh
./scripts/local_atproto_bootstrap.sh
./scripts/local_bridge_e2e.sh
./scripts/local_atproto_down.sh
```

## Scripts

### Smoke & E2E Test Runners

| Script | Description |
|--------|-------------|
| `smoke_bridge.sh` | Deterministic local smoke test suite |
| `local_bridge_e2e.sh` | Full local E2E against dockerized ATProto stack |
| `live_bridge_e2e.sh` | E2E against live Bluesky firehose (needs credentials) |
| `testnet_bridge_e2e.sh` | E2E against testnet ATProto stack |
| `atproto_harness_e2e.sh` | ATProto test harness integration |
| `e2e_tildefriends.sh` | Docker E2E with tildefriends social hub |

### Local ATProto Stack

| Script | Description |
|--------|-------------|
| `local_atproto_up.sh` | Start local PLC + PDS Docker stack |
| `local_atproto_down.sh` | Stop local ATProto Docker stack |
| `local_atproto_bootstrap.sh` | Generate test credentials for local stack |
| `local_atproto_wait.sh` | Wait for local ATProto services to become healthy |

### Testnet ATProto Stack

| Script | Description |
|--------|-------------|
| `testnet_atproto_up.sh` | Start testnet ATProto stack |
| `testnet_atproto_down.sh` | Stop testnet ATProto stack |
| `testnet_atproto_bootstrap.sh` | Generate testnet credentials |
| `testnet_atproto_wait.sh` | Wait for testnet services to become healthy |

### Verification & Setup

| Script | Description |
|--------|-------------|
| `local_room_peer_verify.sh` | Verify Room2 peer connectivity and message replication |
| `local_room_peer_verify_relaxed.sh` | Relaxed Room2 peer verification (HTTP-only) |
| `setup_live_bridge.sh` | Configure a live bridge deployment environment |

## Infrastructure

- **`infra/local-atproto/`** — Docker Compose stack for local development: PLC directory server + PDS instance.
- **`infra/e2e-tildefriends/`** — Docker Compose setup for tildefriends integration testing.

## Documentation

- **[`docs/runbook.md`](docs/runbook.md)** — Operational runbook: startup, restart, retry, incident triage, pre-release E2E gates.
- **[`docs/scratchpad/`](docs/scratchpad/)** — Development milestone notes (001–017) linked to the decision graph.
- **[`reference/`](reference/)** — External project sources used as architectural reference (see [`reference/README.md`](reference/README.md)).
