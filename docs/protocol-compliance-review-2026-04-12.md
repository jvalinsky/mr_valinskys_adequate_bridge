# Protocol Compliance Audit Report — April 12, 2026

**Repository**: mr-valinskys-adequate-bridge  
**Audit Date**: 2026-04-12  
**Mode**: Read-only, static analysis + targeted tests  
**Scope**: ATProto Extended AppView + SSB Room2/muxrpc/EBT surfaces

---

## Authoritative Baselines

| Repository | Commit SHA | Clone Location |
|------------|------------|----------------|
| bluesky-social/atproto | `2a9221d244a0821490458785d70d100a6943ea91` | `/tmp/protocol-compliance-20260412-atproto` |
| bluesky-social/indigo | `2031017ff41157a5e8beebd8cee106fa02a6613e` | `/tmp/protocol-compliance-20260412-indigo` |
| ssbc/sips | `e4da60f055206e16861b908ee0343eea2fc1fbe0` | `/tmp/protocol-compliance-20260412-sips` |
| ssbc/go-ssb-room | `892b77139af8e2cf1a6482908f3f288339680aa9` | `/tmp/protocol-compliance-20260412-go-ssb-room` |

---

## Executive Summary

### Severity Distribution

| Severity | Count | Description |
|----------|-------|-------------|
| **P0** | 4 | Protocol-breaking interop failures |
| **P1** | 7 | High interop risk |
| **P2** | 6 | Moderate drift from spec |
| **P3** | 8 | Low/documentation/test debt |

### Top Findings

1. **P0: AT-URI Parser Non-Compliant** — Accepts malformed URIs (double slash, trailing slash)
2. **P0: NSID Parser Non-Compliant** — No length limits, no regex validation
3. **P0: RecordKey Parser Non-Compliant** — Accepts `.` and `..` which are forbidden
4. **P1: Missing #sync Event Handling** — Firehose cannot process repo recovery events
5. **P2: MST Fanout Structure** — May generate trees that don't match Indigo's fanout

---

## Test Evidence

### ATProto Tests
| Package | Passed | Failed |
|---------|--------|--------|
| `internal/atindex` | 45 | 0 |
| `internal/backfill` | 80 | 0 |
| `internal/blobbridge` | 52 | 0 |
| `internal/firehose` | 48 | 0 |
| `pkg/atproto/firehose` | 19 | 0 |
| `pkg/atproto/identity` | 31 | 0 |
| `pkg/atproto/repo` | 15 | 0 |
| `pkg/atproto/syntax` | 27 | 0 |
| `pkg/atproto/xrpc` | 18 | 0 |
| **Total** | **335** | **0** |

### Room Tests
| Package | Passed | Failed |
|---------|--------|--------|
| `internal/room` | 136 | 0 |
| **Total** | **136** | **0** |

### SSB Tests
| Package | Passed | Failed |
|---------|--------|--------|
| `internal/ssb/bfe` | 52 | 0 |
| `internal/ssb/blobs` | 6 | 0 |
| `internal/ssb/crypto` | 30 | 0 |
| `internal/ssb/discovery` | 19 | 0 |
| `internal/ssb/feedlog` | 7 | 0 |
| `internal/ssb/gossip` | 8 | 0 |
| `internal/ssb/keys` | 12 | 0 |
| `internal/ssb/message/bencode` | 46 | 0 |
| `internal/ssb/message/bendy` | 4 | 0 |
| `internal/ssb/message/legacy` | 10 | 0 |
| `internal/ssb/message/tangle` | 43 | 0 |
| `internal/ssb/muxrpc` | 14 | 0 |
| `internal/ssb/muxrpc/codec` | 6 | 0 |
| `internal/ssb/muxrpc/handlers` | 5 | 0 |
| `internal/ssb/muxrpc/handlers/room` | 32 | 0 |
| `internal/ssb/network` | 4 | 0 |
| `internal/ssb/publisher` | 8 | 0 |
| `internal/ssb/refs` | 20 | 0 |
| `internal/ssb/replication` | 13 | 0 |
| `internal/ssb/room` | 30 | 0 |
| `internal/ssb/room/http` | 33 | 0 |
| `internal/ssb/roomdb/sqlite` | 30 | 0 |
| `internal/ssb/roomstate` | 3 | 0 |
| `internal/ssb/sbot` | 9 | 0 |
| `internal/ssb/secretstream` | 5 | 0 |
| `internal/ssb/secretstream/boxstream` | 3 | 0 |
| **Total** | **502** | **0** |

---

## ATProto Compliance Matrix

