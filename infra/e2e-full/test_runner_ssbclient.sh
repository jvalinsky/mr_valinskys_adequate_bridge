#!/usr/bin/env bash
set -euo pipefail

BRIDGE_HTTP_ADDR="${BRIDGE_HTTP_ADDR:-bridge:8976}"
BRIDGE_DB_PATH="${BRIDGE_DB_PATH:-/bridge-data/bridge.sqlite}"
BRIDGE_REPO_PATH="${BRIDGE_REPO_PATH:-/bridge-data/ssb-repo}"
SSB_CLIENT_BASE_URL="${SSB_CLIENT_BASE_URL:-http://ssb-client-e2e:8080}"
PDS_HOST="${PDS_HOST:-http://pds:80}"
REVERSE_ENV_FILE="${REVERSE_ENV_FILE:-/bridge-data/reverse-bootstrap.env}"
MAX_WAIT_SECS="${MAX_WAIT_SECS:-300}"
POLL_INTERVAL="${POLL_INTERVAL:-5}"

log() { echo "[e2e-ssb-client] $(date +%H:%M:%S) $*"; }
die() { log "FAIL: $*" >&2; exit 1; }

sql_escape() {
  local escaped="${1//\'/\'\'}"
  echo "${escaped}"
}

sql_retry() {
  local db_path="$1"
  local query="$2"
  local attempts=5
  local result=""
  local i
  for ((i = 1; i <= attempts; i++)); do
    result="$(sqlite3 -cmd ".timeout 5000" "${db_path}" "${query}" 2>/dev/null)" && { echo "${result}"; return 0; }
    sleep 1
  done
  echo ""
  return 1
}

url_decode() {
  local encoded="${1//+/ }"
  printf '%b' "${encoded//%/\\x}"
}

atproto_create_session() {
  local identifier="$1"
  local password="$2"
  local payload
  payload="$(jq -cn --arg identifier "${identifier}" --arg password "${password}" '{identifier:$identifier,password:$password}')"
  curl -sS -f -X POST \
    -H 'content-type: application/json' \
    "${PDS_HOST}/xrpc/com.atproto.server.createSession" \
    -d "${payload}"
}

atproto_create_record() {
  local access_jwt="$1"
  local repo="$2"
  local collection="$3"
  local record_json="$4"
  local payload
  payload="$(jq -cn \
    --arg collection "${collection}" \
    --arg repo "${repo}" \
    --argjson record "${record_json}" \
    '{collection:$collection,repo:$repo,record:$record}')"
  curl -sS -f -X POST \
    -H 'authorization: Bearer '"${access_jwt}" \
    -H 'content-type: application/json' \
    "${PDS_HOST}/xrpc/com.atproto.repo.createRecord" \
    -d "${payload}"
}

atproto_get_record() {
  local access_jwt="$1"
  local at_uri="$2"
  local without_prefix="${at_uri#at://}"
  local repo="${without_prefix%%/*}"
  local remainder="${without_prefix#*/}"
  local collection="${remainder%%/*}"
  local rkey="${remainder#*/}"
  curl -sS -f -G \
    -H 'authorization: Bearer '"${access_jwt}" \
    --data-urlencode "repo=${repo}" \
    --data-urlencode "collection=${collection}" \
    --data-urlencode "rkey=${rkey}" \
    "${PDS_HOST}/xrpc/com.atproto.repo.getRecord"
}

atproto_get_record_http() {
  local access_jwt="$1"
  local at_uri="$2"
  local without_prefix="${at_uri#at://}"
  local repo="${without_prefix%%/*}"
  local remainder="${without_prefix#*/}"
  local collection="${remainder%%/*}"
  local rkey="${remainder#*/}"
  curl -sS -o /tmp/ssb-client-get-record.json -w "%{http_code}" -G \
    -H 'authorization: Bearer '"${access_jwt}" \
    --data-urlencode "repo=${repo}" \
    --data-urlencode "collection=${collection}" \
    --data-urlencode "rkey=${rkey}" \
    "${PDS_HOST}/xrpc/com.atproto.repo.getRecord"
}

