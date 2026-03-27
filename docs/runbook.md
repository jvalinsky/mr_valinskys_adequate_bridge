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
  --ssb-repo-path .ssb-bridge \
  --room-enable \
  --room-listen-addr 0.0.0.0:8989 \
  --room-http-listen-addr 0.0.0.0:8976 \
  --room-repo-path .ssb-room \
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
  --ssb-repo-path .ssb-bridge \
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

## Go Documentation Maintenance
- Write package comments and exported declaration comments so `go doc` output stays useful.
- Start declaration comments with the declaration name (or `A`/`An` + type name) and use complete sentences.
- Keep comments focused on behavior/contract and avoid repeating obvious implementation details.
- Use these references when updating comments:
  - https://go.dev/doc/comment
  - https://go.dev/doc/effective_go#commentary
  - https://go.dev/wiki/CodeReviewComments
  - https://google.github.io/styleguide/go/decisions.html#doc-comments
