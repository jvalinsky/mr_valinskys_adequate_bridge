# SSB Implementation Reference

This document is a file map for the SSB implementation in this repository. It points to the code that matters when you are reading or changing the bridge's SSB behavior. Use the protocol docs for background; use this page when you need to find the implementation.

## Message Model and Identity

| File | Role |
| --- | --- |
| [`internal/ssb/refs/refs.go`](../internal/ssb/refs/refs.go) | Feed, message, and blob ref parsing and formatting |
| [`internal/ssb/keys/io.go`](../internal/ssb/keys/io.go) | Secret key serialization and loading |
| [`internal/ssb/message/legacy/message.go`](../internal/ssb/message/legacy/message.go) | Classic message representation and verification |
| [`internal/ssb/message/legacy/sign.go`](../internal/ssb/message/legacy/sign.go) | Canonical JSON and message signing |

Start here when you are debugging:
- message ref formatting
- key import/export
- signature mismatches
- classic-message JSON layout

## Feed Storage and Local Node

| File | Role |
| --- | --- |
| [`internal/ssb/feedlog/feedlog.go`](../internal/ssb/feedlog/feedlog.go) | Stored-message log used by the local sbot |
| [`internal/ssb/sbot/sbot.go`](../internal/ssb/sbot/sbot.go) | Local node wiring, publishers, receive log, and handlers |
| [`internal/ssb/sbot/feed_manager_adapter.go`](../internal/ssb/sbot/feed_manager_adapter.go) | Feed adapter used by EBT replication |
| [`internal/ssbruntime/runtime.go`](../internal/ssbruntime/runtime.go) | Bridge-facing wrapper around the local SSB runtime |

Read these files when you need to understand:
- how the bridge publishes into SSB
- how receive-log entries are stored
- which feeds are exposed for replication
- how the bridge resolves DID-to-feed mappings at publish time

## Replication, Networking, and Transport

| File or directory | Role |
| --- | --- |
| [`internal/ssb/replication/ebt.go`](../internal/ssb/replication/ebt.go) | EBT state matrix and frontier diff logic |
| [`internal/ssb/muxrpc/`](../internal/ssb/muxrpc/) | Muxrpc framing, stream handling, and request routing |
| [`internal/ssb/secretstream/`](../internal/ssb/secretstream/) | SHS and box-stream transport |
| [`internal/ssb/network/`](../internal/ssb/network/) | Outbound SSB peer connection helpers |

Read these files when you are working on:
- `createHistoryStream`
- `ebt.replicate`
- tunnel streams
- SHS and encrypted transport behavior

## Room Integration

| File | Role |
| --- | --- |
| [`internal/room/`](../internal/room/) | Embedded Room2 runtime |
| [`cmd/bridge-cli/bridged_room_peers.go`](../cmd/bridge-cli/bridged_room_peers.go) | Per-bridged-account room peer sessions |
| [`cmd/bridge-cli/room_member_ingest.go`](../cmd/bridge-cli/room_member_ingest.go) | Ingests room member history into the local receive log |
| [`cmd/room-tunnel-feed-verify/main.go`](../cmd/room-tunnel-feed-verify/main.go) | Strict tunnel verifier used by interop tests |

Read these files when you need to understand:
- how the embedded room is started and supervised
- how bridged feeds appear as room peers
- how tunnel-based verification works in tests
- how room member activity reaches the local SSB node

## Bridge Touchpoints

These are not SSB protocol packages, but they are where SSB behavior is wired into the bridge:

| File | Role |
| --- | --- |
| [`cmd/bridge-cli/app.go`](../cmd/bridge-cli/app.go) | Builds the runtime, room, MCP server, metrics server, and reverse processor |
| [`internal/bridge/processor.go`](../internal/bridge/processor.go) | Forward bridge that publishes mapped ATProto records into SSB |
| [`internal/bridge/reverse_sync.go`](../internal/bridge/reverse_sync.go) | Reverse sync that reads SSB receive-log entries and writes ATProto records |
| [`internal/db/schema.sql`](../internal/db/schema.sql) | SQLite tables for forward bridge state, reverse events, peers, and DMs |

## Useful Tests

| Path | Coverage |
| --- | --- |
| [`internal/ssb/`](../internal/ssb/) | Unit tests for refs, messages, feed log, sbot, replication, and keys |
| [`cmd/bridge-cli/bridged_room_peers_test.go`](../cmd/bridge-cli/bridged_room_peers_test.go) | Bridged room peer behavior |
| [`internal/e2e/e2e_test.go`](../internal/e2e/e2e_test.go) | End-to-end bridge coverage |
| [`internal/livee2e/live_reverse_ssb_client_test.go`](../internal/livee2e/live_reverse_ssb_client_test.go) | Live reverse sync using `ssb-client` |

## Related Docs

- [SSB Protocol Fundamentals](./ssb-protocol-fundamentals.md)
- [SSB Replication](./ssb-replication.md)
- [SSB Rooms](./ssb-rooms.md)
- [EBT Replication Notes](./ebt-replication.md)
