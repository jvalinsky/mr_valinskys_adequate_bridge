# Local ATProto Stack

This directory contains the Docker Compose stack used for local bridge development and local E2E runs. It provides:

- a lightweight PLC-compatible directory service
- a local relay with PostgreSQL state
- a local Cocoon PDS
- local reverse proxies for `plc.directory` and `pds.test`

The compose file is [`infra/local-atproto/docker-compose.yml`](./docker-compose.yml).

## Typical Workflow

```bash
./scripts/local_atproto_up.sh
./scripts/local_atproto_bootstrap.sh /tmp/mvab-local-atproto.env
./scripts/local_bridge_e2e.sh
./scripts/local_atproto_down.sh
```

What the helper scripts do:
- `local_atproto_up.sh` starts the stack, builds the local relay image, waits for health, and seeds the relay with the PDS host.
- `local_atproto_bootstrap.sh` creates source and target test accounts and writes a `LIVE_*` env file.
- `local_bridge_e2e.sh` runs the bridge against that local stack.

## Generated Environment File

`scripts/local_atproto_bootstrap.sh` writes:

- `LIVE_ATPROTO_HOST`
- `LIVE_ATPROTO_PLC_URL`
- `LIVE_RELAY_URL`
- `LIVE_ATPROTO_SOURCE_IDENTIFIER`
- `LIVE_ATPROTO_SOURCE_APP_PASSWORD`
- `LIVE_ATPROTO_TARGET_IDENTIFIER`
- `LIVE_ATPROTO_TARGET_APP_PASSWORD`
- `LIVE_ATPROTO_FOLLOW_TARGET_DID`
- legacy compatibility vars `LIVE_ATPROTO_IDENTIFIER` and `LIVE_ATPROTO_PASSWORD`

The script also sets `LIVE_ROOM_MODE=open` and uses `./scripts/local_room_peer_verify.sh` as the default strict room verifier.

## Data Directory

The stack stores data under `LOCAL_ATPROTO_DATA_DIR`, which defaults to `/tmp/mvab-local-atproto`.

Example:

```bash
export LOCAL_ATPROTO_DATA_DIR=/tmp/my-atproto-stack
./scripts/local_atproto_up.sh
```

## Ports

| Service | Host port |
| --- | --- |
| PLC | `2582` |
| PDS | `2583` |
| Relay | `2584` |
| HTTP / HTTPS proxy for `pds.test` | `80` / `443` |

## See Also

- [Bridge Operator Runbook](../../docs/runbook.md)
- [Contributor Setup Profiles](../../docs/agents.md)
- [Docker E2E Stack](../e2e-full/README.md)
