# Bridge Cutover Off Indigo

## Scope

- Swap bridge runtime from upstream Indigo commit handling to local ATIndex subscription
- Move blob bridge, dependency fetch, admin posting, and seed helpers to local ATProto package
- Preserve existing `messages` table semantics and admin UI behavior

## Notes

- Bridge should consume normalized local record events instead of raw upstream firehose commits.
- Existing bridge message persistence remains downstream state, separate from the new ATProto source-of-truth tables.
- `cmd/bridge-cli/app.go` now starts the indexer and a local consumer that replays from `atproto_event_cursor`.
- `bridge.Processor` gained `HandleRecordEvent` so the bridge can consume normalized index events instead of only upstream commit ops.
- `cmd/bridge-cli/commands.go` backfill now tracks repos through the indexer and waits for terminal sync states rather than calling the old snapshot path directly.
- `cmd/bridge-cli/helpers.go` auto-tracking now seeds the indexer from active bridged accounts.
- `cmd/atproto-seed`, blob bridge, dependency resolver, mapper, room public API, and PDS UI client paths now import the local ATProto package tree.

## Migration Order

1. admin and seed write paths
2. identity and resolver code
3. dependency fetcher
4. blob bridge
5. mapper and blob decoding
6. firehose and backfill cutover
7. tests and Indigo removal

## Test Status

- Green after local-model fixes: mapper, blob bridge, web handlers, CLI, indexer, DB, and local ATProto package tests.
- Remaining Indigo coupling is concentrated in test-only CAR builders and older firehose / bridge integration fixtures.
- The next cleanup step is either a tiny local repo writer for tests or fixture rewrites that stop depending on Indigo repo creation helpers.

## Operator Surface Update

- `serve-ui` now instantiates and attaches a local `atindex.Service` so `/api/atproto/*` can operate against the same Go API shape used by `start`.
- Dashboard runtime state now prefers `atproto_event_cursor` and falls back to legacy `firehose_seq` only when the new cursor is absent.
- State view now groups `atproto_*` keys with firehose-related runtime keys for incident inspection.
- `bridge-cli stats` now reports the ATProto event cursor and relay-source cursor separately from the legacy firehose checkpoint.

## Update 2026-04-01

- `bridge-cli backfill` now starts the same local indexer replay consumer path used by the runtime bridge. It no longer runs as an index-only command.
- The command now uses `RequestResync` instead of `TrackRepo`, waits for repo sync completion, and then waits for the downstream bridge replay cursor to drain through the largest requested repo event cursor.
- `BridgeApp.Start()` was split so `StartIndexerPipeline()` can be reused by `backfill` without also enabling the live firehose, retry scheduler, deferred resolver scheduler, or track scheduler.
- Dashboard/API/stats semantics were clarified:
  - bridge replay cursor = `bridge_state.atproto_event_cursor`
  - relay source cursor = `atproto_sources.last_seq`
  - event-log head = latest row in `atproto_event_log`
- The dashboard no longer treats the legacy firehose checkpoint as the active ATProto cursor readout. Legacy `firehose_seq` remains visible only as legacy state/API output.

## Validation 2026-04-01

- Added regression coverage for the operator surfaces:
  - dashboard renders bridge replay cursor, relay source cursor, and event-log head separately
  - `/api/atproto/health` reports the corrected cursor fields
  - `bridge-cli stats` prints separated replay/source/head/legacy cursor lines
- Confirmed green targeted suite:
  - `go test ./pkg/atproto/... ./internal/db ./internal/atindex ./internal/backfill ./internal/firehose ./internal/bridge ./internal/web/handlers ./internal/blobbridge ./internal/mapper ./cmd/bridge-cli ./cmd/atproto-seed`