wait_for_bridge_health() {
  local deadline=$((SECONDS + MAX_WAIT_SECS))
  while true; do
    if curl -sS -f "http://${BRIDGE_HTTP_ADDR}/healthz" >/dev/null 2>&1; then
      return 0
    fi
    if ((SECONDS >= deadline)); then
      die "bridge healthz timed out after ${MAX_WAIT_SECS}s"
    fi
    sleep "${POLL_INTERVAL}"
  done
}

wait_for_ssb_client() {
  local deadline=$((SECONDS + MAX_WAIT_SECS))
  while true; do
    if curl -sS -f "${SSB_CLIENT_BASE_URL}/api/whoami" >/tmp/ssb-client-whoami.json 2>/dev/null; then
      return 0
    fi
    if ((SECONDS >= deadline)); then
      die "ssb-client API timed out after ${MAX_WAIT_SECS}s"
    fi
    sleep 1
  done
}

wait_for_ssb_client_peers() {
  local minimum="$1"
  local deadline=$((SECONDS + MAX_WAIT_SECS))
  while true; do
    local count
    count="$(curl -sS "${SSB_CLIENT_BASE_URL}/api/peers" | jq -r '.count // 0')"
    if [[ "${count}" -ge "${minimum}" ]]; then
      return 0
    fi
    if ((SECONDS >= deadline)); then
      die "ssb-client peers remained below ${minimum}"
    fi
    sleep 1
  done
}

ssb_client_publish() {
  local payload="$1"
  curl -sS -f -X POST \
    -H 'content-type: application/json' \
    "${SSB_CLIENT_BASE_URL}/api/publish" \
    -d "${payload}" | jq -r '.key // empty'
}

ssb_client_upload_blob() {
  local file_path="$1"
  local headers="/tmp/ssb-client-upload-blob.headers"
  local http_code
  http_code="$(curl -sS -D "${headers}" -o /tmp/ssb-client-upload-blob.body -w "%{http_code}" \
    -X POST \
    -F "file=@${file_path}" \
    "${SSB_CLIENT_BASE_URL}/blobs/upload")"
  if [[ "${http_code}" != "303" ]]; then
    die "ssb-client blob upload failed: http=${http_code} body=$(cat /tmp/ssb-client-upload-blob.body)"
  fi
  local blob_hash
  blob_hash="$(openssl dgst -sha256 -binary "${file_path}" | openssl base64 -A | tr -d '=')"
  echo "&${blob_hash}.sha256"
}

wait_for_reverse_event_published() {
  local source_ref="$1"
  local source_ref_escaped
  source_ref_escaped="$(sql_escape "${source_ref}")"
  local deadline=$((SECONDS + MAX_WAIT_SECS))
  while true; do
    local row
    row="$(sql_retry "${BRIDGE_DB_PATH}" "SELECT event_state || '|' || COALESCE(result_at_uri, '') || '|' || COALESCE(result_at_cid, '') FROM reverse_events WHERE source_ssb_msg_ref='${source_ref_escaped}' LIMIT 1;" || true)"
    if [[ -n "${row}" ]]; then
      local state
      state="$(echo "${row}" | cut -d'|' -f1)"
      if [[ "${state}" == "published" ]]; then
        echo "${row}"
        return 0
      fi
      if [[ "${state}" == "failed" ]]; then
        die "reverse event ${source_ref} failed"
      fi
    fi
    if ((SECONDS >= deadline)); then
      die "timed out waiting for reverse event ${source_ref}"
    fi
    sleep "${POLL_INTERVAL}"
  done
}

wait_for_record_deleted() {
  local access_jwt="$1"
  local at_uri="$2"
  local deadline=$((SECONDS + MAX_WAIT_SECS))
  while true; do
    local http_code
    http_code="$(atproto_get_record_http "${access_jwt}" "${at_uri}")"
    if [[ "${http_code}" != "200" ]]; then
      return 0
    fi
    if ((SECONDS >= deadline)); then
      die "timed out waiting for record deletion ${at_uri}"
    fi
    sleep "${POLL_INTERVAL}"
  done
}

