# Track 043: Reverse Sync V2 Media + Facets (2026-04)

DG Nodes: `#1041-#1065`

## Scope

- Extend reverse-created ATProto `post` and `reply` records with image embeds and rich-text facets.
- Keep reverse media conservative:
  - structured SSB blob mentions only
  - images only
  - defer on media failures instead of degrading to text-only output
- Reuse the existing reverse queue, allowlist, credential model, and loop-suppression behavior.

## Decision Graph Map

- Goal: `#1041` (active)
- Observation:
  - `#1042` reverse-sync v1 is complete; next value is content fidelity
- Decisions:
  - `#1043` reverse media scope -> chose `#1044`
  - `#1045` canonical reverse media source -> chose `#1046`
  - `#1047` blob-link text rewrite policy -> chose `#1048`
  - `#1049` facet placement policy -> chose `#1050`
  - `#1051` unmapped feed-mention handling -> chose `#1052`
  - `#1053` media failure policy -> chose `#1054`
- Actions:
  - `#1055` wire reverse blob-store and DB lookup access
  - `#1056` add reverse image embed pipeline
  - `#1057` add reverse text shaping and facet generation
  - `#1058` surface reverse media defer reasons in queue views
  - `#1059` track reverse-sync v2 in scratchpad and docs
  - `#1060` extend repo `ssb-client` reverse E2E for media
  - `#1061` extend Tildefriends reverse E2E for media
  - `#1065` fetch remote SSB blobs for reverse media publishing
- Outcomes:
  - `#1062` reverse processor now has blob-store access for media-aware retries and publishing
  - `#1063` reverse-created ATProto posts now support image embeds and rich-text facets

## Implementation Log

- `#1059`:
  - Created this track and linked it to the new reverse-sync v2 decision graph nodes.
  - Reserved this file for incremental execution notes as each implementation slice lands.
- `#1055`:
  - Added blob-store access to `ReverseProcessorConfig` so reverse translation can read SSB blob bytes during live processing and retries.
  - Added reverse DB blob lookup usage for MIME fallback and mismatch detection.
- `#1056`:
  - Added reverse image embed construction from structured blob mentions.
  - Enforced image-only handling, dedupe-by-ref ordering, four-image cap, and defer-on-media-failure behavior.
- `#1057`:
  - Added text shaping that strips markdown targeting embedded image blob refs.
  - Added rich-text facet generation for resolved feed mentions, structured tag/link mentions, and bare URLs on the final ATProto text.
- `#1062`:
  - Reverse retries are now media-aware because the processor can reopen SSB blobs instead of only replaying raw text payloads.
- `#1063`:
  - Reverse-created ATProto posts and replies now emit `app.bsky.embed.images` plus `facets` when the SSB source message contains supported media and resolvable rich-text targets.
- `#1065`:
  - Added a room-aware reverse blob fetch path that first tries direct SSB peers and then falls back to `tunnel.connect` + `blobs.get` for room-only peers.
  - Fixed `blobs.get`/`blobs.getSlice` source handlers to close their streams so real blob consumers can read to EOF instead of hanging.

## Files Changed

- `internal/bridge/reverse_sync.go`
- `internal/bridge/reverse_sync_test.go`
- `cmd/bridge-cli/app.go`
- `cmd/bridge-cli/commands.go`
- `cmd/bridge-cli/reverse_blob_fetcher.go`
- `internal/ssb/blobs/blobs.go`
- `internal/ssb/sbot/sbot.go`
- `internal/ssb/sbot/sbot_test.go`
- `docs/scratchpad/043-reverse-sync-v2-media-facets.md`
- `docs/scratchpad/README.md`
- `docs/atproto-ssb-record-translation.md`

## Validation

- `go test ./internal/bridge -run TestReverseProcessor -count=1`
- `go test ./cmd/bridge-cli -run TestNonExistent -count=1`
- `go test ./internal/web/handlers -run TestHandleReverse -count=1`
- `go test ./internal/db -run TestGetBlobBySSBRef -count=1`
- `cd internal/ssb && go test ./sbot -run TestSbotEnsureBlobFetchesFromConnectedPeer -count=1`

## Working Notes

- Exact-match facet placement is based on the final UTF-8 text bytes after stripping embedded image blob markdown.
- A bare `&blob...` in text without a structured mention remains plain text and does not create an embed.

## Completion Log

- **2026-04-09**: All implementation complete. E2E tests in infra/e2e-full/test_runner.sh verify image embeds work. Live test in internal/livee2e/live_reverse_ssb_client_test.go covers ssb-client blob fetch. Closed outcomes 1062-1066 and actions 1061, 1065, 1068. Goal 1041 marked complete.
