# Mr Valinsky's Adequate Bridge

An ATProto-to-SSB bridge with an embedded Room2 server.

## Introduction

This software connects two different decentralized social networks: **ATProto** (used by Bluesky) and **Secure Scuttlebutt (SSB)**. 

Because these networks use entirely different formats for accounts, posts, and media, they cannot naturally communicate. This bridge solves that problem by reading public data from ATProto (like posts and profiles) and automatically translating it into SSB-compatible formats. It then publishes that translated data so that SSB users can see and interact with it.

If you are new to the project, this document provides a high-level overview. For deep technical details on how the translation works, see the [Documentation Index](docs/README.md).

## What It Does

- Ingests ATProto repository events from the Bluesky firehose (`subscribeRepos`).
- Maps supported record types to SSB message formats and publishes to SSB feeds.
- Deterministically derives SSB bot identities from ATProto DIDs using a master seed.
- Runs an integrated SSB Room2 server for peer discovery and message distribution.
- Mirrors ATProto blobs (images, media) into the SSB blob store.
- Provides an admin web UI for monitoring, triage, and operations.

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
GOFLAGS=-mod=mod go build -o bridge-cli ./cmd/bridge-cli

# Add bridged accounts
./bridge-cli --db bridge.sqlite --bot-seed <seed> account add did:plc:example123

# Start the bridge (firehose + Room2 + admin UI)
./bridge-cli --db bridge.sqlite --bot-seed <seed> --relay-url wss://bsky.network \
    start --repo-path .ssb-bridge --firehose-enable --room-enable \
    --room-listen-addr 127.0.0.1:9898 --room-http-listen-addr 127.0.0.1:9876
```

See [`docs/runbook.md`](docs/runbook.md) for full operational procedures.

## Setup Modes

Use one of these setup profiles depending on whether you are validating locally or operating a host.

| Mode | Use for | Entry points |
|------|---------|--------------|
| Local Docker dependencies + local bridge process | Day-to-day development and integration testing without touching public ATProto services | `scripts/local_atproto_up.sh`, `scripts/local_atproto_bootstrap.sh`, `scripts/local_atproto_down.sh` |
| Full Docker E2E (bridge + tildefriends) | Full containerized compatibility verification including Room2 replication | `scripts/e2e_tildefriends.sh`, `infra/e2e-full/docker-compose.yml` |
| NixOS module deployment | Persistent staging/production service management via systemd | `nix/modules/mr-valinskys-adequate-bridge.nix`, `nix/examples/bridge-host.nix` |

### Local Testing Setup (Docker)

Fast path:

```bash
./scripts/local_bridge_e2e.sh
```

Manual loop:

```bash
./scripts/local_atproto_up.sh
./scripts/local_atproto_bootstrap.sh /tmp/mvab-local-atproto-live.env
source /tmp/mvab-local-atproto-live.env

export BRIDGE_BOT_SEED="dev-local-seed"
GOFLAGS=-mod=mod go run ./cmd/bridge-cli \
  --db bridge-local.sqlite \
  --relay-url "${LIVE_RELAY_URL}" \
  --bot-seed "${BRIDGE_BOT_SEED}" \
  start \
  --repo-path .ssb-bridge-local \
  --xrpc-host "${LIVE_ATPROTO_HOST}" \
  --plc-url "${LIVE_ATPROTO_PLC_URL}" \
  --room-enable \
  --room-listen-addr 127.0.0.1:8989 \
  --room-http-listen-addr 127.0.0.1:8976
