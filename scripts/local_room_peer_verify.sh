#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOM_MUXRPC_ADDR="${LIVE_ROOM_MUXRPC_ADDR:-127.0.0.1:9898}"
ROOM_HTTP_ADDR="${LIVE_ROOM_HTTP_ADDR:-127.0.0.1:9876}"
BRIDGE_DB_PATH="${LIVE_BRIDGE_DB_PATH:-}"
BRIDGE_REPO_PATH="${LIVE_BRIDGE_REPO_PATH:-}"
SOURCE_DID="${LIVE_BRIDGE_SOURCE_DID:-}"
EXPECTED_URIS="${LIVE_BRIDGE_EXPECTED_URIS:-}"
ATTEMPTS="${LIVE_SSB_PEER_VERIFY_ATTEMPTS:-20}"
SLEEP_SECS="${LIVE_SSB_PEER_VERIFY_INTERVAL_SECS:-2}"
PEER_TIMEOUT="${LIVE_SSB_PEER_VERIFY_TIMEOUT:-20s}"

if [[ -z "${BRIDGE_DB_PATH}" || -z "${BRIDGE_REPO_PATH}" || -z "${SOURCE_DID}" ]]; then
  echo "[local-room-verify] missing one of LIVE_BRIDGE_DB_PATH/LIVE_BRIDGE_REPO_PATH/LIVE_BRIDGE_SOURCE_DID" >&2
  exit 1
fi

for required in jq sqlite3 go; do
  if ! command -v "${required}" >/dev/null 2>&1; then
    echo "[local-room-verify] ${required} is required" >&2
    exit 1
  fi
done

HTTP_URL="http://${ROOM_HTTP_ADDR}/healthz"
echo "[local-room-verify] checking room HTTP health: ${HTTP_URL}"
curl -sS -f "${HTTP_URL}" >/dev/null

SOURCE_FEED="$(sqlite3 "${BRIDGE_DB_PATH}" "select ssb_feed_id from bridged_accounts where at_did='${SOURCE_DID}' and active=1 limit 1;")"
SOURCE_FEED="$(echo "${SOURCE_FEED}" | tr -d '[:space:]')"
if [[ -z "${SOURCE_FEED}" ]]; then
  echo "[local-room-verify] no source feed mapping found for DID ${SOURCE_DID}" >&2
  exit 1
fi

EXPECTED_COUNT=1
if [[ -n "${EXPECTED_URIS}" ]]; then
  EXPECTED_COUNT="$(echo "${EXPECTED_URIS}" | tr ',' '\n' | sed '/^\s*$/d' | wc -l | tr -d ' ')"
  if [[ -z "${EXPECTED_COUNT}" || "${EXPECTED_COUNT}" -le 0 ]]; then
    EXPECTED_COUNT=1
  fi
fi

echo "[local-room-verify] source feed: ${SOURCE_FEED}"
echo "[local-room-verify] expecting at least ${EXPECTED_COUNT} messages"

PEER_REPO="$(mktemp -d "${TMPDIR:-/tmp}/mvab-ssb-peer-repo-XXXXXX")"
SNAPSHOT_REPO="$(mktemp -d "${TMPDIR:-/tmp}/mvab-ssb-snapshot-repo-XXXXXX")"
BIN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/mvab-ssb-peer-bin-XXXXXX")"
ROOM_ERR_LOG="${BIN_DIR}/room.err"
COUNT_ERR_LOG="${BIN_DIR}/count.err"

cleanup() {
  rm -rf "${PEER_REPO}" "${SNAPSHOT_REPO}" "${BIN_DIR}"
}
trap cleanup EXIT

echo "[local-room-verify] building go-ssb helper binaries"
(
  cd "${ROOT_DIR}/reference/go-ssb"
  go build -o "${BIN_DIR}/ssb-keygen" ./cmd/ssb-keygen
  go build -o "${BIN_DIR}/sbotcli" ./cmd/sbotcli
)
(
  cd "${ROOT_DIR}"
  go build -o "${BIN_DIR}/ssb-feed-count" ./cmd/ssb-feed-count
)

"${BIN_DIR}/ssb-keygen" -repo "${PEER_REPO}" secret >/dev/null

