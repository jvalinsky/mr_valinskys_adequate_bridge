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

echo "[bridge-entry] starting bridge engine connecting to ${BRIDGE_RELAY_URL} ..."

bridge_start_args=(
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
)

exec bridge-cli "${bridge_start_args[@]}"
