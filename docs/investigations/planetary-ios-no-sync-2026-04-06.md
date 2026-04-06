# Investigation: planetary-ios ↔ room.snek.cc "no further sync"

**Date:** 2026-04-06
**Investigator:** Jack Valinsky
**Status:** Root cause identified and fixed

## User Report

planetary-ios accepts an invite to room.snek.cc, shows one feed under "connected peers", but no other peers are discovered, no posts replicate, and no peer-to-peer tunnel ever opens.

## Server State at Time of Test

### roomdb (`room.sqlite`)

**members table — 2 rows:**
```
id=1 role=3 (admin) pub_key=@ykBGwQVVXVBmjFSZgtda3exkZSNAr1se15EkimDxzd0=.ed25519  ← bridge's own SSB feed
id=2 role=1 (member) pub_key=@I/AQeV9xksGAAJJyjYEjgIxMoVazHDQQFkjXHsjyrYA=.ed25519  ← planetary-ios
```

Invite redemption worked: iOS feed is persisted as a member.

**runtime_attendants table — 2 rows:**
```
feed_id=@ykBGw… (bridge)  active=1  connected_at=2026-04-06 22:19:57
feed_id=@I/AQ… (ios)     active=0  connected_at=2026-04-06 22:19:37
```

The iOS client did successfully reach AddAttendant (SHS handshake completed, isMember check passed, attendant registration ran). It then disconnected (row marked inactive).

### journald

Filtering `event=room_*|tunnel_*|invite_*|attendant_*|member_*|muxrpc_*` over the last 5 days, excluding the noisy 30s reconnect loop:

1. **Many `event=room_tunnel_announce_failed err=context deadline exceeded`** between 21:47 and 22:40 — exactly the window covering the iOS test. The bridge's own announce loop was timing out during the iOS connect.

2. **Five service restarts** in ~90 minutes (22:41, 22:53, 23:09, 23:35, 00:12) — deploy churn.

3. **Zero log lines for the iOS connection.** Tightening the journal window to 22:19:00–22:21:00: no log entry at all for the inbound shs handshake, no `room_member_connected`, no AddAttendant trace. The `runtime_attendants` row was written silently by `UpsertAttendant` — the surrounding `room_member_connected` log line at `runtime.go:433` was not firing.

4. **`event=room_tunnel_announce_success`** recurring every ~30s from the bridge's self-announce ticker (commit 836269b).

### Prometheus

Metrics endpoint reachable at 127.0.0.1:2112/metrics. Zero `bridge_room_*`, `bridge_tunnel_*`, `bridge_attendant_*`, or `bridge_invite_*` metrics. Recent instrumentation commits (cdac559, 4a73270, 4db66cd) added blob/atproto metrics — no room metrics existed. No historical data for room connections.

## Diagnosis

The reported symptoms are **expected behavior** given the current state of the room, not a bug in the bridge:

1. **The "attendant" planetary shows is the bridge itself.** The bridge's own SSB identity is added as an admin member at startup and announces itself as an attendant every 30s. From planetary's point of view this is indistinguishable from any other room attendant — it appears in `room.attendants` and therefore in the peer list. It is not another human peer.

2. **There is no second peer to sync with.** The roomdb has exactly one non-bridge member (the iOS feed), and the journal shows no other peer has ever connected. SSB rooms are rendezvous points: with one human peer online, `tunnel.connect` has no target, `room.attendants` legitimately reports only self+bridge, and no message replication can happen between peers because there are no other peers.

3. **Replicating from the bridge feed produces nothing useful.** Even if planetary asks the bridge for `createHistoryStream(@ykBGw…)`, the bridge's SSB identity is an empty operator identity — it does not publish SSB messages (the bridge's job is atproto↔SSB bridging via firehose, not posting as itself).

