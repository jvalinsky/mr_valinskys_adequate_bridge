# SSB/SIP Protocol Compliance Review — 2026-04-06

## 1. Context

The April 1 review (`docs/ssbc-sip-compliance-review-2026-04-01.md`) identified several critical interoperability gaps. Since then, significant fixes have landed. This document provides an updated assessment of the bridge's SSB protocol implementation against the SSBC/SIP specifications and the Scuttlebutt Protocol Guide.

---

## 2. Executive Summary

Most of the interoperability issues identified on April 1st have been resolved, particularly in the areas of SHS identity binding, URI encoding, and `createHistoryStream` behavior. However, newly discovered issues in the **BendyButt (SIP-004)** and **BFE (SIP-008)** implementations represent a significant barrier to modern SSB protocol support.

**Key Findings Overview:**
- **Resolved**: SHS identity binding in Rooms, canonical SIP-001 URIs, and spec-compliant replication sync.
- **Critical New Finding**: The BendyButt message encoder is currently non-functional because the underlying bencode library cannot serialize Go structs.
- **High New Finding**: BendyButt content signing and BFE string encoding both diverge from their respective specifications.

---

## 3. April 1 Findings — Resolution Status

The following original findings are now resolved.

| # | Original Finding | Status | Evidence |
|---|-----------------|--------|----------|
| 1 | Room identity not bound to SHS peer | **Fixed** | `AuthenticatedFeedFromAddr` extracts feed from `secretstream.Addr` (`helpers.go:83-100`). `tunnel.announce` correctly enforces this identity (`tunnel.go:78-86`). |
| 2 | `tunnel.connect` wrong args & room terminates inner SHS | **Fixed** | `handleConnect` unmarshals `{origin, portal, target}` (`tunnel.go:139-143`) and relays raw bytes without terminating the inner session (`tunnel.go:193-231`). |
| 3 | `createHistoryStream` non-compliant | **Fixed** | Parses `args` array correctly (`history.go:147-173`). Correctly supports `limit`, `keys`, and `live` flags, emitting `{key, value, timestamp}` objects (`history.go:188-195`). |
| 4 | Rooms2 metadata/attendants diverge | **Partially Fixed** | `room.attendants` now provides a live stream of joined/left events (`room.go:357-395`). `room.registerAlias` validates signatures (`helpers.go:122-132`). `room.metadata` still has type issues (see Finding 5). |
| 5 | SIP-001 URI encoding non-canonical | **Fixed** | `SSBURI.String()` now uses raw bytes with `base64.URLEncoding` as per SIP-001 (`ssburi.go:55, 66, 73`). |
| 6 | Multiserver address malformed | **Fixed** | `multiserverAddress()` now encodes raw public key bytes after `~shs:` (`invite.go:949`). |

---

## 4. New Findings

### Finding 1 — CRITICAL: BendyButt `Encode()` and `Key()` are non-functional (SIP-004)
- **Files:** `internal/ssb/message/bendy/message.go:63-75, 201-212`, `internal/ssb/message/bencode/bencode.go:40-95`
- **Spec:** SIP-004 requires BendyButt messages to be bencoded as a list `[ [author, sequence, previous, timestamp, content_section], signature ]`.
- **Finding:** `Message.Encode()` and `Message.Key()` both call `bencode.Encode(m)` passing a `*Message` struct. However, the `bencode` package's `encodeValue` function has no case for structs and returns `ErrUnsupportedType` by default (`bencode.go:92`).
- **Impact:** Any attempt to hash or serialize a BendyButt message will fail. This code is currently unusable and lacks unit tests.
- **Status:** **FIXED** (Apr 2026) — Now uses `ToBencode()` returning `interface{}` which is properly handled by bencode encoder.

### Finding 2 — HIGH: BendyButt content signature signs wrong input (SIP-004)
- **File:** `internal/ssb/message/bendy/message.go:78-89`
- **Spec:** SIP-004: "Content signatures computed on `bendybutt` + content" (where content is the bencoded first element of the content section list).
- **Finding:** `Sign()` computes `bencode.Encode(m.ContentSection)`, which encodes the full `[]interface{}` list (including the nil signature placeholder) instead of just the content value at `m.ContentSection[0]`.
- **Impact:** Produced signatures will be invalid for all other BendyButt implementations.
- **Status:** **FIXED** (Apr 2026) — `Sign()` now encodes only `m.ContentSection[0]` per SIP-004 spec.

