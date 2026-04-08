# EBT Replication Notes

This document collects the bridge-specific entrypoints, files, and checks used when EBT or Room2 replication is not behaving as expected. It is not a protocol history. It is a practical guide to the current implementation.

## What Runs in This Repo

The bridge's SSB path is split across a few layers:

| Layer | File or package |
| --- | --- |
| Local SSB runtime | [`internal/ssbruntime/runtime.go`](../internal/ssbruntime/runtime.go) |
| Local sbot and publishers | [`internal/ssb/sbot/sbot.go`](../internal/ssb/sbot/sbot.go) |
| EBT state matrix | [`internal/ssb/replication/ebt.go`](../internal/ssb/replication/ebt.go) |
| Feed adapter for history delivery | [`internal/ssb/sbot/feed_manager_adapter.go`](../internal/ssb/sbot/feed_manager_adapter.go) |
| Muxrpc streams and handlers | [`internal/ssb/muxrpc/`](../internal/ssb/muxrpc/) |
| SHS and box stream | [`internal/ssb/secretstream/`](../internal/ssb/secretstream/) |
| Embedded Room2 runtime | [`internal/room/`](../internal/room/) |
| Bridged room peer sessions | [`cmd/bridge-cli/bridged_room_peers.go`](../cmd/bridge-cli/bridged_room_peers.go) |
| Room member ingest | [`cmd/bridge-cli/room_member_ingest.go`](../cmd/bridge-cli/room_member_ingest.go) |

## Test Entry Points

Use the smallest test stack that answers the question you have.

| Entry point | What it checks |
| --- | --- |
| `./scripts/smoke_bridge.sh` | Forward bridge without room interoperability |
| `./scripts/e2e_tildefriends.sh` | Room2 and EBT replication with Tildefriends |
| `./scripts/e2e_full_up.sh` | Full Docker stack with reverse bootstrap, admin UI, and Tildefriends |
| `./scripts/local_room_peer_verify.sh` | Strict room tunnel verification |
| `./scripts/local_room_peer_verify_relaxed.sh` | Reduced room verification when strict tunnel assertions are too strong for the target environment |
| `GOFLAGS=-mod=mod go test ./internal/livee2e -run TestBridgeLiveInteropReverseSSBClient` | Live reverse-sync coverage using `ssb-client` |

## What to Check First

1. Confirm the bridge is actually publishing SSB messages.
   - `bridge-cli --db bridge.sqlite stats`
   - admin UI `/messages` and `/feed`
2. Confirm the room is live.
   - room HTTP `/healthz`
   - logs containing `event=room_dial_success` and `event=room_tunnel_announce_success`
3. Confirm the peer or verifier is reaching the room.
   - `./scripts/local_room_peer_verify.sh`
   - `cmd/room-tunnel-feed-verify`
4. Confirm the runtime is exporting the expected feeds.
   - `internal/ssbruntime/runtime.go`
   - `internal/ssb/replication/ebt.go`

## Useful Debugging Tools

| Tool | Purpose |
| --- | --- |
| [`scripts/debug_ebt_state.sh`](../scripts/debug_ebt_state.sh) | Inspect EBT state from the Docker E2E stack |
| [`scripts/debug_muxrpc_capture.sh`](../scripts/debug_muxrpc_capture.sh) | Capture muxrpc traffic for protocol debugging |
| [`cmd/room-tunnel-feed-verify/main.go`](../cmd/room-tunnel-feed-verify/main.go) | Serve, read, or probe tunnel snapshots |

Example:

```bash
./scripts/e2e_tildefriends.sh
docker compose -f infra/e2e-full/docker-compose.yml logs test-runner
```

## Common Failure Shapes

| Symptom | Usually means |
| --- | --- |
| Room `/healthz` fails | Room runtime did not start cleanly or is bound to a different address |
| SSB messages exist but peers do not replicate them | EBT state, feed registration, or muxrpc stream handling needs inspection |
| Tunnel verifier cannot read expected refs | Room peer session or tunnel path is incomplete |
| Reverse-sync live test fails after local publishing | `ssb-client` reached the room, but reverse mappings or ATProto credentials are missing |

## Relevant Data

- Forward bridge state lives in `messages`, `bridged_accounts`, and `bridge_state`.
- Reverse-sync state lives in `reverse_identity_mappings` and `reverse_events`.
- Room runtime data lives under `<repo-path>/room/`.

The Docker E2E stack exposes:
- room HTTP on `8976`
- room muxrpc on `8989`
- admin UI on `8080`

## Reference Sources

When protocol behavior is unclear, compare against:
- [`reference/tildefriends/src/ssb.c`](../reference/tildefriends/src/ssb.c)
- [`reference/tildefriends/src/ssb.ebt.c`](../reference/tildefriends/src/ssb.ebt.c)

## Related Docs

- [SSB Replication](./ssb-replication.md)
- [SSB Rooms](./ssb-rooms.md)
- [SSB Implementation Reference](./ssb-implementations.md)
- [Docker E2E Stack](../infra/e2e-full/README.md)
