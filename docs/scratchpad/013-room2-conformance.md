# 013 - Room2 Conformance

## Objective
Replace the embedded room HTTP stub with a real `go-ssb-room/v2` runtime integrated into `bridge-cli start`, with shared lifecycle and ordered teardown.

## Chosen Option + Rejected Option Summary
- Chosen: in-process `go-ssb-room/v2` adapter runtime in `internal/room` with explicit config validation and shared context.
- Rejected: keep the previous minimal HTTP-only room stub and defer real Room2 behavior.

## Interfaces/Flags/Schema Touched
- `internal/room/runtime.go`
  - `Start(ctx, Config, logger)` now boots a real Room2 runtime.
  - `Config` includes `ListenAddr`, `HTTPListenAddr`, `RepoPath`, `Mode`, `HTTPSDomain`.
  - Added validation for mode and non-loopback domain requirement.
  - Added `HTTPAddr()` for reporting Room HTTP interface address.
- `cmd/bridge-cli/main.go`
  - `start` flags added:
    - `--room-http-listen-addr`
    - `--room-repo-path`
    - `--room-mode`
    - `--room-https-domain`
  - Updated room startup call to pass `room.Config`.
  - Ordered shutdown enforced: firehose cancel -> room close -> SSB runtime close.
- DB schema: unchanged in this track.

## Test Evidence
- `GOCACHE=/tmp/go-build-cache go test ./internal/room ./cmd/bridge-cli`
  - `internal/room` tests pass.
  - `cmd/bridge-cli` compiles.
- Added/updated tests in `internal/room/runtime_test.go`:
  - rejects invalid mode
  - requires domain for non-loopback exposure
  - starts runtime and serves `/healthz`

## Risks and Follow-ups
- Reflection-based constructor wiring has been removed by introducing a typed adapter layer at `reference/go-ssb-room/bridgeadapter/runtime.go`.
- The adapter now provides a concrete Sign-in-with-SSB bridge object; end-to-end Sign-in-with-SSB behavior should still be validated separately before relying on it operationally.
- Additional integration tests should assert Room websocket/muxrpc behavior under signal shutdown and restart loops.
