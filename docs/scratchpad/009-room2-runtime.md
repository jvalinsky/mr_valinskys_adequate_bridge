# 009 - Hybrid Room Runtime

## Objective
Run room runtime and bridge firehose processor in the same process with shared context lifecycle and graceful shutdown.

## Chosen Option + Rejected Option
Chosen option: add an embedded room runtime component (`internal/room`) started by `bridge-cli start` with shared cancelation context.
Rejected option: run room as a separate process supervised externally.
Reason: single-process lifecycle simplifies startup/shutdown behavior and satisfies hybrid runtime orchestration requirements.

## Interfaces / Flags / Schema Touched
- CLI `start` flags added:
  - `--room-enable` (default `true`)
  - `--room-listen-addr`
- Runtime orchestration:
  - start room runtime + firehose bridge in one command
  - shared signal context for coordinated shutdown
- Logging:
  - room start/stop and runtime errors are logged with structured event keys

## Test Evidence
- `go test ./...` passing.
- `internal/room/runtime_test.go` covers runtime start + `/healthz` endpoint where sandbox allows listeners; test skips under sandbox port restrictions.

## Risks and Follow-ups
- Embedded room runtime is currently a minimal runtime endpoint for lifecycle orchestration; deeper Room2 protocol surface integration remains a next hardening step.
- Need future conformance tests against full Room2 expectations and peer interoperability.
