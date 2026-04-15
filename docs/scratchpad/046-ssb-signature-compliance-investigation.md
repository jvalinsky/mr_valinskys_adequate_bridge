# Track 046: SSB Signature Compliance Investigation

**Date**: 2026-04-15
**Context**: Investigating signature verification failures for bridged SSB feeds in Planetary iOS (scuttlego/go-ssb)
**Deciduous Node**: #1189 (Goal: Fix SSB Signature Compliance)

## User Request

> Investigate why signature verification is failing for a specific bridged SSB identity (`@BkmgP1GVrlj7QvCmazOTbzS+8Y+C9Hxi4TEuXkRudSY=.ed25519`) in Planetary Social's iOS client logs.

## Investigation Summary

Spawned 4 parallel subagents to investigate:
1. Bridge signing code analysis
2. Planetary iOS/scuttlego verification logic
3. SSB protocol specification research
4. Live bridge configuration analysis

### Key Findings

#### Issue A: `.ggfeed-v1` Feed Format Not Recognized

**Not a bridge bug** - scuttlego doesn't recognize `.ggfeed-v1` suffix:

```
error="could not create a feed ref: invalid suffix" contact="@ydiiK1WtmWLbCPC1DpZXC57QyAZFM7JeOP9oFxpgVuQ=.ggfeed-v1"
```

**Root cause**: Gabby Grove spec defines `.ggfeed-v1` suffix, but scuttlego only recognizes `gabbygrove-v1`.

**Resolution**: Upstream fix needed in scuttlego.

#### Issue B: Bridged Feed Signature Verification Failure (MAIN ISSUE)

**Symptoms**:
- All 82+ messages from `@BkmgP1GVrlj7QvCmazOTbzS+8Y+C9Hxi4TEuXkRudSY=.ed25519` rejected
- `messages_persisted=0` in scuttlego logs
- Cascades to `unknownAuthor` errors in Planetary iOS

**Root Cause Hypothesis**: HMAC key mismatch (initially suspected, but ruled out)

### SSB Protocol Guide Reference

