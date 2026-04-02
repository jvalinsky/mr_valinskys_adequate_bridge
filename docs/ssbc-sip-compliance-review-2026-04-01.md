# SSBC / SIP Compliance Review

Date: 2026-04-01

Scope:
- Project-owned SSB-facing code in `internal/ssb`, `internal/room`, `internal/ssbruntime`, and `cmd/room-tunnel-feed-verify`
- Public wire/API behavior against the Secure Scuttlebutt protocol guide, the SSB message spec, SIP-001, and the Rooms2 spec

Primary sources reviewed:
- SSB message spec: <https://spec.scuttlebutt.nz/feed/messages.html>
- Scuttlebutt protocol guide: <https://ssbc.github.io/scuttlebutt-protocol-guide/>
- SIP-001 (SSB URIs): <https://raw.githubusercontent.com/ssbc/sips/master/001.md>
- Rooms2 spec: <https://ssbc.github.io/rooms2/>

## Executive Summary

The repository has real SSB protocol machinery, not just wrappers around external libraries. The SHS/boxstream, muxrpc framing, classic-message signing, EBT note encoding, blob APIs, and runtime bootstrapping are implemented locally and most of the `internal/ssb` unit tests pass.

However, the externally visible interoperability surface is only partially compliant. The largest gaps are in Room2 and classic replication:

1. Room membership and alias identity are not bound to the authenticated SHS peer, which breaks the Rooms2 trust model and makes alias ownership spoofable.
2. `tunnel.connect` does not implement the standard Room tunnel contract and does not transparently relay the inner origin<->target SHS session.
3. `createHistoryStream` does not emit spec-shaped responses and ignores key request semantics.
4. `room.metadata`, `room.attendants`, and `room.registerAlias` / `room.revokeAlias` diverge from the Rooms2 schema and behavior.
5. SIP-001 URI encoding is self-consistent inside the repo but not canonical.
6. HTTP invite responses produce a malformed `multiserverAddress`.

Net assessment:
- Transport primitives: mostly aligned
- Classic message publication: probably interoperable enough for the bridge's own produced messages
- Classic replication fallback: not compliant
- Rooms2: materially non-compliant today
- SIP-001 URI support: non-compliant

## Detailed Findings

### 1. Critical: Room identity and alias ownership are not tied to the authenticated SHS peer

Relevant code:
- `internal/room/runtime.go:343-362`
- `internal/ssb/muxrpc/handlers/room/tunnel.go:66-108`
- `internal/ssb/muxrpc/handlers/room/room.go:159-174`
- `internal/ssb/muxrpc/handlers/room/room.go:362-374`
- `internal/ssb/roomdb/sqlite/sqlite.go:302-312`

What the spec expects:
- Rooms2 internal-user recognition is based on the peer's secret-handshake identity.
- Alias registration requires a signature proving `feedId` authorized `alias`, and the room is supposed to trust the caller identity coming from SHS, not from a self-declared payload.

What the code does:
- `handleMUXRPCConn` accepts the SHS connection and starts muxrpc, but never records the authenticated remote feed from the SHS layer.
- `tunnel.announce` trusts a caller-supplied `id` and stores it in room state without checking that it matches the SHS-authenticated remote peer.
- `getCallerFeed` later identifies the caller by matching `req.RemoteAddr().String()` against that room-state entry.
- `Aliases.Register` stores the provided signature bytes but never validates the alias string or the signature payload required by Rooms2.

Impact:
- A peer can announce an arbitrary feed ID and then act as that feed for later room operations that rely on `getCallerFeed`.
- Because alias signatures are never verified, alias ownership is not cryptographically enforced at all.
- This is both a protocol-compliance failure and a real security issue.

Assessment:
- Non-compliant with Rooms2 internal-user and alias requirements
- High-confidence exploit path in open/community room deployments

### 2. High: `tunnel.connect` does not implement the standard Room tunnel wire contract

Relevant code:
- `internal/ssb/muxrpc/handlers/room/tunnel.go:143-185`
- `internal/ssb/muxrpc/handlers/room/tunnel.go:188-244`
- `internal/ssb/muxrpc/handlers/room/tunnel.go:257-276`
- `cmd/room-tunnel-feed-verify/main.go:297-299`

