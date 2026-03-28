---
description: Run security checks against the bridge codebase - static analysis, dependency vulns, secrets, SQL injection, template safety, and more
allowed-tools: Bash(go vet:*, go build:*, govulncheck:*, staticcheck:*, deciduous:*), Read, Grep, Glob
argument-hint: [scope: all|sql|http|deps|secrets|templates|crypto|race|headers|errors|<file-path>]
---

# Security Check

**Audit the bridge codebase for security issues across 11 check categories.**

Run targeted or full-sweep security analysis with severity-rated findings and actionable remediation advice.

---

## Step 1: Decision Graph Node

```bash
deciduous add observation "Security check: $ARGUMENTS" -c 80
```

Store the node ID for linking findings at the end.

---

## Step 2: Determine Scope

Parse `$ARGUMENTS` to decide which checks to run:

| Argument | Checks Run |
|----------|-----------|
| `all` or empty | All 11 checks |
| `sql` | Check 3: SQL injection patterns |
| `http` | Checks 5, 7, 8, 9: Input validation, security headers, TLS/network, error leakage |
| `deps` | Check 2: Dependency vulnerabilities |
| `secrets` | Check 4: Hardcoded secrets |
| `templates` | Check 6: Template safety |
| `crypto` | Check 11: Crypto practices |
| `race` | Check 10: Race conditions |
| `headers` | Check 7: HTTP security headers |
| `errors` | Check 9: Error leakage |
| A file path | All checks scoped to that file and its package |

---

## Step 3: Run Checks

Run each applicable check below. For each finding, classify severity:

- **CRITICAL**: Exploitable vulnerability (SQL injection, hardcoded production secret, RCE)
- **HIGH**: Likely vulnerability requiring fix (InsecureSkipVerify, missing auth, math/rand for security)
- **MEDIUM**: Weakness worth addressing (missing security headers, verbose errors)
- **LOW**: Minor issue or defense-in-depth improvement
- **INFO**: Acceptable pattern worth noting, or suggestion

### Exclusions (apply to all checks)

- Skip `_test.go` files (test fixtures are acceptable)
- Skip `reference/` directory (vendored upstream code)
- Skip `vendor/` if present

---

### Check 1: Static Analysis (scope: `all`)

Run Go's built-in vet and staticcheck if available:

```bash
go vet ./...
```

```bash
# Only if installed
which staticcheck && staticcheck ./...
```

- Severity: MEDIUM for most findings, HIGH for memory safety or concurrency issues

---

### Check 2: Dependency Vulnerabilities (scope: `deps`)

```bash
# Preferred — install if missing: go install golang.org/x/vuln/cmd/govulncheck@latest
which govulncheck && govulncheck ./...
```

Also review `go.mod` for:
- `replace` directives pointing to local paths (flag for manual review — this project legitimately uses `replace` for `go-ssb-room` and `go-ssb`)
- Severely outdated dependencies

- Severity: CRITICAL for known exploited vulns, HIGH for other CVEs, INFO for local replace directives

---

### Check 3: SQL Injection Patterns (scope: `sql`)

Search for unsafe SQL construction patterns in `.go` files (excluding tests and `reference/`):

1. **`fmt.Sprintf` near SQL keywords** — grep for `Sprintf` in files that also contain `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `WHERE`, `ORDER BY`
2. **String concatenation in query strings** — look for `"SELECT` or `"WHERE` combined with `+` string concatenation
3. **Non-parameterized Exec/Query calls** — check that all `QueryContext`, `ExecContext`, `Query`, `Exec` calls use `?` placeholders with separate args

Key file: `internal/db/db.go` — this project uses `database/sql` with `?` placeholders throughout. Verify that pattern holds everywhere and flag any deviations.

Also check `internal/db/` for any dynamic `ORDER BY` or `LIMIT` built via string formatting (the `ListMessagesPage` function builds queries dynamically — verify the sort column is validated against an allowlist, not passed raw from user input).

- Severity: CRITICAL for confirmed string interpolation in SQL, MEDIUM for suspicious patterns needing manual review

---

### Check 4: Hardcoded Secrets (scope: `secrets`)

Grep for these patterns in non-test `.go` files (case-insensitive):

1. `password\s*[:=]\s*"[^"]+"` — hardcoded passwords
2. `token\s*[:=]\s*"[^"]+"` — hardcoded tokens
3. `secret\s*[:=]\s*"[^"]+"` — hardcoded secrets
4. `apikey\s*[:=]\s*"[^"]+"` or `api_key` — API keys
5. `seed\s*[:=]\s*"[^"]+"` — seed values

