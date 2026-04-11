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
REQUIRE_ACTIVE_BRIDGED_PEERS="${LIVE_REQUIRE_ACTIVE_BRIDGED_PEERS:-1}"

if [[ -z "${BRIDGE_DB_PATH}" || -z "${BRIDGE_REPO_PATH}" || -z "${SOURCE_DID}" ]]; then
  echo "[local-room-verify] missing one of LIVE_BRIDGE_DB_PATH/LIVE_BRIDGE_REPO_PATH/LIVE_BRIDGE_SOURCE_DID" >&2
  exit 1
fi
case "${REQUIRE_ACTIVE_BRIDGED_PEERS}" in
  0|1)
    ;;
  *)
    echo "[local-room-verify] LIVE_REQUIRE_ACTIVE_BRIDGED_PEERS must be 0 or 1 (got ${REQUIRE_ACTIVE_BRIDGED_PEERS})" >&2
    exit 1
    ;;
esac

for required in jq sqlite3 go curl; do
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

# Read active feeds into an array (compatible with bash 3.2)
# Store in a temp file to avoid mapfile (bash 4+ only)
ACTIVE_FEEDS_FILE="$(mktemp)"
sqlite3 "${BRIDGE_DB_PATH}" "select ssb_feed_id from bridged_accounts where active=1 and ssb_feed_id is not null and trim(ssb_feed_id) <> '' order by ssb_feed_id;" | sed '/^[[:space:]]*$/d' > "${ACTIVE_FEEDS_FILE}"
ACTIVE_FEED_COUNT="$(wc -l < "${ACTIVE_FEEDS_FILE}" | tr -d '[:space:]')"
if [[ "${ACTIVE_FEED_COUNT}" -eq 0 ]]; then
  rm -f "${ACTIVE_FEEDS_FILE}"
  echo "[local-room-verify] no active bridged account feeds found in DB" >&2
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
if [[ "${REQUIRE_ACTIVE_BRIDGED_PEERS}" == "1" ]]; then
  echo "[local-room-verify] strict bridged-peer presence check enabled for ${ACTIVE_FEED_COUNT} active feed(s)"
else
  echo "[local-room-verify] strict bridged-peer presence check disabled"
fi

PEER_DIR="$(mktemp -d "${TMPDIR:-/tmp}/mvab-ssb-peer-XXXXXX")"
BIN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/mvab-ssb-peer-bin-XXXXXX")"
SERVE_LOG="${BIN_DIR}/serve.log"
READ_LOG="${BIN_DIR}/read.log"
PROBE_LOG="${BIN_DIR}/probe.log"

cleanup() {
  rm -rf "${PEER_DIR}" "${BIN_DIR}" "${ACTIVE_FEEDS_FILE}"
}
trap cleanup EXIT

echo "[local-room-verify] building tunnel verifier helper"
(
  cd "${ROOT_DIR}"
  if [[ "${GOFLAGS:-}" != *"-mod="* ]]; then
    export GOFLAGS="-mod=mod ${GOFLAGS:-}"
  fi
  go build -o "${BIN_DIR}/room-tunnel-feed-verify" ./cmd/room-tunnel-feed-verify
)

if [[ -f "${BRIDGE_REPO_PATH}/room/secret" ]]; then
  ROOM_SECRET_PATH="${BRIDGE_REPO_PATH}/room/secret"
elif [[ -f "${BRIDGE_REPO_PATH}/secret" ]]; then
  ROOM_SECRET_PATH="${BRIDGE_REPO_PATH}/secret"
else
  echo "[local-room-verify] bridge repo missing secret file at ${BRIDGE_REPO_PATH}/room/secret or ${BRIDGE_REPO_PATH}/secret" >&2
  exit 1
fi

ROOM_FEED="$(jq -r '.id // empty' "${ROOM_SECRET_PATH}" | tr -d '[:space:]')"
if [[ -z "${ROOM_FEED}" ]]; then
  echo "[local-room-verify] failed to parse room feed id from ${ROOM_SECRET_PATH}" >&2
  exit 1
fi
if [[ "${ROOM_FEED}" != @*".ed25519" ]]; then
  echo "[local-room-verify] invalid room feed id in ${ROOM_SECRET_PATH}: ${ROOM_FEED}" >&2
  exit 1
fi

SERVE_KEY_FILE="${PEER_DIR}/serve-secret"
READ_KEY_FILE="${PEER_DIR}/read-secret"

