# Agent Setup Profiles

This file is the quick setup reference for contributors and coding agents.
Use it to choose the right environment before making code or ops changes.

## Non-Negotiable Defaults

- Run Go tooling in module mode: `GOFLAGS=-mod=mod`.
- Keep local test environments isolated from production credentials.
- Use loopback binds by default for Room and admin UI in local and single-host setups.

## Profile 1: Local Integration (Docker Dependencies + Local Bridge)

Use this for most feature work and correctness debugging.

```bash
./scripts/local_atproto_up.sh
./scripts/local_atproto_bootstrap.sh /tmp/mvab-local-atproto-live.env
source /tmp/mvab-local-atproto-live.env

export BRIDGE_BOT_SEED="dev-local-seed"
GOFLAGS=-mod=mod go run ./cmd/bridge-cli \
  --db bridge-local.sqlite \
  --relay-url "${LIVE_RELAY_URL}" \
  --bot-seed "${BRIDGE_BOT_SEED}" \
  start \
  --repo-path .ssb-bridge-local \
  --xrpc-host "${LIVE_ATPROTO_HOST}" \
  --plc-url "${LIVE_ATPROTO_PLC_URL}" \
  --room-enable \
  --room-listen-addr 127.0.0.1:8989 \
  --room-http-listen-addr 127.0.0.1:8976
```

Optional admin UI process:

```bash
export BRIDGE_UI_PASSWORD="dev-ui-password"
GOFLAGS=-mod=mod go run ./cmd/bridge-cli \
  --db bridge-local.sqlite \
  serve-ui \
  --listen-addr 127.0.0.1:8080 \
  --ui-auth-user admin \
  --ui-auth-pass-env BRIDGE_UI_PASSWORD \
  --repo-path .ssb-bridge-local
```

Cleanup:

```bash
./scripts/local_atproto_down.sh
```

## Profile 2: Full Docker E2E (Bridge + Tildefriends)

Use this for SSB Room/EBT interoperability and demo-style validation.

```bash
./scripts/e2e_tildefriends.sh
```

Relevant infra:

- `infra/e2e-full/`
- `infra/e2e-tildefriends/`

## Profile 3: NixOS Staging/Production

Use the module in `nix/modules/mr-valinskys-adequate-bridge.nix`.

Minimum checklist:

- Configure `services.mr-valinskys-adequate-bridge.environmentFile`.
- Include `BRIDGE_BOT_SEED` in that environment file.
- Include `BRIDGE_UI_PASSWORD` in that environment file when UI auth is enabled.
- Enable room with `httpsDomain` when exposed off-loopback.
- Enable UI auth when binding UI off-loopback.
- Pass UI repo path for blob pages: `ui.extraArgs = [ "--repo-path" "/var/lib/mr-valinskys-adequate-bridge/.ssb-bridge" ];`
- Put TLS/reverse proxy in front of room/UI public domains.

After config changes:

```bash
sudo nixos-rebuild switch
sudo systemctl status mr-valinskys-adequate-bridge
sudo systemctl status mr-valinskys-adequate-bridge-ui
```

## Which Profile to Use

- Local feature work: Profile 1.
- SSB client interoperability/demos: Profile 2.
- Real host deployment changes: Profile 3.

For full operator procedures, use [`docs/runbook.md`](./runbook.md).
