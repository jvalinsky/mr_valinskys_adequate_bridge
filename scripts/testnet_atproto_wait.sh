#!/usr/bin/env bash
set -euo pipefail

TESTNET_PLC_HOST="${TESTNET_PLC_HOST:-http://127.0.0.1:7000}"
TESTNET_PDS_HOST="${TESTNET_PDS_HOST:-http://127.0.0.1:6000}"
TESTNET_RELAY_HOST="${TESTNET_RELAY_HOST:-http://127.0.0.1:7001}"
TESTNET_JETSTREAM_HOST="${TESTNET_JETSTREAM_HOST:-http://127.0.0.1:7002}"
TESTNET_WAIT_TIMEOUT_SECS="${TESTNET_WAIT_TIMEOUT_SECS:-180}"

wait_http_any() {
  local url="$1"
  local timeout_secs="$2"
  local started
  started="$(date +%s)"

  while true; do
    code="$(curl -sS -o /dev/null -w '%{http_code}' "${url}" || true)"
    if [[ "${code}" != "000" ]]; then
      return 0
    fi

    if (( $(date +%s) - started >= timeout_secs )); then
      echo "[testnet-atproto] timed out waiting for ${url}" >&2
      return 1
    fi

    sleep 1
  done
}

wait_http_200() {
  local url="$1"
  local timeout_secs="$2"
  local started
  started="$(date +%s)"

  while true; do
    if curl -sS -f "${url}" >/dev/null 2>&1; then
      return 0
    fi

    if (( $(date +%s) - started >= timeout_secs )); then
      echo "[testnet-atproto] timed out waiting for ${url}" >&2
      return 1
    fi

    sleep 1
  done
}

wait_http_any "${TESTNET_PLC_HOST}/" "${TESTNET_WAIT_TIMEOUT_SECS}"
wait_http_any "${TESTNET_RELAY_HOST}/" "${TESTNET_WAIT_TIMEOUT_SECS}"
wait_http_any "${TESTNET_JETSTREAM_HOST}/" "${TESTNET_WAIT_TIMEOUT_SECS}"
wait_http_200 "${TESTNET_PDS_HOST}/xrpc/_health" "${TESTNET_WAIT_TIMEOUT_SECS}"

echo "[testnet-atproto] PLC, relay, jetstream, and PDS are reachable"
