# SSB Client Parity Matrix (Tildefriends + Planetary Baseline)

_Last updated: 2026-04-13_

This matrix tracks `cmd/ssb-client` capability parity across protocol/RPC and UI workflows.

## Legend
- `done`: implemented and exposed
- `partial`: implemented with known limits
- `planned`: tracked but not yet implemented

## RPC Surface

| Surface | Baseline | Status | Notes |
| --- | --- | --- | --- |
| `whoami` | Tildefriends, Planetary | done | muxrpc async |
| `createHistoryStream` | Tildefriends, Planetary | done | source stream with `live/old/keys` handling |
| `ebt.replicate` | Tildefriends, Planetary | done | duplex EBT handler |
| `blobs.*` (`add/get/has/size/want/createWants`) | Tildefriends, Planetary | done | blob plugin registered |
| `room.metadata/members/attendants` | Tildefriends, Planetary | partial | metadata shape divergence documented |
| `tunnel.*` | Tildefriends, Planetary | done | announce/leave/connect/endpoints/isRoom/ping |
| `httpAuth.*` | Tildefriends, Planetary | done | requestSolution + invalidateAllSolutions |
| `invite.use` | Tildefriends, Planetary | done | room HTTP consume path |
| `invite.create` | Tildefriends, Planetary | partial | manifest present; muxrpc handler wiring still pending |

## HTTP/API Surface

| Endpoint | Status | Notes |
| --- | --- | --- |
| `/api/capabilities` | done | exposes manifest, required methods, missing methods, known gaps |
| `/api/timeline` | done | modes: `inbox`,`network`,`profile`,`channel`,`mentions` |
| `/api/thread/{msgKey}` | done | root + replies/backlinks query |
| `/api/channels` | done | channel counts + last activity |
| `/api/votes` | done | aggregated vote stats by target |
| `/api/search` | done | text/raw author search |
| `/api/followers` | done | computed from latest contact graph |
| `/api/conversations` | done | conversation list from DM index + outbox |
| `/api/conversations/{id}` | done | conversation messages (inbox + outbox) |
| `/api/messages/send` | done | encrypted DM send + outbox indexing |
| `/api/room/state` | done | room config + connected peers |
| `/api/room/invites` | done | list + create + revoke exposed via HTTP |

## UI Workflow Surface

| Workflow | Status | Notes |
| --- | --- | --- |
| Feed browsing | partial | baseline feed exists; timeline-mode UI migration in progress |
| Threads/replies | partial | API thread support implemented; compose/view UI needs full integration |
| Channels/hashtags | partial | indexed + API support; compose/filter UI expansion pending |
| Followers/following graph | partial | API implemented; followers page UX currently minimal |
| Reactions/votes | partial | API aggregation implemented; vote UI actions pending |
| Encrypted DM send/read | partial | APIs implemented; richer inbox UX pending |
| Room diagnostics | partial | API state endpoint added; full room UX still limited |
| Mobile UX hardening | planned | table overflow and nav responsiveness tracked |

## Known Gaps

- `private-box` message decryption indexing is not implemented.
- Timeline-mode feed UI migration is incomplete (API-first delivered).
- Vote/reaction compose UX is still pending.
- Full private-group membership UX is pending (multi-recipient orchestration only).

## Verification Commands

```bash
GOFLAGS=-mod=mod go test ./cmd/ssb-client -count=1
GOFLAGS=-mod=mod go run ./cmd/ssb-client compat probe
```
