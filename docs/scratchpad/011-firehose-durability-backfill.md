# 011 - Firehose Durability and Backfill

## Objective
Persist firehose cursor state for restart safety, resume from last processed sequence, and add backfill tooling with idempotent record upserts.

## Chosen Option + Rejected Option
Chosen option: persist cursor in `bridge_state` and add `bridge-cli backfill` using `com.atproto.sync.getRepo` snapshot processing.
Rejected option: stateless firehose consumer with ad hoc manual replay scripts.
Reason: lightweight durable state in SQLite provides deterministic restart behavior and repeatable operational recovery.

## Interfaces / Flags / Schema Touched
- DB schema/API:
  - `bridge_state(key, value, updated_at)` table
  - `GetBridgeState` / `SetBridgeState` / list APIs
- Firehose client:
  - cursor option added (`cursor` query param)
- Bridge processor:
  - persists `firehose_seq` after commit handling
- New CLI command:
  - `bridge-cli backfill --did <did> [--did ...] [--since <timestamp|seq>] [--active-accounts]`

## Test Evidence
- `go test ./...` passing.
- `internal/firehose/client_test.go` validates cursor URL behavior.
- `internal/backfill/backfill_test.go` validates since parsing/filtering logic.
- Existing DB/processor tests validate idempotent upsert semantics keyed by `messages.at_uri`.

## Risks and Follow-ups
- Sequence-style `--since` for backfill is currently informational because `sync.getRepo` is snapshot-based; timestamp filtering is applied when available via `createdAt`.
- Next step: add commit-sequence-aware replay source for strict sequence-window backfill.
- Consider explicit audit log entries for cursor jumps.
