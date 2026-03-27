# 010 - Blob Bridge

## Objective
Bridge ATProto blob/media references into SSB blob storage, persist CID-to-blob mappings, and annotate mapped payloads with bridged blob references.

## Chosen Option + Rejected Option
Chosen option: dedicated `internal/blobbridge` service that extracts blob CIDs from record payloads, fetches via `com.atproto.sync.getBlob`, stores in SSB blob store, and upserts `blobs` table.
Rejected option: ignore blobs and publish text-only records unconditionally.
Reason: preserving media linkage is required for bridge completeness and operational visibility.

## Interfaces / Flags / Schema Touched
- New package: `internal/blobbridge`
- DB table/API usage:
  - `blobs` upsert/read/count/recent queries
- Mapped payload updates:
  - profile avatar placeholder (`_atproto_avatar_cid`) -> `image`
  - `blob_refs` array added when blobs are bridged
- Failure handling:
  - blob bridge errors are recorded in `publish_error`
  - publish still proceeds as text-first fallback when possible

## Test Evidence
- `go test ./...` passing.
- `internal/blobbridge/bridge_test.go` validates:
  - blob fetch/store/path and DB mapping persistence
  - reuse of existing blob mapping without remote fetch
- `internal/bridge/processor_test.go` validates blob failure fallback annotation behavior.

## Risks and Follow-ups
- Blob reference mapping is intentionally generic; richer record-type-specific media mapping can be added.
- No explicit blob retry queue yet.
- Remote blob fetch resilience (timeouts/backoff/circuit behavior) can be improved.
