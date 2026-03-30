# EBT Replication Debugging & SSB Protocol

EBT (Epidemic Broadcast Trees) is the replication protocol used by Secure Scuttlebutt for efficient peer-to-peer message synchronization. This document covers the debugging of EBT replication between the bridge's embedded SSB node and Tildefriends (a Go server that bridges SSB to Nostr).

## Executive Summary

**Problem:** Tildefriends connects to the bridge room and initiates EBT replication, but bridged ATProto messages are never indexed (`messages_from_bot=0`).

**Solution:** 9+ bugs were identified and fixed in the bridge's SSB/EBT implementation, primarily around message format compatibility with Tildefriends.

**Current Status:** Bridge-side replication is working correctly. Tildefriends receives messages but has an outstanding issue with message storage/indexing.

## Architecture

```
ATProto Firehose
      │
      ▼
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Bridge    │────▶│  SSB Log    │────▶│  Room2      │
│  Processor  │     │  (margo)    │     │  Server     │
└─────────────┘     └─────────────┘     └─────────────┘
                                              │
                                              ▼
Tildefriends ◀───── MUXRPC/EBT ◀───── EBT Handler
    │
    ▼
SQLite (messages_from_bot=0 - not storing)
```

### EBT Replication Flow

```
1. Tildefriends connects to Room2 MUXRPC port
2. Tildefriends sends ebt.replicate duplex call
3. Bridge EBT handler receives call
4. Bridge sends initial state (frontier)
5. Tildefriends responds with its frontier
6. Bridge calls Changed() to compute differential
7. Bridge streams history for wanted feeds via createHistoryStream
8. FeedManagerAdapter.GetMessage() returns formatted messages
9. Tildefriends receives and verifies signatures
```

## Tildefriends Message Format Requirements

Tildefriends has specific requirements for SSB message format. These were reverse-engineered from `reference/tildefriends/src/ssb.c`.

### Field Ordering

Tildefriends reorders message fields to a specific sequence before signing and verification. From `ssb.c` lines 1184-1191:

```c
JSValue reordered = JS_NewObject(context);
JS_SetPropertyStr(context, reordered, "previous", ...);   // 1
JS_SetPropertyStr(context, reordered, "author", ...);     // 2
JS_SetPropertyStr(context, reordered, "sequence", ...);   // 3
JS_SetPropertyStr(context, reordered, "timestamp", ...); // 4
JS_SetPropertyStr(context, reordered, "hash", ...);      // 5
JS_SetPropertyStr(context, reordered, "content", ...);   // 6
JS_SetPropertyStr(context, reordered, "signature", ...); // 7
```

Tildefriends also tries an alternate ordering with `sequence` before `author` (lines 1204-1211), setting a `k_tf_ssb_message_flag_sequence_before_author` flag on success.

### What Gets Signed

Tildefriends signs the JSON **without** the signature field. From `ssb.c` lines 1107-1117:

```c
// Calculate message ID from full message
tf_ssb_calculate_message_id(context, val, out_id, out_id_size);

// DELETE the signature field
JSAtom sigatom = JS_NewAtom(context, "signature");
JS_DeleteProperty(context, val, sigatom, 0);

// Stringify WITHOUT signature
JSValue sigval = JS_JSONStringify(context, val, JS_NULL, JS_NewInt32(context, 2));
const char* sigstr = JS_ToCString(context, sigval);
```

### Signature Format

Tildefriends expects signatures in a specific format. From `ssb.c` lines 1119-1130 and 1988-1992:

```c
// Signature must end with ".sig.ed25519"
const char* sigkind = strstr(str, ".sig.ed25519");

// Base64 decode the portion BEFORE ".sig.ed25519"
uint8_t binsig[crypto_sign_BYTES];
r = tf_base64_decode(str, sigkind - str, binsig, sizeof(binsig));

// Verify using crypto_sign_verify_detached
r = crypto_sign_verify_detached(binsig, (const uint8_t*)sigstr, strlen(sigstr), publickey);
```

**Critical:** The signature must be base64-encoded (not hex) and end with `.sig.ed25519`.

### JSON Indentation

Tildefriends uses indent level 2 (two spaces) for JSON operations. From `ssb.c` lines 1117 and 1970:

```c
JSValue sigval = JS_JSONStringify(context, val, JS_NULL, JS_NewInt32(context, 2));
```

### Complete Message Structure

```json
{
  "previous": null,
  "author": "@{base64pubkey}.ed25519",
  "sequence": 1,
  "timestamp": 1234567890000,
  "hash": "sha256",
  "content": {...},
  "signature": "{base64sig}.sig.ed25519"
}
```

**Note:** The `key` field should NOT be included in EBT messages. Tildefriends computes the message ID by hashing the full JSON including `key`, then verifies the signature on JSON without `key`, causing verification failure.

## Bugs Found & Fixed

### Critical Protocol Bugs

| # | Issue | File | Fix |
|---|-------|------|-----|
| 1 | ByteSink buffering - data never sent | `internal/ssb/muxrpc/stream.go` | Switched to `PacketStream` for immediate sending |
| 2 | ByteSink.Packer() returns nil | `internal/ssb/muxrpc/stream.go` | Added `Writer()` method returning underlying `PacketWriter` |
| 3 | PacketStream encoding not set | `internal/ssb/sbot/ebt_handler.go` | Set `TypeJSON` encoding flag |
| 4 | Positive Req duplex responses not routed | `internal/ssb/muxrpc/muxrpc.go` | Added routing for positive Req numbers to existing duplex stream |
| 5 | Stream end detection wrong condition | `internal/ssb/muxrpc/muxrpc.go` | Changed `FlagEndErr && !FlagStream` to just `FlagEndErr` |
| 6 | Boxstream goodbye sentinel wrong | `internal/ssb/secretstream/boxstream/boxstream.go` | Changed to 18 zero bytes per spec |

