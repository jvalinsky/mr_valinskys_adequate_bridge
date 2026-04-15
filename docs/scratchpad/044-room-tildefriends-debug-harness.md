# Track 044: Room2/Tildefriends Debug Harness (2026-04)

DG Nodes: `#1145`, `#1151`, `#1152`, `#1184`, `#1185`

## Scope

- Make Room2/Tildefriends failures diagnosable by phase:
  - room connection
  - `tunnel.connect`
  - tunneled inner SHS
  - inner MuxRPC
  - `createHistoryStream`
  - Tildefriends replication
- Keep protocol tracing opt-in and safe to share by default.
- Use Deciduous as the durable decision graph and this file as the attached experiment ledger for action `#1152`.

## Decision Graph Map

- Goal: `#1145` debug SSB Room2/Tildefriends protocol interop
- Observation: `#1151` Tildefriends e2e still fails without enough phase-local evidence
- Action: `#1152` build a debug harness for room tunnel, inner SHS, history, and e2e artifacts
- Outcome: `#1184` Room2/Tildefriends debug harness passed positive and broken e2e
- Outcome: `#1185` room-member ingest skips non-classic history frames cleanly

## Hypotheses

1. The minimal Room2 tunnel primitive is already correct; failures likely occur when the full bridge, Tildefriends, and bridged feed history path are combined.
2. Opening `tunnel.connect` is insufficient as a success criterion; the useful invariant is inner SHS plus `createHistoryStream` returning classic SSB envelopes for the expected bridged feed.
3. Debugging needs correlated metadata across logs and probes (`run_id`, `conn_id`, `stream_id`, `feed`, `method`, `req`, `phase`, `origin`, `portal`, `target`, `err_kind`, durations), not raw secretstream payloads.
4. Docker e2e failure artifacts should be collected at the failure point so the next investigation can identify the broken phase without rerunning blindly.

## Implementation Log

- Added opt-in protocol tracing behind `bridge-cli start --ssb-protocol-trace`.
  - Trace events are metadata-only and carry correlation fields.
  - Sensitive field names are redacted before logging.
  - Error values are normalized into shareable `err_kind` classes.
- Added trace emissions around:
  - MuxRPC request/response/stream frames
  - room tunnel handler forwarding
  - client tunnel inner SHS
  - bridged peer tunnel/inner SHS path
  - room-member ingest tunnel/history path
- Extended `cmd/room-tunnel-feed-verify` with `history` mode.
  - Connects to the room.
  - Opens `tunnel.connect`.
  - Completes tunneled inner SHS.
  - Calls `createHistoryStream`.
  - Validates classic SSB envelope shape.
  - Emits JSON summary with phase and count information.
- Upgraded bridge-cli integration coverage.
  - Active bridged-room peer integration now proves `tunnel.connect` plus inner SHS/history.
  - Added focused room-member ingest integration proving a room member can ingest a target feed through the room tunnel.
- Extended Tildefriends e2e debug controls.
  - `E2E_TF_DEBUG=1`
  - `E2E_TF_ARTIFACT_DIR`
  - `MVAB_TEST_RUN_ID`
  - Failure artifacts now include logs, room JSON, selected SQLite output, metrics/probe output, history verifier output, and `summary.md`.

## Correlation Model

Common fields:

- `run_id`: run-level correlation, usually `MVAB_TEST_RUN_ID`
- `conn_id`: connection-level correlation
- `stream_id`: byte or muxrpc stream correlation
- `feed`: relevant feed reference
- `method`: MuxRPC method name
- `req`: MuxRPC request ID
- `phase`: protocol phase or checkpoint name
- `origin`: local actor initiating the action
- `portal`: room feed or room address when useful
- `target`: tunneled target feed
- `err_kind`: redacted error class
- `duration`: elapsed time for operations with a meaningful boundary
- `bytes`: byte counts for stream framing and byte-stream boundaries

## Run Ledger

### `manual-20260414-room-tf-harness`

Commands run during implementation:

- `cd internal/ssb && go test -count=1 ./protocoltrace ./muxrpc`
  - Result: pass
  - Evidence: trace redaction/field propagation/error classification and byte-stream close behavior covered.
- `go test -count=1 ./cmd/bridge-cli -run 'TestRoomMemberIngestViaRoomTunnelInnerSHS|TestBridgedRoomPeerManagerReconcile|Test.*Config' -v`
  - Result: pass
  - Evidence: new focused ingest integration passes alongside nearby manager/config coverage.
- `bash -n scripts/e2e_tildefriends.sh infra/e2e-tildefriends/test_runner.sh infra/shared/bridge_entrypoint.sh`
  - Result: pass
  - Evidence: shell changes parse.
- `docker compose -f infra/e2e-tildefriends/docker-compose.e2e-tildefriends.yml config`
  - Result: pass
  - Evidence: compose wiring resolves with debug/run-id environment fields.
- `env MVAB_BRIDGED_ROOM_INTEGRATION=1 go test -count=1 ./cmd/bridge-cli -run TestBridgedRoomPeerManagerIntegrationActivePeersAndTunnelTargets -v`
  - Result: pass
  - Evidence: active bridged-room peer integration now reaches inner SHS/history.
- `go test -count=1 ./cmd/bridge-cli -run TestRoomMemberIngestViaRoomTunnelInnerSHS -v`
  - Result: pass
  - Evidence: room-member ingest path reads and appends tunneled history.
- `go test -count=1 ./internal/room -run 'TestTunnelConnectInnerProtocol|TestInboundTunnelConnectFromManyverseSimulatedClient' -v`
  - Result: pass
  - Evidence: existing inner-SHS room tunnel tests remain green.
- `go test -count=1 ./cmd/room-tunnel-feed-verify -run Test -v`
  - Result: pass
  - Evidence: history argument validation and classic envelope validation pass.