wait_for_bridge_health

if [[ ! -f "${REVERSE_ENV_FILE}" ]]; then
  die "reverse env file not found at ${REVERSE_ENV_FILE}"
fi
set -a
# shellcheck source=/dev/null
source "${REVERSE_ENV_FILE}"
set +a

if [[ -z "${E2E_REVERSE_SOURCE_DID:-}" || -z "${E2E_REVERSE_TARGET_DID:-}" || -z "${E2E_REVERSE_SOURCE_IDENTIFIER:-}" || -z "${E2E_REVERSE_SOURCE_PASSWORD:-}" ]]; then
  die "reverse bootstrap env missing source/target credentials"
fi

REVERSE_SOURCE_FEED=""
REVERSE_TARGET_FEED=""
deadline=$((SECONDS + MAX_WAIT_SECS))
while [[ -z "${REVERSE_SOURCE_FEED}" || -z "${REVERSE_TARGET_FEED}" ]]; do
  REVERSE_SOURCE_FEED="$(sql_retry "${BRIDGE_DB_PATH}" "SELECT ssb_feed_id FROM bridged_accounts WHERE at_did='$(sql_escape "${E2E_REVERSE_SOURCE_DID}")' AND active=1 LIMIT 1;" || true)"
  REVERSE_SOURCE_FEED="$(echo "${REVERSE_SOURCE_FEED}" | tr -d '[:space:]')"
  REVERSE_TARGET_FEED="$(sql_retry "${BRIDGE_DB_PATH}" "SELECT ssb_feed_id FROM bridged_accounts WHERE at_did='$(sql_escape "${E2E_REVERSE_TARGET_DID}")' AND active=1 LIMIT 1;" || true)"
  REVERSE_TARGET_FEED="$(echo "${REVERSE_TARGET_FEED}" | tr -d '[:space:]')"
  if [[ -n "${REVERSE_SOURCE_FEED}" && -n "${REVERSE_TARGET_FEED}" ]]; then
    break
  fi
  if ((SECONDS >= deadline)); then
    die "reverse source/target feeds not registered in bridge db"
  fi
  sleep 2
done

wait_for_ssb_client
SSB_CLIENT_FEED="$(jq -r '.feedId // empty' /tmp/ssb-client-whoami.json | tr -d '[:space:]')"
if [[ -z "${SSB_CLIENT_FEED}" ]]; then
  die "ssb-client whoami returned empty feed"
fi

BRIDGE_FEED="$(jq -r '.id // empty' "${BRIDGE_REPO_PATH}/secret" | tr -d '[:space:]')"
if [[ -z "${BRIDGE_FEED}" ]]; then
  die "bridge feed id missing from ${BRIDGE_REPO_PATH}/secret"
fi

log "creating room invite for ssb-client ..."
invite_headers="/tmp/ssb-client-create-invite.headers"
invite_body="/tmp/ssb-client-create-invite.json"
invite_http="$(curl -sS -D "${invite_headers}" -o "${invite_body}" -w "%{http_code}" -X POST "http://${BRIDGE_HTTP_ADDR}/create-invite" -H 'Accept: application/json' -H 'X-Forwarded-Proto: https')"
invite_resp="$(cat "${invite_body}")"
invite_url="$(echo "${invite_resp}" | jq -r '.url // empty' 2>/dev/null || true)"
if [[ "${invite_http}" != "200" || -z "${invite_url}" ]]; then
  die "room invite response missing url: http=${invite_http} body=${invite_resp}"
fi
invite_url="http://bridge:8976${invite_url#https://bridge}"
log "using invite url ${invite_url}"

join_headers="/tmp/ssb-client-join.headers"
curl -sS -D "${join_headers}" -o /tmp/ssb-client-join.body -X POST \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode "invite=${invite_url}" \
  "${SSB_CLIENT_BASE_URL}/room" >/dev/null