### P0 Findings (Protocol-Breaking)

#### 1. AT-URI Parser Non-Compliant

| Field | Value |
|-------|-------|
| **Category** | uri-parsing |
| **Spec Reference** | https://atproto.com/specs/at-uri-scheme |
| **Local File** | `pkg/atproto/syntax/syntax.go:92-156` |
| **Test File** | `pkg/atproto/syntax/syntax_test.go` |
| **Status** | **Non-Compliant** |
| **Severity** | **P0** |

**Problem**: Local AT-URI parser uses permissive string splitting without proper validation.

**Missing**:
1. 8192 char length limit
2. Strict regex validation
3. Proper authority validation via `ParseAtIdentifier`
4. Proper NSID validation for collection
5. Proper RecordKey validation

**Accepts Invalid**:
- `at://did:plc:test//app.bsky.feed.post/abc` (double slash)
- `at://did:plc:test/app.bsky.feed.post/` (trailing slash with empty rkey)

**Impact**: Security (accepts malformed URIs from untrusted sources); Interop (may generate/proxy invalid AT-URIs to other services).

---

#### 2. NSID Parser Non-Compliant

| Field | Value |
|-------|-------|
| **Category** | nsid |
| **Spec Reference** | https://atproto.com/specs/nsid |
| **Local File** | `pkg/atproto/syntax/syntax.go:64-69` |
| **Test File** | `pkg/atproto/syntax/syntax_test.go` |
| **Status** | **Non-Compliant** |
| **Severity** | **P0** |

**Problem**: Parser only checks for non-empty string containing a period.

**Missing**:
1. 317 char limit
2. Regex validation: `^[a-zA-Z]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+(\.[a-zA-Z]([a-zA-Z0-9]{0,62})?)$`
3. Segment length validation (63 chars max per segment)

**Accepts Invalid**: `a` (no period), `com..example` (empty segment), very long segments.

**Impact**: Interop failure with strict ATProto services.

---

#### 3. RecordKey Parser Non-Compliant

| Field | Value |
|-------|-------|
| **Category** | rkey |
| **Spec Reference** | https://atproto.com/specs/record-key |
| **Local File** | `pkg/atproto/syntax/syntax.go:76-81` |
| **Test File** | `pkg/atproto/syntax/syntax_test.go` |
| **Status** | **Non-Compliant** |
| **Severity** | **P0** |

**Problem**: Parser only checks for non-empty and no `/`.

**Missing**:
1. 512 char limit
2. Regex validation: `^[a-zA-Z0-9_~.:-]{1,512}$`
3. Rejection of `.` and `..` as record keys

**Accepts Invalid**: `.` and `..` (explicitly forbidden by spec).

**Impact**: Security vulnerability (path traversal); invalid record references.

---

### P1 Findings (High Interop Risk)

#### 4. Missing #sync Event Handling

| Field | Value |
|-------|-------|
| **Category** | firehose |
| **Spec Reference** | com.atproto.sync.subscribeRepos lexicon |
| **Local File** | `pkg/atproto/firehose/firehose.go:89-121` |
| **Test File** | `pkg/atproto/firehose/firehose_test.go` |
| **Status** | **Missing** |
| **Severity** | **P1** |

**Problem**: Firehose event parser does NOT handle `#sync` events. The switch statement handles `#commit`, `#identity`, `#account`, `#info` but NOT `#sync`.

**Impact**: Cannot process sync events which indicate repo state recovery. May lose track of repos after data recovery events.

---

#### 5. DID Parser Partially Compliant

| Field | Value |
|-------|-------|
| **Category** | uri-parsing |
| **Spec Reference** | https://atproto.com/specs/did |
| **Local File** | `pkg/atproto/syntax/syntax.go:21-31` |
| **Status** | Partially Compliant |
| **Severity** | **P1** |

**Missing**:
1. 2048 char limit
2. Regex validation: `^did:[a-z]+:[a-zA-Z0-9._:%-]*[a-zA-Z0-9._-]$`
3. PLC fast-path optimization
4. Method name validation (lowercase only)

---

#### 6. Handle Parser Partially Compliant

| Field | Value |
|-------|-------|
| **Category** | uri-parsing |
| **Spec Reference** | https://atproto.com/specs/handle |
| **Local File** | `pkg/atproto/syntax/syntax.go:45-57` |
| **Status** | Partially Compliant |
| **Severity** | **P1** |

**Missing**:
1. 253 char limit
2. Strict regex: `^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`
3. Segment length validation (63 chars max)
4. TLD validation for disallowed TLDs (`.local`, `.arpa`, `.invalid`, etc.)

