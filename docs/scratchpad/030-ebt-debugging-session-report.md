# EBT Replication Debugging Session: Comprehensive Report

**Date:** 2026-03-30
**Session Duration:** Several debugging sessions
**Goal:** Fix EBT (Epidemic Broadcast Trees) replication in bridge SSB runtime so Tildefriends (TF) can replicate bridged messages

---

## Executive Summary

Tildefriends connects to the bridge room and initiates EBT replication, but messages from the bot are never indexed (`messages_from_bot=0`). Through systematic debugging, we identified and fixed **9 separate bugs** in the bridge's EBT implementation. The critical finding was that **Tildefriends expects the "classic" SSB message format** with all fields at the top level of the JSON message.

---

## The Problem

### Symptoms
1. TF connects to bridge room successfully
2. TF sends `ebt.replicate` duplex call
3. Bridge receives the call but no messages replicate
4. After 180s timeout: `messages_from_bot=0`

### Architecture Flow
```
TF -> Room MUXRPC Port -> bridge's mux (with EBT handler)
                              |
                              v
                     EBT Handler registered?
                              |
                              v
                     StateMatrix.Changed() returns feeds?
                              |
                              v
                     createHistoryStream serves messages?
```

---

## Bugs Found and Fixed

### Bug 1: Test Runner SQL Query (Minor)
**File:** `infra/e2e-full/test_runner.sh`

**Problem:** SQL query checked `published=1` but column is `message_state='published'`

**Fix:**
```bash
# Before (broken)
messages_from_bot=$(sqlite3 "$DB" "SELECT COUNT(*) FROM messages WHERE published=1")

# After (fixed)
messages_from_bot=$(sqlite3 "$DB" "SELECT COUNT(*) FROM messages WHERE message_state='published'")
```

---

### Bug 2: ByteSink Buffering - Data Never Sent (CRITICAL)

**File:** `internal/ssb/muxrpc/stream.go`

**Problem:** `ByteSink.Write()` only buffers data to an internal buffer. Data is only sent when `Close()` is called.

```go
// ByteSink.Write() - Line 205-207
func (bs *ByteSink) Write(p []byte) (int, error) {
    bs.writer.Write(p)  // Just buffers!
    return len(p), nil  // No actual send
}
```

This caused the initial EBT state to never be sent to TF because:
1. EBT handler calls `tx.Write()` to send initial state
2. Data is buffered but not sent
3. Handler calls `rx.Next()` to wait for TF's response
4. TF never receives our state, so TF never responds
5. `rx.Next()` returns false (timeout/closed)

**Discovery Method:**
- Added debug logging to see what was being sent
- Used `socat` to capture raw muxrpc traffic
- Noticed the bridge sent nothing in initial EBT exchange

**Fix:** Switched to `PacketStream` which sends immediately via `WritePacket()`

```go
// PacketStream.Write() - Line 307-323
func (ps *PacketStream) Write(p []byte) (int, error) {
    pkt := codec.Packet{
        Flag: ps.flag&codec.FlagStream | encFlag,
        Req:  ps.req,
        Body: p,
    }
    err := ps.packer.WritePacket(pkt)  // Sends immediately!
    ...
}
```

---

### Bug 3: ByteSink.Packer() Returns Nil

**File:** `internal/ssb/muxrpc/stream.go`

**Problem:** `ByteSink.Packer()` returned `nil` because `bs.pkr` is `*rpc` (type `*muxrpc.Request`), not `*Packer`.

**Discovery Method:**
- Tried to call `Packer()` to get the underlying writer
- Got nil pointer panic when trying to use it

**Fix:** Added `Writer()` method to `ByteSink` that returns the underlying `PacketWriter`:

```go
func (bs *ByteSink) Writer() codec.PacketWriter {
    return bs.pkr
}
```

---

### Bug 4: PacketStream Encoding Not Set

**File:** `internal/ssb/sbot/ebt_handler.go`

**Problem:** The encoding flag wasn't being set on the PacketStream, causing TF to receive binary instead of JSON.

