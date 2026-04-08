# Mr Valinsky's Adequate Bridge

Mr Valinsky's Adequate Bridge bridges selected ATProto records into SSB. It can also mirror an allowlisted subset of SSB activity back into ATProto. The repository includes an embedded Room2 server, an admin UI, MCP surfaces, Prometheus hooks, and a standalone `ssb-client` binary used for local SSB work and reverse-sync coverage.

## Forward Bridge Coverage

| ATProto collection | SSB output | Notes |
| --- | --- | --- |
| `app.bsky.feed.post` | `post` | Handles replies, rich-text mentions, tags, quoted-post embeds, and bridged blobs |
| `app.bsky.feed.like` | `vote` | Resolves the subject record to a published SSB message ref |
| `app.bsky.graph.follow` | `contact` | Publishes `following=true` |
| `app.bsky.graph.block` | `contact` | Publishes `blocking=true` |
| `app.bsky.actor.profile` | `about` | Maps display name and description; avatar/blob data is mirrored separately |
| `app.bsky.graph.list` | `list` | Preserves list metadata |
| `app.bsky.graph.listitem` | `listitem` | Resolves both the target DID and the parent list record |
| `app.bsky.feed.threadgate` | `threadgate` | Resolves the subject AT URI against the local message table |

Notes:
- Standalone `app.bsky.feed.repost` events are not in the bridge processor's supported collection set.
- Quote posts are handled inside `app.bsky.feed.post` embed records, not as standalone repost records.

## Optional Reverse Sync

Reverse sync is off by default. When enabled, the bridge scans the SSB receive log and can publish:

| SSB message shape | ATProto result | Conditions |
| --- | --- | --- |
| `post` | `app.bsky.feed.post` | Source feed must be allowlisted for reverse sync |
| `post` with `root`/`branch` | `app.bsky.feed.post` reply | Referenced SSB message refs must already map to AT URIs/CIDs |
| `contact` with `following=true` | `app.bsky.graph.follow` | Target feed must resolve to an AT DID |
| `contact` with `following=false` and `blocking=false` | Delete previously published follow | Requires an earlier reverse-synced follow record |

Notes:
- `contact` messages with `blocking=true` are skipped.
- Reverse sync requires a feed-to-DID allowlist and per-DID credentials. See [docs/reverse-sync.md](docs/reverse-sync.md).

## Main Binaries

| Binary | Role |
| --- | --- |
| `bridge-cli` | Main operator entrypoint: accounts, runtime, backfill, retries, admin UI, MCP |
| `ssb-client` | Standalone SSB node with web UI and JSON API |
| `atproto-seed` | Test helper that seeds ATProto records for Docker E2E runs |
| `bridge-demo-init` | Generates a demo SQLite DB and repo for UI work |
| `room-tunnel-feed-verify` | Tunnel verifier used by room/interop tests |

Other `cmd/` directories are test or utility entrypoints.

## Quick Start

Fast path for local validation:

```bash
./scripts/local_bridge_e2e.sh
```

Manual local loop:

```bash
GOFLAGS=-mod=mod go build -o ./bridge-cli ./cmd/bridge-cli

./scripts/local_atproto_up.sh
./scripts/local_atproto_bootstrap.sh /tmp/mvab-local-atproto.env
set -a
source /tmp/mvab-local-atproto.env
set +a

export BRIDGE_BOT_SEED="dev-local-seed"

./bridge-cli \
  --db bridge-local.sqlite \
  --bot-seed "${BRIDGE_BOT_SEED}" \
  account add "${LIVE_ATPROTO_SOURCE_IDENTIFIER}"

./bridge-cli \
  --db bridge-local.sqlite \
  --relay-url "${LIVE_RELAY_URL}" \
  --bot-seed "${BRIDGE_BOT_SEED}" \
  start \
  --repo-path .ssb-bridge-local \
  --xrpc-host "${LIVE_ATPROTO_HOST}" \
  --plc-url "${LIVE_ATPROTO_PLC_URL}" \
  --atproto-insecure \
  --room-enable \
  --room-listen-addr 127.0.0.1:8989 \
  --room-http-listen-addr 127.0.0.1:8976 \
  --mcp-listen-addr "" \
  --metrics-listen-addr ""
```

Optional standalone admin UI:

```bash
export BRIDGE_UI_PASSWORD="dev-ui-password"

./bridge-cli \
  --db bridge-local.sqlite \
  serve-ui \
  --listen-addr 127.0.0.1:8080 \
  --ui-auth-user admin \
  --ui-auth-pass-env BRIDGE_UI_PASSWORD \
  --repo-path .ssb-bridge-local \
  --room-http-base-url http://127.0.0.1:8976
```

