# 016 - Release Smoke and Runbook

## Objective
Add release-grade deterministic smoke validation, wire it into CI as a gated check, and document operator procedures for startup, resume, retries, and incident triage.

## Chosen Option + Rejected Option Summary
- Chosen: deterministic in-repo smoke harness (`go test` driven) plus shell wrapper + CI workflow + runbook.
- Rejected: rely only on unit tests and defer release operations documentation.

## Interfaces/Flags/Schema Touched
- `internal/smoke/smoke_test.go`
  - Added deterministic end-to-end smoke scenario covering:
    - account seeding
    - fixture processing (post/like/repost)
    - persisted SSB publish refs
    - UI auth-protected page checks for success/failure/cursor state
- `scripts/smoke_bridge.sh`
  - Added reproducible smoke command wrapper.
- `.github/workflows/bridge-smoke.yml`
  - Added CI gate job running deterministic smoke workflow.
- `docs/runbook.md`
  - Added startup, restart/resume, retry-drain, and incident triage procedures.

## Test Evidence
- `./scripts/smoke_bridge.sh`
  - executes deterministic smoke test via `go test ./internal/smoke -run TestBridgeSmoke -count=1`.
- CI workflow prepared to run same deterministic smoke path on `push` and `pull_request`.

## Risks and Follow-ups
- CI stability depends on module availability and runner environment parity.
- Additional production-like smoke checks (real relay/firehose interaction) can be layered later as non-blocking nightly jobs.
