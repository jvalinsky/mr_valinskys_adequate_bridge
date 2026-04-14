#!/usr/bin/env bash
set -euo pipefail

BRIDGE_HTTP_ADDR="${BRIDGE_HTTP_ADDR:-bridge:8976}"
BRIDGE_DB_PATH="${BRIDGE_DB_PATH:-/bridge-data/bridge.sqlite}"
BRIDGE_REPO_PATH="${BRIDGE_REPO_PATH:-/bridge-data/ssb-repo}"
BRIDGE_MUXRPC_ADDR="${BRIDGE_MUXRPC_ADDR:-bridge:8008}"
SSB_CLIENT_BASE_URL="${SSB_CLIENT_BASE_URL:-http://ssb-client-demo:8080}"
SSB_CLIENT_AUTH_USER="${SSB_CLIENT_AUTH_USER:-demo}"
SSB_CLIENT_AUTH_PASS="${SSB_CLIENT_AUTH_PASS:-ssbclient-password}"
BOT_SEED="${BOT_SEED:-e2e-full-seed}"
PDS_HOST="${PDS_HOST:-http://pds:80}"
BRIDGE_PLC_URL="${BRIDGE_PLC_URL:-http://plc:2582}"
MAX_WAIT_SECS="${MAX_WAIT_SECS:-300}"
POLL_INTERVAL="${POLL_INTERVAL:-5}"

log() { echo "[demo-ssb-client] $(date +%H:%M:%S) $*"; }
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

ssb_client_http() {
  curl -sS -u "${SSB_CLIENT_AUTH_USER}:${SSB_CLIENT_AUTH_PASS}" "$@"
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
    if ssb_client_http -f "${SSB_CLIENT_BASE_URL}/api/whoami" >/tmp/ssb-client-demo-whoami.json 2>/dev/null; then
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
    count="$(ssb_client_http "${SSB_CLIENT_BASE_URL}/api/peers" | jq -r '.count // 0')"
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
  local attempts=5
  local body=""
  local key=""
  local i
  for ((i = 1; i <= attempts; i++)); do
    body="$(ssb_client_http -X POST \
      -H 'content-type: application/json' \
      "${SSB_CLIENT_BASE_URL}/api/publish" \
      -d "${payload}" 2>/dev/null || true)"
    key="$(echo "${body}" | jq -r '.key // empty' 2>/dev/null || true)"
    if [[ -n "${key}" ]]; then
      echo "${key}"
      return 0
    fi
    sleep 1
  done
  return 1
}

run_bridge_backfill() {
  local did="$1"
  bridge-cli \
    --db "${BRIDGE_DB_PATH}" \
    --bot-seed "${BOT_SEED}" \
    backfill \
    --repo-path "${BRIDGE_REPO_PATH}" \
    --did "${did}" \
    --xrpc-host "${PDS_HOST}" \
    --plc-url "${BRIDGE_PLC_URL}" \
    --atproto-insecure
}

wait_for_bridged_bot_posts() {
  local bot_feed="$1"
  local deadline=$((SECONDS + MAX_WAIT_SECS))
  while true; do
    local count
    count="$(ssb_client_http -G \
      --data-urlencode "author=${bot_feed}" \
      --data-urlencode "type=post" \
      --data-urlencode "limit=200" \
      "${SSB_CLIENT_BASE_URL}/api/messages" | jq -r '.count // 0')"
    if [[ "${count}" -ge 1 ]]; then
      echo "${count}"
      return 0
    fi
    if ((SECONDS >= deadline)); then
      return 1
    fi
    sleep "${POLL_INTERVAL}"
  done
}

wait_for_bridge_health

log "waiting for ATProto seeder marker ..."
deadline=$((SECONDS + MAX_WAIT_SECS))
BOT_DID=""
while true; do
  if [[ -f "/bridge-data/atproto-seed-complete" ]]; then
    BOT_DID="$(cat /bridge-data/atproto-seed-complete | tr -d '[:space:]')"
    break
  fi
  if ((SECONDS >= deadline)); then
    die "timed out waiting for /bridge-data/atproto-seed-complete"
  fi
  sleep "${POLL_INTERVAL}"