What the spec expects:
- The protocol guide shows `tunnel.connect` with `{portal, target}` from the origin and `{origin, portal, target}` from the portal to the target.
- After setup, the portal forwards the encrypted duplex stream unmodified; the inner origin<->target connection remains an end-to-end SHS/boxstream channel.

What the code does:
- The handler expects `{id, addr}` instead of `{portal, target}` / `{origin, portal, target}`.
- The room actively dials the target over TCP itself and performs a secret-handshake as the room's own keypair.
- `dialPeer` returns the raw `net.Conn`, not the `secretstream.Client`, so the forwarding path is not the authenticated inner SHS stream.

Impact:
- Standard Room clients cannot call this endpoint with the documented argument shape.
- Even if adapted, the room is not behaving as a transparent portal for the inner SHS session.
- This breaks the interoperability model described by the Rooms spec and protocol guide.

Assessment:
- Non-compliant with Room tunnel semantics
- Likely incompatible with standard room clients without custom shims

### 3. High: `createHistoryStream` is not spec-compatible

Relevant code:
- `internal/ssb/muxrpc/handlers/history.go:17-23`
- `internal/ssb/muxrpc/handlers/history.go:39-78`
- `internal/ssb/muxrpc/handlers/history.go:107-117`

What the spec expects:
- `createHistoryStream` receives one argument object inside the `args` array.
- It should support the `sequence`/`seq`, `limit`, `live`, `old`, and `keys` controls.
- With `keys=true`, responses are `{key, value, timestamp}` where `value` is the full signed message object.
- With `keys=false`, responses are the raw signed message object only.
- Default `limit` is unlimited.

What the code does:
- Unmarshals `req.RawArgs` directly into a struct instead of decoding the standard `args` array.
- Silently defaults `limit=100` rather than unlimited.
- Ignores `keys`, `old`, and `live`.
- Emits `value` as decoded message content only, not the signed message object.
- Emits a top-level hex `signature` field, which is not part of the `createHistoryStream` response contract.

Impact:
- Classic replication fallback is not interoperable with standard peers.
- Any peer relying on non-EBT feed sync will receive malformed payloads.

Assessment:
- Non-compliant with classic replication behavior

### 4. High: `room.metadata`, `room.attendants`, and alias muxrpc behavior diverge from Rooms2

Relevant code:
- `internal/ssb/muxrpc/handlers/room/room.go:135-175`
- `internal/ssb/muxrpc/handlers/room/room.go:290-324`
- `internal/ssb/muxrpc/handlers/room/room.go:326-359`
- `internal/room/runtime.go:408-423`

What the spec expects:
- `room.metadata()` returns `{name, membership, features}`.
- `room.attendants()` is a source stream whose first event is `{type:"state", ids:[...]}` followed by `joined` / `left` events.
- `room.registerAlias(alias, signature)` validates alias syntax and signature, stores the claim, and returns an alias URL string.
- If alias support is present, there should also be alias-consumption HTTP behavior behind the alias endpoint.

What the code does:
- `room.metadata` returns `{roomId, roomInfo, domain, mode, description}` and omits membership/features entirely.
- `room.attendants` streams a finite list of `{id, addr}` objects and closes.
- `room.registerAlias` returns `{"alias": ...}` instead of a URL string.
- Alias persistence only lowercases and stores the row; it does not validate signature or alias format.
- The room HTTP mux mounts invite and auth endpoints, but there is no room-alias resolution endpoint in `internal/room`.

Impact:
- A standard Rooms2 client cannot feature-detect this room correctly.
- Live attendant presence tracking will not work.
- Alias registration/consumption cannot interoperate as specified.

Assessment:
- Non-compliant with the Rooms2 API surface

### 5. Medium: SIP-001 URI encoding is not canonical

Relevant code:
- `internal/ssb/refs/ssburi.go:47-65`
- `internal/ssb/refs/ssburi.go:133-180`