join_location="$(awk 'BEGIN{IGNORECASE=1} /^Location:/ {sub(/\r$/, "", $2); print $2}' "${join_headers}" | tail -n1)"
if [[ -n "${join_location}" && "${join_location}" == *"error="* ]]; then
  die "ssb-client room join failed: ${join_location}"
fi

wait_for_ssb_client_peers 1

log "publishing follow bootstrap messages from repo ssb-client ..."
follow_source_key="$(ssb_client_publish "$(jq -cn --arg contact "${REVERSE_SOURCE_FEED}" '{type:"contact",contact:$contact,following:true,blocking:false}')")"
follow_bridge_key="$(ssb_client_publish "$(jq -cn --arg contact "${BRIDGE_FEED}" '{type:"contact",contact:$contact,following:true,blocking:false}')")"
if [[ -z "${follow_source_key}" || -z "${follow_bridge_key}" ]]; then
  die "failed to publish bootstrap follow messages"
fi

log "adding reverse allowlist mapping for repo ssb-client identity ..."
sqlite3 -cmd ".timeout 5000" "${BRIDGE_DB_PATH}" "INSERT INTO reverse_identity_mappings (ssb_feed_id, at_did, active, allow_posts, allow_replies, allow_follows, updated_at) VALUES ('$(sql_escape "${SSB_CLIENT_FEED}")', '$(sql_escape "${E2E_REVERSE_SOURCE_DID}")', 1, 1, 1, 1, CURRENT_TIMESTAMP) ON CONFLICT(ssb_feed_id) DO UPDATE SET at_did=excluded.at_did, active=1, allow_posts=1, allow_replies=1, allow_follows=1, updated_at=CURRENT_TIMESTAMP;"

source_session_json="$(atproto_create_session "${E2E_REVERSE_SOURCE_IDENTIFIER}" "${E2E_REVERSE_SOURCE_PASSWORD}")"
SOURCE_ACCESS_JWT="$(echo "${source_session_json}" | jq -r '.accessJwt // empty')"
SOURCE_SESSION_DID="$(echo "${source_session_json}" | jq -r '.did // empty')"
if [[ -z "${SOURCE_ACCESS_JWT}" || "${SOURCE_SESSION_DID}" != "${E2E_REVERSE_SOURCE_DID}" ]]; then
  die "failed to create reverse source session"
fi

root_marker="ssb-client reverse root $(date +%s)"
root_key="$(ssb_client_publish "$(jq -cn --arg text "${root_marker}" '{type:"post",text:$text}')")"
root_row="$(wait_for_reverse_event_published "${root_key}")"
ROOT_AT_URI="$(echo "${root_row}" | cut -d'|' -f2)"
ROOT_AT_CID="$(echo "${root_row}" | cut -d'|' -f3)"
root_record="$(atproto_get_record "${SOURCE_ACCESS_JWT}" "${ROOT_AT_URI}")"
if [[ "$(echo "${root_record}" | jq -r '.value.text // empty')" != "${root_marker}" ]]; then
  die "root reverse post text mismatch"
fi
if [[ "$(echo "${root_record}" | jq -r '.value.reply // empty')" != "" ]]; then
  die "root reverse post unexpectedly has reply refs"
fi
if [[ -z "${ROOT_AT_URI}" || -z "${ROOT_AT_CID}" || -z "${root_key}" ]]; then
  die "root reverse post missing uri/cid/source ref"
fi

reply_marker="ssb-client reverse reply $(date +%s)"
reply_key="$(ssb_client_publish "$(jq -cn --arg text "${reply_marker}" --arg root "${root_key}" '{type:"post",text:$text,root:$root,branch:$root}')")"
reply_row="$(wait_for_reverse_event_published "${reply_key}")"
REPLY_AT_URI="$(echo "${reply_row}" | cut -d'|' -f2)"
reply_record="$(atproto_get_record "${SOURCE_ACCESS_JWT}" "${REPLY_AT_URI}")"
if [[ "$(echo "${reply_record}" | jq -r '.value.reply.root.uri // empty')" != "${ROOT_AT_URI}" ]]; then
  die "reply root uri mismatch"