done
if [[ -z "${BOT_DID}" ]]; then
  die "bot did marker is empty"
fi
log "seed bot DID: ${BOT_DID}"

BOT_SSB_FEED=""
deadline=$((SECONDS + MAX_WAIT_SECS))
while [[ -z "${BOT_SSB_FEED}" ]]; do
  BOT_SSB_FEED="$(sql_retry "${BRIDGE_DB_PATH}" "SELECT ssb_feed_id FROM bridged_accounts WHERE at_did='$(sql_escape "${BOT_DID}")' AND active=1 LIMIT 1;" || true)"
  BOT_SSB_FEED="$(echo "${BOT_SSB_FEED}" | tr -d '[:space:]')"
  if [[ -n "${BOT_SSB_FEED}" ]]; then
    break
  fi
  if ((SECONDS >= deadline)); then
    die "bot feed mapping did not appear in bridge DB"
  fi
  sleep 2
done
log "bridged bot feed: ${BOT_SSB_FEED}"

log "running explicit bridge backfill for ${BOT_DID} ..."
if ! run_bridge_backfill "${BOT_DID}" >/tmp/ssb-client-demo-backfill.log 2>&1; then
  log "backfill command failed; continuing with firehose-only wait"
  tail -n 40 /tmp/ssb-client-demo-backfill.log || true
fi

