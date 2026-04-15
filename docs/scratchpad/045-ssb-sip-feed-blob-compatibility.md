# Track 045: SSB SIP Feed Format + Blob Compatibility (2026-04)

DG Nodes: `#1186`, `#1187`, `#1188`

## Goal

Implement Room/replication-focused SSB SIP compatibility in phases:

- Make feed/message format handling explicit and debuggable.
- Preserve classic Tildefriends replication.
- Support modern BendyButt/metafeed paths before broader gabby/bamboo/indexed work.
- Harden bidirectional blob replication over direct and room-tunneled connections.

## Current Findings

- Room2 transport identity is SHS ed25519 and should remain separate from replicated feed identity.
- `refs` can parse `ed25519`, `bendybutt-v1`, and `gabbygrove-v1` feed refs, but not bamboo/indexed feed refs yet.
- `bfe` knows feed/message format codes for `ed25519`, `gabbygrove-v1`, `bamboo`, `bendybutt-v1`, `buttwoo-v1`, and `indexed-v1`.
- `message/bendy` has isolated BendyButt encode/sign/verify support.
- Live EBT/history/feedlog paths are currently classic-oriented.
- Blob handlers exist, but `blobs.add` buffers the whole blob and `createWants` currently emits an initial snapshot then waits.

## Design Decisions

- Add a registry first so every feed/message format is either supported or explicitly reported as unsupported.
- Keep classic `ed25519` + `sha256` fully supported.
- Treat BendyButt as the first non-classic implementation target.
- Treat gabby/bamboo/indexed as classified unsupported in V1 unless the full storage/replication path is implemented in this track.
- Keep payload logging off by default; trace metadata can include format, ref, byte counts, and error class.

## Run Ledger

- `done`: implement registry + tests.
  - Added `internal/ssb/formats` registry with support status and structured unsupported-format errors.
  - Expanded refs/SSB URI parsing for `bamboo` and `indexed-v1` feed/message refs, plus gabby message refs.
  - Added protocol trace fields for peer/replicated feed, feed/message/history format, blob ref, and transport.
- `done`: add storage metadata/raw-byte compatibility.
  - Added feedlog/receive-log columns for `feed_format`, `message_format`, `raw_value`, `canonical_ref`, and `validation_status`.
  - Changed schema version lookup to use `MAX(version)` so appended migrations do not re-run old migrations.
  - Added classic defaults and raw-byte preservation on receive-log writes.
  - Added BendyButt append/read path through `FeedManagerAdapter` using existing Bendy parser/signature verifier.
- `done`: harden blob streaming/live wants.
  - Added live want subscriptions and createWants emission for wants added after the stream opens.
  - Added max-size enforcement for `blobs.add`.
  - Changed blob file storage to stream through a temp file while hashing instead of buffering all data.
- `done`: rerun Room2/Tildefriends harness.

## Checkpoint 2026-04-14

Theory:

- Classic SSB success should remain unchanged if feed/message format defaults are applied only at storage boundaries.
- Non-classic refs should parse and classify even when replication support is not complete.
- BendyButt can be promoted from isolated tests into the EBT adapter if raw Bendy bytes are preserved and returned unchanged.
- Blob add/get paths need byte-count observability and bounded streaming before blob e2e failures are diagnosable.

Expected verification:

- `go test -count=1 ./internal/ssb/formats ./internal/ssb/refs ./internal/ssb/protocoltrace ./internal/ssb/feedlog ./internal/ssb/sbot ./internal/ssb/blobs ./internal/ssb/muxrpc/handlers ./cmd/room-tunnel-feed-verify -run Test -v`
- `go test -count=1 ./internal/ssb/replication -run Test -v`

Observed:

- `go test -count=1 ./formats ./refs ./protocoltrace ./feedlog ./sbot ./blobs ./muxrpc/handlers ./replication -run Test -v` from `internal/ssb`: pass.
- `go test -count=1 ./cmd/room-tunnel-feed-verify -run Test -v`: pass.
- `go test -count=1 ./internal/room -run 'TestTunnelConnectInnerProtocol|TestInboundTunnelConnectFromManyverseSimulatedClient' -v`: pass.
- `go test -count=1 ./cmd/bridge-cli -run 'TestRoomMemberIngest|TestRoomMember' -v`: pass.
- `env MVAB_BRIDGED_ROOM_INTEGRATION=1 go test -count=1 ./cmd/bridge-cli -run TestBridgedRoomPeerManagerIntegrationActivePeersAndTunnelTargets -v`: pass.
- `env MVAB_TEST_RUN_ID=sip-compat-20260414-r1 E2E_TF_DEBUG=1 ./scripts/e2e_tildefriends.sh`: pass.
- `env MVAB_TEST_RUN_ID=sip-compat-20260414-broken-r1 E2E_TF_DEBUG=1 E2E_TF_SCENARIO=broken-room E2E_TF_EXPECT=fail ./scripts/e2e_tildefriends.sh`: pass as expected-failure.

Artifacts:

- `tmp/e2e-tildefriends/sip-compat-20260414-r1/`
- `tmp/e2e-tildefriends/sip-compat-20260414-broken-r1/`

Decisions:

- Keep SQL `messages.key` as the existing SHA-256 wrapper key for compatibility. Store canonical SSB refs separately in `canonical_ref`.
- Keep `createWants` wire-compatible by emitting blob refs for active wants; cancellation is propagated to internal subscribers and kept off the string-only wire stream for now.
- Treat BendyButt EBT as raw-byte supported through append/read. `createHistoryStream` remains classic-only and returns structured unsupported-format errors for non-classic feeds/messages until a SIP-compatible history representation is confirmed.

## Artifacts

- Positive e2e artifacts should use `MVAB_TEST_RUN_ID` and live under `tmp/e2e-tildefriends/<run_id>/`.
- Any unsupported-format probe should emit JSON with `feed_format`, `message_format`, `method`, `phase`, and `err_kind`.
