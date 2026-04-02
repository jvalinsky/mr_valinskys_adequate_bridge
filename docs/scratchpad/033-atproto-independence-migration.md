# ATProto Independence Migration

## Goal

Replace Indigo usage with local ATProto primitives and a Hydrant-style in-process indexer.

## Session Notes

- Root migration scratchpad for decision-graph attachments.
- Tracks overall scope, cross-cutting decisions, migration risks, and milestone outcomes.
- Hydrant reference clone reviewed in `/tmp/hydrant` on 2026-04-01.

## Current Findings

- Indigo currently supplies four major capabilities in this repo:
  - generated XRPC methods and auth helpers
  - ATProto syntax and identity resolution helpers
  - firehose framing and repo stream handling
  - repo/CAR traversal plus CBOR decoding
- Bridge runtime currently consumes upstream firehose commits directly and persists only downstream bridge state.
- Existing SQLite schema does not model repo sync state, buffered live commits, or replayable ATProto events.

## Migration Risks

- Cutover affects backfill, dependency resolution, blob fetching, post creation, and firehose ingestion.
- Repo-state persistence must be introduced without regressing existing bridge/admin behavior.
- Tests currently rely on Indigo helpers for CAR construction and some firehose fixtures.

## Milestones

- Done: create local `pkg/atproto` boundary with syntax, XRPC, identity, firehose, repo, and minimal `app.bsky` models.
- Done: introduce `atproto_sources`, `atproto_repos`, `atproto_commit_buffer`, `atproto_records`, and `atproto_event_log`.
- Done: cut runtime to `relay -> atindex -> local event log -> bridge processor`.
- In progress: operator/debug surface and residual UI/runtime state cleanup.
- In progress: remove Indigo from remaining test-only imports.

## Runtime Shape

- Live relay commits no longer flow directly into `bridge.Processor` in the main runtime.
- `internal/firehose.Client` now feeds `internal/atindex.Service`.
- `internal/atindex.Service` persists generic ATProto records and ordered replayable `record` events.
- `cmd/bridge-cli/app.go` now consumes `atindex.Subscribe(cursor)` and forwards normalized events into `bridge.Processor.HandleRecordEvent`.
- Upstream relay cursor state now belongs in `atproto_sources`; downstream bridge replay position is `atproto_event_cursor`.

## Hydrant Alignment Notes

- Keep Hydrant-style client-managed replay cursors and broadcast subscription semantics.
- Keep `record` events replayable and `identity` / `account` ephemeral.
- Keep repo state queryable as current metadata rather than rebuilding it from the stream.
- Keep backfill as repo-state transition plus event-log insertion, not a separate acked outbox.

## Validation Snapshot

- Passing: `go test ./pkg/atproto/...`
- Passing: `go test ./internal/db`
- Passing: `go test ./internal/atindex`
- Passing: `go test ./internal/backfill`
- Passing: `go test ./internal/firehose`
- Passing: `go test ./internal/bridge`
- Passing: `go test ./cmd/bridge-cli`
- Passing: `go test ./cmd/atproto-seed`
- Passing: `go test ./internal/mapper ./internal/blobbridge ./internal/web/handlers`
- Targeted green suite:
  - `go test ./pkg/atproto/... ./internal/db ./internal/atindex ./internal/backfill ./internal/firehose ./internal/bridge ./internal/mapper ./internal/blobbridge ./internal/web/handlers ./cmd/bridge-cli ./cmd/atproto-seed`
- Remaining Indigo footprint is now concentrated in test-only CAR builders and live/docker integration paths, especially `internal/livee2e` and tagged docker tests.

## Update 2026-04-01

- Indigo is now fully removed from the Go module graph for this repo.
- `go.mod`, `go.sum`, and `vendor/modules.txt` no longer reference `github.com/bluesky-social/indigo`, and `vendor/github.com/bluesky-social/indigo` is gone after `go mod vendor`.
- The remaining Indigo mentions are documentary or operational references only:
  - migration scratchpads
  - bootstrap/reference notes
  - the local relay build helper that intentionally clones Indigo source to build an external relay image
- Full repository verification passed after the cleanup, including the smoke test update for the new bridge replay / relay source dashboard cursor model.