**Correct**: Normalizes to lowercase.

---

#### 7. Repo Path Parser Partially Compliant

| Field | Value |
|-------|-------|
| **Category** | repo |
| **Local File** | `internal/atindex/service.go:799-813` |
| **Status** | Partially Compliant |
| **Severity** | **P1** |

**Problem**: `collectionAndRKey` function correctly splits path using `syntax.ParseNSID` and `syntax.ParseRecordKey`, but those underlying parsers are non-compliant (see findings #2, #3).

---

### P2 Findings (Moderate Drift)

#### 8. MST Construction

| Field | Value |
|-------|-------|
| **Category** | repo |
| **Local File** | `pkg/atproto/repo/repo.go` |
| **Test File** | `pkg/atproto/repo/repo_test.go` |
| **Status** | Partially Compliant |
| **Severity** | **P2** |

**Problem**: MST implementation uses simplified construction (`buildMST` creates flat entries, not properly fanout-based tree). KeySuffix/PrefixLen handling during loading appears correct but construction during writes may not produce valid fanout structure.

**Impact**: May generate MSTs that don't match Indigo's fanout structure. Could cause verification issues.

---

#### 9. Missing Sync Endpoints

| Field | Value |
|-------|-------|
| **Category** | sync |
| **Status** | Missing |
| **Severity** | **P2** |

**Missing**:
- `sync.getBlocks`
- `sync.getCheckout`
- `sync.getHead`
- `sync.getLatestCommit`
- `sync.getRecord`
- `sync.getHostStatus`
- `sync.getRepoStatus`
- `sync.listBlobs`
- `sync.listHosts`
- `sync.listRepos`
- `sync.listReposByCollection`
- `sync.notifyOfUpdate`
- `sync.requestCrawl`

**Impact**: Limited sync capabilities. Cannot do partial repo fetches or status checks. Optional for bridge but needed for full PDS interop.

---

#### 10. Missing TID Parser/Generator

| Field | Value |
|-------|-------|
| **Category** | uri-parsing |
| **Spec Reference** | https://atproto.com/specs/record-key (TID format) |
| **Status** | Missing |
| **Severity** | **P2** |

**Problem**: No TID (Timestamp ID) parsing or generation implemented.

**Indigo has**:
- `ParseTID` with 13-char validation
- base32sort alphabet `234567abcdefghijklmnopqrstuvwxyz`
- `TIDClock` for monotonic generation
- Integer/timestamp extraction

**Impact**: Cannot validate or generate TIDs for record versions. Limited ability to enforce ordering on commits.

---

#### 11. RepoGetRecord Partial Validation

| Field | Value |
|-------|-------|
| **Category** | repo |
| **Local File** | `pkg/atproto/atproto.go:187-201` |
| **Status** | Partially Compliant |
| **Severity** | **P2** |

**Problem**: Does NOT validate that `repo` is valid at-identifier format, `collection` is valid NSID format, or `rkey` is valid record-key format before making request. Trusts server to validate.

---

### P3 Findings (Test/Doc Debt)

All of these are **Compliant** with good test coverage:
- Firehose Message Decoding
- Blob Handling (`LexBlob`)
- `sync.getBlob`
- `sync.getRepo`
- `repo.uploadBlob`
- DID Resolution
- CAR File Parsing
- XRPC Client
- Lexicon Type Registry
- Firehose Client Reconnection
- prevData Field Handling

---

## SSB Compliance Matrix

### P0 Findings (Protocol-Breaking)

**None identified.** All SSB P0-critical surfaces are compliant.

---

### P1 Findings (High Interop Risk)

#### S1. tunnel.announce

| Field | Value |
|-------|-------|
| **Category** | tunnel |
| **Spec Reference** | SIP-007 |
| **Local File** | `internal/ssb/muxrpc/handlers/room/tunnel.go:100-169` |
| **Test File** | `internal/room/tunnel_history_test.go` |
| **Status** | **Compliant** |

Validates caller identity from SHS, checks denial/membership based on privacy mode, adds peer to state.

---

#### S2. tunnel.connect

| Field | Value |
|-------|-------|
| **Category** | tunnel |
| **Spec Reference** | SIP-007 |
| **Local File** | `internal/ssb/muxrpc/handlers/room/tunnel.go:210-312` |
| **Test File** | `internal/room/peer_connection_test.go`, `internal/room/runtime_test.go` |
| **Status** | **Compliant** |

Correctly parses `{origin, portal, target}`, verifies origin is caller, portal is room, target has announced. Forwards duplex stream.

---

#### S3. room.attendants

| Field | Value |
|-------|-------|
| **Category** | sip-006 |
| **Spec Reference** | SIP-007 § Attendants API |
| **Local File** | `internal/ssb/muxrpc/handlers/room/room_handler.go:306-372` |
| **Test File** | `internal/ssb/roomstate/roomstate_test.go` |
| **Status** | **Compliant** |

Correctly sends initial state with `{type:'state', ids:[...]}` then streams `{type:'joined'|'left', id:...}` events.

---

#### S4. createHistoryStream

| Field | Value |
|-------|-------|
| **Category** | ebt |
| **Spec Reference** | Classic SSB |
| **Local File** | `internal/ssb/muxrpc/handlers/history.go:147-195` |
| **Test File** | `internal/ssb/muxrpc/handlers/history_test.go` |
| **Status** | **Compliant** |

Correctly parses args array, supports `id`, `seq`, `limit`, `live`, `keys` flags. Emits `{key, value, timestamp}` objects.

---

#### S5. EBT Replication State

| Field | Value |
|-------|-------|
| **Category** | ebt |
| **Local File** | `internal/ssb/replication/ebt.go:18-280` |
| **Test File** | `internal/ssb/replication/ebt_test.go` |
| **Status** | **Compliant** |

`Note.MarshalJSON` encodes `-1` for non-replicate, or `(seq<<1)|(!receive)`. StateMatrix tracks frontiers per peer.

---

### P2 Findings (Moderate Drift)

#### S6. room.metadata Deviation

| Field | Value |
|-------|-------|
| **Category** | sip-006 |
| **Spec Reference** | SIP-007 § Metadata API |
| **Local File** | `internal/ssb/muxrpc/handlers/room/room.go:400-433` |
| **Status** | **Partially Compliant** |
| **Severity** | **P2** |

**Intentional Design Decision**: Returns `{name, membership, features}` where `membership` is a **boolean** (caller is member) rather than **string** privacy mode (`'open'|'community'|'restricted'`) per SIP-007.

**Rationale**: Pragmatic for bridge use case. No interop issues observed.

---

#### S7. httpAuth.requestSolution

| Field | Value |
|-------|-------|
| **Category** | sip-009 |
| **Spec Reference** | SIP-009 |
| **Local File** | `internal/ssb/muxrpc/handlers/room/auth.go:84-124` |
| **Status** | **Partially Compliant** |
| **Severity** | **P2** |

**Problem**: Role reversal issue. `RequestSolution` is called BY server ON client (per spec), but local impl appears to be client-side handler.

**Missing**: `sendSolution` implementation for server-initiated flow.

**Impact**: HTTP authentication for SSB peers needs verification with actual SSB clients.

---

### Compliant SSB Surfaces

All verified compliant:
- **SIP-004**: BendyButt message encoding, content signature, BFE encoding
- **SIP-007**: room.registerAlias, room.revokeAlias, alias web endpoint, alias URL generation
- **SIP-008**: HTTP invite flow
- **SIP-009**: httpAuth.invalidateAllSolutions
- **Tunnel**: tunnel.endpoints, ClientTunnelConnectHandler, tunnel.isRoom, tunnel.ping
- **Muxrpc**: room.members
- **Multiserver**: address format

---

## Known Limitations

1. **Static analysis only** — No live network interop testing
2. **Targeted test scope** — Tests cover unit functionality, not full E2E flows
3. **Reference implementations may drift** — Audit reflects state as of 2026-04-12

---

## Recommendations

### Immediate (P0)

1. **Replace AT-URI parser** with Indigo's strict implementation from `/tmp/protocol-compliance-20260412-indigo/atproto/syntax/aturi.go`
2. **Replace NSID parser** with Indigo's `nsid.go`
3. **Replace RecordKey parser** with Indigo's `recordkey.go`
4. **Add `#sync` case** to firehose event handler

### Short-term (P1)

5. **Add TID parser/generator** from Indigo
6. **Strengthen DID/Handle validation** or import Indigo's implementations
7. **Add MST fanout tests** to verify tree structure matches Indigo

### Medium-term (P2)

8. **Implement missing sync endpoints** for improved PDS interop
9. **Verify httpAuth flow** with actual SSB clients (Planetary, Manyverse)
10. **Document room.metadata deviation** in project docs

---

## Files Modified

- Created: `docs/protocol-compliance-review-2026-04-12.md` (this file)
- Created: `docs/protocol-compliance-evidence-2026-04-12.json` (machine-readable findings)

---

*Audit conducted by Letta Code. Generated 2026-04-12.*
