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

`scripts/local_room_peer_verify.sh` is the default strict verifier used by local live E2E. It checks room health, starts an announced peer that serves bridged record refs over a real room `tunnel.connect` stream, and requires a separate second peer to read/validate expected bridged URIs through that tunnel.

`scripts/local_atproto_bootstrap.sh` writes both legacy and per-account credential variables into the generated env file, including:
- `LIVE_ATPROTO_SOURCE_IDENTIFIER`
- `LIVE_ATPROTO_SOURCE_APP_PASSWORD`
- `LIVE_ATPROTO_TARGET_IDENTIFIER`
- `LIVE_ATPROTO_TARGET_APP_PASSWORD`
- `LIVE_ATPROTO_FOLLOW_TARGET_DID`

## Data Directory

Container state defaults to `/tmp/mvab-local-atproto` and can be overridden:

```bash
export LOCAL_ATPROTO_DATA_DIR=/tmp/my-atproto-stack
```

## See Also

- [Bridge Operator Runbook](../docs/runbook.md) — Operational procedures for bridge startup, retry, and incident triage
- [Agent Setup Profiles](../docs/agents.md) — Local Docker vs NixOS production setup
