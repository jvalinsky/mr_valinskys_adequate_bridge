# Phase 2: Debug Signature Verification Failures - Detailed Plan

**Date**: 2026-04-15
**Parent Plan**: `docs/plans/2026-04-15-ssb-signature-compliance-fix.md`
**Deciduous Node**: #1196 (Action: Debug actual signature failure)
**Status**: In Progress

## Problem Statement

After removing the non-standard HMAC feature (Phase 1), the root cause of signature verification failures in scuttlego/go-ssb remains unknown. The error pattern:

```
error="verification failed: ssb Verify(@BkmgP1GVrlj7QvCmazOTbzS+8Y+C9Hxi4TEuXkRudSY=.ed25519:1): invalid signature"
```

All 82+ messages from this bridged feed are rejected with `messages_persisted=0` in scuttlego logs.

## Hypotheses to Test

### H1: Canonical JSON Formatting Mismatch

**Why**: SSB uses a specific canonical JSON format (ECMA-262 compliant). Any deviation could cause verification failures.

**SSB Canonical JSON Rules** (per Protocol Guide):
- 2-space indentation (`"  "`)
- `\n` newlines (LF only, no CRLF)
- No trailing newline after closing `}`
- Dictionary keys sorted alphabetically
- One space after `:` in key-value pairs
- No trailing commas

**Test Plan**:
- [ ] Compare `marshalForSigning()` output byte-by-byte against go-ssb's equivalent
- [ ] Create test with known-good SSB message, verify formatting matches
- [ ] Check edge cases: null previous, special characters in content

**Files**:
- Bridge: `internal/ssb/message/legacy/sign.go:132-168` (`marshalForSigning()`)
- go-ssb: `refs.go` or `message.go` in `github.com/ssbc/go-ssb`

### H2: Key Derivation Discrepancy

**Why**: The bridge derives Ed25519 keypairs from `HMAC-SHA256(masterSeed, did)`. If this doesn't match what go-ssb expects, signatures will fail.

**Bridge Key Derivation** (`publisher.go:162-176`):
```go
mac := hmac.New(sha256.New, masterSeed)
mac.Write([]byte(did))
seed := mac.Sum(nil)
kp := keys.FromSeed(*(*[32]byte)(seed[:32]))
```

**Test Plan**:
- [ ] Verify the derived keypair matches expected feed ID
- [ ] Compare against known test vectors (if available)
- [ ] Check if go-ssb uses same seed derivation method
- [ ] Verify Ed25519 key generation is standard `nacl.sign.keyPair.fromSeed`

**Potential Issue**: Bridge derives from DID, but go-ssb might expect different derivation.

### H3: Signature Format Mismatch

**Why**: Ed25519 signatures should be 64 bytes. But how they're encoded in the message could differ.

**SSB Signature Format** (per Protocol Guide):
```
signature = base64(sig_bytes) + ".sig.ed25519"
```

**Test Plan**:
- [ ] Verify signature is raw 64-byte Ed25519 signature
- [ ] Check encoding matches go-ssb (base64.StdEncoding, no padding issues)
- [ ] Verify signature placement in JSON is correct

**Bridge Code**: `sign.go:206-208`:
```go
buf.WriteString(`  "signature": "`)
buf.WriteString(base64.StdEncoding.EncodeToString(sig))
buf.WriteString(`.sig.ed25519"` + "\n")
```

### H4: Message ID (Hash) Computation

**Why**: The message ID is a SHA-256 hash of the full signed message. If computed wrong, message references break.

**Test Plan**:
- [ ] Verify `HashMessage()` uses SHA-256
- [ ] Verify it hashes the exact bytes of the signed message
- [ ] Compare against go-ssb's hash computation

**Bridge Code**: `sign.go:116`:
```go
msgRef, err := refs.NewMessageRef(HashMessage(msgToHash), refs.RefAlgoMessageSSB1)
```

### H5: Field Order in JSON

**Why**: JSON field order MUST be deterministic for signature verification.

**Required Order** (per SSB Protocol Guide):
```
previous, author, sequence, timestamp, hash, content, signature
```

**Test Plan**:
- [ ] Compare field order against go-ssb message format
- [ ] Verify bridge's `marshalForSigning()` and `marshalWithSignature()` use same order

**Bridge Code**: Already verified - uses correct order:
```go
// marshalForSigning fields: previous, author, sequence, timestamp, hash, content
// marshalWithSignature fields: previous, author, sequence, timestamp, hash, content, signature
```

### H6: Content Encoding

**Why**: The `content` field must be properly encoded JSON. Nested objects, arrays, strings all affect the signature.

**Test Plan**:
- [ ] Compare content encoding against go-ssb
- [ ] Check `marshalLegacyContent()` behavior
- [ ] Verify indentation is correct (2 spaces)
- [ ] Check special character escaping

## Implementation Plan

### Step 2.1: Create Comparison Test [ ]

Create test file `internal/ssb/message/legacy/sign_compat_test.go`:

```go
package legacy_test

