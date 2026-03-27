# 012 - Admin and Ops Hardening

## Objective
Make operational state visible through CLI/UI: publish failures, blob sync status, and cursor state, plus structured logging fields for bridge records.

## Chosen Option + Rejected Option
Chosen option: expand existing Go template admin UI and add `serve-ui` command for direct runtime visibility.
Rejected option: defer all observability to external logs only.
Reason: operators need low-friction local introspection for bridge health and incident triage.

## Interfaces / Flags / Schema Touched
- New CLI command:
  - `bridge-cli serve-ui --listen-addr <addr>`
- UI routes/pages added:
  - `/failures`
  - `/blobs`
  - `/state`
  - dashboard now includes publish/failure/blob/cursor metrics
- Logging fields added in processor/runtime paths:
  - `did`
  - `at_uri`
  - `record_type`
  - `seq`
  - `ssb_msg_ref`

## Test Evidence
- `go test ./...` passing for all Go packages.
- UI rendering and handler code compiles and serves via `serve-ui` command.
- Failure/blob/cursor data backed by DB methods with unit test coverage in `internal/db` and `internal/bridge` paths.

## Risks and Follow-ups
- No authentication/authorization on admin UI yet.
- UI is server-rendered and intentionally simple; no live push updates.
- Add operational smoke script (account add -> ingest/backfill -> publish -> UI verify) for release gate automation.