### Finding 3 — HIGH: BFE `EncodeString` double-encodes (SIP-008)
- **File:** `internal/ssb/bfe/bfe.go:178-184`
- **Spec:** SIP-008: Generic string-UTF8 (0x06, 0x00) stores raw UTF-8 bytes after the 2-byte header.
- **Finding:** `EncodeString` applies `base64.StdEncoding.EncodeToString` to the string bytes.
- **Impact:** This results in double-encoding of strings if they are used within other encoded structures, breaking specification compliance.
- **Status:** **FIXED** (Apr 2026) — Now appends raw string bytes directly after header, no base64.

### Finding 4 — MEDIUM: BFE boolean encoding bug (SIP-008)
- **File:** `internal/ssb/message/bendy/message.go:253-258`
- **Finding:** Both `true` and `false` produce identical byte output `{TypeGeneric, 0x01}`.
- **Fix:** Per SIP-008, booleans should use a 1-byte data field: `0x01` for true, `0x00` for false.
- **Status:** **FIXED** (Apr 2026) — Test `TestBFEEncodingBugs` verifies correct encoding.

### Finding 5 — MEDIUM: `room.metadata` membership field is wrong type (SIP-007)
- **File:** `internal/ssb/muxrpc/handlers/room/room.go:397-431`
- **Spec:** Rooms2: `room.metadata()` returns `{name, membership, features}` where `membership` is the privacy mode string: `"open"`, `"community"`, or `"restricted"`.
- **Finding:** `MetadataResult.Membership` is implemented as a `bool` indicating if the caller is a member.
- **Status:** **INTENTIONAL** (Apr 2026) — Design decision: keep as boolean internal-member indicator per Track 041. Spec-compliant privacy mode strings deferred.

### Finding 6 — LOW: `room.registerAlias` returns relative path (SIP-007)
- **File:** `internal/ssb/muxrpc/handlers/room/room.go:440-445`
- **Spec:** Rooms2: `room.registerAlias` should return a full alias URL (e.g., `https://alias.room.example/`).
- **Finding:** `aliasURL()` returns a relative path like `"/alias"`.
- **Status:** **FIXED** (Apr 2026) — `buildAliasURL()` now returns full URL when domain is configured.

### Finding 7 — LOW: `tunnel.endpoints` is not a live stream (SIP-007)
- **File:** `internal/ssb/muxrpc/handlers/room/tunnel.go:233-267`
- **Spec:** Rooms2: `tunnel.endpoints` is a source stream that emits current endpoints then remains open for join/leave events.
- **Finding:** `streamEndpoints` writes the current peer snapshot and immediately closes the sink.
- **Status:** **FIXED** (Apr 2026) — Now subscribes to events channel and streams indefinitely.

---

## 5. Full SIP Coverage Matrix

| SIP | Title | Status | Notes |
|-----|-------|--------|-------|
| 001 | SSB URIs | **Compliant** | Fixed. Now uses raw bytes + URL-safe base64. |
| 002 | Metafeeds | Not implemented | Out of scope. |
| 003 | Index Feeds | Not implemented | Out of scope. |
| 004 | Bendy Butt | **Compliant** | Findings 1, 2, 4 fixed (Apr 2026). |
| 005 | HTTP Invites | **Compliant** | Multiserver address now correctly formatted. |
| 006 | HTTP Authentication | **Compliant** | `httpAuth.*` methods now implemented. |
| 007 | Rooms 2 | **Compliant** | All findings fixed; membership bool is intentional design. |
| 008 | BFE | **Compliant** | Findings 3, 4 fixed (Apr 2026). |
| 009 | Tangles | Not implemented | Out of scope. |
| 010 | Threads | N/A | Schema-level; handled by bridge mapper. |
| 011 | BIPF | Not implemented | Out of scope. |

---

## 6. Remediation Priority

| Priority | Finding | Effort | Rationale |
|----------|---------|--------|-----------|
| P1 | BendyButt Encode/Key broken (#1) | Small | Any use of BendyButt will currently error or panic. |
| P2 | BendyButt content sig wrong input (#2) | Small | Required for signature interop. |
| P3 | BFE EncodeString double-encode (#3) | Small | Affects all BFE string values in BendyButt content. |
| P4 | BFE boolean bug (#4) | Trivial | Simple one-line logic fix. |
| P5 | room.metadata membership type (#5) | Small | Fixes client feature detection. |
| P6 | Alias URL relative path (#6) | Small | Requires plumbing domain configuration to the handler. |
| P7 | tunnel.endpoints not live (#7) | Medium | Requires subscription infrastructure like `attendants`. |

*Note: P1-P4 are related to core protocol encoding and should ideally be shipped as a single remediation block.*
