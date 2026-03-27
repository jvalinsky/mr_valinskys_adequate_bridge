# Local ATProto Stack

This directory contains the fully local ATProto dependencies used by bridge live E2E:

- `plc/server.mjs`: lightweight local PLC-compatible directory service
- `docker-compose.yml`: local PLC + local Bluesky PDS

The stack is intended for local bridge validation where public services must not be touched.

## Commands

```bash
./scripts/local_atproto_up.sh
./scripts/local_atproto_bootstrap.sh /tmp/mvab-local-atproto-live.env
./scripts/local_bridge_e2e.sh
./scripts/local_atproto_down.sh
```

`scripts/local_room_peer_verify.sh` is the default strict verifier used by local live E2E. It checks room health, performs a real room muxrpc handshake from a temporary second peer (`whoami`, `tunnel.isRoom`, `tunnel.announce`), snapshots the bridge repo, and asserts the bridged source feed contains the expected message count using `cmd/ssb-feed-count`.

## Data Directory

Container state defaults to `/tmp/mvab-local-atproto` and can be overridden:

```bash
export LOCAL_ATPROTO_DATA_DIR=/tmp/my-atproto-stack
```
