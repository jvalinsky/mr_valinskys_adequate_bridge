# Bridge Operator Runbook

## Prerequisites
- Go 1.26.1+ (CGO_ENABLED=1 for SQLite)
- Docker + Docker Compose for local ATProto and full E2E stacks
- For live/production profile: network access to `bsky.network` (firehose), `public.api.bsky.app` (AppView), `plc.directory` (DID resolution)

## Deployment Profiles

| Profile | Use for | Recommended entrypoint |
|---------|---------|------------------------|
| Local Docker testing | Integration work, bug repro, and non-production validation | `scripts/local_bridge_e2e.sh` or manual local stack + `bridge-cli` |
| Full Docker interoperability | Tildefriends + Room2/EBT compatibility checks | `scripts/e2e_tildefriends.sh` |
| NixOS service deployment | Staging/production operations with systemd and reverse proxy | `nix/modules/mr-valinskys-adequate-bridge.nix` |

## Local Docker Setup (Testing)

### One-command local E2E

```bash
./scripts/local_bridge_e2e.sh
```

This brings up local PLC/PDS/relay dependencies, provisions local test accounts, and runs the live interop test.

### Linux containerized test entrypoints

```bash
# Standard non-privileged Linux container test run
docker compose -f infra/linux-test/docker-compose.yml run --rm go-test

# Optional privileged Linux eBPF smoke run
docker compose -f infra/linux-test/docker-compose.yml --profile ebpf run --rm ebpf-smoke
```

Notes:
- `ebpf-smoke` is Linux-only and requires privileged container execution.
- macOS hosts can still run the standard `go-test` service.

### Manual local loop (debug-friendly)

1. Start local dependencies and generate env vars.

```bash
./scripts/local_atproto_up.sh
./scripts/local_atproto_bootstrap.sh /tmp/mvab-local-atproto-live.env
source /tmp/mvab-local-atproto-live.env
```

2. Start bridge runtime against local ATProto services.

```bash
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

3. Optional: run the admin UI process locally.

```bash
export BRIDGE_UI_PASSWORD="dev-ui-password"
GOFLAGS=-mod=mod go run ./cmd/bridge-cli \
  --db bridge-local.sqlite \
  serve-ui \
  --listen-addr 127.0.0.1:8080 \
  --ui-auth-user admin \
  --ui-auth-pass-env BRIDGE_UI_PASSWORD \
  --repo-path .ssb-bridge-local
```

4. Tear down local dependencies.

```bash
./scripts/local_atproto_down.sh
```

## Production Initial Setup

Use `scripts/setup_live_bridge.sh` for the full lifecycle, or run CLI commands directly.

### 1. Prepare a DID list

Create a text file with one `did:plc:` per line (blank lines and `#` comments are ignored).

### 2. Add accounts and backfill

```bash
export BRIDGE_BOT_SEED="<stable-secret-seed>"

# Register bridged accounts
scripts/setup_live_bridge.sh setup --dids dids.txt --bot-seed "$BRIDGE_BOT_SEED"

# Backfill existing records from each DID's PDS
scripts/setup_live_bridge.sh backfill --bot-seed "$BRIDGE_BOT_SEED"

# Check status
scripts/setup_live_bridge.sh status
```

The bot seed must be kept stable across restarts - it deterministically derives SSB feed identities from AT DIDs.

## Production Startup

1. Export required secrets.

```bash
export BRIDGE_BOT_SEED="<seed>"
export BRIDGE_UI_PASSWORD="<strong-password>"
```

2. Start the bridge runtime (firehose + Room2 + embedded sbot).

```bash
bridge-cli start \
  --db bridge.sqlite \
  --relay-url wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos \
  --repo-path .ssb-bridge \
  --ssb-listen-addr :8008 \
  --room-enable \
  --room-listen-addr 0.0.0.0:8989 \
  --room-http-listen-addr 0.0.0.0:8976 \
  --room-mode community \
  --room-https-domain room.example.com \
  --publish-workers 4
```

Or use the wrapper:

```bash
scripts/setup_live_bridge.sh start \
  --bot-seed "$BRIDGE_BOT_SEED" \
  --room-https-domain room.example.com \
  --room-listen-addr 0.0.0.0:8989 \
  --room-http-addr 0.0.0.0:8976
```

