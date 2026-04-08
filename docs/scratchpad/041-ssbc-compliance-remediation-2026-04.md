# Track 041: SSBC Compliance Remediation (2026-04)

## Scope

- Execute the remediation plan under decision graph goal `#959` (SSB Protocol Compliance Review).
- Address confirmed protocol gaps in:
  - SIP-004 (BendyButt message signing/validation semantics)
  - SIP-007 (Rooms2 metadata membership semantics + alias URL behavior)
- Keep runtime/test behavior explicit via targeted test additions.

## Decision Graph Map

- Goal: `#959` (completed)
- New observation: `#987`
- New decision: `#988`
- Options:
  - `#990` chosen (strict active-path fixes)
  - `#989` rejected (defer/document only)
- Actions:
  - `#991` SIP-004 bendy remediations
  - `#993` Rooms2 membership semantics remediation
  - `#995` alias URL normalization
  - `#997` targeted regression verification
- Outcomes:
  - `#992` bendy signature/previous behavior corrected
  - `#994` metadata membership semantics corrected
  - `#996` alias URL behavior stabilized for runtime flows
  - `#998` regression suites passing

## Notes While Implementing

- BendyButt message handling in `internal/ssb/message/bendy/message.go` was tightened to:
  - enforce bendy format codes for author/previous refs;
  - require BFE nil previous for sequence 1;
  - sign content separately (`bendybutt` prefix + bencoded content);
  - sign payload separately (bencoded payload);
  - encode both content and payload signatures using BFE signature wrappers.
- Added tests in `internal/ssb/message/bendy/message_test.go` for:
  - BFE-wrapped signature expectations;
  - BFE nil previous on first message;
  - rejection of non-bendy author/previous formats.
- Rooms metadata and alias behavior updated:
  - `room.metadata.membership` now reports authenticated internal-user membership (`bool`);
  - alias URL generation is centralized and normalizes:
    - configured HTTPS domain with/without scheme,
    - runtime loopback host fallback to absolute `http://host:port/...`.
- Runtime now backfills `roomSrv.Domain` from the bound HTTP address when no `HTTPSDomain` is configured, enabling full alias URLs in local mode.

## Files Changed

- `internal/ssb/message/bendy/message.go`
- `internal/ssb/message/bendy/message_test.go`
- `internal/ssb/muxrpc/handlers/room/helpers.go`
- `internal/ssb/muxrpc/handlers/room/room.go`
- `internal/ssb/muxrpc/handlers/room/room_handler.go`
- `internal/room/runtime.go`
- `internal/room/runtime_test.go`
- `cmd/room-tunnel-feed-verify/main.go`

## Validation

- `go test ./... -count=1` (from `internal/ssb`) passes.
- `go test ./internal/room ./cmd/room-tunnel-feed-verify -count=1` (repo root) passes.
- `go test ./... -count=1` (repo root) passes.
- Focused runtime coverage passes:
  - `TestRuntimeRoomMetadataReportsAuthenticatedMembership`
  - `TestRuntimeAliasRegisterEndpointAndRevoke`
  - `TestRuntimeAliasJSONUsesHTTPSDomainHost`
  - `TestRuntimeRegisterAliasURLUsesConfiguredHTTPSDomain`

## Follow-ups

- If strict external compatibility requires alias subdomain form (instead of path form), add explicit host template configuration (e.g. `https://{alias}.example/`) and conformance tests.
