# Docker E2E Test Infrastructure

Integration testing environment for the ATProto-to-SSB bridge with Tildefriends social hub.

## Overview

This stack provisions a complete end-to-end environment including:
- ATProto stack (PLC, PDS, Relay)
- Bridge with embedded Room2 server
- Tildefriends SSB client for replication testing

## Components

| Service | Image | Purpose |
|---------|-------|---------|
| `plc` | node:20-alpine | ATProto PLC directory server |
| `plc-proxy` | caddy:alpine | PLC HTTP proxy with custom domain |
| `relay_pg` | postgres:16-alpine | Relay database |
| `relay` | blebbit/relay:latest | ATProto relay for firehose |
| `relay-seed` | postgres:16-alpine | Seeds relay with PDS |
| `init-keys` | ghcr.io/haileyok/cocoon:latest | Generates PDS signing keys |
| `pds` | ghcr.io/haileyok/cocoon:latest | ATProto PDS instance |
| `bridge` | local build | ATProto-to-SSB bridge with Room2 |
| `seeder` | local build | Seeds test data into PDS |
| `test-runner` | tildefriends | Tildefriends SSB client for replication verification |

## Usage

### Full E2E Test

```bash
# Run the complete E2E test
./scripts/e2e_tildefriends.sh

# Or directly with docker compose
docker compose -f infra/e2e-full/docker-compose.yml up --abort-on-container-exit
```

### Individual Services

```bash
# Start ATProto stack only
docker compose -f infra/e2e-full/docker-compose.yml up -d plc plc-proxy relay pds

# Start bridge only
docker compose -f infra/e2e-full/docker-compose.yml up -d bridge

# Run seeder
docker compose -f infra/e2e-full/docker-compose.yml run --rm seeder
```

## Environment Variables

### Bridge Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_PATH` | `/data/bridge.sqlite` | SQLite database path |
| `REPO_PATH` | `/data/ssb-repo` | SSB repository path |
| `BOT_SEED` | `e2e-full-seed` | Deterministic bot identity seed |
| `ROOM_MUXRPC_ADDR` | `0.0.0.0:8989` | Room2 MUXRPC listen address |
| `ROOM_HTTP_ADDR` | `0.0.0.0:8976` | Room2 HTTP listen address |
| `ROOM_MODE` | `open` | Room mode (open/community) |
| `BRIDGE_FIREHOSE_ENABLE` | `1` | Enable ATProto firehose |
| `BRIDGE_RELAY_URL` | `ws://relay:2470/...` | ATProto relay URL |

### Test Runner Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `BRIDGE_HTTP_ADDR` | `bridge:8976` | Bridge HTTP address |
| `BRIDGE_MUXRPC_ADDR` | `bridge:8989` | Bridge MUXRPC address |
| `BRIDGE_DB_PATH` | `/bridge-data/bridge.sqlite` | Bridge DB path |
| `BRIDGE_REPO_PATH` | `/bridge-data/ssb-repo` | Bridge repo path |
| `TF_DB_PATH` | `/tf-data/db.sqlite` | Tildefriends DB path |
| `MAX_WAIT_SECS` | `180` | Max wait time for replication |
| `POLL_INTERVAL` | `5` | Poll interval in seconds |

## Debugging

### Debug Scripts

| Script | Purpose |
|--------|---------|
| `scripts/debug_ebt_state.sh` | Diagnose EBT state from inside container |
| `scripts/debug_muxrpc_capture.sh` | Capture raw muxrpc traffic |

### Usage

```bash
# Debug EBT state
docker exec -it <bridge-container> /scripts/debug_ebt_state.sh

# Capture muxrpc traffic
docker exec -it <bridge-container> /scripts/debug_muxrpc_capture.sh

# Check test runner logs
docker compose -f infra/e2e-full/docker-compose.yml logs test-runner

# Inspect bridge database
docker exec -it <bridge-container> sqlite3 /data/bridge.sqlite "SELECT * FROM messages LIMIT 5"
```

## Files

| File | Purpose |
|------|---------|
| `docker-compose.yml` | Service definitions and networking |
| `docker-compose.override.yml` | Local overrides (optional) |
| `bridge_entrypoint.sh` | Bridge startup script |
| `seeder_entrypoint.sh` | ATProto data seeder script |
| `relay_startup.sh` | Relay bootstrap script |
| `relay_seed.sh` | Relay database seeding |
| `Caddyfile` | HTTP reverse proxy configuration |
| `test_runner.sh` | Replication verification script |

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Docker Network (10.42.0.0/24)                │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────┐   ┌──────────────┐   ┌─────────┐   ┌──────────┐ │
│  │   plc    │──▶│  plc-proxy   │   │  relay  │──▶│relay_pg  │ │
│  │          │   │ (plc.directory)  │         │   │          │ │
│  └──────────┘   └──────────────┘   └────┬────┘   └──────────┘ │
│                                          │                     │
│                                          ▼                     │
│  ┌──────────┐   ┌──────────────┐   ┌─────────┐               │
│  │init-keys │──▶│     pds      │   │relay-   │               │
│  │          │   │  (pds.test)  │   │  seed   │               │
│  └──────────┘   └──────────────┘   └─────────┘               │
│                                          │                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                        bridge                               ││
│  │  ┌────────────┐  ┌────────────┐  ┌─────────────────────┐ ││
│  │  │ ATProto    │  │  SSB Log   │  │      Room2          │ ││
│  │  │ Firehose   │  │  (margo)   │  │  MUXRPC (:8989)    │ ││
│  │  └────────────┘  └────────────┘  │  HTTP   (:8976)     │ ││
│  │                                   └─────────────────────┘ ││
│  └─────────────────────────────────────────────────────────────┘│
│                                    │                           │
├─────────────────────────────────────────────────────────────────┤
│                                    │                           │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                    test-runner (Tildefriends)              │ │
│  │  Connects to bridge Room2, replicates via EBT, verifies   │ │
│  │  messages_from_bot count                                   │ │
│  └────────────────────────────────────────────────────────────┘ │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Success Criteria

The test runner verifies:
1. Tildefriends successfully connects to bridge Room2
2. EBT replication negotiation completes
3. Bridged messages are replicated to Tildefriends
4. `messages_from_bot > 0` in test results

## Related Documentation

- [Bridge Operator Runbook](../../docs/runbook.md) — Operational procedures
- [EBT Replication Debugging](../../docs/ebt-replication.md)
- [Tildefriends Source](../../reference/tildefriends/)
- [E2E Test Script](../../scripts/e2e_tildefriends.sh)