Tear down the local ATProto stack with:

```bash
./scripts/local_atproto_down.sh
```

See [docs/runbook.md](docs/runbook.md) for longer operator procedures and [infra/local-atproto/README.md](infra/local-atproto/README.md) for the local stack details.

## Runtime Modes

| Mode | Use for | Entry points |
| --- | --- | --- |
| Local ATProto stack + local bridge process | Day-to-day development, bug repro, reverse-sync work, local integration | `scripts/local_atproto_up.sh`, `scripts/local_atproto_bootstrap.sh`, `scripts/local_atproto_down.sh` |
| Full Docker E2E | Tildefriends/Room2 interoperability and full-stack coverage | `scripts/e2e_tildefriends.sh`, `scripts/e2e_full_up.sh`, `infra/e2e-full/docker-compose.yml` |
| NixOS module deployment | Staging/production service management | `nix/modules/mr-valinskys-adequate-bridge.nix`, `nix/examples/bridge-host.nix` |

## CLI Surface

### `bridge-cli`

| Command | Description |
| --- | --- |
| `account list` | List bridged AT DIDs and activation state |
| `account add <did>` | Register a DID for forward bridging |
| `account remove <did>` | Deactivate a bridged DID |
| `stats` | Show bridge, replay, and blob counters |
| `start` | Start the forward bridge, embedded sbot, Room2 runtime, and optional reverse sync |
| `backfill` | Queue `sync.getRepo` backfills through the ATProto indexer |
| `retry-failures` | Retry failed unpublished forward-bridge messages |
| `serve-ui` | Run the admin UI as a separate process |
| `mcp bridge-ops` | MCP server for bridge status, accounts, messages, failures, and retries |
| `mcp ssb` | MCP server for local SSB feeds, peers, blobs, and room state |
| `mcp atproto` | MCP server for ATProto identity, profiles, records, and repo tracking |

Notable `start` flags:
- `--room-enable` defaults to `true`.
- `--firehose-enable` defaults to `true`.
- `--mcp-listen-addr` defaults to `127.0.0.1:8081`; set it to an empty string to disable the live SSE server.
- `--metrics-listen-addr` enables a dedicated Prometheus listener when set.
- `--max-msgs-per-did-per-min` defaults to `300`; set `0` to disable rate limiting.
- `--reverse-sync-enable`, `--reverse-credentials-file`, `--reverse-sync-scan-interval`, and `--reverse-sync-batch-size` control SSB-to-ATProto reverse sync.

Global flags include `--db`, `--relay-url`, `--bot-seed`, `--otel-logs-*`, `--local-log-output`, and `--log-level`.

### `ssb-client`

`ssb-client` runs a local SSB node with:
- a web UI and JSON API (`serve`)
- offline identity management (`identity`)
- peer and room helpers (`peers`, `room`)
- feed, message, and replication inspection commands

Run `GOFLAGS=-mod=mod go run ./cmd/ssb-client --help` for the full surface.

## Architecture

```
ATProto subscribeRepos + sync.getRepo
              │
              ▼
           atindex
              │
              ▼
     atproto_event_log / repo state
              │
              ▼
       bridge processor ───────▶ blobbridge
              │
              ▼
         publishqueue
              │
              ▼
          ssbruntime ───────────▶ embedded Room2
              │
              ▼
      SSB publish log / receive log
              │
              ▼
   reverse processor (optional)
              │
              ▼
          ATProto PDS writes
```

Most persistent bridge state lives in SQLite:
- forward bridge state in `bridged_accounts`, `messages`, `blobs`, and `bridge_state`
- reverse-sync state in `reverse_identity_mappings` and `reverse_events`
- ATProto indexing state in `atproto_*` tables

## Package Map

| Package | Role |
| --- | --- |
| `internal/atindex` | Tracks repo state, event-log replay, and queued backfills |
| `internal/blobbridge` | Mirrors ATProto blobs into the SSB blob store |
| `internal/bots` | Deterministic DID-to-feed derivation |
| `internal/bridge` | Forward processor, deferred resolution, reverse sync, follower tracking |
| `internal/db` | SQLite schema and persistence |
| `internal/firehose` | `subscribeRepos` client |
| `internal/logutil` | Structured logging setup |
| `internal/mapper` | ATProto-to-SSB record mapping and placeholder replacement |
| `internal/metrics` | Prometheus collectors and registration |
| `internal/presentation` | CLI and UI-friendly formatting helpers |
| `internal/publishqueue` | Ordered publishing with bounded worker parallelism |
| `internal/room` | Embedded Room2 runtime |
| `internal/ssbruntime` | Local sbot, blob store, feed resolver, and receive log |
| `internal/web` | Admin UI handlers, templates, and security middleware |

