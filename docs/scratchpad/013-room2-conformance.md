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
- `go-ssb-room/v2` exported API uses internal package types in constructor signatures; adapter uses reflection for `roomsrv.New` network-details construction.
- The sign-in bridge argument is passed as nil in this adapter path, so Sign-in-with-SSB paths should be treated as out-of-scope unless explicitly validated and wired.
- Additional integration tests should assert Room websocket/muxrpc behavior under signal shutdown and restart loops.