Also check:
- `cmd/bridge-cli/main.go` uses `"dev-insecure-seed-change-me"` as a default seed — this is intentional for dev but flag as INFO to remind about production configuration
- `internal/bots/` — verify `masterSeed` is always passed in, never hardcoded
- Look for `.env` files or config files with secrets committed to the repo

- Severity: CRITICAL for hardcoded production secrets, LOW for dev defaults with clear naming, INFO for patterns worth verifying

---

### Check 5: Input Validation (scope: `http`)

Find all HTTP handler functions and check for missing input validation:

1. Grep for `func.*http.ResponseWriter.*http.Request` to find all handlers
2. For each handler that reads user input (`r.URL.Query().Get`, `r.FormValue`, `chi.URLParam`, `r.Body`), check that input is validated before use
3. Check that DID values from URL parameters are validated with `syntax.ParseDID()` or equivalent

Key files:
- `internal/web/handlers/ui.go` — admin UI (behind BasicAuth, so MEDIUM severity for issues)
- `internal/room/public.go` — public-facing (HIGH severity for issues)

Check specifically:
- Are query parameters like `at_uri`, `search`, `type`, `state`, `did` sanitized?
- Are pagination parameters validated (numeric, within bounds)?
- Does the `/bots/{did}` route in `public.go` validate the DID parameter?

- Severity: HIGH for missing validation on public endpoints, MEDIUM for admin-only endpoints

---

### Check 6: Template Safety (scope: `templates`)

1. **Verify `html/template` usage** — grep for `"text/template"` imports in non-test files. Should find zero results. Any use of `text/template` for rendering HTML is a **HIGH** XSS risk.

2. **Find escaping bypasses** — grep for `template.HTML(`, `template.HTMLAttr(`, `template.JS(`, `template.CSS(`, `template.URL(` type conversions. For each:
   - Check if the value is static/developer-controlled → INFO
   - Check if the value could contain user input → HIGH

Key file: `internal/web/templates/templates.go` — known to use `template.HTMLAttr` for `aria-current` attributes with static strings (this is safe, classify as INFO).

- Severity: HIGH for user-controlled values in template bypass types, INFO for static developer strings

---

### Check 7: HTTP Security Headers (scope: `headers`)

Check for presence of security headers in middleware and handler setup:

1. Grep for `Header().Set` or `Header().Add` calls across handler and middleware files
2. Check for these headers:
   - `Content-Security-Policy` — prevents XSS, clickjacking
   - `X-Frame-Options` — prevents clickjacking
   - `X-Content-Type-Options` — prevents MIME sniffing
   - `Strict-Transport-Security` — enforces HTTPS
   - `Referrer-Policy` — controls referrer leakage
   - `Permissions-Policy` — restricts browser features
3. Check that authenticated endpoints set `Cache-Control: no-store` or equivalent

Key file: `internal/web/security/middleware.go`

- Severity: MEDIUM for missing headers on public pages (`internal/room/public.go`), LOW for admin-only pages behind BasicAuth

---

### Check 8: TLS / Network Safety (scope: `http`)

1. **Insecure URLs** — grep for `http://` in non-test `.go` files. Exclude:
   - `localhost`, `127.0.0.1`, `::1` references (local dev)
   - `httptest.` references
   - Comment lines
   - String constants for URL scheme comparison

2. **TLS bypass** — grep for `InsecureSkipVerify` in all `.go` files
3. **Default transport weakening** — grep for `http.DefaultTransport` being modified

Key files: `internal/backfill/pds.go`, `internal/firehose/client.go`, `internal/blobbridge/bridge.go`