## Development

Requirements:
- Go `1.26.1`
- Docker for local ATProto stacks and containerized E2E runs

Recommended Go setting:

```bash
export GOFLAGS=-mod=mod
```

Common commands:

```bash
GOFLAGS=-mod=mod go test ./...
./scripts/smoke_bridge.sh
./scripts/local_bridge_e2e.sh
./scripts/e2e_tildefriends.sh
./scripts/e2e_full_up.sh
```

Live and testnet coverage:
- `./scripts/live_bridge_e2e.sh`
- `./scripts/testnet_bridge_e2e.sh`
- `./scripts/atproto_harness_e2e.sh mini`
- `./scripts/atproto_harness_e2e.sh testnet`

## Scripts

### Test runners

| Script | Description |
| --- | --- |
| `scripts/smoke_bridge.sh` | Deterministic local smoke coverage |
| `scripts/local_bridge_e2e.sh` | Local ATProto stack + bridge integration run |
| `scripts/live_bridge_e2e.sh` | Live Bluesky firehose and room interop run |
| `scripts/testnet_bridge_e2e.sh` | Testnet-backed bridge E2E |
| `scripts/atproto_harness_e2e.sh` | Local or verdverm/testnet-backed ATProto harness run |
| `scripts/e2e_tildefriends.sh` | Room/EBT-focused Docker E2E with Tildefriends |
| `scripts/e2e_full_up.sh` | Full-stack Docker E2E with reverse bootstrap and admin UI |

### Local and testnet ATProto stacks

| Script | Description |
| --- | --- |
| `scripts/local_atproto_up.sh` | Start the local PLC, relay, and PDS stack |
| `scripts/local_atproto_bootstrap.sh` | Create source/target test accounts and write a `LIVE_*` env file |
| `scripts/local_atproto_wait.sh` | Wait for the local stack to become healthy |
| `scripts/local_atproto_down.sh` | Stop the local stack |
| `scripts/testnet_atproto_up.sh` | Start the verdverm/testnet-backed stack |
| `scripts/testnet_atproto_bootstrap.sh` | Write testnet credentials and env state |
| `scripts/testnet_atproto_wait.sh` | Wait for the testnet stack |
| `scripts/testnet_atproto_down.sh` | Stop the testnet stack |

### Verification, setup, and debugging

| Script | Description |
| --- | --- |
| `scripts/local_room_peer_verify.sh` | Strict Room2 tunnel verification |
| `scripts/local_room_peer_verify_relaxed.sh` | Reduced room verification when tunnel assertions are too strict for the target environment |
| `scripts/setup_live_bridge.sh` | Wrapper for live setup, backfill, start, status, and UI commands |
| `scripts/debug_ebt_state.sh` | Inspect EBT state in the Docker E2E stack |
| `scripts/debug_muxrpc_capture.sh` | Capture muxrpc traffic in the Docker E2E stack |

## Infrastructure

- [infra/local-atproto/README.md](infra/local-atproto/README.md): local PLC, relay, and PDS stack used by day-to-day development.
- [infra/e2e-full/README.md](infra/e2e-full/README.md): full Docker E2E stack with reverse bootstrap, bridge admin UI, and Tildefriends.
- [`reference/tildefriends/`](reference/tildefriends/): checked-in compatibility reference used for SSB protocol work.

## Documentation

- [docs/README.md](docs/README.md): index for project-owned docs.
- [docs/runbook.md](docs/runbook.md): operator procedures, deployment notes, and incident triage.
- [docs/reverse-sync.md](docs/reverse-sync.md): SSB-to-ATProto reverse-sync behavior and configuration.
- [docs/atproto-ssb-translation-overview.md](docs/atproto-ssb-translation-overview.md): forward-bridge translation model.
- [docs/atproto-ssb-identity-mapping.md](docs/atproto-ssb-identity-mapping.md): DID-to-feed derivation and account policy.
- [docs/atproto-ssb-record-translation.md](docs/atproto-ssb-record-translation.md): placeholder resolution and deferred state.
- [docs/rate-limiting.md](docs/rate-limiting.md): per-DID forward-bridge rate limiting.
- [docs/ssb-implementations.md](docs/ssb-implementations.md): file map for the repo's SSB implementation.
- [docs/ebt-replication.md](docs/ebt-replication.md): bridge-specific EBT and Room2 debugging notes.
- [docs/scratchpad/README.md](docs/scratchpad/README.md): archived design notes and implementation logs.
