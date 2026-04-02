# ATIndex State Machine And Backfill

## Scope

- Repo metadata and sync state in SQLite
- Pending/backfilling/desynchronized queues
- Buffered live commit storage during backfill
- Replayable ordered record event log
- Track/untrack APIs and local subscription stream

## Notes

- Hydrant-inspired model: backfill and live ingestion cooperate through persisted repo state.
- `record` events are replayable; identity/account changes are current-state metadata plus live notifications.
- Backfill should enqueue work and optionally wait; worker does the actual `sync.getRepo` snapshot and buffer drain.
- `internal/atindex.Service` now implements `TrackRepo`, `UntrackRepo`, `GetRepoInfo`, `GetRecord`, `ListRecords`, and `Subscribe`.
- `HandleCommit` persists relay cursor state in `atproto_sources`, validates continuity against `current_rev`, and marks repos `desynchronized` on chain breaks.
- Commits arriving while repos are not `synced` are persisted into `atproto_commit_buffer` keyed by `(did, generation, rev)`.
- Backfill snapshots insert generic records into `atproto_records`, append `live=false` events to `atproto_event_log`, then drain buffered live commits in `seq/rev` order.

## Open Questions

- SQLite rowid / autoincrement cursor in `atproto_event_log` is adequate for the local replay cursor in v1.
- Distinguishing relay-source metadata from local replay state is handled by splitting `atproto_sources` and `atproto_event_log`; no extra provenance table is needed yet.

## Remaining Gaps

- Add the `/api/atproto/*` operator/debug surface over repo info, records, events, and source state.
- Add direct tests for the state machine around buffer drain, desync recovery, and replay subscription semantics.
- Decide whether to expose a richer health summary than the current repo/source/event counts.

## Update

- `/api/atproto/*` debug routes are now mounted in the UI router for health, source state, repo list/detail, track/untrack, record lookup/listing, and replayable event-log queries.
- The HTTP replay surface is query-based (`cursor`, `limit`) over `atproto_event_log`, which matches the Hydrant-style client-managed cursor model.

## Update 2026-04-01

- `TrackRepo` is now idempotent for steady-state repos. Existing `synced`, `deleted`, `deactivated`, `takendown`, and `suspended` repos stay in place and are not re-enqueued or generation-bumped by the periodic scheduler.
- Explicit snapshot replay moved to `RequestResync(did, reason)`. That path increments generation, resets sync state to `pending`, and enqueues the repo even when it was previously `synced`.
- `atproto_sources.last_seq` now advances only after the local ingest decision is durable:
  - ignored untracked repo events advance after the ignore decision
  - buffered commit paths advance after the buffer row is written
  - synced commit paths advance after commit application finishes
  - identity/account events advance after repo metadata is written
- Producer writes to `bridge_state.atproto_event_cursor` were removed. That key is now reserved for the downstream bridge replay consumer only.
- `Subscribe(cursor)` now de-dupes replayed `record` events against later live notifications by cursor, which avoids duplicate delivery when replay catches up to freshly appended rows.
- Backfill snapshot rows are broadcast to live subscribers after they are persisted, so the bridge consumer can see snapshot work without relying on a second polling pass.

## Validation 2026-04-01

- Added direct service tests for:
  - steady-state `TrackRepo` idempotency
  - `RequestResync` generation bump + queue behavior
  - source-cursor non-advancement on failed commit apply
  - producer not writing the bridge replay cursor
  - replay/live duplicate suppression in `Subscribe`
