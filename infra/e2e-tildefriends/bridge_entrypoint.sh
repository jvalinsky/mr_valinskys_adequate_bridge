#!/usr/bin/env bash
# bridge_entrypoint.sh — seeds bot accounts + SSB messages, then starts the bridge engine.
set -euo pipefail

DB_PATH="${DB_PATH:-/data/bridge.sqlite}"
REPO_PATH="${REPO_PATH:-/data/ssb-repo}"
BOT_SEED="${BOT_SEED:-e2e-docker-seed}"
BOT_DID="${BOT_DID:-did:plc:e2e-docker-bot}"
BOT_TARGET_DID="${BOT_TARGET_DID:-did:plc:e2e-docker-target}"
ROOM_MUXRPC_ADDR="${ROOM_MUXRPC_ADDR:-0.0.0.0:8989}"
ROOM_HTTP_ADDR="${ROOM_HTTP_ADDR:-0.0.0.0:8976}"
ROOM_MODE="${ROOM_MODE:-open}"
ROOM_HTTPS_DOMAIN="${ROOM_HTTPS_DOMAIN:-bridge}"
BRIDGE_FIREHOSE_ENABLE="${BRIDGE_FIREHOSE_ENABLE:-0}"
BRIDGE_RELAY_URL="${BRIDGE_RELAY_URL:-}"

echo "[bridge-entry] seeding bot accounts and SSB messages ..."

# Seed the primary bot account with test SSB messages
e2e-seed \
  --db "${DB_PATH}" \
  --repo-path "${REPO_PATH}" \
  --bot-seed "${BOT_SEED}" \
  --did "${BOT_DID}"

# Also register the target DID
bridge-cli --db "${DB_PATH}" --bot-seed "${BOT_SEED}" account add "${BOT_TARGET_DID}" || true

echo "[bridge-entry] seeding complete, starting bridge engine ..."

case "${BRIDGE_FIREHOSE_ENABLE}" in
  0|1)
    ;;
  *)
    echo "[bridge-entry] invalid BRIDGE_FIREHOSE_ENABLE=${BRIDGE_FIREHOSE_ENABLE} (expected 0 or 1)" >&2
    exit 1
    ;;
esac

bridge_start_args=(
  --db "${DB_PATH}"
  --bot-seed "${BOT_SEED}"
)

if [[ "${BRIDGE_FIREHOSE_ENABLE}" == "1" ]]; then
  if [[ -z "${BRIDGE_RELAY_URL}" ]]; then
    echo "[bridge-entry] BRIDGE_RELAY_URL is required when BRIDGE_FIREHOSE_ENABLE=1" >&2
    exit 1
  fi
  echo "[bridge-entry] firehose mode: external relay (${BRIDGE_RELAY_URL})"
  bridge_start_args+=(--relay-url "${BRIDGE_RELAY_URL}")
else
  echo "[bridge-entry] firehose mode: off"
fi

bridge_start_args+=(
  start
  --firehose-enable="${BRIDGE_FIREHOSE_ENABLE}"
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
