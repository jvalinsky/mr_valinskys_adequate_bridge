# 008 - SSB Publish Pipeline

## Objective
Implement actual SSB publish behavior for mapped ATProto records instead of DB-only persistence, and persist publish metadata (`ssb_msg_ref`, `published_at`, `publish_attempts`, `publish_error`) with non-fatal per-record failure handling.

## Chosen Option + Rejected Option
Chosen option: in-process SSB runtime using `go-ssb` receive log + `userFeeds` multilog + deterministic DID-derived publishers, wired directly into the bridge processor.
Rejected option: defer publishing to a separate async worker process and keep runtime DB-only for now.
Reason: direct in-process publish closed the primary behavior gap with lower operational overhead and deterministic ordering via single worker default.

## Interfaces / Flags / Schema Touched
- CLI `start` flags added:
  - `--ssb-repo-path`
  - `--hmac-key`
  - `--publish-workers` (default `1`)
- DB schema and API updates:
  - `messages.published_at DATETIME NULL`
  - `messages.publish_error TEXT NULL`
  - `messages.publish_attempts INTEGER NOT NULL DEFAULT 0`
  - migration-safe `ALTER TABLE` handling for existing DBs
- Bridge processor:
  - publisher injection + publish metadata persistence
  - structured publish success/failure logs
- New runtime package:
  - `internal/ssbruntime` for publisher + index refresh lifecycle

## Test Evidence
- `go test ./...` passing.
- `internal/bridge/processor_test.go` validates:
  - successful publish stores `ssb_msg_ref`, `published_at`, attempts
  - publish failure stores `publish_error` and increments attempts without hard failure
- `internal/db/db_test.go` validates publish metadata upsert semantics.
- `internal/ssbruntime/runtime_test.go` validates runtime opens and publishes message refs.

## Risks and Follow-ups
- `publish-workers` currently logs note and keeps sequential behavior; parallel publish strategy is not implemented yet.
- No dead-letter queue yet for repeated failures; failures are visible in DB/UI.
- Future work: retry policy/backoff and explicit failure state transitions.
