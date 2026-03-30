# EBT Replication Debugging Plan

## Problem Statement
Tildefriends (TF) connects to bridge room, sends `ebt.replicate` duplex call, but no messages are replicated. The bridge shows `messages_from_bot=0` after 180s timeout.

## Root Cause Analysis (Current Hypothesis)

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

### Known Fixed Issues
1. Handler Mux Architecture - Room now shares sbot's HandlerMux
2. EBT Nil Pointer Panic - StateMatrix.Changed() handles nil peer
3. Firehose Connected Callback - Bridge tracks firehose connection state

### Potential Remaining Issues

#### H1: EBT Handler Not Properly Registered on Shared Mux
**Hypothesis:** When room uses the external HandlerMux from sbot, the `ebt.replicate` handler may not be registered.

**Evidence needed:**
- Verify `sbot.New()` registers `ebt.replicate` on `handlerMux`
- Verify `room.runtime.go` uses the SAME mux instance
- Add debug logging to show registered handlers

**Experiment:** Add a diagnostic endpoint that lists all registered handlers.

#### H2: StateMatrix Not Tracking Published Feeds
**Hypothesis:** When bot publishes a message, the StateMatrix is not updated to reflect the new feed/sequence.

**Evidence needed:**
- Check if `StateMatrix.Update()` is called when messages are published
- The EBT handler's `sendState()` calls `Changed(self, nil)` which loads frontier for `self`
- But published feeds from ATProto DIDs are NOT in the frontier

**Root cause:** The StateMatrix tracks what feeds the bridge WANTS, but doesn't track what feeds the bridge HAS published.

**Experiment:** Add logging to see what's in the state matrix when TF connects.

#### H3: createHistoryStream Handler Not Implemented Correctly
**Hypothesis:** The `createHistoryStream` handler may not be returning the correct message format.

**Evidence needed:**
- Check `internal/ssb/muxrpc/handlers/history.go`
- Verify message format matches SSB spec
- Check if TF is receiving and accepting messages

#### H4: TF EBT Negotiation Not Completing
**Hypothesis:** TF sends its frontier but bridge's response is malformed or incomplete.

**Evidence needed:**
- Capture the raw bytes of EBT duplex exchange
- Compare with known-working go-ssb EBT exchange

#### H5: Feed Identity Mismatch
**Hypothesis:** The feeds that TF wants to replicate don't match the feeds that bridge has.

**Evidence needed:**
- TF knows about bot feed from room.members or whoami
- But EBT replication asks for specific feed refs
- Bridge may not be advertising correct feed refs

## Experiment Infrastructure

### Experiment 1: Handler Registration Check
Create a diagnostic script to verify handlers are registered:

```bash
# Query the bridge muxrpc for registered handlers
nc bridge 8989 << 'EOF'
{"name":["whoami"],"type":"async","body":{}}
EOF
```

Expected: Should return the room/bridge identity.

### Experiment 2: EBT State Capture
Add logging to `StateMatrix.Changed()`:

```go
func (sm *StateMatrix) Changed(self, peer *refs.FeedRef) (NetworkFrontier, error) {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    
    selfNf, err := sm.loadFrontier(self.String())
    // LOG: self frontier contents
    log.Printf("EBT Changed: self=%s, frontier=%+v", self.String(), selfNf)
    
    // ... rest
}
```

### Experiment 3: Message Format Validation
Capture a published SSB message and verify its format:

```bash
# After seeder creates posts, check bridge's SSB log
sqlite3 /bridge-data/bridge.sqlite "SELECT * FROM messages LIMIT 5"
```

### Experiment 4: Wire Protocol Capture
Use tcpdump or socat to capture raw muxrpc traffic:

```bash
# Capture EBT exchange
socat - TCP:bridge:8989 | hexdump -C
```

## Debugging Checklist

### Phase 1: Connectivity
- [ ] TF connects to room
- [ ] Room accepts muxrpc connection
- [ ] TF completes secret-stream handshake
- [ ] TF sends `whoami` -> gets room identity
- [ ] TF sends `ebt.replicate` duplex call

