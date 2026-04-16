---
description: Build the project and run the test suite
arguments:
  - name: PATTERN
    description: Optional test pattern to filter tests
    required: false
---

# Build and Test

Build the project and run the test suite.

## Instructions

1. Run the full build and test cycle:
   ```bash
   GOFLAGS=-mod=mod go test ./...
   ```

2. If tests fail, analyze the failures and explain:
   - Which test failed
   - What it was testing
   - Likely cause of failure
   - Suggested fix

3. If all tests pass, report success and any warnings from the build.

4. If the user specifies a specific test pattern, run only those tests:
   ```bash
   GOFLAGS=-mod=mod go test ./... -run "$PATTERN"
   ```

## Test categories in this project
- `cmd/room-tunnel-feed-verify` - SSB room/tunnel probe tooling
- `cmd/room-replication-compliance-probe` - mobile-style replication compliance probe
- `internal/ssb/...` - muxrpc, SHS, feedlog, blobs, EBT, and room protocol behavior
- `internal/web/...` - bridge admin UI handlers and templates
- `pkg/atproto/...` - ATProto repo, syntax, firehose, and XRPC primitives
