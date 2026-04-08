#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ENV_OUT_PATH="${1:-/tmp/mvab-testnet-live.env}"
TESTNET_PDS_HOST="${TESTNET_PDS_HOST:-http://127.0.0.1:6000}"
TESTNET_RELAY_URL="${TESTNET_RELAY_URL:-ws://127.0.0.1:7001/xrpc/com.atproto.sync.subscribeRepos}"
TESTNET_PEER_VERIFY_CMD="${TESTNET_PEER_VERIFY_CMD:-./scripts/local_room_peer_verify.sh}"

if ! command -v jq >/dev/null 2>&1; then
  echo "[testnet-atproto] jq is required" >&2
  exit 1
fi

"${ROOT_DIR}/scripts/testnet_atproto_wait.sh" >/dev/null

run_id="${TESTNET_ATPROTO_RUN_ID:-$(date +%s)}"
suffix="${TESTNET_ATPROTO_SUFFIX:-$(printf '%06x' "$((run_id % 1679616))")}"

source_handle="${TESTNET_SOURCE_HANDLE:-s${suffix}.test}"
target_handle="${TESTNET_TARGET_HANDLE:-t${suffix}.test}"
source_email="${TESTNET_SOURCE_EMAIL:-bridge-source-${run_id}@example.test}"
target_email="${TESTNET_TARGET_EMAIL:-bridge-target-${run_id}@example.test}"
source_password="${TESTNET_SOURCE_PASSWORD:-bridge-source-${run_id}-pass}"
target_password="${TESTNET_TARGET_PASSWORD:-bridge-target-${run_id}-pass}"

account_err_file="$(mktemp "${TMPDIR:-/tmp}/mvab-testnet-create-account-XXXXXX.err")"
session_err_file="$(mktemp "${TMPDIR:-/tmp}/mvab-testnet-create-session-XXXXXX.err")"
cleanup() {
  rm -f "${account_err_file}" "${session_err_file}"
}
trap cleanup EXIT

json_post() {
  local path="$1"
  local payload="$2"
  curl -sS -f -X POST \
    -H 'content-type: application/json' \
    "${TESTNET_PDS_HOST}${path}" \
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

  if output="$(create_account "${handle}" "${email}" "${password}" 2>"${account_err_file}")"; then
    echo "${output}"
    return
  fi

  if output="$(create_session "${handle}" "${password}" 2>"${session_err_file}")"; then
    echo "${output}"
    return
  fi

  echo "[testnet-atproto] failed to create/login account for handle=${handle}" >&2
  echo "[testnet-atproto] createAccount error:" >&2
  sed -n '1,120p' "${account_err_file}" >&2 || true
  echo "[testnet-atproto] createSession error:" >&2
  sed -n '1,120p' "${session_err_file}" >&2 || true
  exit 1
}

echo "[testnet-atproto] provisioning source and target accounts"
source_session="$(create_or_login "${source_handle}" "${source_email}" "${source_password}")"
target_session="$(create_or_login "${target_handle}" "${target_email}" "${target_password}")"

source_did="$(echo "${source_session}" | jq -r '.did // empty')"
target_did="$(echo "${target_session}" | jq -r '.did // empty')"

if [[ -z "${source_did}" || -z "${target_did}" ]]; then
  echo "[testnet-atproto] failed to derive source/target DIDs" >&2
  echo "source session: ${source_session}" >&2
  echo "target session: ${target_session}" >&2
  exit 1
fi

mkdir -p "$(dirname "${ENV_OUT_PATH}")"
cat > "${ENV_OUT_PATH}" <<VARS
ATPROTO_HARNESS_PROFILE=testnet
LIVE_E2E_ENABLED=1
LIVE_ATPROTO_HOST=${TESTNET_PDS_HOST}
LIVE_RELAY_URL=${TESTNET_RELAY_URL}
LIVE_ATPROTO_IDENTIFIER=${source_handle}
LIVE_ATPROTO_PASSWORD=${source_password}
LIVE_ATPROTO_FOLLOW_TARGET_DID=${target_did}
LIVE_ROOM_MODE=open
LIVE_ROOM_PEER_VERIFY_CMD=${TESTNET_PEER_VERIFY_CMD}
LIVE_REQUIRE_ACTIVE_BRIDGED_PEERS=1
TESTNET_SOURCE_DID=${source_did}
TESTNET_TARGET_DID=${target_did}
TESTNET_SOURCE_HANDLE=${source_handle}
TESTNET_TARGET_HANDLE=${target_handle}
VARS

echo "[testnet-atproto] wrote live env: ${ENV_OUT_PATH}"
echo "[testnet-atproto] source DID: ${source_did}"
echo "[testnet-atproto] target DID: ${target_did}"
