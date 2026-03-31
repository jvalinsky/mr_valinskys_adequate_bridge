#!/usr/bin/env bash
# bridge_entrypoint.sh — starts the bridge engine in firehose mode.
set -euo pipefail

DB_PATH="${DB_PATH:-/data/bridge.sqlite}"
REPO_PATH="${REPO_PATH:-/data/ssb-repo}"
BOT_ID="${BOT_ID:-e2e-full-seed}"
ROOM_MUXRPC_ADDR="${ROOM_MUXRPC_ADDR:-0.0.0.0:8989}"
ROOM_HTTP_ADDR="${ROOM_HTTP_ADDR:-0.0.0.0:8976}"
ROOM_MODE="${ROOM_MODE:-open}"
ROOM_HTTPS_DOMAIN="${ROOM_HTTPS_DOMAIN:-bridge}"
BRIDGE_RELAY_URL="${BRIDGE_RELAY_URL:-}"
BRIDGE_PLC_URL="${BRIDGE_PLC_URL:-https://plc.directory}"
BRIDGE_ATPROTO_INSECURE="${BRIDGE_ATPROTO_INSECURE:-0}"
BRIDGE_CLEAN_START="${BRIDGE_CLEAN_START:-1}"

# In e2e, wipe stale state to avoid FutureCursor errors from prior runs.
if [ "${BRIDGE_CLEAN_START}" = "1" ]; then
  echo "[bridge-entry] clean start: removing stale db and repo..."
  rm -rf "${DB_PATH}" "${DB_PATH}-wal" "${DB_PATH}-shm" "${REPO_PATH}"
fi

echo "[bridge-entry] starting bridge engine connecting to ${BRIDGE_RELAY_URL} ..."

# Note: Global flags must come before 'start'
# Subcommand flags must come after 'start'
bridge_cli_args=(
  --db "${DB_PATH}"
  --bot-seed "${BOT_ID}"
  --relay-url "${BRIDGE_RELAY_URL}"
  start
  --firehose-enable=1
  --repo-path "${REPO_PATH}"
  --ssb-listen-addr :8008
  --room-enable
  --room-listen-addr "${ROOM_MUXRPC_ADDR}"
  --room-http-listen-addr "${ROOM_HTTP_ADDR}"
  --room-mode "${ROOM_MODE}"
  --room-https-domain "${ROOM_HTTPS_DOMAIN}"
  --publish-workers 2
  --plc-url "${BRIDGE_PLC_URL}"
)

if [ "${BRIDGE_ATPROTO_INSECURE}" = "1" ]; then
  bridge_cli_args+=(--atproto-insecure)
fi

exec bridge-cli "${bridge_cli_args[@]}"