3. Start the admin UI with auth (separate process or alongside).

```bash
bridge-cli serve-ui \
  --db bridge.sqlite \
  --listen-addr 0.0.0.0:8080 \
  --ui-auth-user admin \
  --ui-auth-pass-env BRIDGE_UI_PASSWORD \
  --repo-path .ssb-bridge
```

`--repo-path` on `serve-ui` is required for blob browsing and message detail blob previews.

### NixOS Production Profile

Use the NixOS module for long-running managed deployment.

```nix
services.mr-valinskys-adequate-bridge = {
  enable = true;
  environmentFile = "/run/secrets/bridge.env"; # BRIDGE_BOT_SEED + BRIDGE_UI_PASSWORD
  relayUrl = "wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos";
  firehoseEnable = true;
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

Apply and verify:

```bash
sudo nixos-rebuild switch
sudo systemctl status mr-valinskys-adequate-bridge
sudo systemctl status mr-valinskys-adequate-bridge-ui
curl -f http://127.0.0.1:8976/healthz
curl -f http://127.0.0.1:8080/healthz
```

Production hardening defaults:
- Keep room and UI bound to loopback unless explicitly exposing through a reverse proxy.
- Set `room.httpsDomain` whenever room listens on non-loopback.
- Require UI auth for any non-loopback UI bind.
- Terminate TLS in Caddy/nginx for public room and admin domains.

Key runtime flags:
- `--firehose-enable` (default: true) - set `=false` to run room-only without firehose ingestion
- `--ssb-listen-addr` (default: `:8008`) - the embedded sbot MUXRPC listener for peer EBT replication
- `--xrpc-host` - override the ATProto read host for dependency/blob fetches (defaults to AppView)
- `--otel-logs-endpoint` - optional OTLP logs endpoint for OpenTelemetry log export
- `--otel-logs-protocol` (`grpc|http`, default `grpc`) - OTLP transport protocol
- `--otel-logs-insecure` - disable OTLP transport security when needed for local collectors
- `--otel-service-name` - override `service.name` resource attribute
- `--local-log-output` (`text|none`, default `text`) - keep or suppress local stdout logs while OTLP export runs
- `--max-msgs-per-did-per-min` (default: 300) - per-DID message rate limit; set to 0 to disable. See [Rate Limiting](./rate-limiting.md) for details.

## Health Monitoring

### Endpoints
- **Room `/healthz`** (port 8976) — HTTP liveness check. Returns `200 ok` if the room HTTP server is accepting connections. Used by Docker health checks.
- **Admin `/healthz`** (port 8080) — Bridge runtime health. Returns `200 ok` when `bridge_runtime_status=live` and last heartbeat is within 60 seconds. Returns `503` otherwise. Use for reverse proxy or uptime monitoring.

### Log events to watch
- `event=firehose_enabled` / `event=firehose_disabled` — firehose mode
- `event=room_tunnel_announce_success` — room tunnel active (re-announces every 30s)
- `event=room_dial_success` — bridge sbot connected to room
- `event=publish_failed` / `event=retry_failed` — message publish issues
- `event=cursor_resume seq=N` — firehose cursor resumed on restart

## Restart and Resume
- `start` resumes firehose consumption from `bridge_state.firehose_seq`.
- On shutdown, runtime teardown order is: firehose stop, room stop, SSB runtime close.
- Verify resumed cursor after restart:

```bash
bridge-cli stats --db bridge.sqlite
```

## Backfill

Replay historical records for one or more DIDs from their ATProto PDS via `sync.getRepo`. Use this after adding new accounts or to catch up accounts that missed records before activation.

### Backfill specific DIDs

```bash
bridge-cli backfill \
  --db bridge.sqlite \
  --repo-path .ssb-bridge \
  --did did:plc:example1 \
  --did did:plc:example2
```

### Backfill all active accounts

```bash
bridge-cli backfill \
  --db bridge.sqlite \
  --repo-path .ssb-bridge \
  --active-accounts
```

### Backfill with a time filter

```bash
bridge-cli backfill \
  --db bridge.sqlite \
  --repo-path .ssb-bridge \
  --active-accounts \
  --since "2025-01-01T00:00:00Z"
