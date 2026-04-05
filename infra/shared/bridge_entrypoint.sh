#!/usr/bin/env bash
# bridge_entrypoint.sh — unified bridge startup for all E2E environments.
#
# Modes (BRIDGE_MODE):
#   seeded   — Seed bot accounts via e2e-seed, then start bridge (e2e-tildefriends)
#   firehose — Wipe stale state, connect to real relay (e2e-full)
#
# Both modes share the same engine-start logic.  Mode selection is via
# the BRIDGE_MODE environment variable (default: seeded).
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
E2E_SEED_INCLUDE_BLOB_POST="${E2E_SEED_INCLUDE_BLOB_POST:-0}"
BRIDGE_MODE="${BRIDGE_MODE:-seeded}"
BRIDGE_FIREHOSE_ENABLE="${BRIDGE_FIREHOSE_ENABLE:-0}"
BRIDGE_RELAY_URL="${BRIDGE_RELAY_URL:-}"
BRIDGE_PLC_URL="${BRIDGE_PLC_URL:-https://plc.directory}"
BRIDGE_ATPROTO_INSECURE="${BRIDGE_ATPROTO_INSECURE:-0}"
BRIDGE_CLEAN_START="${BRIDGE_CLEAN_START:-0}"

# ── Mode-specific pre-start ────────────────────────────────────────

case "${BRIDGE_MODE}" in
  seeded)
    echo "[bridge-entry] mode=seeded: seeding bot accounts and SSB messages ..."
    e2e-seed \
      --db "${DB_PATH}" \
      --repo-path "${REPO_PATH}" \
      --bot-seed "${BOT_SEED}" \
      --did "${BOT_DID}" \
      --include-blob-post="${E2E_SEED_INCLUDE_BLOB_POST}"

    bridge-cli --db "${DB_PATH}" --bot-seed "${BOT_SEED}" account add "${BOT_TARGET_DID}" || true
    echo "[bridge-entry] seeding complete"
    ;;

  firehose)
    if [ "${BRIDGE_CLEAN_START}" = "1" ]; then
      echo "[bridge-entry] mode=firehose: clean start, removing stale db and repo ..."
      rm -rf "${DB_PATH}" "${DB_PATH}-wal" "${DB_PATH}-shm" "${REPO_PATH}"
    else
      echo "[bridge-entry] mode=firehose: starting without state wipe"
    fi
    ;;

  *)
    echo "[bridge-entry] invalid BRIDGE_MODE=${BRIDGE_MODE} (expected: seeded, firehose)" >&2
    exit 1
    ;;
esac

# ── Engine start (shared by all modes) ─────────────────────────────

case "${BRIDGE_FIREHOSE_ENABLE}" in
  0|1) ;;
  *)
    echo "[bridge-entry] invalid BRIDGE_FIREHOSE_ENABLE=${BRIDGE_FIREHOSE_ENABLE} (expected 0 or 1)" >&2
    exit 1
    ;;
esac

if [[ "${BRIDGE_FIREHOSE_ENABLE}" == "1" && -z "${BRIDGE_RELAY_URL:-}" ]]; then
  echo "[bridge-entry] BRIDGE_RELAY_URL is required when BRIDGE_FIREHOSE_ENABLE=1" >&2
  exit 1
fi

relay_desc="off"
[[ "${BRIDGE_FIREHOSE_ENABLE}" == "1" ]] && relay_desc="${BRIDGE_RELAY_URL}"
echo "[bridge-entry] starting bridge engine (firehose=${relay_desc}, plc=${BRIDGE_PLC_URL}) ..."

bridge_cli_args=(
  --db "${DB_PATH}"
  --bot-seed "${BOT_SEED}"
)

if [[ "${BRIDGE_FIREHOSE_ENABLE}" == "1" ]]; then
  bridge_cli_args+=(--relay-url "${BRIDGE_RELAY_URL}")
fi

bridge_cli_args+=(
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
  --plc-url "${BRIDGE_PLC_URL}"
)

if [ "${BRIDGE_ATPROTO_INSECURE}" = "1" ]; then
  bridge_cli_args+=(--atproto-insecure)
fi

exec bridge-cli "${bridge_cli_args[@]}"