- Severity: HIGH for `InsecureSkipVerify: true`, MEDIUM for hardcoded `http://` to external services

---

### Check 9: Error Information Leakage (scope: `errors`)

1. Check all `http.Error(w,` calls — does the error message include internal details?
   - Stack traces → HIGH
   - SQL error messages → HIGH
   - File paths → MEDIUM
   - Generic "internal error" → OK

2. Check that `fmt.Errorf` wrapping in HTTP handlers does not surface internal errors directly to the response writer
3. Verify `/healthz` endpoint does not leak sensitive operational details (should only return status, not config/state)

Key files: `internal/web/handlers/ui.go`, `internal/room/public.go`

- Severity: MEDIUM for detailed error messages in HTTP responses, LOW for verbose health checks

---

### Check 10: Race Conditions (scope: `race`)

1. **Build with race detector:**
   ```bash
   go build -race ./cmd/bridge-cli/
   ```
   This checks that the code compiles cleanly with the race detector (runtime detection requires running the binary).

2. **Review shared mutable state:**
   - Grep for global `var` declarations that are written to (not just read)
   - Check that `internal/bots/` mutex usage (the publisher cache uses `sync.Mutex`) follows consistent lock/unlock discipline
   - Look for goroutines accessing shared maps or slices without synchronization

- Severity: HIGH for confirmed races, MEDIUM for suspicious unprotected shared state

---

### Check 11: Crypto Practices (scope: `crypto`)

1. **`math/rand` vs `crypto/rand`** — grep for `"math/rand"` imports in non-test files.
   - `math/rand` for reconnect jitter/backoff in `internal/firehose/client.go` is acceptable (INFO)
   - `math/rand` for anything security-sensitive (tokens, keys, nonces) is HIGH

2. **Constant-time comparison** — verify `crypto/subtle.ConstantTimeCompare` is used for all auth/secret comparisons. Already confirmed in `internal/web/security/middleware.go` — check for any other auth paths.

3. **Key derivation** — review `internal/bots/` for:
   - HMAC-SHA256 usage for seed derivation (expected)
   - No seed logging or exposure in error messages
   - Seed never appears in HTTP responses or DB in plaintext

4. **Deprecated algorithms** — grep for imports of `crypto/md5`, `crypto/sha1`, `crypto/des`, `crypto/rc4` in non-test files. Note: SHA1/MD5 for content-addressing (not security) is acceptable (INFO).

- Severity: HIGH for `math/rand` in security contexts, MEDIUM for deprecated algorithms in security contexts, INFO for acceptable uses

---

## Step 4: Compile Report

After running all applicable checks, produce a structured report:

```
## Security Check Report — Scope: <scope>

### Summary
- CRITICAL: N findings
- HIGH: N findings
- MEDIUM: N findings
- LOW: N findings
- INFO: N findings

### Findings

#### [SEVERITY] Finding Title
- **File**: path/to/file.go:LINE
- **Check**: Which check found this
- **Description**: What was found
- **Risk**: Why this matters for the bridge
- **Remediation**: Specific fix recommendation

### Clean Checks
- List checks that passed with no findings
```

---

## Step 5: Decision Graph Outcome

```bash
# If CRITICAL or HIGH findings exist:
deciduous add observation "Security: N critical, N high findings in <scope>" -c 60 -f "<comma-separated-files>"
deciduous link <check_node_id> <observation_id> -r "Security findings"

# If all clean:
deciduous add observation "Security check clean: <scope>" -c 95
deciduous link <check_node_id> <observation_id> -r "No findings"
```

---

## Prioritization Notes

- Focus remediation on CRITICAL and HIGH first
- `math/rand` for jitter/backoff is acceptable — do not flag as HIGH
- `template.HTMLAttr` for static `aria-current` values is acceptable — INFO only
- Admin UI endpoints behind BasicAuth have lower severity than public-facing `internal/room/public.go` routes
- The `"dev-insecure-seed-change-me"` default is intentional for dev — INFO, not a vulnerability
- `replace` directives in `go.mod` for local SSB forks are expected — INFO for awareness only

---

**Now run security check: $ARGUMENTS**