import (
    "testing"
    "github.com/ssbc/go-ssb/refs"
    // ... imports
)

func TestSignCompatWithGoSSB(t *testing.T) {
    // Use same seed/keypair
    seed := [32]byte{/* test seed */}
    content := map[string]interface{}{
        "type": "post",
        "text": "Hello, world!",
    }

    // Sign with bridge implementation
    bridgeMsg := &legacy.Message{...}
    bridgeRef, bridgeSig, _ := bridgeMsg.Sign(keypair)

    // Sign with go-ssb reference (if possible to construct)
    // Or compare against known-good signed message

    // Compare signature bytes
    // Compare message ID
}
```

### Step 2.2: Add Debug Dump Function [ ]

Add to `sign.go`:

```go
func (m *Message) DebugDump() string {
    contentToSign, _ := m.marshalForSigning()
    return fmt.Sprintf(
        "Author: %s\nSequence: %d\nTimestamp: %d\nContentToSign (hex):\n%x\nContentToSign (len=%d)",
        m.Author.String(),
        m.Sequence,
        m.Timestamp,
        contentToSign,
        len(contentToSign),
    )
}
```

### Step 2.3: Instrument Scuttlego Verification [ ]

If we can modify scuttlego for debugging:
- Add logging showing exact bytes being verified
- Log the public key, signature, and content
- Compare against what the bridge sends

### Step 2.4: Create Known-Good Test Vector [ ]

From a working SSB client (e.g., patchwork, go-ssb test):
1. Extract a known-good message with signature
2. Parse the JSON
3. Verify with bridge's verification code
4. If fails, identify the mismatch

### Step 2.5: Comparison Script [ ]

Create `Tools/signature_compare.ts` (Deno):

```typescript
// Compare bridge-signed message against go-ssb verification
// Read from SSB repo, verify each message
// Report any that fail verification
```

## Expected Outcomes

| Hypothesis | Status | Next Action |
|------------|--------|-------------|
| H1: JSON Formatting | Pending | Byte-by-byte comparison |
| H2: Key Derivation | Pending | Test vector verification |
| H3: Signature Format | Pending | Encoding verification |
| H4: Message ID Hash | Pending | Hash computation check |
| H5: Field Order | Low Risk | Already verified |
| H6: Content Encoding | Pending | Nested content testing |

## Files to Create/Modify

| File | Purpose |
|------|---------|
| `internal/ssb/message/legacy/sign_compat_test.go` | Comparison tests |
| `internal/ssb/message/legacy/sign.go` | Add DebugDump() |
| `docs/scratchpad/046b-signature-debug-analysis.md` | Analysis findings |
| `Tools/signature_compare.ts` | Comparison script |

## Success Criteria

- [ ] Identify exact cause of signature verification failure
- [ ] Create test demonstrating the issue
- [ ] Fix identified issue(s)
- [ ] All bridge-signed messages verify in go-ssb/scuttlego
- [ ] Add regression test to prevent future issues

## References

- SSB Protocol Guide: https://ssbc.github.io/scuttlebutt-protocol-guide/
- go-ssb: https://github.com/ssbc/go-ssb
- Known-good messages: Extract from existing SSB repos or test fixtures

## Progress Log

- [x] Phase 1 complete (HMAC removed)
- [ ] Phase 2.1: Comparison test
- [ ] Phase 2.2: Debug dump function
- [ ] Phase 2.3: Scuttlego instrumentation
- [ ] Phase 2.4: Known-good test vector
- [ ] Phase 2.5: Comparison script