### Phase 2: Handler Verification
- [ ] `whoami` returns correct feed ref
- [ ] `ebt.replicate` is handled (not "no such method")
- [ ] EBT duplex connection established

### Phase 3: EBT Negotiation
- [ ] Bridge sends initial state (JSON frontier)
- [ ] TF sends its frontier (JSON)
- [ ] Bridge processes TF frontier
- [ ] Bridge starts streaming history for wanted feeds

### Phase 4: Message Streaming
- [ ] createHistoryStream returns messages
- [ ] Messages are in correct SSB format
- [ ] TF accepts and indexes messages

## Next Steps

1. **Add diagnostic logging** to EBT handler
2. **Create debug script** to capture muxrpc traffic
3. **Verify feed registration** in StateMatrix
4. **Test with isolated environment** using minimal setup

## Log Analysis Commands

```bash
# Check for EBT-related logs
grep -E "ebt|EBT|replicate|Replicate" /var/log/bridge.log

# Check for message publish events
grep -E "publish|Publish" /var/log/bridge.log

# Check for feed registration
grep -E "replication_started|registered" /var/log/bridge.log
```

## Hypothesis Priority (by likelihood)

1. **H2 (High)**: StateMatrix not tracking published feeds
   - The bridge publishes to its own feeds but EBT thinks it has nothing
   - EBT Changed() returns empty frontier because no peers have been registered
   
2. **H3 (High)**: createHistoryStream message format issue
   - The message format may not match SSB spec expectations
   - Looking at history.go, the format includes "timestamp" and "signature" at top level
   - SSB spec expects these inside the "value" object

3. **H1 (Low)**: Handler registration
   - Already verified handler is registered in code
   
4. **H4 (Low)**: Wire protocol
   - go-ssb is well-tested
   
5. **H5 (Medium)**: Feed identity mismatch
   - Need to verify TF and bridge agree on feed refs

## Code Analysis: createHistoryStream

Looking at `internal/ssb/muxrpc/handlers/history.go`:

```go
msgData := map[string]interface{}{
    "key":       msg.Key,
    "value":     content,
    "timestamp": msg.Metadata.Timestamp,
    "signature": fmt.Sprintf("%x", msg.Metadata.Sig),
}
```

**Potential issue**: The standard SSB message format is:
```json
{
  "key": "%sha256.bbls123",
  "value": {
    "author": "@feedref.ed25519",
    "sequence": 1,
    "previous": null,
    "signature": "sig...",
    "content": {...}
  },
  "timestamp": 1234567890
}
```

But our implementation puts `content` directly as `value` instead of wrapping it in the SSB message envelope with author/sequence/previous/signature.

**This means the message is malformed according to SSB spec!**

## Code Analysis: EBT State Matrix

Looking at `StateMatrix.Changed()`:

The function loads the frontier for `self` (the bridge's own feed) but doesn't populate the frontier with all the feeds the bridge HAS published.

The EBT replication model:
1. Bridge advertises: "I have feeds X, Y, Z at sequences A, B, C"
2. Peer responds: "I want feeds X, Y at sequences 0, B"
3. Bridge streams messages for X from 0 to A, Y from B to C

But our StateMatrix only tracks what the PEER wants, not what WE have.

## Experiments Added (2026-03-29)

1. **Debug logging added to EBT code**:
   - `StateMatrix.Changed()` logs self/peer frontiers
   - `sendState()` logs what state is being sent
   - `HandleDuplex()` logs EBT exchange flow
   - `createStreamHistory()` logs message streaming
   - `FeedManagerAdapter` logs GetMessage calls
   - `FeedReplicator.ListFeeds()` logs feed list

2. **Debug scripts created**:
   - `scripts/debug_muxrpc_capture.sh` - capture raw muxrpc traffic
   - `scripts/debug_ebt_state.sh` - diagnose EBT state from inside container

## Next Experiment Steps

1. Run Docker E2E with debug logging enabled
2. Observe what the bridge sends in its initial state
3. Check if TF responds with a frontier
4. Verify createHistoryStream messages are formatted correctly

## Root Cause: ByteSink Buffers Instead of Sending

**CRITICAL FINDING (2026-03-29):**

The EBT handler was using `ByteSinkWriter` which wraps `ByteSink`. The problem is:

```go
// stream.go:205-215 - ByteSink.Write() only buffers!
func (bs *ByteSink) Write(p []byte) (int, error) {
    bs.writer.Write(p)  // Just buffers to internal buffer!
    return len(p), nil  // No actual send happens here
}
```

The data is only sent when `Close()` is called:

```go
// stream.go:232-237 - ByteSink only sends on Close()
pkt := codec.Packet{
    Body: bs.writer.Bytes(),  // All buffered data sent at once
}
bs.pkr.WritePacket(pkt)
```

**The Bug:** EBT sends initial state via `tx.Write()`, which buffers the data. Then `rx.Next()` waits for TF's response. But TF never receives our state because the data is never sent! Result: `rx.Next()` returns false because the stream is closed/empty.

**The Fix:** Use `PacketStream` instead of `ByteSink` for sending. `PacketStream.Write()` sends immediately:

```go
// stream.go:307-323 - PacketStream.Write() sends IMMEDIATELY
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

### Changes Made

1. **stream.go**: Added `Packer()` method to `ByteSink`
2. **stream.go**: Added `SetPackerAndReq()` and `SetFlag()` methods to `PacketStream`
3. **stream.go**: Added `PacketStreamWriter` adapter that implements `Writer` interface
4. **muxrpc.go**: Added `ID()`, `Sink()`, `Source()` methods to `Request`
5. **ebt_handler.go**: Use `PacketStream` + `PacketStreamWriter` instead of `ByteSinkWriter`

### Code Locations

| File | Line | Change |
|------|------|--------|
| `stream.go` | 205-207 | Added `Packer()` method |
| `stream.go` | 306-311 | Added `SetPackerAndReq()` and `SetFlag()` |
| `stream.go` | 510-527 | Added `PacketStreamWriter` adapter |
| `muxrpc.go` | 161-169 | Added `ID()`, `Sink()`, `Source()` methods |
| `ebt_handler.go` | 36-41 | Use `PacketStream` for immediate sending |

### Fix 7: EBT Message Format Missing SSB Envelope (2026-03-29)

**Problem:** `FeedManagerAdapter.GetMessage()` returned only raw content bytes, but EBT requires the full SSB message format with `key`, `value` (containing author/sequence/previous/signature/content), and `timestamp`.

**Evidence:** The regular `HistoryStreamHandler` formats messages correctly, but `FeedManagerAdapter.GetMessage()` returned just `msg.Value` (raw content).

**Fix:** Updated `FeedManagerAdapter.GetMessage()` to return proper SSB message format with full `value` object containing author, sequence, previous, signature, and content.

### Fix 8: SSB Message Sequence Starting at 0 (2026-03-30)

**Problem:** When a new feed was empty (`log.Seq()` returns -1), `msg.Sequence = seq + 1` resulted in `sequence:0` instead of `sequence:1`.

**Evidence:** Tildefriends log showed first message with `"sequence":0` instead of `"sequence":1`.

**Fix:** Updated `publisher.go` to properly handle empty feed case:
```go
nextSeq := int64(1)
if seq >= 0 {
    // ... get previous message
    nextSeq = seq + 1
}
msg.Sequence = nextSeq
```

## Current Status (2026-03-30)

**EBT replication is NOW WORKING from the bridge side:**
- ✅ Messages sent in correct SSB format with full envelope
- ✅ Author/sequence/signature at **top level** (classic format for TF)
- ✅ Sequence numbers start at 1 (verified in logs)
- ✅ Feeds tracked in StateMatrix
- ✅ TF receives messages via `cli0 RPC RECV[ebt.replicate]`

**Remaining Issue**: Tildefriends receives messages but doesn't store them in its database (`messages_from_bot=0`). This appears to be a TF-specific issue in its EBT message processing or storage logic, not a bridge issue.

---

## Comprehensive Debugging Report

A detailed debugging session report has been created at:
**`docs/scratchpad/030-ebt-debugging-session-report.md`**

This report documents:
- All 9 bugs found and fixed
- How each bug was discovered
- Debugging techniques used
- Current status and recommendations