if [[ ! -f "${BRIDGE_REPO_PATH}/secret" ]]; then
  echo "[local-room-verify] bridge repo missing secret file at ${BRIDGE_REPO_PATH}/secret" >&2
  exit 1
fi

ROOM_PUB_RAW="$(jq -r '.public // empty' "${BRIDGE_REPO_PATH}/secret")"
if [[ -z "${ROOM_PUB_RAW}" ]]; then
  echo "[local-room-verify] failed to parse room public key from ${BRIDGE_REPO_PATH}/secret" >&2
  exit 1
fi
ROOM_PUB="${ROOM_PUB_RAW#@}"
ROOM_PUB="${ROOM_PUB%.ed25519}"

ROOM_CLI=(
  "${BIN_DIR}/sbotcli"
  --unixsock "${PEER_REPO}/does-not-exist.sock"
  --key "${PEER_REPO}/secrets/secret"
  --addr "${ROOM_MUXRPC_ADDR}"
  --remotekey "${ROOM_PUB}"
  --timeout "${PEER_TIMEOUT}"
)

echo "[local-room-verify] verifying second peer can connect to room muxrpc at ${ROOM_MUXRPC_ADDR}"
if ! "${ROOM_CLI[@]}" call whoami >"${BIN_DIR}/room-whoami.json" 2>"${ROOM_ERR_LOG}"; then
  echo "[local-room-verify] second peer failed room whoami call" >&2
  sed -n '1,120p' "${ROOM_ERR_LOG}" >&2 || true
  exit 1
fi

if ! "${ROOM_CLI[@]}" call tunnel.isRoom >"${BIN_DIR}/room-isroom.json" 2>"${ROOM_ERR_LOG}"; then
  echo "[local-room-verify] second peer failed tunnel.isRoom call" >&2
  sed -n '1,120p' "${ROOM_ERR_LOG}" >&2 || true
  exit 1
fi

if ! jq -e '(.features // [] | index("tunnel")) != null' "${BIN_DIR}/room-isroom.json" >/dev/null 2>&1; then
  echo "[local-room-verify] tunnel.isRoom response missing tunnel feature" >&2
  cat "${BIN_DIR}/room-isroom.json" >&2 || true
  exit 1
fi

if ! "${ROOM_CLI[@]}" call tunnel.announce >"${BIN_DIR}/room-announce.json" 2>"${ROOM_ERR_LOG}"; then
  echo "[local-room-verify] second peer failed tunnel.announce call" >&2
  sed -n '1,120p' "${ROOM_ERR_LOG}" >&2 || true
  exit 1
fi

echo "[local-room-verify] room handshake verified; checking source feed on repo snapshot"
attempt=1
while [[ "${attempt}" -le "${ATTEMPTS}" ]]; do
  rm -rf "${SNAPSHOT_REPO:?}/"*
  cp -R "${BRIDGE_REPO_PATH}/." "${SNAPSHOT_REPO}/"

  if msg_count="$("${BIN_DIR}/ssb-feed-count" --repo "${SNAPSHOT_REPO}" --feed "${SOURCE_FEED}" 2>"${COUNT_ERR_LOG}")"; then
    msg_count="$(echo "${msg_count}" | tr -d '[:space:]')"
    if [[ -z "${msg_count}" ]]; then
      msg_count=0
    fi
    echo "[local-room-verify] attempt ${attempt}/${ATTEMPTS}: peer observed ${msg_count} messages"
    if [[ "${msg_count}" -ge "${EXPECTED_COUNT}" ]]; then
      echo "[local-room-verify] strict peer verification passed"
      exit 0
    fi
  else
    echo "[local-room-verify] attempt ${attempt}/${ATTEMPTS}: feed count query failed" >&2
    sed -n '1,80p' "${COUNT_ERR_LOG}" >&2 || true
  fi

  attempt=$((attempt + 1))
  sleep "${SLEEP_SECS}"
done

echo "[local-room-verify] strict peer verification failed after ${ATTEMPTS} attempts" >&2
sed -n '1,120p' "${ROOM_ERR_LOG}" >&2 || true
sed -n '1,120p' "${COUNT_ERR_LOG}" >&2 || true
exit 1
