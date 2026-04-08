# Documentation Index

This index covers the project-owned documentation that tracks the current codebase. Dated files in `docs/investigations/`, `docs/ssbc-*.md`, and `docs/scratchpad/` are snapshots of earlier work, not living operator guides.

## Start Here

- [Root README](../README.md) - Project overview, quick start, runtime modes, and main tooling.
- [Bridge Operator Runbook](./runbook.md) - Runtime procedures, deployment notes, and incident triage.
- [Contributor Setup Profiles](./agents.md) - Which local or production profile to use for common tasks.
- [Documentation Guide](./documentation-guide.md) - House style and maintenance rules for docs in this repo.

## Environments and Test Stacks

- [Local ATProto Stack](../infra/local-atproto/README.md) - Local PLC, relay, and PDS stack used by development and local E2E.
- [Docker E2E Stack](../infra/e2e-full/README.md) - Full containerized bridge, reverse bootstrap, admin UI, and Tildefriends coverage.

## Bridge Behavior

- [ATProto to SSB Translation Overview](./atproto-ssb-translation-overview.md) - Forward-bridge model: DID, message, and blob mapping.
- [ATProto to SSB Identity Mapping](./atproto-ssb-identity-mapping.md) - Deterministic DID-to-feed derivation and account activation policy.
- [ATProto to SSB Record Translation](./atproto-ssb-record-translation.md) - Placeholder fields, dependency resolution, and deferred records.
- [SSB to ATProto Reverse Sync](./reverse-sync.md) - Optional reverse path for allowlisted SSB feeds.
- [Per-DID Rate Limiting](./rate-limiting.md) - Forward-bridge rate limiter behavior and configuration.

## SSB and Interoperability

- [SSB Protocol Fundamentals](./ssb-protocol-fundamentals.md) - Identity, feeds, message format, and signing rules.
- [SSB Replication](./ssb-replication.md) - SHS, box stream, muxrpc, classic replication, and EBT.
- [SSB Rooms](./ssb-rooms.md) - Room2 model, tunnel behavior, and room-specific APIs.
- [SSB Implementation Reference](./ssb-implementations.md) - File map for the repo's SSB implementation.
- [EBT Replication Notes](./ebt-replication.md) - Bridge-specific EBT and room debugging entrypoints.

## Historical Reports

- [SSBC Compliance Migration Notes](./ssbc-compliance-migration-notes.md) - Migration summary after SIP/SSBC compatibility work.
- [SSBC / SIP Compliance Review (2026-04-01)](./ssbc-sip-compliance-review-2026-04-01.md) - Dated audit snapshot.
- [SSBC / SIP Compliance Review (2026-04-06)](./ssbc-sip-compliance-review-2026-04-06.md) - Follow-up audit snapshot.
- [Investigations](./investigations/planetary-ios-no-sync-2026-04-06.md) - Dated incident write-ups under `docs/investigations/`.
- [Scratchpad Index](./scratchpad/README.md) - Archived design notes, implementation logs, and partial plans.

## Code References

### Bridge runtime

- [`cmd/bridge-cli/main.go`](../cmd/bridge-cli/main.go)
- [`cmd/bridge-cli/app.go`](../cmd/bridge-cli/app.go)
- [`internal/atindex/service.go`](../internal/atindex/service.go)
- [`internal/bridge/processor.go`](../internal/bridge/processor.go)
- [`internal/bridge/reverse_sync.go`](../internal/bridge/reverse_sync.go)
- [`internal/mapper/mapper.go`](../internal/mapper/mapper.go)
- [`internal/db/schema.sql`](../internal/db/schema.sql)

### SSB runtime and room integration

- [`cmd/ssb-client/main.go`](../cmd/ssb-client/main.go)
- [`cmd/bridge-cli/bridged_room_peers.go`](../cmd/bridge-cli/bridged_room_peers.go)
- [`cmd/bridge-cli/room_member_ingest.go`](../cmd/bridge-cli/room_member_ingest.go)
- [`internal/ssbruntime/runtime.go`](../internal/ssbruntime/runtime.go)
- [`internal/ssb/sbot/sbot.go`](../internal/ssb/sbot/sbot.go)
- [`internal/ssb/replication/ebt.go`](../internal/ssb/replication/ebt.go)
- [`internal/room/`](../internal/room/)

### Admin UI and tooling

- [`internal/web/handlers/ui.go`](../internal/web/handlers/ui.go)
- [`internal/web/handlers/reverse.go`](../internal/web/handlers/reverse.go)
- [`internal/web/templates/templates.go`](../internal/web/templates/templates.go)
- [`cmd/room-tunnel-feed-verify/main.go`](../cmd/room-tunnel-feed-verify/main.go)
