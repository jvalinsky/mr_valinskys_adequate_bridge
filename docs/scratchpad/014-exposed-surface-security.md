# 014 - Exposed Surface Security

## Objective
Harden `serve-ui` for internet-exposed operation with mandatory auth on non-loopback binds, credentials from environment, and redacted request logging.

## Chosen Option + Rejected Option Summary
- Chosen: application-level HTTP Basic auth with explicit startup guardrails.
- Rejected: rely only on perimeter/network controls and keep UI unauthenticated.

## Interfaces/Flags/Schema Touched
- `cmd/bridge-cli/main.go`
  - `serve-ui` flags added:
    - `--ui-auth-user`
    - `--ui-auth-pass-env`
  - Startup guard:
    - fail fast when bind address is non-loopback and auth is not configured.
  - Credentials handling:
    - password is read from named env var; empty/unset values are rejected.
- `internal/web/security/middleware.go`
  - `RequireAuthForBind()` and loopback classification helpers.
  - `BasicAuthMiddleware()` for UI route protection.
  - `RequestLogMiddleware()` with sensitive query field redaction.
- DB schema: unchanged in this track.

## Test Evidence
- `GOCACHE=/tmp/go-build-cache go test ./internal/web/security ./cmd/bridge-cli`
  - security middleware tests pass.
  - CLI compiles with new auth flags/guards.
- Added tests in `internal/web/security/middleware_test.go`:
  - loopback vs non-loopback auth requirement behavior
  - Basic auth allow/deny paths
  - request log redaction for sensitive query fields

## Risks and Follow-ups
- Basic auth is sufficient for this cycle but should be paired with TLS termination in production.
- Current redaction focuses on query keys; future POST/admin mutations should keep structured payload redaction centrally enforced if request bodies are logged.
