#!/usr/bin/env bash
set -euo pipefail

LOCAL_ATPROTO_HOST="${LOCAL_ATPROTO_HOST:-http://127.0.0.1:2583}"
LOCAL_ATPROTO_PLC_HOST="${LOCAL_ATPROTO_PLC_HOST:-http://127.0.0.1:2582}"
LOCAL_ATPROTO_RELAY_HOST="${LOCAL_ATPROTO_RELAY_HOST:-http://127.0.0.1:2584}"
LOCAL_ATPROTO_WAIT_TIMEOUT_SECS="${LOCAL_ATPROTO_WAIT_TIMEOUT_SECS:-90}"

wait_http() {
  local url="$1"
  local timeout_secs="$2"
  local started
  started="$(date +%s)"

  while true; do
    if curl -sS -f "$url" >/dev/null 2>&1; then
      return 0
    fi

    if (( $(date +%s) - started >= timeout_secs )); then
      echo "[local-atproto] timed out waiting for ${url}" >&2
      return 1
    fi

    sleep 1
  done
}

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
      echo "[local-atproto] timed out waiting for ${url}" >&2
      return 1
    fi

    sleep 1
  done
}

wait_http "${LOCAL_ATPROTO_PLC_HOST}/_health" "${LOCAL_ATPROTO_WAIT_TIMEOUT_SECS}"
wait_http_any "${LOCAL_ATPROTO_RELAY_HOST}/" "${LOCAL_ATPROTO_WAIT_TIMEOUT_SECS}"
wait_http "${LOCAL_ATPROTO_HOST}/xrpc/_health" "${LOCAL_ATPROTO_WAIT_TIMEOUT_SECS}"

echo "[local-atproto] PLC, relay, and PDS are healthy"
