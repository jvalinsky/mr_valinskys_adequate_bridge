# 015 - Publish Workers and Retry Flow

## Objective
Complete publish reliability by implementing real `--publish-workers` behavior with per-DID ordering, bounded retry scheduling, and an explicit `retry-failures` operator command.

## Chosen Option + Rejected Option Summary
- Chosen: worker-lane publisher + DB-driven retry selector/backoff + operator retry command.
- Rejected: keep single-threaded publish behavior and manual-only retries.

## Interfaces/Flags/Schema Touched
- `internal/publishqueue/publisher.go`
  - Added worker-lane publisher wrapper for concurrent publishing with per-DID lane ordering.
- `internal/bridge/processor.go`
  - Added retry API:
    - `RetryFailedMessages(RetryConfig)`
  - Added retry result reporting and per-record retry persistence behavior.
  - Added retry backoff and due-time checks.
  - `ProcessRecord` now stores `last_publish_attempt_at` when publishing is attempted.
- `internal/db/schema.sql`
  - Added `messages.last_publish_attempt_at DATETIME`.
- `internal/db/db.go`
  - Migration-safe add for `last_publish_attempt_at`.
  - `AddMessage` now persists/upserts `last_publish_attempt_at`.
  - Added `GetRetryCandidates(limit, did, maxAttempts)` selector.
- `cmd/bridge-cli/main.go`
  - `start` and `backfill` now use workerized publisher for `--publish-workers`.
  - Added background retry scheduler in `start`.
  - Added `retry-failures` command with:
    - `--limit`
    - `--did`
    - `--max-attempts`
    - `--base-backoff`
    - `--ssb-repo-path`
    - `--hmac-key`
    - `--publish-workers`

## Test Evidence
- `GOCACHE=/tmp/go-build-cache go test ./internal/db ./internal/bridge ./internal/publishqueue ./cmd/bridge-cli`
  - all modified reliability packages pass.
- Added/updated tests:
  - `internal/publishqueue/publisher_test.go`
    - verifies per-DID ordering under concurrent workload.
  - `internal/bridge/processor_test.go`
    - retry publishes when backoff elapsed.
    - retry defers when backoff window not elapsed.
  - `internal/db/db_test.go`
    - persists `last_publish_attempt_at`.
    - returns correct retry candidates.

## Risks and Follow-ups
- Retry scheduling is time-driven and local-process based; distributed/multi-instance deployment needs leadership/coordination to avoid duplicated retry scans.
- `max-attempts` exclusion leaves hard-failed records for manual intervention; future admin UI may need explicit dead-letter/override controls.