### Message Format Bugs

| # | Issue | File | Fix |
|---|-------|------|-----|
| 7 | EBT message format - fields nested | `internal/ssb/sbot/feed_manager_adapter.go` | Changed to classic format with fields at top level |
| 8 | `key` field included in EBT output | `internal/ssb/sbot/feed_manager_adapter.go` | Removed `key` from EBT message output |
| 9 | Sequence starts at 0 | `internal/ssb/publisher/publisher.go` | Handle empty feed case: `nextSeq = max(1, seq + 1)` |
| 10 | JSON field ordering alphabetical | `internal/ssb/message/legacy/sign.go` | Changed to TF's order: previous, author, sequence, timestamp, hash, content |
| 11 | Signature format hex not base64 | `internal/ssb/message/legacy/sign.go` | Changed to base64 + `.sig.ed25519` suffix |

### Test Infrastructure Bugs

| # | Issue | File | Fix |
|---|-------|------|-----|
| 12 | SQL query checked wrong column | `infra/e2e-full/test_runner.sh` | Changed `published=1` to `message_state='published'` |
| 13 | atproto-seed double registration | `cmd/atproto-seed/main.go` | Only register when `targetDID == ""` |

### Signature Verification Bug

| # | Issue | File | Fix |
|---|-------|------|-----|
| 14 | Signature verification stub always true | `internal/ssb/message/legacy/message.go` | Replaced with real `ed25519.Verify()` |

## Key Code Locations

### EBT Replication

| File | Lines | Description |
|------|-------|-------------|
| `internal/ssb/sbot/ebt_handler.go` | — | EBT replicate handler registration and duplex handling |
| `internal/ssb/sbot/feed_manager_adapter.go` | 35-97 | `GetMessage()` returns classic-format SSB messages |
| `internal/ssb/replication/ebt.go` | — | `StateMatrix` tracking and `Changed()` diff computation |
| `internal/ssb/muxrpc/handlers/history.go` | — | `createHistoryStream` implementation |

### Message Signing

| File | Lines | Description |
|------|-------|-------------|
| `internal/ssb/message/legacy/sign.go` | 103-143 | `marshalForSigning()` with correct field ordering |
| `internal/ssb/message/legacy/message.go` | 93-98 | Real signature verification with `ed25519.Verify()` |

### Streaming

| File | Lines | Description |
|------|-------|-------------|
| `internal/ssb/muxrpc/stream.go` | 205-215 | `ByteSink.Write()` (buffering - use `PacketStream` instead) |
| `internal/ssb/muxrpc/stream.go` | 307-323 | `PacketStream.Write()` (immediate send) |
| `internal/ssb/muxrpc/muxrpc.go` | 383 | Stream end detection condition |

## Debugging Tools

### Docker E2E Infrastructure

| File | Purpose |
|------|---------|
| `infra/e2e-full/docker-compose.yml` | Service definitions (bridge, tildefriends, seeder, relay) |
| `infra/e2e-full/test_runner.sh` | Validates replication success |
| `infra/e2e-full/bridge_entrypoint.sh` | Bridge startup with firehose control |

### Debug Scripts

| File | Purpose |
|------|---------|
| `scripts/debug_ebt_state.sh` | Diagnose EBT state from inside Docker container |
| `scripts/debug_muxrpc_capture.sh` | Capture raw muxrpc traffic for wire analysis |

### Usage

```bash
# Run full E2E test with tildefriends
./scripts/e2e_tildefriends.sh

# Or directly with docker compose
docker compose -f infra/e2e-full/docker-compose.yml up --abort-on-container-exit

# Debug EBT state from inside the container
docker exec -it <container> /scripts/debug_ebt_state.sh

# Capture muxrpc traffic
docker exec -it <container> /scripts/debug_muxrpc_capture.sh
```

## Current Status

### Working

- [x] Bridge publishes ATProto messages to SSB feeds
- [x] EBT handler receives and processes `ebt.replicate` calls
- [x] State matrix tracks published feeds correctly
- [x] `createHistoryStream` returns formatted messages
- [x] Message format matches Tildefriends expectations (classic format)
- [x] Signature format is base64 + `.sig.ed25519`
- [x] JSON field ordering matches Tildefriends expectations
- [x] Tildefriends receives EBT messages

### Outstanding Issue

- [ ] Tildefriends receives messages but doesn't store them (`messages_from_bot=0`)

This appears to be a Tildefriends-side issue in its EBT message processing or storage logic, not a bridge issue. Tildefriends successfully receives and verifies messages but fails to persist them.

### Debugging Next Steps

1. Enable Tildefriends debug output to see verification results
2. Check Tildefriends logs for `verifying author=... success=1`
3. Investigate Tildefriends EBT storage path (`ssb.ebt.c`)

## Related Documentation

- [EBT debugging notes](scratchpad/022-ebt-replication-debugging.md)
- [Session report](scratchpad/030-ebt-debugging-session-report.md)
- [Signature fix plan](scratchpad/031-tildefriends-signature-fix-plan.md)
- [Protocol audit fixes](scratchpad/032-protocol-audit-fixes.md)
- [Tildefriends C source](../../reference/tildefriends/src/ssb.c)
- [Tildefriends EBT implementation](../../reference/tildefriends/src/ssb.ebt.c)
