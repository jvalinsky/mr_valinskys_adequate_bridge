# 021 README Section: Development, Scripts & Infrastructure

## Development

Requires Go 1.26.1+.

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

- **`infra/local-atproto/`** — Docker Compose stack for local development: PLC directory server + PDS instance. Used by `local_atproto_*.sh` scripts.
- **`infra/e2e-tildefriends/`** — Docker Compose setup for tildefriends integration testing. Used by `e2e_tildefriends.sh`.

## Documentation

- **`docs/runbook.md`** — Operational runbook: startup, restart, retry, incident triage, pre-release E2E gates.
- **`docs/scratchpad/`** — Development milestone notes (001–017) linked to the decision graph. Historical record of design decisions and implementation progress.
- **`reference/`** — External project sources used as architectural reference (see `reference/README.md`).