fi
if [[ "$(echo "${reply_record}" | jq -r '.value.reply.parent.uri // empty')" != "${ROOT_AT_URI}" ]]; then
  die "reply parent uri mismatch"
fi
if [[ "$(echo "${reply_record}" | jq -r '.value.reply.root.cid // empty')" != "${ROOT_AT_CID}" ]]; then
  die "reply root cid mismatch"
fi

log "publishing reverse image post from repo ssb-client ..."
media_image_path="/tmp/ssb-client-reverse-image.png"
printf '%s' 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVQI12P4z8AAAwABAAX+1O4AAAAASUVORK5CYII=' | base64 -d > "${media_image_path}"
media_blob_ref="$(ssb_client_upload_blob "${media_image_path}")"
media_marker="ssb-client reverse image $(date +%s)"
media_key="$(ssb_client_publish "$(jq -cn \
  --arg text "ssb-client reverse image @target https://example.com ![preview](${media_blob_ref})" \
  --arg target "${REVERSE_TARGET_FEED}" \
  --arg blob "${media_blob_ref}" \
  '{type:"post",text:$text,mentions:[{link:$target,name:"@target"},{link:$blob,name:"live image",type:"image/png"}]}')")"
media_row="$(wait_for_reverse_event_published "${media_key}")"
MEDIA_AT_URI="$(echo "${media_row}" | cut -d'|' -f2)"
media_record="$(atproto_get_record "${SOURCE_ACCESS_JWT}" "${MEDIA_AT_URI}")"
if [[ "$(echo "${media_record}" | jq -r '.value.text // empty')" != "ssb-client reverse image @target https://example.com" ]]; then
  die "reverse image post text mismatch"
fi
if [[ "$(echo "${media_record}" | jq -r '.value.embed.images | length // 0')" != "1" ]]; then
  die "reverse image post missing embed.images"
fi
if [[ "$(echo "${media_record}" | jq -r '.value.embed.images[0].alt // empty')" != "live image" ]]; then
  die "reverse image post alt mismatch"
fi
if ! echo "${media_record}" | jq -e --arg did "${E2E_REVERSE_TARGET_DID}" 'any(.value.facets[]?; any(.features[]?; .did? == $did))' >/dev/null; then
  die "reverse image post missing mention facet"
fi
if ! echo "${media_record}" | jq -e 'any(.value.facets[]?; any(.features[]?; .uri? == "https://example.com"))' >/dev/null; then
  die "reverse image post missing link facet"
fi

follow_key="$(ssb_client_publish "$(jq -cn --arg contact "${REVERSE_TARGET_FEED}" '{type:"contact",contact:$contact,following:true,blocking:false}')")"
follow_row="$(wait_for_reverse_event_published "${follow_key}")"
FOLLOW_AT_URI="$(echo "${follow_row}" | cut -d'|' -f2)"
follow_record="$(atproto_get_record "${SOURCE_ACCESS_JWT}" "${FOLLOW_AT_URI}")"
if [[ "$(echo "${follow_record}" | jq -r '.value.subject // empty')" != "${E2E_REVERSE_TARGET_DID}" ]]; then
  die "follow subject mismatch"
fi

unfollow_key="$(ssb_client_publish "$(jq -cn --arg contact "${REVERSE_TARGET_FEED}" '{type:"contact",contact:$contact,following:false,blocking:false}')")"
unfollow_row="$(wait_for_reverse_event_published "${unfollow_key}")"
UNFOLLOW_AT_URI="$(echo "${unfollow_row}" | cut -d'|' -f2)"
if [[ "${UNFOLLOW_AT_URI}" != "${FOLLOW_AT_URI}" ]]; then
  die "unfollow uri mismatch"
fi
wait_for_record_deleted "${SOURCE_ACCESS_JWT}" "${FOLLOW_AT_URI}"

log "reverse sync verified for repo ssb-client root/reply/image/follow/unfollow"
