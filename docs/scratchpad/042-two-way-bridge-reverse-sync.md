# Track 042: Two-Way Bridge Reverse Sync (2026-04)

DG Nodes: `#999`, `#1000-#1035`

## Scope

- Implement the first conservative SSB-to-ATProto reverse-sync path.
- Authorize reverse writes through explicit bridge mappings, not Room2 roles.
- Persist reverse queue state, receive-log cursor state, and correlation rows needed for loop prevention.
- Expose operator controls for reverse mappings, credential status, queue inspection, and retries.

## Decision Graph Map

- Goal: `#999` (completed)
- Observation: `#1000`
- Decisions:
  - `#1001` authority model -> chose `#1002`
  - `#1003` credential source -> chose `#1004`
  - `#1005` reverse action scope -> chose `#1006`
  - `#1007` input source -> chose `#1008`
  - `#1009` unresolved-target handling -> chose `#1010`
  - `#1011` delete policy -> chose `#1012`
  - `#1013` loop prevention -> chose `#1014`
  - `#1015` interop targets -> chose `#1016`
- Actions:
  - `#1017` reverse schema and migrations
  - `#1018` credential loader and status model
  - `#1019` receive-log reverse processor
  - `#1020` reverse queue state and retry handling
  - `#1021` reverse mappings and queue admin UI
  - `#1022` same-CID forward replay suppression
  - `#1023` Tildefriends reverse E2E coverage
  - `#1024` repo `ssb-client` reverse E2E coverage
  - `#1025` scratchpad and operator notes
- Outcomes:
  - `#1026` reverse schema/cursor persistence landed
  - `#1027` credential loading landed
  - `#1028` allowlisted post/reply/follow/unfollow translation landed
  - `#1029` reverse admin UI landed
  - `#1030` forward replay suppression landed
  - `#1031` targeted reverse tests passing
  - `#1032` scratchpad/index updated
  - `#1034` repo `ssb-client` reverse E2E passed
  - `#1035` Tildefriends reverse E2E passed

## Implementation Log

- `#1017`:
  - Added `reverse_identity_mappings` and `reverse_events` to the schema and migration set.
  - Added SQLite helpers for reverse mappings, reverse queue rows, target DID resolution, and follow lookup.
  - Added receive-log cursor persistence through `bridge_state`.
- `#1018`:
  - Added `bridge.LoadReverseCredentials()` for JSON credential maps keyed by AT DID.
  - Kept passwords in environment variables only via `password_env`.
  - Added UI/runtime credential status reporting.
- `#1019`:
  - Added `bridge.ReverseProcessor` to scan the SSB receive log and translate:
    - root `post` -> `app.bsky.feed.post`
    - reply `post` -> `app.bsky.feed.post` with reply refs
    - `contact following=true` -> `app.bsky.graph.follow`
    - `contact following=false` -> delete prior reverse-created follow record
  - Chose defer-over-degrade semantics for unresolved reply/follow targets and missing credentials.
- `#1020`:
  - Persisted reverse event attempts, defer reasons, publish errors, source refs, targets, and result URIs/CIDs.
  - Added manual retry support that reuses persisted raw SSB JSON instead of rewinding the receive-log cursor.
- `#1021`:
  - Added `/reverse` UI with:
    - mapping add/update/disable controls
    - per-DID credential status
    - reverse queue filters, state badges, and retry actions
  - Added reverse-sync status wiring to standalone `serve-ui`.
- `#1022`:
  - Added same-CID forward replay suppression in the ATProto processor.
  - Reverse-created AT records now persist correlation rows in `messages` so later firehose/backfill replays do not publish duplicate SSB messages.
- `#1023`:
  - Added Tildefriends reverse E2E coverage and verified root post, reply, follow, and unfollow against the reverse-sync bridge stack.
- `#1024`:
  - Added repo `ssb-client` reverse E2E coverage and verified root post, reply, follow, and unfollow against the reverse-sync bridge stack.
  - Fixed room client persistence, tunnelled history streaming, SQLite busy retries, and signed-message receive-log reconstruction needed for live reverse ingestion.
- `#1025`:
  - Linked this track to the decision graph and scratchpad index.

## Files Changed

- `internal/db/schema.sql`
- `internal/db/migrations/002_reverse_sync.sql`
- `internal/db/reverse_sync.go`
- `internal/db/messages.go`
- `internal/db/db.go`
- `internal/bridge/reverse_sync.go`
- `internal/bridge/processor.go`
- `internal/ssbruntime/runtime.go`
- `internal/web/handlers/ui.go`
- `internal/web/handlers/reverse.go`
- `internal/web/templates/templates.go`
- `pkg/atproto/atproto.go`
- `cmd/bridge-cli/main.go`
- `cmd/bridge-cli/commands.go`
- `cmd/bridge-cli/app.go`
- `cmd/bridge-cli/helpers.go`
- `cmd/bridge-cli/room_member_ingest.go`
- `cmd/ssb-client/commands.go`
- `internal/livee2e/live_reverse_ssb_client_test.go`
- `internal/room/tunnel_history_test.go`
- `internal/ssb/message/legacy/message.go`
- `internal/ssb/muxrpc/handlers/room/client_tunnel.go`
- `internal/ssb/muxrpc/stream_conn.go`
- `internal/ssb/feedlog/feedlog.go`
- `internal/ssb/sbot/sbot.go`
- reverse-specific tests in `internal/db`, `internal/bridge`, and `internal/web/handlers`
- reverse interop tests in `internal/livee2e`, `cmd/bridge-cli`, `internal/room`, and `internal/ssb`

## Validation

- `go test ./internal/db ./internal/bridge ./internal/web/handlers`
- `go test ./cmd/bridge-cli`
- `go test ./cmd/ssb-client ./cmd/bridge-cli ./internal/livee2e`
- `cd internal/ssb && go test ./sbot ./feedlog ./publisher`
- `./scripts/e2e_tildefriends.sh`
- `./scripts/live_reverse_ssb_client_e2e.sh`

## Remaining Work

- No open items remain for reverse-sync v1 in this track.
- If reverse scope expands beyond v1, split follow-on work into `043-...` instead of overloading this track.