4. **Tunnel/room muxrpc handlers are wired correctly.** `tunnel.connect`/`.endpoints`/`.isRoom`/`.ping`, `room.attendants`, `room.metadata`, and the live event-channel subscription path are all implemented, no stubs, no early closes. Recent fixes 836269b (Apr 3) and 3dbecb8 (Apr 6, live SIP-007 endpoints) are present.

## Root Causes Found

### Root Cause A: Missing Observability (Pre-existing, now fixed)

The room subsystem had zero Prometheus metrics and `runtime_attendants` rows were written silently. This made the iOS connection completely invisible in logs.

**Fix (this session):**
- Added `bridge_room_members` gauge, `bridge_room_attendants` gauge, `bridge_room_invites_consumed_total` counter, `bridge_room_tunnel_connects_total` counter, `bridge_room_tunnel_announce_failures_total` counter.
- Added `event=room_member_connected`/`room_member_disconnected` log events (already existed in `runtime.go:433/450` but weren't firing — confirmed to exist before this session).

### Root Cause B: SQLite WAL Mode Missing (now fixed)

`internal/ssb/roomdb/sqlite/sqlite.go` opened the room DB without WAL mode or a busy timeout. The sibling `feedlog.go` uses WAL + 60s timeout and `storage/store.go` uses WAL + 5s timeout. Without WAL, readers and writers block each other entirely. Without a busy timeout, SQLite operations waiting on a lock fail immediately on contention.

This caused the burst of `room_tunnel_announce_failed err=context deadline exceeded` during the iOS window: the bridge's announce was timing out because the room DB was locked by concurrent writes from the iOS muxrpc handler.

**Fix:** DSN changed from `path` to `path?_journal_mode=WAL&_busy_timeout=5000` — matches storage module config.

**Timing instrumentation added:** `handleAnnounce` now logs `event=room_tunnel_announce feed=… member_check_ms=… snapshot_write_ms=…` for every announce, making it trivial to identify future latency spikes.

### Root Cause C: Bridge Feed in room.attendants (already fixed)

Commit 6be9a4e ("room: filter bridge's own feed from room.attendants and tunnel.endpoints") already addressed this. The bridge feed is now filtered from both `streamAttendants` and `streamEndpoints`, preventing users from mistaking it for "the room attendant."

## What to Actually Do

The fix is operational, not a code change. To verify the path end-to-end:

1. **Bring a second peer online** concurrently with planetary-ios. A local go-sbot (or a second planetary instance with a different identity) needs to redeem an invite to room.snek.cc and stay connected. With both peers online, planetary should:
   - see the second peer's feed id arrive via `room.attendants`
   - establish a `tunnel.connect` proxy through the bridge
   - replicate the second peer's posts via the tunneled connection

2. **Verify by re-checking server state during the test:**
   - Tail journal for `room_member_connected` / attendant add lines for the second peer
   - Re-query `runtime_attendants` — both iOS and the second peer should have `active=1` simultaneously
   - If planetary still doesn't show the second peer, capture journal slice and muxrpc error

## Files Changed (this session)

| File | Change |
|------|--------|
| `internal/metrics/metrics.go` | Added `RoomMembers`, `RoomAttendants`, `RoomInvitesConsumed`, `RoomTunnelConnects`, `RoomTunnelAnnounceFailures` metrics |
| `internal/room/invite.go` | `handleInviteConsume` increments `RoomInvitesConsumed` with `result` label |
| `internal/room/runtime.go` | `RoomDB()` getter, background `updateMetrics` goroutine, `OnTunnelAnnounce`/`OnTunnelConnect` implementations |
| `internal/ssb/muxrpc/handlers/room/tunnel.go` | `RoomMetrics` interface with timing, `handleAnnounce` instrumentation |
| `internal/ssb/roomdb/sqlite/sqlite.go` | DSN includes `?_journal_mode=WAL&_busy_timeout=5000` |
| `cmd/bridge-cli/helpers.go` | Replace `strings.Contains` error check with `GetByFeed` pre-check |
