#!/usr/bin/env bash
set -euo pipefail

PDS_HOST="${PDS_HOST:-http://pds:80}"
OUT_DIR="${OUT_DIR:-/bridge-data}"
SOURCE_HANDLE="${E2E_REVERSE_SOURCE_HANDLE:-reverse-source.test}"
TARGET_HANDLE="${E2E_REVERSE_TARGET_HANDLE:-reverse-target.test}"
SOURCE_PASSWORD="${E2E_REVERSE_SOURCE_PASSWORD:-password}"
TARGET_PASSWORD="${E2E_REVERSE_TARGET_PASSWORD:-password}"
SOURCE_EMAIL="${E2E_REVERSE_SOURCE_EMAIL:-${SOURCE_HANDLE}@example.test}"
TARGET_EMAIL="${E2E_REVERSE_TARGET_EMAIL:-${TARGET_HANDLE}@example.test}"
SOURCE_PASSWORD_ENV="${E2E_REVERSE_SOURCE_PASSWORD_ENV:-E2E_REVERSE_SOURCE_PASSWORD}"
TARGET_PASSWORD_ENV="${E2E_REVERSE_TARGET_PASSWORD_ENV:-E2E_REVERSE_TARGET_PASSWORD}"

log() { echo "[atproto-bootstrap] $(date +%H:%M:%S) $*"; }
die() { log "FAIL: $*" >&2; exit 1; }

json_post() {
  local path="$1"
  local payload="$2"
  curl -sS -f -X POST \
    -H 'content-type: application/json' \
    "${PDS_HOST}${path}" \
    -d "${payload}"
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

create_or_login() {
  local handle="$1"
  local email="$2"
  local password="$3"

  if output="$(create_account "${handle}" "${email}" "${password}" 2>/tmp/atproto-bootstrap-create.err)"; then
    echo "${output}"
    return 0
  fi

  if output="$(create_session "${handle}" "${password}" 2>/tmp/atproto-bootstrap-session.err)"; then
    echo "${output}"
    return 0
  fi

  log "createAccount error:"
  sed -n '1,120p' /tmp/atproto-bootstrap-create.err >&2 || true
  log "createSession error:"
  sed -n '1,120p' /tmp/atproto-bootstrap-session.err >&2 || true
  die "failed to create/login account for handle=${handle}"
}

log "waiting for local PDS at ${PDS_HOST}"
deadline=$((SECONDS + 120))
while true; do
  if curl -sS -f "${PDS_HOST}/xrpc/_health" >/dev/null 2>&1; then
    break
  fi
  if ((SECONDS >= deadline)); then
    die "timed out waiting for PDS health"
  fi
  sleep 2
done

mkdir -p "${OUT_DIR}"

log "creating or reusing reverse source/target accounts"
source_session="$(create_or_login "${SOURCE_HANDLE}" "${SOURCE_EMAIL}" "${SOURCE_PASSWORD}")"
target_session="$(create_or_login "${TARGET_HANDLE}" "${TARGET_EMAIL}" "${TARGET_PASSWORD}")"

source_did="$(echo "${source_session}" | jq -r '.did // empty')"
target_did="$(echo "${target_session}" | jq -r '.did // empty')"

[[ -n "${source_did}" ]] || die "missing source did from createSession/createAccount response"
[[ -n "${target_did}" ]] || die "missing target did from createSession/createAccount response"

cat > "${OUT_DIR}/reverse-bootstrap.env" <<EOF
E2E_REVERSE_SOURCE_DID=${source_did}
E2E_REVERSE_SOURCE_IDENTIFIER=${SOURCE_HANDLE}
E2E_REVERSE_SOURCE_PASSWORD=${SOURCE_PASSWORD}
E2E_REVERSE_SOURCE_PASSWORD_ENV=${SOURCE_PASSWORD_ENV}
E2E_REVERSE_TARGET_DID=${target_did}
E2E_REVERSE_TARGET_IDENTIFIER=${TARGET_HANDLE}
E2E_REVERSE_TARGET_PASSWORD=${TARGET_PASSWORD}
E2E_REVERSE_TARGET_PASSWORD_ENV=${TARGET_PASSWORD_ENV}
EOF

jq -n \
  --arg did "${source_did}" \
  --arg identifier "${SOURCE_HANDLE}" \
  --arg pds_host "${PDS_HOST}" \
  --arg password_env "${SOURCE_PASSWORD_ENV}" \
  '{($did): {identifier: $identifier, pds_host: $pds_host, password_env: $password_env}}' \
  > "${OUT_DIR}/reverse-credentials.json"

log "wrote ${OUT_DIR}/reverse-bootstrap.env"
log "wrote ${OUT_DIR}/reverse-credentials.json for did=${source_did}"
