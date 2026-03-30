# Documentation Index

This repo contains four documentation areas:

## ATProto to SSB Bridge

The bridge logic has two separate translation layers:

1. ATProto account identity (`did:...`) to deterministic SSB feed identity (`@...`).
2. ATProto record references (`at://...`) to published SSB message refs (`%...`).

The detailed docs for that flow live here:

- [ATProto to SSB Translation Overview](./atproto-ssb-translation-overview.md)
- [ATProto to SSB Identity Mapping](./atproto-ssb-identity-mapping.md)
- [ATProto to SSB Record Translation](./atproto-ssb-record-translation.md)
- [Bridge Operator Runbook](./runbook.md)

## SSB Protocol

The bridge implements Secure Scuttlebutt protocols including EBT replication, Room2, and message signing. These documents cover the SSB protocol stack with ASCII diagrams and code examples:

- [SSB Protocol Fundamentals](./ssb-protocol-fundamentals.md) - Identity, feeds, messages, and signing
- [SSB Replication](./ssb-replication.md) - Secret handshake, box stream, MUXRPC, and EBT
- [SSB Rooms](./ssb-rooms.md) - Room2 architecture and tunnel connections
- [SSB Implementations](./ssb-implementations.md) - Go code examples from the bridge
- [EBT Replication Debugging](./ebt-replication.md) - Bridge-specific EBT debugging notes

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
