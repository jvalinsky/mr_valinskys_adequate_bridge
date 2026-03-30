# Scratchpad Index

Development notes and debugging session records from the decision graph workflow.

## How Scratchpads Relate to Decision Graph

Scratchpads are created during implementation sessions. Each file corresponds to one or more decision graph nodes. See `deciduous nodes` for the full graph.

## EBT Replication & SSB Protocol

| File | Description | DG Nodes | Status |
|------|-------------|----------|--------|
| [022-ebt-replication-debugging.md](022-ebt-replication-debugging.md) | EBT debugging plan, 5 hypotheses, ByteSink buffering bug, message format issues, 7+ fixes | — | Historical |
| [030-ebt-debugging-session-report.md](030-ebt-debugging-session-report.md) | Comprehensive debugging report: 9 bugs found and fixed | — | Historical |
| [031-tildefriends-signature-fix-plan.md](031-tildefriends-signature-fix-plan.md) | Signature format analysis: JSON field ordering, base64 format, detailed C code references | — | **Pending** |
| [032-protocol-audit-fixes.md](032-protocol-audit-fixes.md) | Protocol audit findings: `key` field root cause, real signature verification, 5 protocol fixes | 638, 642 | Historical |

## Project Milestones (Historical)

| File | Description | Decision Graph Period |
|------|-------------|----------------------|
| [000-bootstrap-prompt.md](000-bootstrap-prompt.md) | Original AI bootstrapping prompt defining project goals, tech stack, TDD process | Inception |
| [001-project-init.md](001-project-init.md) | Go module initialization and directory structure (cmd/, internal/, web/, docs/) | Nodes 1-8 |
| [002-db-schema.md](002-db-schema.md) | SQLite schema design with `bridged_accounts`, `messages`, `blobs` tables | Node 9 |
| [003-firehose-client.md](003-firehose-client.md) | ATProto firehose client connecting to `subscribeRepos` via WebSocket | Node 10 |
| [004-mapper.md](004-mapper.md) | Record mapper converting ATProto posts/likes/reposts/follows/blocks/profiles to SSB | Node 11 |
| [005-bot-manager.md](005-bot-manager.md) | SSB bot manager deriving deterministic identities from ATProto DIDs via HMAC-SHA256 | Node 12 |
| [006-cli.md](006-cli.md) | CLI tool with account commands (list/add/remove) using urfave/cli | Node 13 |
| [007-web-ui.md](007-web-ui.md) | HTMX + TailwindCSS admin UI with chi router and dashboard | Node 14 |
| [008-ssb-publish-pipeline.md](008-ssb-publish-pipeline.md) | Built in-process SSB runtime with publisher injection, `--publish-workers` flag | Nodes 28-34 |
| [009-room2-runtime.md](009-room2-runtime.md) | Embedded Room2 runtime in same process as firehose bridge | Nodes 41-46 |
| [010-blob-bridge.md](010-blob-bridge.md) | Blob bridge service fetching ATProto blobs via `sync.getBlob` | Nodes 47-53 |
| [011-firehose-durability-backfill.md](011-firehose-durability-backfill.md) | Firehose cursor persistence, `bridge_state` table, `backfill` CLI command | Nodes 54-60 |
| [012-admin-ops-hardening.md](012-admin-ops-hardening.md) | Admin UI hardening with failure/blob/cursor views, structured logging | Nodes 61-66 |
| [013-room2-conformance.md](013-room2-conformance.md) | Real go-ssb-room/v2 integration replacing HTTP stub with typed adapter | Nodes 72-78 |
| [014-exposed-surface-security.md](014-exposed-surface-security.md) | HTTP Basic auth with fail-fast guardrails for non-loopback binds | Nodes 79-85 |
| [015-publish-retry-workers.md](015-publish-retry-workers.md) | Worker-lane publisher with per-DID ordering, bounded retry, `retry-failures` command | Nodes 86-92 |
| [016-release-smoke-and-runbook.md](016-release-smoke-and-runbook.md) | Deterministic smoke test harness, CI gate workflow, operational runbook | Nodes 93-106 |
| [017-next-cycle-scope.md](017-next-cycle-scope.md) | Next milestone plan: profile/block record type expansion (deferred) | Node 117+ |

## README Section Drafts (Completed)

These drafts were incorporated into the main README:

| File | Section |
|------|---------|
| [018-readme-overview.md](018-readme-overview.md) | Project overview, feature list, supported record types |
| [019-readme-quickstart-cli.md](019-readme-quickstart-cli.md) | Quick start commands and CLI command reference |
| [020-readme-architecture.md](020-readme-architecture.md) | Data flow diagram and internal packages table |
| [021-readme-dev-scripts-infra.md](021-readme-dev-scripts-infra.md) | Development workflow, scripts (smoke/E2E/local/testnet), Docker infra |

## Understanding Scratchpad Status

- **Historical**: Completed work, implementation details are in source code
- **Pending**: Work in progress or not yet fully implemented
- **Reference**: Draft content incorporated into other documents

## Debugging Tools Reference

Scripts created during debugging sessions (also documented in `scripts/`):

| File | Purpose |
|------|---------|
| `scripts/debug_ebt_state.sh` | Diagnose EBT state from inside Docker container |
| `scripts/debug_muxrpc_capture.sh` | Capture raw muxrpc traffic for protocol analysis |

## Quick Links

- [Main documentation index](../README.md)
- [EBT replication documentation](../ebt-replication.md)
- [Operational runbook](../runbook.md)
- [Decision graph](.deciduous/) — Run `deciduous nodes` for current state
