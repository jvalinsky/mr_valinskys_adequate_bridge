#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ENV_OUT_PATH="${1:-/tmp/mvab-local-atproto-live.env}"
LOCAL_ATPROTO_HOST="${LOCAL_ATPROTO_HOST:-http://127.0.0.1:2583}"
LOCAL_ATPROTO_RELAY_URL="${LOCAL_ATPROTO_RELAY_URL:-ws://127.0.0.1:2584/xrpc/com.atproto.sync.subscribeRepos}"

if ! command -v jq >/dev/null 2>&1; then
  echo "[local-atproto] jq is required" >&2
  exit 1
fi

"${ROOT_DIR}/scripts/local_atproto_wait.sh" >/dev/null

run_id="${LOCAL_ATPROTO_RUN_ID:-$(date +%s)}"
suffix="${LOCAL_ATPROTO_SUFFIX:-$(printf '%06x' "$((run_id % 1679616))")}"

source_handle="${LOCAL_ATPROTO_SOURCE_HANDLE:-s${suffix}.test}"
target_handle="${LOCAL_ATPROTO_TARGET_HANDLE:-t${suffix}.test}"
source_email="${LOCAL_ATPROTO_SOURCE_EMAIL:-bridge-source-${run_id}@example.test}"
target_email="${LOCAL_ATPROTO_TARGET_EMAIL:-bridge-target-${run_id}@example.test}"
source_password="${LOCAL_ATPROTO_SOURCE_PASSWORD:-bridge-source-${run_id}-pass}"
target_password="${LOCAL_ATPROTO_TARGET_PASSWORD:-bridge-target-${run_id}-pass}"

json_post() {
  local path="$1"
  local payload="$2"
  curl -sS -f -X POST \
    -H 'content-type: application/json' \
    "${LOCAL_ATPROTO_HOST}${path}" \
    -d "${payload}"
}

create_session() {
  local identifier="$1"
  local password="$2"
  local payload
  payload="$(jq -n \
    --arg identifier "${identifier}" \
    --arg password "${password}" \
    '{identifier:$identifier,password:$password}')"
  json_post "/xrpc/com.atproto.server.createSession" "${payload}"
}

create_account() {
  local handle="$1"
  local email="$2"
  local password="$3"
  local payload
  payload="$(jq -n \
    --arg handle "${handle}" \
    --arg email "${email}" \
    --arg password "${password}" \
    '{handle:$handle,email:$email,password:$password}')"
  json_post "/xrpc/com.atproto.server.createAccount" "${payload}"
}

create_or_login() {
  local handle="$1"
  local email="$2"
  local password="$3"

  if output="$(create_account "${handle}" "${email}" "${password}" 2>/tmp/mvab-create-account.err)"; then
    echo "${output}"
    return
  fi

  if output="$(create_session "${handle}" "${password}" 2>/tmp/mvab-create-session.err)"; then
    echo "${output}"
    return
  fi

  echo "[local-atproto] failed to create/login account for handle=${handle}" >&2
  echo "[local-atproto] createAccount error:" >&2
  sed -n '1,120p' /tmp/mvab-create-account.err >&2 || true
  echo "[local-atproto] createSession error:" >&2
  sed -n '1,120p' /tmp/mvab-create-session.err >&2 || true
  exit 1
}

echo "[local-atproto] provisioning source and target accounts"
source_session="$(create_or_login "${source_handle}" "${source_email}" "${source_password}")"
target_session="$(create_or_login "${target_handle}" "${target_email}" "${target_password}")"

source_did="$(echo "${source_session}" | jq -r '.did // empty')"
target_did="$(echo "${target_session}" | jq -r '.did // empty')"

if [[ -z "${source_did}" || -z "${target_did}" ]]; then
  echo "[local-atproto] failed to derive source/target DIDs" >&2
  echo "source session: ${source_session}" >&2
  echo "target session: ${target_session}" >&2
  exit 1
fi

mkdir -p "$(dirname "${ENV_OUT_PATH}")"
cat > "${ENV_OUT_PATH}" <<VARS
LIVE_E2E_ENABLED=1
LIVE_ATPROTO_HOST=${LOCAL_ATPROTO_HOST}
LIVE_RELAY_URL=${LOCAL_ATPROTO_RELAY_URL}
LIVE_ATPROTO_SOURCE_IDENTIFIER=${source_did}
LIVE_ATPROTO_SOURCE_APP_PASSWORD=${source_password}
LIVE_ATPROTO_TARGET_IDENTIFIER=${target_did}
LIVE_ATPROTO_TARGET_APP_PASSWORD=${target_password}
LIVE_ATPROTO_TARGET_DID=${target_did}
LIVE_ATPROTO_IDENTIFIER=${source_did}
LIVE_ATPROTO_PASSWORD=${source_password}
LIVE_ATPROTO_FOLLOW_TARGET_DID=${target_did}
LIVE_ROOM_MODE=open
LIVE_ROOM_PEER_VERIFY_CMD=./scripts/local_room_peer_verify.sh
LOCAL_ATPROTO_SOURCE_DID=${source_did}
LOCAL_ATPROTO_TARGET_DID=${target_did}
LOCAL_ATPROTO_SOURCE_HANDLE=${source_handle}
LOCAL_ATPROTO_TARGET_HANDLE=${target_handle}
VARS

echo "[local-atproto] wrote live env: ${ENV_OUT_PATH}"
echo "[local-atproto] source DID: ${source_did}"
echo "[local-atproto] target DID: ${target_did}"