- `cd internal/ssb && go test -count=1 ./protocoltrace ./muxrpc ./muxrpc/handlers/room`
  - Result: pass
  - Evidence: trace, stream close, muxrpc, and room tunnel handler coverage pass.
- `go test -count=1 ./cmd/bridge-cli ./cmd/room-tunnel-feed-verify`
  - Result: pass
  - Evidence: command package tests pass after harness changes.

### `manual-20260414-room-tf-harness` e2e attempt

- Command: `env E2E_TF_DEBUG=1 MVAB_TEST_RUN_ID=manual-20260414-room-tf-harness ./scripts/e2e_tildefriends.sh`
- First result: Docker build failed during module download with transient network/proxy EOFs.
- Rerun result: e2e failed because Tildefriends did not replicate the bot feed within the timeout.
- Artifact directory: `tmp/e2e-tildefriends/manual-20260414-room-tf-harness`
- Key evidence:
  - `verifier-history.json` proved the Go harness could connect through the room, complete inner SHS, and read one bot feed envelope.
  - Tildefriends closed the room connection after `blobs.createWants` ended immediately.
- Decision: keep `blobs.createWants` open until the request context ends, even when there are no current wants.

### `manual-20260414-room-tf-harness-r2`

- Command: `env E2E_TF_DEBUG=1 MVAB_TEST_RUN_ID=manual-20260414-room-tf-harness-r2 ./scripts/e2e_tildefriends.sh`
- Result: e2e still failed before the final fix, but the room connection stayed alive long enough to exercise `tunnel.isRoom`, EBT, and bridge-initiated `tunnel.connect`.
- Artifact directory: `tmp/e2e-tildefriends/manual-20260414-room-tf-harness-r2`
- Key evidence:
  - Protocol trace reached the room-member ingest tunnel path.
  - `room_member_ingest_stream_failed` reported signature parsing against a Tildefriends history frame.
- Decision: use `legacy.ParseSignatureString` for SSB signatures in room-member ingest.

### `manual-20260414-room-tf-harness-r3`

- Command: `env E2E_TF_DEBUG=1 MVAB_TEST_RUN_ID=manual-20260414-room-tf-harness-r3 ./scripts/e2e_tildefriends.sh`
- Result: pass.
- Artifact directory: `tmp/e2e-tildefriends/manual-20260414-room-tf-harness-r3`
- Key evidence:
  - Tildefriends replicated bot feed growth `0 -> 3` through the strict room-only path.
  - Active bridged-room peers were present.
  - `verifier-history.json` reported `count: 1` for the bot feed and validated the classic envelope fields.
  - Reverse media pipeline check completed with expected no-real-PDS behavior.

### `manual-20260414-room-tf-harness-broken`

- Command: `env E2E_TF_DEBUG=1 E2E_TF_SCENARIO=broken-room E2E_TF_EXPECT=fail MVAB_TEST_RUN_ID=manual-20260414-room-tf-harness-broken ./scripts/e2e_tildefriends.sh`
- Result: pass as an expected failure.
- Artifact directory: `tmp/e2e-tildefriends/manual-20260414-room-tf-harness-broken`
- Key evidence:
  - `summary.md` records the phase-local failure: `invite-derived muxrpc port mismatch: got 8989, expected 39999`.
  - The host bundle includes compose logs, room state, room attendants/tunnels, selected SQLite outputs, and verifier files.

### `manual-20260414-room-tf-harness-r4`

- Command: `env E2E_TF_DEBUG=1 MVAB_TEST_RUN_ID=manual-20260414-room-tf-harness-r4 ./scripts/e2e_tildefriends.sh`
- Result: pass after the non-classic history frame follow-up.
- Artifact directory: `tmp/e2e-tildefriends/manual-20260414-room-tf-harness-r4`
- Key evidence:
  - Tildefriends replicated bot feed growth `0 -> 3` through the strict room-only path.
  - The new skip classification logged `room_member_ingest_history_skipped reason=non_classic_frame bytes=58`.
  - Protocol trace emitted the matching metadata-only event with `phase=room_member_ingest_history_skipped` and `err_kind=decode`.
  - `verifier-history.json` still reported `count: 1` for the bot feed.

## Artifact Layout

For Tildefriends e2e debug runs, the host artifact directory defaults to:

- `tmp/e2e-tildefriends/<MVAB_TEST_RUN_ID>/`

The test-runner container writes to:

- `/bridge-data/artifacts/<MVAB_TEST_RUN_ID>/`

Expected files include:

- `summary.md`
- `compose.log`
- `bridge.log`
- `tildefriends.log`
- `room-health.json`
- `room-healthz.txt`
- `room-attendants.json`
- `room-tunnels.json`
- `room-probe.log`
- `room-history.log`
- `verifier-history.json`
- `verifier-history.err`
- selected SQLite table dumps

## Decisions

- Treat `createHistoryStream` as the bridge-side success invariant for Room2 debug probes, not just `tunnel.connect` open/close.
- Keep trace logging metadata-only by default; payload capture can be a separate explicit workflow if needed later.
- Keep Docker/Tildefriends e2e outside default `go test ./...`.
- Do not write Deciduous nodes from inside Docker containers; the outer workflow owns graph updates.
- Keep `blobs.createWants` as a long-lived source; Tildefriends treats an immediate source close as a connection-level shutdown.
- Treat unrelated JSON on room-member `createHistoryStream` as a skipped non-classic frame, not a stream-fatal signature parse error. Direct classic signed messages remain accepted, and incomplete classic-looking messages remain strict errors.

## Next Actions

1. Consider making reverse-event processing stronger in a separate pass; the current Docker e2e still treats no-real-PDS reverse behavior as acceptable.