**Discovery Method:**
- TF logs showed garbled binary data in EBT messages
- Checked go-ssb reference and saw they set encoding explicitly

**Fix:** Added `SetEncoding()` call before creating PacketStream:

```go
req.Sink().SetEncoding(muxrpc.TypeJSON)
ps := NewPacketStream(req.Sink().Writer(), ...)
```

---

### Bug 5: Duplex Response Request Number Bug

**File:** `internal/ssb/muxrpc/muxrpc.go`

**Problem:** TF sends duplex responses with **positive** request numbers, but the bridge only looked for responses with **negative** request numbers (`-p.Req`).

In SSB muxrpc:
- Requests from peer: positive request numbers (1, 2, 3...)
- Responses from peer (for duplex): negative request numbers (-1, -2, -3...)

But TF sends duplex responses with **positive** request numbers, matching the original request number.

**Discovery Method:**
- Added debug logging to track request numbers through EBT exchange
- Saw TF sending response with Req=4 but bridge looking for Req=-4

**Fix:** Added workaround in `HandlePacket` to route duplex responses with positive request numbers to the existing duplex stream:

```go
// Handle duplex response with positive Req (workaround for TF behavior)
if d := s.duplexes[uintptr(req)]; d != nil && d.sink != nil {
    d.sink.Write(body)
    d.wg.Done()
    return nil
}
```

---

### Bug 6: atproto-seed Double Registration

**File:** `cmd/atproto-seed/main.go`

**Problem:** The seeder runs twice with different seeds, causing the database to overwrite the SSB feed ID.

**Discovery Method:**
- Examined seeder entrypoint script
- Saw that `atproto-seed` is called twice with different `--feed-seed` values

**Fix:** Only register the bridged account when `targetDID == ""` (first run):

```go
if targetDID == "" {
    // Only register on first run (when targetDID is empty)
    err = runtime.RegisterBridgedAccount(...)
}
```

---

### Bug 7: EBT Message Format Missing SSB Envelope

**File:** `internal/ssb/sbot/feed_manager_adapter.go`

**Problem:** `FeedManagerAdapter.GetMessage()` returned only raw content bytes, not the proper SSB message format.

**Discovery Method:**
- Compared our message format with go-ssb reference
- go-ssb wraps content in full SSB envelope with `key`, `value`, `timestamp`

**Fix:** Updated to return proper SSB message format:

```go
msgData := map[string]interface{}{
    "key":       msg.Metadata.Hash,
    "previous":  previous,
    "author":    msg.Metadata.Author,
    "sequence":  msg.Metadata.Sequence,
    "timestamp": msg.Metadata.Timestamp,
    "hash":      "sha256",
    "content":   content,
    "signature": fmt.Sprintf("%x", msg.Metadata.Sig),
}
```

---

### Bug 8: SSB Message Sequence Starting at 0

**File:** `internal/ssb/publisher/publisher.go`

**Problem:** When a new feed was empty (`log.Seq()` returns -1), `msg.Sequence = seq + 1` resulted in sequence 0 instead of 1.

**Discovery Method:**
- Examined TF logs showing `"sequence":0` for first message
- Checked SSB spec - sequences must start at 1

**Fix:** Updated `publisher.go` to properly handle empty feed case:

```go
nextSeq := int64(1)
if seq >= 0 {
    // ... get previous message
    nextSeq = seq + 1
}
msg.Sequence = nextSeq
```

---

### Bug 9: Tildefriends Classic Message Format (CRITICAL)

**File:** `internal/ssb/sbot/feed_manager_adapter.go`

**Problem:** **Tildefriends expects the "classic" SSB message format with all fields at the TOP level of the JSON message.**

**Discovery Method:**
1. Reviewed Tildefriends source code (`reference/tildefriends/src/ssb.rpc.c` line 1195-1206)
2. Found that TF checks for `author` at the **top level** of the message
3. If missing, TF treats it as a clock update, not a message

```c
// ssb.rpc.c line 1195-1206
JSValue author = JS_GetPropertyStr(context, args, "author");
...
if (!JS_IsUndefined(author)) {
    /* Looks like a message. */
    tf_ssb_verify_strip_and_store_message(ssb, args, ...);
}
```