echo "[local-room-verify] verifying room tunnel peer-read on ${ROOM_MUXRPC_ADDR}"
attempt=1
while [[ "${attempt}" -le "${ATTEMPTS}" ]]; do
  READY_FILE="${PEER_DIR}/serve-ready-${attempt}.json"
  : >"${SERVE_LOG}"
  : >"${READ_LOG}"
  : >"${PROBE_LOG}"
  rm -f "${READY_FILE}"

  "${BIN_DIR}/room-tunnel-feed-verify" serve \
    --room-addr "${ROOM_MUXRPC_ADDR}" \
    --room-feed "${ROOM_FEED}" \
    --key-file "${SERVE_KEY_FILE}" \
    --db "${BRIDGE_DB_PATH}" \
    --source-did "${SOURCE_DID}" \
    --source-feed "${SOURCE_FEED}" \
    --expected-uris "${EXPECTED_URIS}" \
    --ready-file "${READY_FILE}" \
    --timeout "${PEER_TIMEOUT}" \
    >"${SERVE_LOG}" 2>&1 &
  serve_pid=$!

  ready_wait_deadline=$((SECONDS + 15))
  while [[ ! -f "${READY_FILE}" ]]; do
    if ! kill -0 "${serve_pid}" >/dev/null 2>&1; then
      wait "${serve_pid}" || true
      break
    fi
    if ((SECONDS >= ready_wait_deadline)); then
      echo "[local-room-verify] attempt ${attempt}/${ATTEMPTS}: timed out waiting for serve peer readiness" >&2
      kill "${serve_pid}" >/dev/null 2>&1 || true
      wait "${serve_pid}" || true
      break
    fi
    sleep 0.2
  done

  if [[ ! -f "${READY_FILE}" ]]; then
    echo "[local-room-verify] attempt ${attempt}/${ATTEMPTS}: serve peer did not become ready" >&2
    sed -n '1,120p' "${SERVE_LOG}" >&2 || true
    attempt=$((attempt + 1))
    sleep "${SLEEP_SECS}"
    continue
  fi

  TARGET_FEED="$(jq -r '.feed // empty' "${READY_FILE}" | tr -d '[:space:]')"
  if [[ -z "${TARGET_FEED}" ]]; then
    echo "[local-room-verify] attempt ${attempt}/${ATTEMPTS}: ready file missing target feed" >&2
    sed -n '1,120p' "${SERVE_LOG}" >&2 || true
    kill "${serve_pid}" >/dev/null 2>&1 || true
    wait "${serve_pid}" || true
    attempt=$((attempt + 1))
    sleep "${SLEEP_SECS}"
    continue
  fi

  if "${BIN_DIR}/room-tunnel-feed-verify" read \
    --room-addr "${ROOM_MUXRPC_ADDR}" \
    --room-feed "${ROOM_FEED}" \
    --key-file "${READ_KEY_FILE}" \
    --target-feed "${TARGET_FEED}" \
    --expect-source-feed "${SOURCE_FEED}" \
    --expected-uris "${EXPECTED_URIS}" \
    --min-count "${EXPECTED_COUNT}" \
    --timeout "${PEER_TIMEOUT}" \
    >"${READ_LOG}" 2>&1; then
    if [[ "${REQUIRE_ACTIVE_BRIDGED_PEERS}" == "1" ]]; then
      attendants_json="$(curl -sS -f "http://${ROOM_HTTP_ADDR}/api/room/status/attendants")" || {
        echo "[local-room-verify] attempt ${attempt}/${ATTEMPTS}: failed to fetch attendants status" >&2
        kill "${serve_pid}" >/dev/null 2>&1 || true
        wait "${serve_pid}" || true
        attempt=$((attempt + 1))
        sleep "${SLEEP_SECS}"
        continue
      }
      tunnels_json="$(curl -sS -f "http://${ROOM_HTTP_ADDR}/api/room/status/tunnels")" || {
        echo "[local-room-verify] attempt ${attempt}/${ATTEMPTS}: failed to fetch tunnels status" >&2
        kill "${serve_pid}" >/dev/null 2>&1 || true
        wait "${serve_pid}" || true
        attempt=$((attempt + 1))
        sleep "${SLEEP_SECS}"
        continue
      }

      strict_failed=0
      while IFS= read -r expected_feed; do
        [[ -z "${expected_feed}" ]] && continue
        if ! echo "${attendants_json}" | jq -e --arg feed "${expected_feed}" '.attendants | any(.id == $feed)' >/dev/null; then
          echo "[local-room-verify] attempt ${attempt}/${ATTEMPTS}: active attendant missing feed ${expected_feed}" >&2
          strict_failed=1
        fi
        if ! echo "${tunnels_json}" | jq -e --arg feed "${expected_feed}" '.tunnels | any(.target == $feed)' >/dev/null; then
          echo "[local-room-verify] attempt ${attempt}/${ATTEMPTS}: active tunnel endpoint missing feed ${expected_feed}" >&2
          strict_failed=1
        fi
        if ! "${BIN_DIR}/room-tunnel-feed-verify" probe \
          --room-addr "${ROOM_MUXRPC_ADDR}" \
          --room-feed "${ROOM_FEED}" \
          --key-file "${READ_KEY_FILE}" \
          --target-feed "${expected_feed}" \
          --timeout "${PEER_TIMEOUT}" \
          >>"${PROBE_LOG}" 2>&1; then
          echo "[local-room-verify] attempt ${attempt}/${ATTEMPTS}: tunnel.connect probe failed for ${expected_feed}" >&2
          strict_failed=1
        fi
      done < "${ACTIVE_FEEDS_FILE}"

      if [[ "${strict_failed}" -ne 0 ]]; then
        sed -n '1,160p' "${PROBE_LOG}" >&2 || true
        kill "${serve_pid}" >/dev/null 2>&1 || true
        wait "${serve_pid}" || true
        attempt=$((attempt + 1))
        sleep "${SLEEP_SECS}"
        continue
      fi
    fi

    wait "${serve_pid}" || true
    echo "[local-room-verify] attempt ${attempt}/${ATTEMPTS}: peer observed tunnel snapshot with expected records"
    echo "[local-room-verify] strict peer verification passed"
    exit 0
  else
    echo "[local-room-verify] attempt ${attempt}/${ATTEMPTS}: tunnel read assertion failed" >&2
    sed -n '1,120p' "${READ_LOG}" >&2 || true
    sed -n '1,120p' "${SERVE_LOG}" >&2 || true
    kill "${serve_pid}" >/dev/null 2>&1 || true
    wait "${serve_pid}" || true
  fi

  attempt=$((attempt + 1))
  sleep "${SLEEP_SECS}"
done

echo "[local-room-verify] strict peer verification failed after ${ATTEMPTS} attempts" >&2
sed -n '1,120p' "${READ_LOG}" >&2 || true
sed -n '1,120p' "${SERVE_LOG}" >&2 || true
sed -n '1,120p' "${PROBE_LOG}" >&2 || true
exit 1
