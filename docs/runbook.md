# Bridge Operator Runbook

## Startup
1. Export required secrets.

```bash
export BRIDGE_BOT_SEED="<seed>"
export BRIDGE_UI_PASSWORD="<strong-password>"
```

2. Start the bridge runtime (firehose + Room2).

```bash
bridge-cli start \
  --db bridge.sqlite \
  --relay-url wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos \
  --repo-path .ssb-bridge \
  --room-enable \
  --room-listen-addr 0.0.0.0:8989 \
  --room-http-listen-addr 0.0.0.0:8976 \
  --room-mode community \
  --room-https-domain room.example.com \
  --publish-workers 4
```

3. Start the admin UI with auth.

```bash
bridge-cli serve-ui \
  --db bridge.sqlite \
  --listen-addr 0.0.0.0:8080 \
  --ui-auth-user admin \
  --ui-auth-pass-env BRIDGE_UI_PASSWORD
```

## Restart and Resume
- `start` resumes firehose consumption from `bridge_state.firehose_seq`.
- On shutdown, runtime teardown order is: firehose stop, room stop, SSB runtime close.
- Verify resumed cursor after restart:

```bash
bridge-cli stats --db bridge.sqlite
```

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

## Go Documentation Maintenance
- Write package comments and exported declaration comments so `go doc` output stays useful.
- Start declaration comments with the declaration name (or `A`/`An` + type name) and use complete sentences.
- Keep comments focused on behavior/contract and avoid repeating obvious implementation details.
- Use these references when updating comments:
  - https://go.dev/doc/comment
  - https://go.dev/doc/effective_go#commentary
  - https://go.dev/wiki/CodeReviewComments
  - https://google.github.io/styleguide/go/decisions.html#doc-comments