Per the [SSB Protocol Guide](https://ssbc.github.io/scuttlebutt-protocol-guide/):

**HMAC is ONLY for secret handshake, NOT message signing:**

```
Network identifier: d4a1cb88a66f02f8db635ce26441cc5dac1b08420ceaac230839b755845a9ffb

nacl_auth(
  msg: ephemeral_pk,
  key: network_identifier
)
```

**Message signing has NO HMAC:**

```
signature = nacl_sign_detached(
  msg: formatted_json_message,
  key: authors_longterm_sk
)
```

### Bridge HMAC Feature Audit

Found non-standard `--hmac-key` feature that violates SSB protocol:

| File | Lines | Issue |
|------|-------|-------|
| `internal/ssb/message/legacy/sign.go` | 92-105 | Signs `SHA256(hmacKey \|\| msg)` instead of raw `msg` |
| `internal/ssb/publisher/publisher.go` | 34-38, 68, 106 | Passes `hmacKey` to signing |
| `internal/bots/manager.go` | 22, 26, 50-55 | Stores and passes `hmacKey` |
| `internal/ssbruntime/runtime.go` | 82-84, 125 | Propagates `hmacKey` from CLI |
| `cmd/bridge-cli/main.go` | 144 | Defines `--hmac-key` flag |

**However**: Scripts don't use `--hmac-key`, only `--bot-seed`. So HMAC isn't the immediate cause, but the non-standard codepath must be removed for compliance.

### Standard AppKey (Network Identifier)

Found correct implementation in `secretstream.go:29-34`:

```go
// Standard SSB public network identifier (Base64: 1KHLiKZvAvjbY1ziZEHMXawbCEIM6qwjCDm3VYRan/s=)
var DefaultAppKey = AppKey{
    0xd4, 0xa1, 0xcb, 0x88, 0xa6, 0x6f, 0x02, 0xf8,
    0xdb, 0x63, 0x5c, 0xe2, 0x64, 0x41, 0xcc, 0x5d,
    0xac, 0x1b, 0x08, 0x42, 0x0c, 0xea, 0xac, 0x23,
    0x08, 0x39, 0xb7, 0x55, 0x84, 0x5a, 0x9f, 0xfb,
}
```

This matches the protocol guide's network identifier exactly.

## Deciduous Decision Graph

**Nodes created:**
- `#1189` - Goal: Fix SSB Signature Compliance
- `#1190` - Decision: Remove non-standard HMAC from message signing
- `#1191` - Outcome: Two distinct issues found
- `#1192` - Outcome: HMAC not being used in production scripts
- `#1193` - Action: Remove HMAC from sign.go
- `#1194` - Action: Remove HMAC from publisher and manager
- `#1195` - Action: Remove --hmac-key CLI flag
- `#1196` - Action: Debug actual signature failure
- `#1197` - Reference: SSB Protocol Guide
- `#1198` - Reference: Network identifier: d4a1cb88...9ffb

## Plan Created

Detailed implementation plan at `docs/plans/2026-04-15-ssb-signature-compliance-fix.md`:

### Phase 1: Remove Non-Standard HMAC Feature
- Deprecate `--hmac-key` CLI flag
- Remove HMAC from signing path
- Remove HMAC from Publisher and Manager
- Remove HMAC from Runtime config
- Update tests

### Phase 2: Debug Actual Signature Failure
- Add debug logging to signing
- Create isolated test case
- Compare against go-ssb signing
- Verify canonical JSON formatting
- Verify key derivation

### Phase 3: Add Compliance Test Suite
- SSB protocol compliance tests
- Integration test with go-ssb

### Phase 4: Documentation
- Document SSB protocol compliance
- Document key derivation

## Next Steps

1. Execute Phase 1: Remove HMAC from codebase
2. Execute Phase 2: Debug actual signature failure cause
3. Execute Phase 3: Add compliance tests
4. Execute Phase 4: Update documentation

## References

- SSB Protocol Guide: https://ssbc.github.io/scuttlebutt-protocol-guide/
- Gabby Grove Spec: https://github.com/ssbc/ssb-spec-drafts/blob/master/drafts/draft-ssb-core-gabbygrove/00/draft-ssb-core-gabbygrove-00.md
- go-ssb: https://github.com/ssbc/go-ssb

## Log Paths

- `/logs/8FA2460B-CE2E-48D5-9654-256AA1FCD6FA/gobot-2026-04-15_03-17.log`: Scuttlego logs with verification errors
- Plan file: `docs/plans/2026-04-15-ssb-signature-compliance-fix.md`

## Phase 1 Completion

**Date**: 2026-04-15
**Commit**: `b1ffa7c`

All HMAC-related code removed:
- `sign.go`: Removed hmacKey parameter from Sign()
- `publisher.go`: Removed WithHMAC() option, hmacKey field
- `manager.go`: Removed hmacKey field, updated NewManager() signature
- `runtime.go`: Removed HMACKey from Config
- `main.go`: Removed --hmac-key flag (3 occurrences)
- `config_factory.go`: Removed HMACKey parsing
- `app.go`: Removed HMACKey from AppConfig

**Tests**: All 28 test packages pass after changes.

## Phase 2 Completion

**Date**: 2026-04-15

### Findings Summary

After comprehensive testing against go-ssb reference implementation:

| Component | Status | Notes |
|-----------|--------|-------|
| Key derivation | ✅ Compatible | Both use `ed25519.NewKeyFromSeed()` |
| Field order | ✅ Compatible | Bridge matches go-ssb expected order |
| V8 encoding | ✅ Compatible | Identical output for ASCII, BMP, emojis |
| Signature algorithm | ✅ Compatible | Both use `crypto/ed25519.Sign()` |
| Message hashing | ✅ Compatible | SHA256 over V8-encoded signed message |
| String escaping | ✅ Compatible | ECMA-262 compliant |

### Conclusion

**The bridge's signing implementation is correct and compatible with go-ssb.**

The signature verification failures observed in scuttlego must have a different root cause, possibly:

1. **Message corruption in transmission** - Messages modified between signing and verification
2. **Storage serialization issue** - Messages stored differently than sent
3. **Version mismatch** - Scuttlego version expecting different format than tested go-ssb
4. **Feed format issue** - The `.ggfeed-v1` suffix issue (separate problem)

### Files Added

- `internal/ssb/message/legacy/sign_compat_test.go` - Compatibility tests
- `internal/ssb/message/legacy/signature_compat_test.go` - Comprehensive comparison tests
- `docs/scratchpad/046b-signature-debug-analysis.md` - Analysis findings
- `docs/plans/2026-04-15-phase2-signature-debug.md` - Phase 2 plan with findings

### Next Steps

1. Extract actual failing message from scuttlego logs
2. Compare byte-by-byte with what bridge sent
3. Verify with scuttlego directly using actual message content
