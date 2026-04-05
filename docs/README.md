# Documentation Index

This repo contains four documentation areas. For AI agent instructions, see [CLAUDE.md](../CLAUDE.md).

## Getting Started

If you are new to the project or looking to understand how to contribute, start here:

- [Agent Setup Profiles](./agents.md) - Fast local-vs-production setup matrix for contributors.
- [Documentation Guide](./documentation-guide.md) - Instructions and style guidelines for updating documentation.

## Setup and Deployment

Use these docs based on environment:

- [Bridge Operator Runbook](./runbook.md) - Local Docker workflow plus production operations.
- [Per-DID Rate Limiting](./rate-limiting.md) - Configuration details for preventing spam.

## Architecture and Core Logic

### ATProto to SSB Translation

The bridge logic has two separate translation layers. The detailed docs for that flow live here:

- [ATProto to SSB Translation Overview](./atproto-ssb-translation-overview.md) - High level overview of translation layers.
- [ATProto to SSB Identity Mapping](./atproto-ssb-identity-mapping.md) - ATProto account identity (`did:...`) to deterministic SSB feed identity (`@...`).
- [ATProto to SSB Record Translation](./atproto-ssb-record-translation.md) - ATProto record references (`at://...`) to published SSB message refs (`%...`).

### SSB Protocol implementations

The bridge implements Secure Scuttlebutt protocols including EBT replication, Room2, and message signing. These documents cover the protocol stack with ASCII diagrams and code examples:

- [SSB Protocol Fundamentals](./ssb-protocol-fundamentals.md) - Identity, feeds, messages, and signing.
- [SSB Replication](./ssb-replication.md) - Secret handshake, box stream, MUXRPC, and EBT.
- [SSB Rooms](./ssb-rooms.md) - Room2 architecture and tunnel connections.
- [SSB Implementations](./ssb-implementations.md) - Go code examples from the bridge.
- [EBT Replication Debugging](./ebt-replication.md) - Bridge-specific EBT debugging notes.

## Scratchpad Index

Development notes and debugging sessions are indexed in [scratchpad/README.md](scratchpad/README.md).

## Code References

### Bridge Core

- [`cmd/bridge-cli/main.go`](../cmd/bridge-cli/main.go)
- [`internal/bots/manager.go`](../internal/bots/manager.go)
- [`internal/ssbruntime/runtime.go`](../internal/ssbruntime/runtime.go)
- [`internal/bridge/processor.go`](../internal/bridge/processor.go)
- [`internal/bridge/dependencies.go`](../internal/bridge/dependencies.go)
- [`internal/mapper/mapper.go`](../internal/mapper/mapper.go)
- [`internal/config/constants.go`](../internal/config/constants.go) - Rate limiting defaults
- [`internal/db/schema.sql`](../internal/db/schema.sql)

### SSB Protocol Implementation

- [`internal/ssb/sbot/feed_manager_adapter.go`](../internal/ssb/sbot/feed_manager_adapter.go)
- [`internal/ssb/message/legacy/sign.go`](../internal/ssb/message/legacy/sign.go)
- [`internal/ssb/replication/ebt.go`](../internal/ssb/replication/ebt.go)
- [`internal/ssb/muxrpc/`](../internal/ssb/muxrpc/)
- [`internal/ssb/secretstream/`](../internal/ssb/secretstream/)
- [`internal/ssb/network/`](../internal/ssb/network/)
- [`internal/room/`](../internal/room/)

### Reference Implementations

- [`reference/tildefriends/src/ssb.c`](../reference/tildefriends/src/ssb.c) - Tildefriends SSB implementation
