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
export LIVE_ATPROTO_IDENTIFIER="<bridged-account-handle-or-email>"
export LIVE_ATPROTO_PASSWORD="<app-password>"
export LIVE_ATPROTO_FOLLOW_TARGET_DID="<bridged-target-did>"
export LIVE_ROOM_PEER_VERIFY_CMD="<command that verifies separate room peer observation>"
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
```

3. Run the live gate locally.

```bash
./scripts/live_bridge_e2e.sh
```

4. CI gate workflow:
   - `.github/workflows/bridge-live-prerelease.yml`
   - Triggered on `release` (`published`, `prereleased`) and `workflow_dispatch`.
   - Intended for pre-release validation, not pull request blocking.

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
- Local run defaults `LIVE_ROOM_MODE=open` and `LIVE_ROOM_PEER_VERIFY_CMD=./scripts/local_room_peer_verify.sh`.
- The default local verifier is strict: it checks room health, uses an ephemeral second SSB peer to complete a real room muxrpc handshake (`whoami`, `tunnel.isRoom`, `tunnel.announce`), then snapshots the bridge repo and counts source-feed messages via `cmd/ssb-feed-count` to assert expected bridged output is readable from native SSB storage.

## Go Documentation Maintenance
- Write package comments and exported declaration comments so `go doc` output stays useful.
- Start declaration comments with the declaration name (or `A`/`An` + type name) and use complete sentences.
- Keep comments focused on behavior/contract and avoid repeating obvious implementation details.
- Use these references when updating comments:
  - https://go.dev/doc/comment
  - https://go.dev/doc/effective_go#commentary
  - https://go.dev/wiki/CodeReviewComments
  - https://google.github.io/styleguide/go/decisions.html#doc-comments