```

Key flags:
- `--did` — repeatable, target specific DIDs
- `--active-accounts` — backfill all active accounts from the local DB
- `--since` — timestamp filter for partial backfill
- `--xrpc-host` — override PDS host (useful for local/test stacks)
- `--publish-workers` — parallel publish workers (default 1 for deterministic ordering)

## Retry Drain
1. Inspect failure totals.

```bash
bridge-cli stats --db bridge.sqlite
```

2. Drain failed unpublished records.

```bash
bridge-cli retry-failures \
  --db bridge.sqlite \
  --repo-path .ssb-bridge \
  --publish-workers 4 \
  --limit 200
```

3. Target a single DID if needed.

```bash
bridge-cli retry-failures --db bridge.sqlite --did did:plc:example --limit 200
```

## Incident Triage
1. Confirm process health and cursor progression.
   - `bridge-cli stats --db bridge.sqlite`
   - UI `/state` page (`firehose_seq`).
2. Check failure queue.
   - UI `/failures` page.
   - `bridge-cli retry-failures --limit 50` for controlled retry sampling.
3. Inspect room and bridge logs.
   - Look for `event=publish_failed`, `event=retry_failed`, `event=room_*`.
4. If publish failures persist after max attempts:
   - isolate by DID (`--did`).
   - capture failing `at_uri` rows from UI.
   - preserve DB and logs for postmortem.

## SSB Data Reset (Wipe and Re-backfill)

When the SSB log contains stale or malformed messages from a previous bridge version, or when empty feed entries are polluting the EBT state matrix, wipe the SSB data directory and re-backfill from ATProto.

1. Stop the bridge service.

```bash
sudo systemctl stop mr-valinskys-adequate-bridge
```

2. Remove the SSB data directory (preserves the SQLite bridge DB and cursor).

```bash
sudo rm -rf /var/lib/mr-valinskys-adequate-bridge/.ssb-bridge
```

3. Restart the service. The runtime will recreate `.ssb-bridge` with a fresh keypair and empty log.

```bash
sudo systemctl start mr-valinskys-adequate-bridge
```

4. Re-backfill all active accounts to republish records cleanly.

```bash
bridge-cli backfill --db bridge.sqlite --repo-path .ssb-bridge --active-accounts
```

**When to wipe:**
- Planetary or other SSB peers crash when replicating from the bridge (e.g., EBT overload from hundreds of empty feeds).
- Pre-fix messages with `_atproto_*` fields are still in the log and causing decoder issues on peers.
- The `replication_started total=N registered=M` log line shows a large gap between total and registered (many empty feeds).

**Caution:** Wiping regenerates the sbot keypair, so the room identity changes. Peers that followed the old identity will need to re-discover the bridge. The bridge SQLite DB (accounts, cursor, records) is preserved and does not need to be recreated.

## Known Issues

### Planetary GoBot crash from EBT feed overload (fixed in 1d176e1+)

**Symptom:** Planetary iOS/macOS crashes ~10 minutes after connecting to the bridge room. Crash report shows `exit()` → `__cxa_finalize_ranges` → `_objc_msgSend_uncached` on the GoBot-utility thread. The Go runtime inside Planetary calls `exit()` during EBT negotiation.

**Root cause:** The bridge's `GetPublisher(atDID)` creates a feed sublog entry in `userFeeds` even for deferred records that are never published to SSB. At startup, `runtime.go` registered ALL feeds from `userFeeds.List()` for EBT replication — including empty ones. With hundreds of bridged DIDs, the EBT state matrix advertised hundreds of empty feeds. When Planetary connected and tried to negotiate replication for all of them, its GoBot hit a fatal condition.

**Fix:** `runtime.go` now checks `sublog.Seq() != margaret.SeqEmpty` before registering a feed for replication. Only feeds with actual published messages are advertised via EBT.

**Diagnosis log line:**
```
unit=ssbruntime event=replication_started total=401 registered=4
```
A large gap between `total` and `registered` indicates many empty feeds were correctly filtered out.

### Planetary crash from malformed SSB messages (fixed in 1d176e1)

**Symptom:** Planetary crashes with SIGABRT in `swift_arrayDestroy` ~2 seconds after launch.

**Root cause:** Three issues: (1) duplicate EBT contact message floods from `Publish()` publishing a follow for every record, (2) `_atproto_*` internal fields leaking into published SSB messages that Planetary's strict Codable decoders didn't expect, (3) `room.members` muxrpc response wrapping each member in an array.

**Fix:** Dedup contact messages via `sync.Map`, strip `_atproto_*` fields via `SanitizeForPublish()` before publishing, validate required fields via `ReadyForPublish()`, fix `room.members` encoding.

## Pre-release Live Interop Gate
Run this gate before release/staging promotion to validate live firehose ingest plus room peer interoperability.

1. Set required live credentials and verifier command.

```bash
export LIVE_E2E_ENABLED=1
export LIVE_ATPROTO_SOURCE_IDENTIFIER="<bridged-source-handle-email-or-did>"
export LIVE_ATPROTO_SOURCE_APP_PASSWORD="<source-app-password>"
# choose one target strategy:
# 1) provide target DID directly
export LIVE_ATPROTO_FOLLOW_TARGET_DID="<bridged-target-did>"
# 2) OR provide target account credentials and let the test resolve DID
# export LIVE_ATPROTO_TARGET_IDENTIFIER="<bridged-target-handle-email-or-did>"
# export LIVE_ATPROTO_TARGET_APP_PASSWORD="<target-app-password>"
export LIVE_ROOM_PEER_VERIFY_CMD="./scripts/local_room_peer_verify.sh"
```

2. Optional overrides.

```bash
export LIVE_ATPROTO_HOST="https://bsky.social"
export LIVE_RELAY_URL="wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos"
export LIVE_BRIDGE_BOT_SEED="<seed>"
export LIVE_E2E_TIMEOUT="4m"
export LIVE_ROOM_MUXRPC_ADDR="127.0.0.1:9898"
export LIVE_ROOM_HTTP_ADDR="127.0.0.1:9876"
export LIVE_ROOM_MODE="community"
# Optional env-file/config path consumed by scripts/live_bridge_e2e.sh and the Go test:
# export LIVE_ATPROTO_ENV_FILE="/secure/path/live-e2e.env"
# export LIVE_ATPROTO_CONFIG_FILE="/secure/path/live-e2e.env"
```

3. Run the live gate locally.

```bash
./scripts/live_bridge_e2e.sh
```

4. CI gate workflow:
   - `.github/workflows/bridge-live-prerelease.yml`
   - Triggered on `push` to `staging`, `release` (`published`, `prereleased`), and `workflow_dispatch`.
   - Intended for pre-release validation, not pull request blocking.

5. GitHub environment wiring for promotion gates:
   - Workflow routes to environment `staging` for staging pushes/manual staging runs, and `release` for release/manual release runs.
   - Configure required environment secrets in both `staging` and `release`:
     - `LIVE_ATPROTO_SOURCE_IDENTIFIER`
     - `LIVE_ATPROTO_SOURCE_APP_PASSWORD`
     - `LIVE_ATPROTO_FOLLOW_TARGET_DID` (or target account credentials below)
     - `LIVE_ATPROTO_TARGET_IDENTIFIER` (optional, required if `LIVE_ATPROTO_FOLLOW_TARGET_DID` unset)
     - `LIVE_ATPROTO_TARGET_APP_PASSWORD` (optional, required if `LIVE_ATPROTO_FOLLOW_TARGET_DID` unset)
     - `LIVE_BRIDGE_BOT_SEED` (recommended, optional in code)
   - Configure optional environment variables in both `staging` and `release`:
     - `LIVE_ATPROTO_HOST`
     - `LIVE_RELAY_URL`
     - `LIVE_E2E_TIMEOUT`
     - `LIVE_ROOM_MUXRPC_ADDR`
     - `LIVE_ROOM_HTTP_ADDR`
     - `LIVE_ROOM_MODE`
   - `LIVE_ROOM_PEER_VERIFY_CMD` is pinned in workflow to `./scripts/local_room_peer_verify.sh`.

6. Repository protection policy (manual GitHub settings):
   - Require status check `Bridge Live Interop (Pre-release) / Live Relay + Room Peer Interop` on branch `staging`.
   - Gate release promotion on successful run of the same workflow in `release` environment.

7. Verifier policy and temporary rollback procedure:
   - Default verifier for staging/production promotion is strict tunnel-read proof: `./scripts/local_room_peer_verify.sh`.
   - If strict tunnel verification flakes in a target environment, temporarily switch to the relaxed verifier:

```bash
export LIVE_ROOM_PEER_VERIFY_CMD="./scripts/local_room_peer_verify_relaxed.sh"
./scripts/live_bridge_e2e.sh
```

   - Roll forward back to strict verifier immediately after incident mitigation:

```bash
export LIVE_ROOM_PEER_VERIFY_CMD="./scripts/local_room_peer_verify.sh"
```

8. Expected pass/fail signals for on-call triage:
   - Pass indicators:
     - verifier output contains `strict peer verification passed`
     - live harness output ends with `live interoperability test passed`
   - Fail indicators:
     - verifier output contains `peer verification failed` or `tunnel read assertion failed`
     - bridge logs include `event=publish_failed`, `event=retry_failed`, or room muxrpc stream errors

## Local Isolated ATProto Stack (No Live Services)
Use this workflow when you want full bridge E2E coverage without touching public ATProto infrastructure.

1. Start local PLC + local PDS.

```bash
./scripts/local_atproto_up.sh
```

2. Bootstrap two local ATProto accounts and write live test env vars.

```bash
./scripts/local_atproto_bootstrap.sh /tmp/mvab-local-atproto-live.env
set -a
source /tmp/mvab-local-atproto-live.env
set +a
```

3. Run bridge live E2E against the local stack.

```bash
./scripts/live_bridge_e2e.sh
```

4. One-command local flow (start + bootstrap + test).

```bash
./scripts/local_bridge_e2e.sh
```

5. Stop local services when done.

```bash
./scripts/local_atproto_down.sh
```

Notes:
- Default local endpoints are:
  - PDS: `http://127.0.0.1:2583`
  - Relay URL for bridge: `ws://127.0.0.1:2583/xrpc/com.atproto.sync.subscribeRepos`
  - PLC directory: `http://127.0.0.1:2582`
