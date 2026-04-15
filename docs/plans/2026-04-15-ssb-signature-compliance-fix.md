# SSB Signature Compliance Fix Plan

**Date**: 2026-04-15
**Context**: Signature verification failures for bridged SSB feeds in Planetary iOS (scuttlego/go-ssb)
**Related**: `@BkmgP1GVrlj7QvCmazOTbzS+8Y+C9Hxi4TEuXkRudSY=.ed25519` feed

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

## Problem Statement

Bridged SSB messages are failing signature verification in standard SSB clients (scuttlego/go-ssb, Planetary):

```
error="verification failed: ssb Verify(@BkmgP1GVrlj7QvCmazOTbzS+8Y+C9Hxi4TEuXkRudSY=.ed25519:1): invalid signature"
```

Per the [SSB Protocol Guide](https://ssbc.github.io/scuttlebutt-protocol-guide/), message signing MUST NOT use HMAC. The signature is:

```
signature = nacl_sign_detached(
  msg: formatted_json_message,
  key: authors_longterm_sk
)
```

The bridge has a **non-standard `--hmac-key` feature** that, if used, would break protocol compliance by signing `SHA256(hmacKey || msg)` instead of just `msg`.

## Investigation Results

### Code Audit

| File | Lines | Issue |
|------|-------|-------|
| `internal/ssb/message/legacy/sign.go` | 92-105 | Non-standard HMAC signing: `if hmacKey != nil { h.Write(hmacKey); h.Write(data); data = h.Sum(nil) }` |
| `internal/ssb/publisher/publisher.go` | 34-38, 68, 106 | Passes `hmacKey` to `msg.Sign()`, stores in Publisher struct |
| `internal/bots/manager.go` | 22, 26, 50-55 | Receives `hmacKey` from runtime, passes to `publisher.WithHMAC()` |
| `internal/ssbruntime/runtime.go` | 82-84, 125 | If `cfg.HMACKey != nil`, passes to Manager (also overrides appKey) |
| `cmd/bridge-cli/config_factory.go` | 21, 35 | Parses `--hmac-key` CLI flag |
| `cmd/bridge-cli/main.go` | 144 | Defines `--hmac-key` flag |

### Current HMAC Usage Patterns

**Two distinct HMAC concepts conflated:**

1. **Secret Handshake Network Identifier** (CORRECT):
   - `DefaultAppKey` in `secretstream.go:29-34` = standard SSB network ID `d4a1cb88...9ffb`
   - Used via `nacl_auth(ephemeral_pk, network_identifier)` during handshake
   - Controlled by `--app-key` flag (defaults to `"boxstream"` → DefaultAppKey)

2. **Message Signing HMAC** (NON-STANDARD):
   - `--hmac-key` flag in bridge-cli
   - Signs `SHA256(hmacKey || msg)` instead of raw `msg`
   - **VIOLATES SSB PROTOCOL SPEC** - no HMAC in message signing

### Scripts Don't Use HMAC

All scripts (`setup_live_bridge.sh`, E2E scripts, etc.) use `--bot-seed` but NOT `--hmac-key`:

```bash
# From setup_live_bridge.sh:294
bridge_cli --db "$DB_PATH" --bot-seed "$BOT_SEED" backfill
```

So HMAC should NOT be the immediate cause of signature failures... but the non-standard codepath exists and could be accidentally triggered.

## Two Distinct Issues

### Issue A: `.ggfeed-v1` Feed Format Not Recognized

**Not a bridge bug** - scuttlego doesn't recognize `.ggfeed-v1` suffix:

```
error="could not create a feed ref: invalid suffix" contact="@ydiiK1WtmWLbCPC1DpZXC57QyAZFM7JeOP9oFxpgVuQ=.ggfeed-v1"
```

**Root cause**: Gabby Grove spec defines `.ggfeed-v1` suffix, but scuttlego only recognizes `gabbygrove-v1`.

**Resolution**: Upstream fix in scuttlego to accept both suffixes.

### Issue B: Bridged Feed Signature Verification Failure (MAIN ISSUE)

**Symptoms**:
- All 82+ messages from `@BkmgP1GVrlj7QvCmazOTbzS+8Y+C9Hxi4TEuXkRudSY=.ed25519` rejected
- `messages_persisted=0` in scuttlego logs
- Cascades to `unknownAuthor` errors in Planetary iOS

**Root cause**: Unknown - requires further investigation

## Implementation Plan

### Phase 1: Remove Non-Standard HMAC Feature [x] [COMPLIANCE]

**Why**: HMAC has no place in SSB message signing per protocol spec. Even if not currently used, the feature:
- Violates SSB protocol compliance
- Creates confusion with secret handshake network identifier
- Could be accidentally triggered

#### Step 1.1: Deprecate `--hmac-key` CLI flag
- [x] ~~Add deprecation warning in `cmd/bridge-cli/main.go` for `--hmac-key`~~ **REMOVED** (flag deleted)
- [x] ~~Log warning: "hmac-key is deprecated and will be removed; SSB message signing does not use HMAC"~~ **REMOVED** (flag deleted)
- [ ] Document in CLAUDE.md and README

#### Step 1.2: Remove HMAC from signing path
- [x] Modify `internal/ssb/message/legacy/sign.go:Sign()` to:
  ```go
  func (m *Message) Sign(kp *keys.KeyPair) (*refs.MessageRef, []byte, error) {
      // HMAC removed - SSB protocol does not use HMAC for message signing
      contentToSign, err := m.marshalForSigning()
      if err != nil {
          return nil, nil, err
      }
      sig := ed25519.Sign(kp.Private(), contentToSign)
      // ...
  }
  ```

#### Step 1.3: Remove HMAC from Publisher
- [x] Remove `hmacKey` field from `Publisher` struct
- [x] Remove `WithHMAC()` option
- [x] Remove `HMACKey` from `Options`

#### Step 1.4: Remove HMAC from Manager
- [x] Remove `hmacKey` field from `Manager` struct
- [x] Update `NewManager()` signature (breaking change)
- [x] Remove HMAC passing to `publisher.New()`

#### Step 1.5: Remove HMAC from Runtime
- [x] Remove `HMACKey` from `Config` struct
- [x] Remove lines 82-84 that override appKey with HMACKey
- [x] Update `NewManager()` call

#### Step 1.6: Remove HMAC from CLI
- [x] Remove `--hmac-key` flag definitions
- [x] Remove `parseHMACKey()` function (kept for backwards compat with existing tests)
- [x] Remove `HMACKey` from `AppConfig` struct

#### Step 1.7: Update tests
- [x] Remove HMAC-related test cases
- [x] Update any tests using `WithHMAC()` option

**All tests pass after Phase 1 changes.**

### Phase 2: Debug Actual Signature Failure [ ]

**Why**: HMAC isn't being used in scripts, so there must be another cause.

#### Step 2.1: Add debug logging to signing
- [ ] In `sign.go:Sign()`, log the exact bytes being signed (hex dump)
- [ ] Log the exact signature produced
- [ ] Log the keypair public key

#### Step 2.2: Create isolated test case
- [ ] Write test that:
  1. Derives keypair from known seed + DID
  2. Signs a test message
  3. Verifies with standard go-ssb verification code
  4. Compares against reference implementation (go-ssb)

#### Step 2.3: Compare against go-ssb signing
- [ ] Clone go-ssb reference implementation
- [ ] Run same message through go-ssb's signing path
- [ ] Compare output byte-for-byte

#### Step 2.4: Verify canonical JSON formatting
- [ ] Compare `marshalForSigning()` output against ECMA-262 spec
- [ ] Test against known good messages from other SSB clients
- [ ] Verify: 2-space indentation, `\n` newlines, no trailing newline

#### Step 2.5: Verify key derivation
- [ ] Check `DeriveKeyPair()` in `publisher.go:162-176`
- [ ] Verify HMAC-SHA256(masterSeed, did) produces correct seed
- [ ] Compare against go-ssb key derivation (if any)

### Phase 3: Add Compliance Test Suite [ ]

**Why**: Prevent future protocol divergence.

#### Step 3.1: Create SSB protocol compliance tests
- [ ] Test: signature verification against known-good messages
- [ ] Test: canonical JSON formatting matches ECMA-262
- [ ] Test: message ID computation (sha256 of signed message)
- [ ] Test: feed reference parsing (all valid suffixes)

#### Step 3.2: Add integration test with go-ssb
- [ ] Run bridge's signed messages through go-ssb verification
- [ ] Ensure 100% pass rate before merging

### Phase 4: Documentation [ ]

#### Step 4.1: Document SSB protocol compliance
- [ ] Add CLAUDE.md section on SSB protocol compliance
- [ ] Link to official spec: https://ssbc.github.io/scuttlebutt-protocol-guide/
- [ ] Document: "HMAC is ONLY for secret handshake network identifier, NOT message signing"

#### Step 4.2: Document key derivation
- [ ] Explain bridge's key derivation: `HMAC-SHA256(masterSeed, atDID)`
- [ ] Clarify this is for deterministic key generation, not protocol-level HMAC

## Files to Modify

| File | Changes |
|------|---------|
| `internal/ssb/message/legacy/sign.go` | Remove HMAC, add debug logging |
| `internal/ssb/publisher/publisher.go` | Remove `hmacKey` field, `WithHMAC()` option |
| `internal/bots/manager.go` | Remove `hmacKey` field, update `NewManager()` |
| `internal/ssbruntime/runtime.go` | Remove `HMACKey` from Config, remove appKey override |
| `cmd/bridge-cli/main.go` | Remove/deprecate `--hmac-key` flag |
| `cmd/bridge-cli/config_factory.go` | Remove `parseHMACKey()`, `HMACKey` field |
| `cmd/bridge-cli/app.go` | Remove `HMACKey` references |

## Breaking Changes

1. `bots.NewManager()` signature change (removes `hmacKey` parameter)
2. `publisher.New()` no longer accepts `WithHMAC()` option
3. `--hmac-key` CLI flag removed

## Success Criteria

- [ ] All bridge-signed messages verify in go-ssb/scuttlego
- [ ] All existing tests pass
- [ ] No HMAC-related code remains in signing path
- [ ] Protocol compliance tests added and passing
- [ ] Documentation updated

## References

- SSB Protocol Guide: https://ssbc.github.io/scuttlebutt-protocol-guide/
- Message Signing Section: "Signature" - `nacl_sign_detached(msg: formatted_json_message, key: authors_longterm_sk)`
- Secret Handshake: Network identifier `d4a1cb88a66f02f8db635ce26441cc5dac1b08420ceaac230839b755845a9ffb`
- go-ssb reference: https://github.com/ssbc/go-ssb