```

Teardown:

```bash
./scripts/local_atproto_down.sh
```

### NixOS Setup (Production/Staging)

Use the NixOS module for managed services and keep external exposure behind TLS/reverse proxy.

```nix
services.mr-valinskys-adequate-bridge = {
  enable = true;
  environmentFile = "/run/secrets/bridge.env"; # BRIDGE_BOT_SEED + BRIDGE_UI_PASSWORD
  room = {
    enable = true;
    listenAddr = "127.0.0.1:8989";
    httpListenAddr = "127.0.0.1:8976";
    mode = "community";
    httpsDomain = "room.example.com";
  };
  ui = {
    enable = true;
    listenAddr = "127.0.0.1:8080";
    authUser = "admin";
    authPasswordEnvVar = "BRIDGE_UI_PASSWORD";
    extraArgs = [ "--repo-path" "/var/lib/mr-valinskys-adequate-bridge/.ssb-bridge" ];
  };
};
```

The `ui.extraArgs` `--repo-path` is required for blob browsing in `/blobs/view` routes.

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

Global flags: `--db` (SQLite path), `--relay-url` (firehose endpoint), `--bot-seed` (DID→SSB derivation seed), `--otel-logs-endpoint`, `--otel-logs-protocol` (`grpc|http`), `--otel-logs-insecure`, `--otel-service-name`, `--local-log-output` (`text|none`). Run `bridge-cli --help` for full flag reference.

**Notable `start` flags:**
- `--max-msgs-per-did-per-min` (default: 300) - Per-DID rate limit; set to 0 to disable. See [Rate Limiting](./docs/rate-limiting.md).
- `--publish-workers` - Parallel publish workers (default 1)

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

Requires Go 1.26.1+.
Set `GOFLAGS=-mod=mod` for local Go tooling to avoid accidental local `vendor/` shadowing of nested modules.

```bash
# Run all tests
GOFLAGS=-mod=mod go test ./...

# Deterministic smoke test
./scripts/smoke_bridge.sh

# Local E2E (requires Docker for ATProto stack)
./scripts/local_atproto_up.sh
./scripts/local_atproto_bootstrap.sh
./scripts/local_bridge_e2e.sh
./scripts/local_atproto_down.sh

# Linux container test runner (non-privileged)
docker compose -f infra/linux-test/docker-compose.yml run --rm go-test

# Linux eBPF smoke test (privileged; Linux hosts/runners)
docker compose -f infra/linux-test/docker-compose.yml --profile ebpf run --rm ebpf-smoke
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
- **`infra/e2e-full/`** — Docker E2E test infrastructure for tildefriends integration testing with Room2 peer verification.
- **`reference/tildefriends/`** — Tildefriends C source code for SSB protocol compatibility reference (used to verify bridge message format compatibility).

## Documentation

- **[`docs/README.md`](docs/README.md)** — Docs index organized by topic area.
- **[`docs/agents.md`](docs/agents.md)** — Agent/operator setup profile reference (local Docker vs NixOS production).
- **[`docs/atproto-ssb-translation-overview.md`](docs/atproto-ssb-translation-overview.md)** — High-level map of DID, AT URI, and blob translation layers.
- **[`docs/atproto-ssb-identity-mapping.md`](docs/atproto-ssb-identity-mapping.md)** — How DIDs become deterministic SSB feed identities.
- **[`docs/atproto-ssb-record-translation.md`](docs/atproto-ssb-record-translation.md)** — How `_atproto_*` placeholders become SSB refs.
- **[`docs/runbook.md`](docs/runbook.md)** — Operational runbook: startup, restart, retry, incident triage, pre-release E2E gates.

### SSB Protocol

The bridge implements Secure Scuttlebutt protocols. These documents cover the protocol stack with ASCII diagrams and code examples:

- **[`docs/ssb-protocol-fundamentals.md`](docs/ssb-protocol-fundamentals.md)** — Identity, feeds, messages, and signing
- **[`docs/ssb-replication.md`](docs/ssb-replication.md)** — Secret handshake, box stream, MUXRPC, and EBT
- **[`docs/ssb-rooms.md`](docs/ssb-rooms.md)** — Room2 architecture and tunnel connections
- **[`docs/ssb-implementations.md`](docs/ssb-implementations.md)** — Go code examples from the bridge
- **[`docs/ebt-replication.md`](docs/ebt-replication.md)** — EBT debugging findings and Tildefriends compatibility

### Development Notes

- **[`docs/scratchpad/README.md`](docs/scratchpad/README.md)** — Index of development notes and debugging sessions