- `scripts/local_atproto_bootstrap.sh` generates source/target DIDs and writes all required `LIVE_*` variables.
- Legacy live credential variables are still accepted for backward compatibility:
  - `LIVE_ATPROTO_IDENTIFIER` -> source identifier
  - `LIVE_ATPROTO_PASSWORD` -> source app password
- Local run defaults `LIVE_ROOM_MODE=open` and `LIVE_ROOM_PEER_VERIFY_CMD=./scripts/local_room_peer_verify.sh`.
- The default local verifier is strict: it checks room health, launches an announced peer that serves bridged record refs over a real `tunnel.connect` duplex stream, and requires a separate second peer to read that tunnel snapshot and validate expected bridged URIs/refs.

## ATProto Harness Profiles
Use `./scripts/atproto_harness_e2e.sh mini` for the existing fully local harness flow, or `./scripts/atproto_harness_e2e.sh testnet` for a verdverm/testnet-backed stack.

Testnet profile defaults:
- `TESTNET_REPO_URL=https://github.com/verdverm/testnet`
- `TESTNET_REF=7e862f5`
- `TESTNET_DIR=/tmp/mvab-testnet`
- `TESTNET_PROJECT_NAME=mvab-testnet`

The testnet profile is additive operator tooling only. It does not change the current prerelease workflow or the default strict room-peer verifier policy.

## Go Documentation Maintenance
- Write package comments and exported declaration comments so `go doc` output stays useful.
- Start declaration comments with the declaration name (or `A`/`An` + type name) and use complete sentences.
- Keep comments focused on behavior/contract and avoid repeating obvious implementation details.
- Use these references when updating comments:
  - https://go.dev/doc/comment
  - https://go.dev/doc/effective_go#commentary
  - https://go.dev/wiki/CodeReviewComments
  - https://google.github.io/styleguide/go/decisions.html#doc-comments

---

## See Also

- [Documentation Index](./README.md)
- [Per-DID Rate Limiting](./rate-limiting.md)
- [Agent Setup Profiles](./agents.md)