bridge_post_count="$(sql_retry "${BRIDGE_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE at_did='$(sql_escape "${BOT_DID}")' AND message_state='published' AND type='post';" || true)"
bridge_post_count="$(echo "${bridge_post_count}" | tr -d '[:space:]')"
bridge_post_count="${bridge_post_count:-0}"
if [[ "${bridge_post_count}" -ge 1 ]]; then
  log "bridge already published posts for bot: ${bridge_post_count}"
else
  # The bridge DB can be transiently locked while the room/replication runtime is active.
  # Continue and rely on the final ssb-client sync assertion below.
  log "bridge published post count not yet visible (count=${bridge_post_count}); continuing to room join + ssb-client sync check"
fi

wait_for_ssb_client
SSB_CLIENT_FEED="$(jq -r '.feedId // empty' /tmp/ssb-client-demo-whoami.json | tr -d '[:space:]')"
if [[ -z "${SSB_CLIENT_FEED}" ]]; then
  die "ssb-client whoami returned empty feed"
fi

BRIDGE_FEED="$(jq -r '.id // empty' "${BRIDGE_REPO_PATH}/secret" | tr -d '[:space:]')"
if [[ -z "${BRIDGE_FEED}" ]]; then
  die "bridge feed id missing from ${BRIDGE_REPO_PATH}/secret"
fi

log "creating room invite for ssb-client ..."
invite_body="/tmp/ssb-client-demo-invite.json"
invite_http="$(curl -sS -o "${invite_body}" -w "%{http_code}" -X POST "http://${BRIDGE_HTTP_ADDR}/create-invite" -H 'Accept: application/json' -H 'X-Forwarded-Proto: https')"
invite_resp="$(cat "${invite_body}")"
invite_url="$(echo "${invite_resp}" | jq -r '.url // empty' 2>/dev/null || true)"
if [[ "${invite_http}" != "200" || -z "${invite_url}" ]]; then
  die "room invite response missing url: http=${invite_http} body=${invite_resp}"
fi

if [[ "${invite_url}" == https://bridge/* ]]; then
  invite_url="http://bridge:8976${invite_url#https://bridge}"
fi
log "joining room via invite ${invite_url}"

join_headers="/tmp/ssb-client-demo-join.headers"
curl -sS -D "${join_headers}" -o /tmp/ssb-client-demo-join.body -X POST \
  -u "${SSB_CLIENT_AUTH_USER}:${SSB_CLIENT_AUTH_PASS}" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode "invite=${invite_url}" \
  "${SSB_CLIENT_BASE_URL}/room" >/dev/null
join_location="$(awk 'BEGIN{IGNORECASE=1} /^Location:/ {sub(/\r$/, "", $2); print $2}' "${join_headers}" | tail -n1)"
if [[ -n "${join_location}" && "${join_location}" == *"error="* ]]; then
  die "ssb-client room join failed: ${join_location}"
fi

wait_for_ssb_client_peers 1

log "connecting ssb-client directly to bridge muxrpc endpoint ${BRIDGE_MUXRPC_ADDR} ..."
bridge_connect_payload="$(jq -cn --arg address "${BRIDGE_MUXRPC_ADDR}" --arg pubkey "${BRIDGE_FEED#@}" '{address:$address,pubkey:$pubkey}')"
if ! ssb_client_http -f -X POST \
  -H 'content-type: application/json' \
  "${SSB_CLIENT_BASE_URL}/api/connect" \
  -d "${bridge_connect_payload}" >/tmp/ssb-client-demo-connect.json 2>/dev/null; then
  die "failed to connect ssb-client to bridge muxrpc at ${BRIDGE_MUXRPC_ADDR}"
fi

wait_for_ssb_client_peers 2

log "publishing follow bootstrap messages from ssb-client ..."
follow_bot_key="$(ssb_client_publish "$(jq -cn --arg contact "${BOT_SSB_FEED}" '{type:"contact",contact:$contact,following:true,blocking:false}')" || true)"
if [[ -z "${follow_bot_key}" ]]; then
  die "failed to publish bootstrap follow for bot feed ${BOT_SSB_FEED}"
fi
follow_bridge_key="$(ssb_client_publish "$(jq -cn --arg contact "${BRIDGE_FEED}" '{type:"contact",contact:$contact,following:true,blocking:false}')" || true)"
if [[ -z "${follow_bridge_key}" ]]; then
  log "warning: failed to publish bootstrap follow for bridge feed; continuing with bot follow only"
fi

log "waiting for ssb-client to sync bridged bot posts ..."
synced_post_count=0
deadline=$((SECONDS + MAX_WAIT_SECS))
next_follow_bump=${SECONDS}
while true; do
  count="$(ssb_client_http -G \
    --data-urlencode "author=${BOT_SSB_FEED}" \
    --data-urlencode "type=post" \
    --data-urlencode "limit=200" \
    "${SSB_CLIENT_BASE_URL}/api/messages" | jq -r '.count // 0')"
  if [[ "${count}" -ge 1 ]]; then
    synced_post_count="${count}"
    break
  fi

  if ((SECONDS >= next_follow_bump)); then
    log "bot posts not visible yet; re-publishing follow bootstrap contacts"
    ssb_client_publish "$(jq -cn --arg contact "${BOT_SSB_FEED}" '{type:"contact",contact:$contact,following:true,blocking:false}')" >/dev/null || true
    ssb_client_publish "$(jq -cn --arg contact "${BRIDGE_FEED}" '{type:"contact",contact:$contact,following:true,blocking:false}')" >/dev/null || true
    next_follow_bump=$((SECONDS + 10))
  fi

  if ((SECONDS >= deadline)); then
    die "timed out waiting for bridged bot posts to appear in ssb-client"
  fi
  sleep "${POLL_INTERVAL}"
done

sample_post_text="$(ssb_client_http -G \
  --data-urlencode "author=${BOT_SSB_FEED}" \
  --data-urlencode "type=post" \
  --data-urlencode "limit=1" \
  "${SSB_CLIENT_BASE_URL}/api/messages" | jq -r '.messages[0].content.text // empty')"

log "SUCCESS: ssb-client connected to bridge room and synced ${synced_post_count} post(s) from bridged bot ${BOT_DID} (${BOT_SSB_FEED})"
if [[ -n "${sample_post_text}" ]]; then
  log "sample bridged post text: ${sample_post_text}"
fi
log "ssb-client feed id: ${SSB_CLIENT_FEED}"