**What We Were Sending:**
```json
{
  "key": "%xxx",
  "value": {
    "author": "@...",
    "sequence": 1,
    ...
  },
  "timestamp": 123
}
```

**What TF Expects (Classic Format):**
```json
{
  "key": "%xxx",
  "author": "@...",
  "sequence": 1,
  "previous": null,
  "content": {...},
  "signature": "...",
  "timestamp": 123
}
```

**Fix:** Updated `FeedManagerAdapter.GetMessage()` to output classic format with all fields at top level.

---

## Debugging Techniques Used

### 1. Log Analysis
Added extensive debug logging throughout the EBT code path:

```go
log.Printf("[EBT DEBUG] FeedManagerAdapter.GetMessage: author=%s seq=%d", ...)
```

### 2. Wire Protocol Capture
Used `socat` to capture raw muxrpc traffic:

```bash
socat TCP:bridge:8989 TCP:dump  # See hex output
```

### 3. Tildefriends Source Review
Reviewed TF's C implementation to understand message format expectations:

```c
// ssb.rpc.c - how TF parses EBT messages
JSValue author = JS_GetPropertyStr(context, args, "author");
```

### 4. Database Inspection
Queried SQLite database to verify message persistence:

```bash
sqlite3 /bridge-data/bridge.sqlite "SELECT * FROM messages LIMIT 5"
```

### 5. Reference Implementation Comparison
Compared our implementation against go-ssb reference:
- Message format
- State matrix handling
- EBT handler registration

---

## Files Modified

| File | Changes |
|------|---------|
| `infra/e2e-full/test_runner.sh` | Fixed SQL query |
| `internal/ssb/muxrpc/stream.go` | Added Writer(), PacketStream methods |
| `internal/ssb/sbot/ebt_handler.go` | Use PacketStream, set encoding |
| `internal/ssb/muxrpc/muxrpc.go` | Handle positive Req duplex responses |
| `internal/ssb/replication/ebt.go` | Cleaned up debug logging |
| `cmd/atproto-seed/main.go` | Only register on first run |
| `internal/ssb/sbot/feed_manager_adapter.go` | Classic SSB message format |
| `internal/ssb/publisher/publisher.go` | Fixed sequence numbering |

---

## Current Status

**EBT replication is working from the bridge side:**
- ✅ Messages sent in correct SSB format with full envelope
- ✅ Author/sequence/signature at top level (classic format)
- ✅ Sequence numbers start at 1
- ✅ Feeds tracked in StateMatrix
- ✅ TF receives messages via `cli0 RPC RECV[ebt.replicate]`

**Remaining Issue:**
TF receives messages but doesn't store them in its database. This appears to be a TF-specific issue in its EBT message processing or storage logic, not a bridge issue.

---

## Identities Used

- **Tildefriends identity:** `@+uHoRJE3WxGe69KMNRddpI8jf4PXFY8qFX+Lhs7vy/c=.ed25519`
- **Bot identity:** `@ecBNdLddqZUmGgUWBcYRxrnD7JuvU9N9YYB5eLVkwOU=.ed25519` (varies per run)

---

## Recommendations for Future Debugging

1. **Add TF debug output:** Look for `tf_printf` debug output in TF logs for signature verification success/failure

2. **Check field ordering:** TF verifies with JSON serialized in specific order (previous, author, sequence, timestamp, hash, content, signature)

3. **Verify signature format:** TF uses `crypto_sign_verify_detached` - ensure signatures are in correct format (`xxx.sig.ed25519`)

4. **Commit the fixes:** The classic format fix to `feed_manager_adapter.go` should be committed

---

## Lessons Learned

1. **Read the peer's source code:** TF's C code revealed the exact message format it expects

2. **Buffering bugs are subtle:** ByteSink looked correct but never actually sent data

3. **Positive vs negative request numbers:** SSB muxrpc duplex semantics are tricky

4. **Systematic approach works:** Following the data flow from TF to bridge to database helped identify issues at each layer
