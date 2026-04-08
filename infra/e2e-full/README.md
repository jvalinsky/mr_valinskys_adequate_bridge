# Docker E2E Test Infrastructure

This stack runs the bridge, a local ATProto environment, a reverse-sync bootstrap step, a standalone admin UI, and a Tildefriends verifier in Docker.

## What the Stack Contains

The compose file in [`infra/e2e-full/docker-compose.yml`](./docker-compose.yml) includes the shared ATProto services from `infra/shared/docker-compose.atproto.yml` and adds bridge-specific services on top.

### Shared ATProto services

- `plc`
- `plc-proxy`
- `relay_pg`
- `relay`
- `init-keys`
- `pds`

### Bridge-specific services

| Service | Purpose |
| --- | --- |
| `reverse-bootstrap` | Writes reverse-sync credentials and test env files into the shared bridge volume |
| `bridge` | Main bridge runtime with room enabled and reverse sync enabled |
| `seeder` | Publishes test ATProto records into the local PDS |
| `test-runner` | Connects Tildefriends, verifies replication, and checks reverse-sync side effects |
| `bridge-admin-ui` | Standalone admin UI bound to `0.0.0.0:8080` with HTTP basic auth |

## Running the Stack

Fast path:

```bash
./scripts/e2e_full_up.sh
```

Direct compose invocation:

```bash
docker compose -f infra/e2e-full/docker-compose.yml up --build -d
docker compose -f infra/e2e-full/docker-compose.yml wait test-runner
```

If `KEEP_E2E=1` is set, `scripts/e2e_full_up.sh` leaves the stack up for inspection on success.

## Environment and Runtime Defaults

### Bridge

| Variable | Default |
| --- | --- |
| `DB_PATH` | `/data/bridge.sqlite` |
| `REPO_PATH` | `/data/ssb-repo` |
| `BOT_SEED` | `e2e-full-seed` |
| `ROOM_MUXRPC_ADDR` | `0.0.0.0:8989` |
| `ROOM_HTTP_ADDR` | `0.0.0.0:8976` |
| `ROOM_MODE` | `open` |
| `BRIDGE_RELAY_URL` | `ws://pds:80/xrpc/com.atproto.sync.subscribeRepos` |
| `BRIDGE_PLC_URL` | `http://plc:2582` |
| `BRIDGE_ATPROTO_INSECURE` | `1` |
| `BRIDGE_REVERSE_SYNC_ENABLE` | `1` |
| `BRIDGE_REVERSE_CREDENTIALS_FILE` | `/data/reverse-credentials.json` |

### Test runner

| Variable | Default |
| --- | --- |
| `BRIDGE_HTTP_ADDR` | `bridge:8976` |
| `BRIDGE_MUXRPC_ADDR` | `bridge:8989` |
| `BRIDGE_DB_PATH` | `/bridge-data/bridge.sqlite` |
| `BRIDGE_REPO_PATH` | `/bridge-data/ssb-repo` |
| `TF_DB_PATH` | `/tf-data/db.sqlite` |
| `MAX_WAIT_SECS` | `300` |
| `POLL_INTERVAL` | `5` |

## What the Test Runner Verifies

The container script in [`infra/e2e-full/test_runner.sh`](./test_runner.sh) checks:

1. room health
2. seeded forward-bridge records appearing in SQLite and SSB
3. Tildefriends joining through the room
4. room tunnel verification and replication results
5. reverse-sync follow, post, reply, and unfollow side effects

## Useful Files

| File | Purpose |
| --- | --- |
| [`docker-compose.yml`](./docker-compose.yml) | Full stack definition |
| [`atproto_bootstrap.sh`](./atproto_bootstrap.sh) | Reverse bootstrap step used by the compose stack |
| [`seeder_entrypoint.sh`](./seeder_entrypoint.sh) | Seeds ATProto records |
| [`test_runner.sh`](./test_runner.sh) | Main verifier |
| [`../shared/bridge_entrypoint.sh`](../shared/bridge_entrypoint.sh) | Starts the bridge runtime in the container |

## Debugging

Useful commands:

```bash
docker compose -f infra/e2e-full/docker-compose.yml logs bridge
docker compose -f infra/e2e-full/docker-compose.yml logs bridge-admin-ui
docker compose -f infra/e2e-full/docker-compose.yml logs test-runner
docker exec -it <bridge-container> /scripts/debug_ebt_state.sh
docker exec -it <bridge-container> /scripts/debug_muxrpc_capture.sh
```

The admin UI is exposed on `http://127.0.0.1:8080` with:
- user: `admin`
- password env in compose: `BRIDGE_UI_AUTH_PASS=e2e-password`

## See Also

- [Bridge Operator Runbook](../../docs/runbook.md)
- [SSB to ATProto Reverse Sync](../../docs/reverse-sync.md)
- [EBT Replication Notes](../../docs/ebt-replication.md)
- [Local ATProto Stack](../local-atproto/README.md)
