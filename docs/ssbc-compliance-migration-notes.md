# SSBC Compliance Migration Notes

Date: 2026-04-02

## Summary

This merge brings the bridge's embedded SSB room and protocol surface closer to SSBC, Rooms2, and SIP-001 expectations on top of the current ATProto/indexer mainline. The changes are wire-visible. Clients or local tooling that depended on the previous non-canonical or repo-specific behavior may need adjustment.

## Behavior Changes

### SIP-001 URIs

- `ssb:` URIs for feeds, messages, and blobs now encode the raw ref bytes using URL-safe base64.
- Emitted URIs are canonical slash-separated forms such as `ssb:feed/classic/...`.
- The parser still accepts canonical colon-separated forms and the deprecated `ed25519` / `sha256` path aliases.
- The previous malformed repo-local payload format, which base64-encoded the legacy ref text minus the sigil, is now rejected.

Operator impact:
- Any stored or generated links using the old malformed payload format must be regenerated.
- External clients now see canonical SIP-001 output instead of a local-only variant.

### Room Identity and Membership

- Room identity is now derived from the SHS-authenticated remote feed, not caller-supplied muxrpc arguments or room state lookups.
- In restricted mode, non-members are rejected after handshake.
- In open and community modes, non-members may connect, but they do not count as internal room attendants and do not get member-only room operations.

Operator impact:
- Peers can no longer spoof room identity by announcing arbitrary feed IDs.
- Membership-sensitive operations now reflect authenticated feed identity consistently.

### Rooms2 Metadata, Attendants, Aliases, and Invites

- `room.metadata()` now returns the Rooms2-style shape:
  - `name`
  - `membership`
  - `features`
- `room.attendants()` now emits a live source stream starting with:
  - `{type:"state", ids:[...]}`
  and then `joined` / `left` events.
- `room.registerAlias` and `room.revokeAlias` use positional muxrpc args and require authenticated membership.
- Alias HTTP endpoints now resolve at `/<alias>` in open and community modes.
- Alias JSON responses now expose:
  - `status`
  - `multiserverAddress`
  - `roomId`
  - `userId`
  - `alias`
  - `signature`
- Invite and alias `multiserverAddress` values now use `net:<addr>~shs:<base64(pubkey)>` with the raw public key bytes after `~shs:`.

Operator impact:
- Clients relying on the previous custom metadata payload or finite attendants list need to update to the live Rooms2 event format.
- Consumers expecting `~shs:@...ed25519` in invite-style responses must switch to raw base64 public keys.
- Alias consumption is now available over stable HTTP paths rather than only internal muxrpc state.

### Tunnel Behavior

- `tunnel.announce` and `tunnel.leave` are bound to the authenticated SHS identity.
- Announced tunnel targets are kept distinct from connected attendants.
- `tunnel.connect` now uses the room as a portal between the origin and an already-connected announced target instead of dialing the target directly.
- The bridge's own tunnel bootstrap now uses the room's muxrpc `tunnel.announce` flow instead of mutating room state directly in-process.

Operator impact:
- Peers must already be connected and announced for room tunneling to succeed.
- Any code that assumed the room would terminate or re-dial the target side of the tunnel must be updated.

### `createHistoryStream`

- The handler now parses the muxrpc args array correctly.
- It accepts both `sequence` and legacy `seq`.
- Defaults now match the classic expectations more closely:
  - `limit` unlimited when omitted or zero
  - `old=true`
  - `live=false`
  - `keys=true`
- `keys=false` returns the signed message object.
- `keys=true` returns `{key,value,timestamp}` where `value` is the signed message object.

Operator impact:
- Clients that previously consumed the repo's custom wrapper shape need to update to the classic output shape.
- Tests or fixtures that assumed a default limit of `100` need to be corrected.

## Compatibility Notes

- This merge is intentionally not backward-compatible with the repo's malformed SIP-001 payload format.
- The ATProto runtime/indexer cutover already present on `main` remains authoritative; this merge does not reintroduce Indigo-era UI types or old cursor semantics.
- Alias HTML output renders a browser-safe experimental `consume-alias` link, and alias JSON output is stable enough for automated consumers.

## Validation

The following verification passed on the integration branch:

- `go test ./...` from `internal/ssb`
- `go test -vet=off ./cmd/room-tunnel-feed-verify`
- `go test -vet=off ./internal/room`
- `go test -vet=off ./internal/web/handlers`
- `go test -vet=off ./cmd/bridge-cli`

Notes:
- Root-package verification was run with `GOFLAGS=-mod=mod` in this worktree to avoid a stale local `vendor/` directory that is not part of the repo state.
- A full root `go test ./...` was not used as the acceptance gate on this machine because the local disk is nearly full and previously failed during link/write steps with `no space left on device`.
