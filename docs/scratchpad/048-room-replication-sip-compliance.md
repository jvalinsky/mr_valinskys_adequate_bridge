# Room + Replication SIP/SSB Compliance

Track nodes:
- Goal: #1206 Implement Room+Replication SIP/SSB compliance slice
- Decision: #1205 Use current Rooms 2 metadata schema as wire contract
- Action: #1207 Fix muxrpc JSON termination and strict compliance probes

## Scope

The active scope is Room2 + replication interoperability:
- SHS with the standard SSB app key `d4a1cb88a66f02f8db635ce26441cc5dac1b08420ceaac230839b755845a9ffb`
- muxrpc packet framing and terminations
- `manifest`, `whoami`, `room.*`, `tunnel.*`
- `createHistoryStream`, `ebt.replicate`, classic signed-message bytes
- `blobs.*`

Out of scope and not advertised as supported: metafeeds, index feeds, BIPF history payloads, and other feed/message families that are not implemented by the Room+Replication surface.

## External Contract Checks

Checked current Rooms 2.0 spec at `https://ssbc.github.io/rooms2/` on 2026-04-15.

Relevant points:
- `room.metadata` is an async muxrpc method with JSON output.
- The schema defines `membership` as boolean and `features` as an array.
- Features must only advertise supported capabilities.
- Tunneled connections require an inner secret-handshake between the origin and target peer; the room is only the portal.

Decision: keep `room.metadata.membership` boolean on the wire. The earlier plan text saying "membership string" conflicts with the current Rooms 2.0 schema.

## Mobile Export Evidence

Input logs:
- `logs/8FA2460B-CE2E-48D5-9654-256AA1FCD6FA/gobot-2026-04-15_03-17.log`
- `logs/8FA2460B-CE2E-48D5-9654-256AA1FCD6FA/com.planetary.ios 2026-04-15--03-17-02-694.log`
- `logs/38BAC680-9B65-4F55-AD27-EE976B1D0D62/d4a1cb88a66f02f8db635ce26441cc5dac1b08420ceaac230839b755845a9ffb/`

Observed failure signatures from the export and prior scratchpads:
- `terminations should have body type set to JSON`
- `error getting metadata: received no responses`
- `verification failed: ssb Verify(...): invalid signature`
- `messages_all=... messages_considered=... messages_persisted=0`
- `.ggfeed-v1` feed suffix parse errors in scuttlego

Interpretation for this slice:
- The muxrpc termination error maps directly to `ByteSink.Close()` emitting binary end frames for binary streams.
- The metadata timeout can be worsened by missing/incorrect manifest discovery, so muxrpc now serves built-in `manifest`.
- Signature failures remain open until a raw bridge payload from the wire is captured and replay-verified.
- `.ggfeed-v1` parse failures stay classified as unsupported/client compatibility unless the bridge emits or advertises those feeds.

## Implemented In This Slice

- Stream close packets now use `FlagJSON|FlagStream|FlagEndErr` even when the stream payload encoding is binary.
- Added strict regression coverage that keeps binary payload frames binary but requires JSON stream termination frames.
- Added built-in muxrpc `manifest` handling backed by the existing `Manifest` field.
- Added nested wire manifest shape while preserving the repo's internal by-type JSON shape.
- Added room runtime manifest entries for the registered Room2/httpAuth/tunnel methods.
- Removed unregistered `invite.create` and `httpAuth.*` advertisements from the standalone Sbot manifest.
- `/api/capabilities` now separates required implemented methods from explicit non-support.
- Added `docs/ssb-room-replication-compliance-matrix.json` as the machine-readable Room+Replication matrix.
- `cmd/room-tunnel-feed-verify history` now verifies every classic history frame signature, checks envelope key/message-ref agreement when a key is present, and reports `signature_valid` plus the raw JSON SHA-256.

## Verification

Commands run on 2026-04-15:
- `GOFLAGS=-mod=mod go test ./...` from the repository root: passed.
- `cd internal/ssb && go test ./...`: passed.
- `go test -count=1 ./cmd/room-tunnel-feed-verify ./cmd/bridge-cli`: passed.
- `go test ./cmd/ssb-client`: passed.
- `go test ./internal/room`: passed.
- `env E2E_TF_DEBUG=1 ./scripts/e2e_tildefriends.sh`: passed.

Tildefriends run:
- Run ID: `e2e-tf-20260415T194229Z`
- Host artifacts: `tmp/e2e-tildefriends/e2e-tf-20260415T194229Z`
- Harness result: `E2E PASSED: scenario=positive expect=pass compose_exit=0`
- Forward replication checks: OK
- Reverse media pipeline checks: OK
- The harness emitted a non-fatal warning that reverse events were not fully processed within 120 seconds. It still passed the required reverse media pipeline checks.

## Open Work

- Add raw-payload capture mode to `room-tunnel-feed-verify history`.
- Add a local Planetary/scuttlego-style probe that requests `manifest`, `room.metadata`, `tunnel.endpoints`, EBT, and tunneled `createHistoryStream`, failing on invalid signatures or non-JSON terminations.
- Capture an actual bridge-authored raw payload from the failing mobile path and verify it with both bridge verifier and a scuttlego/go-ssb-compatible verifier.
- Extend blob and EBT fixtures for note encoding, duplicate/gap paths, live streaming, append rejection, and ranged blob reads.