What the spec expects:
- `ssb:feed/classic/<FEEDID>` / `ssb:message/classic/<MSGID>` / `ssb:blob/classic/<BLOBID>`
- `<FEEDID>`, `<MSGID>`, and `<BLOBID>` are URI-safe base64 of the underlying ref payload bytes.
- The algorithm component (`classic`, `bendybutt-v1`, etc.) is already in the path and should not be embedded inside the payload.

What the code does:
- Serializes by URI-base64-encoding the full legacy ref string minus the sigil, e.g. `base64("@...".String()[1:])`, which still contains `.ed25519` / `.sha256`.
- Parses by reconstructing a full sigil-prefixed ref string out of the decoded payload.

Impact:
- URIs round-trip within this codebase but do not match canonical SIP-001 URIs used by other clients.
- Any external URI exchange will be wrong unless the other side copies this same bug.

Assessment:
- Non-compliant with SIP-001 canonical URI encoding

### 6. Medium: HTTP invite success responses contain a malformed multiserver SHS transform

Relevant code:
- `internal/room/invite.go:389-394`
- `internal/room/invite.go:496-506`

What the spec expects:
- `multiserverAddress` should use the multiserver `~shs:` transform format, which carries the raw public key bytes in base64, not a full `@...ed25519` feed ref string.

What the code does:
- Returns `net:<addr>~shs:<feedRef.String()>`, where `feedRef.String()` includes the `@` sigil and `.ed25519` suffix.

Impact:
- A claimed invite can be successfully consumed at the HTTP layer but still hand the client an invalid address for peer connection.

Assessment:
- HTTP invite flow is only partially compliant

## Areas That Look Reasonably Aligned

These are not a clean bill of health, only the areas where I did not find an obvious spec divergence during this pass:

- SHS / secretstream state machine in `internal/ssb/secretstream`
- Boxstream framing in `internal/ssb/secretstream/boxstream`
- Muxrpc packet framing and request/response direction handling in `internal/ssb/muxrpc`
- EBT note encoding in `internal/ssb/replication/ebt.go`
- Bridge-side classic message construction in `internal/ssb/message/legacy` and `internal/ssb/publisher`

Important caveat:
- These areas are mostly validated by repo-local tests, not by known-good interop vectors from other SSB implementations.

## Test Evidence

Commands run:

```bash
go test ./...              # in internal/ssb
go test ./internal/room ./internal/ssbruntime ./cmd/room-tunnel-feed-verify
```

Observed results:
- `internal/ssb/...`: passed
- `internal/ssbruntime`: passed
- `cmd/room-tunnel-feed-verify`: passed
- `internal/room`: failed in `TestInviteHandlersErrors`

Interpretation:
- The low-level SSB packages are internally consistent enough for their own test suite.
- The room implementation is not even fully stable against its own local tests, which lowers confidence further for external Rooms2 interoperability.

## Recommended Remediation Order

1. Bind room identity to SHS-authenticated peers.
   - Derive caller identity from `secretstream` remote pubkey, not from `tunnel.announce` payloads.
   - Enforce restricted-mode admission at connection time.

2. Rebuild the Room tunnel flow around the standard `{portal,target}` / `{origin,portal,target}` contract.
   - The room should broker the tunnel, not terminate the origin<->target inner SHS itself.

3. Fix `createHistoryStream`.
   - Decode `args` arrays correctly.
   - Implement `keys`, `live`, `old`, and unlimited `limit` semantics.
   - Emit standard classic message payloads.

4. Make the Room2 APIs spec-shaped.
   - `room.metadata` => `{name,membership,features}`
   - `room.attendants` => `state` then `joined` / `left`
   - `room.registerAlias` => validate alias/signature and return alias URL string

5. Correct URI and address encoding.
   - SIP-001 payloads should encode raw ref bytes.
   - `multiserverAddress` should use raw SHS public key material after `~shs:`.

## Bottom Line

The codebase contains enough real SSB infrastructure to publish bridge messages and run its own tests, but it is not currently SSBC / SIP compliant at the public interoperability layer. In particular, the Room2 implementation should be treated as experimental and non-standard until the identity, tunnel, metadata, attendants, alias, and invite/address issues above are fixed.
