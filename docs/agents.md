# Contributor Setup Profiles

This file maps common tasks to the least surprising environment. Use it before changing bridge, room, or ATProto behavior.

## Defaults

- Run Go tooling in module mode: `GOFLAGS=-mod=mod`.
- Keep local and production credentials separate.
- Bind room, UI, MCP, and metrics listeners to loopback unless you are deliberately exposing them.
- Treat reverse sync as a separate feature flag. It is off unless you turn it on.

## Profile 1: Local Integration

Use this for most feature work, bug repro, and reverse-sync development.

```bash
./scripts/local_atproto_up.sh
./scripts/local_atproto_bootstrap.sh /tmp/mvab-local-atproto.env
set -a
source /tmp/mvab-local-atproto.env
set +a

export BRIDGE_BOT_SEED="dev-local-seed"

GOFLAGS=-mod=mod go run ./cmd/bridge-cli \
  --db bridge-local.sqlite \
  --relay-url "${LIVE_RELAY_URL}" \
  --bot-seed "${BRIDGE_BOT_SEED}" \
  start \
  --repo-path .ssb-bridge-local \
  --xrpc-host "${LIVE_ATPROTO_HOST}" \
  --plc-url "${LIVE_ATPROTO_PLC_URL}" \
  --atproto-insecure \
  --room-enable \
  --room-listen-addr 127.0.0.1:8989 \
  --room-http-listen-addr 127.0.0.1:8976 \
  --mcp-listen-addr "" \
  --metrics-listen-addr ""
```

Optional standalone UI:

```bash
export BRIDGE_UI_PASSWORD="dev-ui-password"

GOFLAGS=-mod=mod go run ./cmd/bridge-cli \
  --db bridge-local.sqlite \
  serve-ui \
  --listen-addr 127.0.0.1:8080 \
  --ui-auth-user admin \
  --ui-auth-pass-env BRIDGE_UI_PASSWORD \
  --repo-path .ssb-bridge-local \
  --room-http-base-url http://127.0.0.1:8976
```

Cleanup:

```bash
./scripts/local_atproto_down.sh
```

Use `GOFLAGS=-mod=mod go run ./cmd/ssb-client --help` when you need a standalone SSB node for room or reverse-sync tests.

## Profile 2: Full Docker E2E

Use this when you need containerized interoperability coverage.

Room/EBT-focused stack:

```bash
./scripts/e2e_tildefriends.sh
```

Full-stack bridge + reverse bootstrap + admin UI:

```bash
./scripts/e2e_full_up.sh
```

Relevant infra:
- `infra/e2e-full/`
- `infra/e2e-tildefriends/`

## Profile 3: NixOS Staging or Production

Use the NixOS module in `nix/modules/mr-valinskys-adequate-bridge.nix`.

Minimum checklist:
- Set `services.mr-valinskys-adequate-bridge.environmentFile`.
- Provide `BRIDGE_BOT_SEED`.
- Provide `BRIDGE_UI_PASSWORD` when UI auth is enabled.
- Keep room and UI behind TLS or a reverse proxy when exposed publicly.
- Set `room.httpsDomain` when the room listens off-loopback.
- Pass `ui.extraArgs = [ "--repo-path" "/var/lib/mr-valinskys-adequate-bridge/.ssb-bridge" ];` so blob pages work.
- Decide whether the live MCP listener should stay on loopback or be disabled.

After config changes:

```bash
sudo nixos-rebuild switch
sudo systemctl status mr-valinskys-adequate-bridge
sudo systemctl status mr-valinskys-adequate-bridge-ui
```

## Which Profile to Use

- Local feature work or reverse-sync work: Profile 1.
- Containerized interop checks: Profile 2.
- Real host deployment work: Profile 3.

For operator procedures, use [docs/runbook.md](./runbook.md). For reverse-sync behavior and credentials, use [docs/reverse-sync.md](./reverse-sync.md).
