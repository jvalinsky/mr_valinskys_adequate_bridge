# SSB Protocol Audit Fixes

**Date:** 2026-03-29
**Decision Graph Node:** 638 (goal), 642 (action)

---

## Root Cause Found

**TF receives EBT messages but doesn't store them** because our EBT messages included a `key` field that shouldn't be there.

When TF receives an EBT message, it:
1. Computes the message ID by hashing the FULL JSON (with all fields)
2. Deletes the `signature` field
3. Does `JSON.stringify(val, null, 2)` on the remainder
4. Verifies the ed25519 signature against those bytes

Our EBT messages included `"key": "%hash.sha256"` at the top level. This field was present during both the message ID computation AND the signature verification bytes, but it was NOT present when we originally signed the message. Result: signature verification fails.

**Fix:** Removed `key` from `feed_manager_adapter.go`'s EBT output. EBT messages should be raw message objects: `{previous, author, sequence, timestamp, hash, content, signature}`.

---

## Investigation Path

1. **Initial theory:** Message ID computed wrong (hashing without signature) -- WRONG
2. **Actual finding:** `Sign()` in `sign.go` correctly hashes the full signed message via `marshalWithSignature()`. The `ContentHash()` method is broken but unused in the publish path.
3. **Root cause:** `feed_manager_adapter.go` prepended `"key"` field to EBT messages, contaminating TF's signature verification

---

## All Fixes Applied

### Fix 1: Remove `key` field from EBT messages (ROOT CAUSE)
**File:** `internal/ssb/sbot/feed_manager_adapter.go`
Removed the `key` field from the JSON output. EBT messages are raw message objects.

### Fix 2: Real signature verification
**File:** `internal/ssb/message/legacy/message.go:93-98`
Replaced always-true stub with `ed25519.Verify(pubKey, content, sig)`.

### Fix 3: Boxstream goodbye sentinel
**File:** `internal/ssb/secretstream/boxstream/boxstream.go:18`
Changed from `"goodbye!" + zeros` to 18 zero bytes per spec.

### Fix 4: MuxRPC stream end detection
**File:** `internal/ssb/muxrpc/muxrpc.go:383`
Changed from `FlagEndErr && !FlagStream` to just `FlagEndErr`. Stream termination signals have both flags set.

### Fix 5: EBT state matrix peer tracking
**Files:** `internal/ssb/replication/ebt.go`, `internal/ssb/sbot/ebt_handler.go`
- Remote peer's frontier now stored under remote peer's FeedRef, not self
- `HandleDuplex` now receives `*refs.FeedRef` for the remote peer
- After updating remote frontier, calls `Changed(self, remote)` for correct diffs
- EBT handler wrapper extracts FeedRef from SHS connection's pubkey

---

## Remaining Known Issues (not fixed)

1. **CanonicalJSON sorts keys alphabetically** -- affects verification of received messages (not our own). `PrettyPrint` / `CanonicalJSON` sort alphabetically but SSB preserves insertion order. Only impacts receiving and verifying other peers' messages.

2. **V8Binary string escaping incomplete** -- Unicode handling not fully implemented. May cause issues with non-ASCII content.

3. **Duplex/Sink RPC client not implemented** -- Can't initiate outbound EBT or blob uploads. Server-side works via workaround.

4. **EBT Note round-trip lossy for Seq=-1** -- Minor edge case in marshaling.

---

## Test Results

All SSB unit tests pass after fixes:
- publisher, muxrpc/codec, boxstream, secretstream, refs, keys, muxrpc -- all OK
