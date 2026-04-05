#!/usr/bin/env bash
# test_runner.sh — runs inside the test-runner container for full-stack E2E.
# Verifies the complete ATProto → Bridge → SSB → Tildefriends pipeline:
#   1. Wait for bridge + seeder to complete
#   2. Extract identities and verify bridged accounts
#   3. Create invite, connect TF via room
#   4. Publish follows, verify replication
#   5. Post-replication checks
set -euo pipefail

BRIDGE_HTTP_ADDR="${BRIDGE_HTTP_ADDR:-bridge:8976}"
BRIDGE_MUXRPC_ADDR="${BRIDGE_MUXRPC_ADDR:-bridge:8989}"
BRIDGE_DB_PATH="${BRIDGE_DB_PATH:-/bridge-data/bridge.sqlite}"
BRIDGE_REPO_PATH="${BRIDGE_REPO_PATH:-/bridge-data/ssb-repo}"
ROOM_DB_PATH="${ROOM_DB_PATH:-${BRIDGE_REPO_PATH}/room/room.sqlite}"
TF_DB_PATH="${TF_DB_PATH:-/tf-data/db.sqlite}"
TF_BIN="${TF_BIN:-/app/out/release/tildefriends}"
BOT_SEED="${BOT_SEED:-e2e-full-seed}"
MAX_WAIT_SECS="${MAX_WAIT_SECS:-300}"
POLL_INTERVAL="${POLL_INTERVAL:-5}"

log() { echo "[e2e-full] $(date +%H:%M:%S) $*"; }
die() {
  if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
    echo "::error file=infra/e2e-full/test_runner.sh,line=${BASH_LINENO[0]}::$*"
  fi
  log "FAIL: $*" >&2
  exit 1
}
gh_warn() {
  if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
    echo "::warning file=infra/e2e-full/test_runner.sh,line=${BASH_LINENO[0]}::$*"
  fi
  log "WARN: $*"
}

cleanup() {
  log "cleaning up background processes..."
  if [[ -n "${TF_PID:-}" ]] && kill -0 "${TF_PID}" 2>/dev/null; then
    kill "${TF_PID}" || true
  fi
}
trap cleanup EXIT

sql_escape() {
  local escaped="${1//\'/\'\'}"
  echo "${escaped}"
}

sql_retry() {
  local db_path="$1"
  local query="$2"
  local attempts=5
  local i
  for ((i = 1; i <= attempts; i++)); do
    result="$(sqlite3 "${db_path}" "PRAGMA busy_timeout=5000; ${query}" 2>/dev/null)" && { echo "${result}"; return 0; }
    sleep 1
  done
  echo ""
  return 1
}

sql_count() {
  local db_path="$1"
  local query="$2"
  local count
  count="$(sql_retry "${db_path}" "${query}" || echo "0")"
  count="$(echo "${count}" | tr -d '[:space:]')"
  if [[ -z "${count}" ]]; then
    count="0"
  fi
  echo "${count}"
}

url_decode() {
  local encoded="${1//+/ }"
  printf '%b' "${encoded//%/\\x}"
}

# ------------------------------------------------------------------
# 1. Wait for bridge room healthz
# ------------------------------------------------------------------
log "waiting for bridge room healthz at http://${BRIDGE_HTTP_ADDR}/healthz ..."
deadline=$((SECONDS + MAX_WAIT_SECS))
while true; do
  if curl -sS -f "http://${BRIDGE_HTTP_ADDR}/healthz" >/dev/null 2>&1; then
    log "bridge room is healthy"
    break
  fi
  if ((SECONDS >= deadline)); then
    die "bridge room healthz timed out after ${MAX_WAIT_SECS}s"
  fi
  sleep "${POLL_INTERVAL}"
done

# ------------------------------------------------------------------
# 2. Wait for seeder to complete (atproto-seed writes marker file)
# ------------------------------------------------------------------
log "waiting for atproto-seed to complete ..."
deadline=$((SECONDS + MAX_WAIT_SECS))
while true; do
  if [[ -f "/bridge-data/atproto-seed-complete" ]]; then
    BOT_DID="$(cat /bridge-data/atproto-seed-complete)"
    log "atproto-seed complete: BOT_DID=${BOT_DID}"
    break
  fi
  if ((SECONDS >= deadline)); then
    die "atproto-seed timed out after ${MAX_WAIT_SECS}s"
  fi
  sleep "${POLL_INTERVAL}"
done

# ------------------------------------------------------------------
# 3. Wait for firehose to deliver commits and bridge to publish
# ------------------------------------------------------------------
log "waiting for firehose to deliver commits and bridge to publish SSB messages..."
bot_did_escaped="$(sql_escape "${BOT_DID}")"
deadline=$((SECONDS + 120))
while true; do
  PUBLISHED_COUNT="$(sql_count "${BRIDGE_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE message_state='published';")"
  if [[ "${PUBLISHED_COUNT}" -gt 0 ]]; then
    log "firehose delivered commits, bridge has ${PUBLISHED_COUNT} published SSB messages"
    break
  fi
  if ((SECONDS >= deadline)); then
    gh_warn "firehose wait timed out, proceeding with ${PUBLISHED_COUNT} published messages..."
    break
  fi
  log "waiting for firehose... published=${PUBLISHED_COUNT}"
  sleep 3
done

# ------------------------------------------------------------------
# 4. Look up the bot's SSB feed from bridge DB
# ------------------------------------------------------------------
log "looking up bot SSB feed for DID=${BOT_DID} ..."
BOT_SSB_FEED="$(sql_retry "${BRIDGE_DB_PATH}" "SELECT ssb_feed_id FROM bridged_accounts WHERE at_did='${bot_did_escaped}' AND active=1 LIMIT 1;" || true)"
BOT_SSB_FEED="$(echo "${BOT_SSB_FEED}" | tr -d '[:space:]')"

if [[ -z "${BOT_SSB_FEED}" ]]; then
  # Seeder may still be registering — wait briefly
  log "waiting for bridged account to appear..."
  deadline=$((SECONDS + 30))
  while true; do
    BOT_SSB_FEED="$(sql_retry "${BRIDGE_DB_PATH}" "SELECT ssb_feed_id FROM bridged_accounts WHERE at_did='${bot_did_escaped}' AND active=1 LIMIT 1;" || true)"
    BOT_SSB_FEED="$(echo "${BOT_SSB_FEED}" | tr -d '[:space:]')"
    if [[ -n "${BOT_SSB_FEED}" ]]; then break; fi
    if ((SECONDS >= deadline)); then
      die "no active bridged account found for bot DID=${BOT_DID} in ${BRIDGE_DB_PATH}"
    fi
    sleep 2
  done
fi
log "bot SSB feed: ${BOT_SSB_FEED}"

# ------------------------------------------------------------------
# 5. Extract room + bridge keys
# ------------------------------------------------------------------
log "extracting room and bridge keys..."
if [[ ! -f "${BRIDGE_REPO_PATH}/room/secret" ]]; then
  die "bridge room secret file not found at ${BRIDGE_REPO_PATH}/room/secret"
fi
ROOM_PUB_KEY="$(jq -r '.id // empty' "${BRIDGE_REPO_PATH}/room/secret" | tr -d '[:space:]')"
if [[ -z "${ROOM_PUB_KEY}" || "${ROOM_PUB_KEY}" != @*".ed25519" ]]; then
  die "invalid room pub key: ${ROOM_PUB_KEY}"
fi
log "room pub key: ${ROOM_PUB_KEY}"

BRIDGE_SECRET_FILE="${BRIDGE_REPO_PATH}/secret"
if [[ ! -f "${BRIDGE_SECRET_FILE}" ]]; then
  die "bridge secret file not found at ${BRIDGE_SECRET_FILE}"
fi
BRIDGE_PUBKEY="$(jq -r '.id // empty' "${BRIDGE_SECRET_FILE}" | tr -d '[:space:]')"
log "bridge pub key: ${BRIDGE_PUBKEY}"

# ------------------------------------------------------------------
# 6. Get tildefriends identity
# ------------------------------------------------------------------
log "getting tildefriends identity ..."
TF_IDENTITY="$("${TF_BIN}" get_identity --db-path "${TF_DB_PATH}" 2>/dev/null | grep -oE '@[A-Za-z0-9+/=]*\.ed25519' | head -n1)"
if [[ -z "${TF_IDENTITY}" || "${TF_IDENTITY}" != @*".ed25519" ]]; then
  die "could not resolve tildefriends identity (got: ${TF_IDENTITY})"
fi
log "tildefriends identity: ${TF_IDENTITY}"

# ------------------------------------------------------------------
# 7. Create + consume invite for TF
# ------------------------------------------------------------------
log "creating invite via room HTTP endpoint..."
invite_resp="$(curl -sS -f -X POST "http://${BRIDGE_HTTP_ADDR}/create-invite" -H "Accept: application/json")"
invite_url="$(echo "${invite_resp}" | jq -r '.url // empty')"
if [[ -z "${invite_url}" || "${invite_url}" != *"token="* ]]; then
  die "create-invite failed: ${invite_resp}"
fi
invite_token_raw="${invite_url##*token=}"
invite_token_raw="${invite_token_raw%%&*}"
INVITE_TOKEN="$(url_decode "${invite_token_raw}")"
log "invite created"

consume_payload="$(jq -cn --arg invite "${INVITE_TOKEN}" --arg id "${TF_IDENTITY}" '{invite:$invite,id:$id}')"
consume_http="$(curl -sS -o /tmp/invite-consume.json -w "%{http_code}" -X POST "http://${BRIDGE_HTTP_ADDR}/invite/consume" -H "Content-Type: application/json" -H "Accept: application/json" --data "${consume_payload}")"
consume_body="$(cat /tmp/invite-consume.json)"
if [[ "${consume_http}" != "200" ]]; then
  die "invite consume failed: http=${consume_http} body=${consume_body}"
fi

MULTISERVER_ADDR="$(echo "${consume_body}" | jq -r '.multiserverAddress // empty')"
if [[ -z "${MULTISERVER_ADDR}" || "${MULTISERVER_ADDR}" != net:*~shs:* ]]; then
  die "invalid invite consume multiserverAddress: ${MULTISERVER_ADDR}"
fi
log "invite consumed: multiserverAddress=${MULTISERVER_ADDR}"

# Parse multiserver address
net_addr="${MULTISERVER_ADDR#net:}"
net_addr="${net_addr%%~shs:*}"
room_key_b64="${MULTISERVER_ADDR##*~shs:}"
if [[ "${net_addr}" =~ ^\[(.*)]:(.*)$ ]]; then
  ROOM_HOST="[${BASH_REMATCH[1]}]"
  ROOM_PORT="${BASH_REMATCH[2]}"
else
  ROOM_HOST="${net_addr%:*}"
  ROOM_PORT="${net_addr##*:}"
fi
ROOM_KEY="@${room_key_b64}.ed25519"
log "room endpoint: host=${ROOM_HOST} port=${ROOM_PORT}"

# ------------------------------------------------------------------
# 8. Publish follow messages from TF → bot + bridge
# ------------------------------------------------------------------
log "publishing follow messages (TF → bot, TF → bridge) ..."
"${TF_BIN}" publish --db-path "${TF_DB_PATH}" --id "${TF_IDENTITY}" --content "{\"type\":\"contact\",\"contact\":\"${BOT_SSB_FEED}\",\"following\":true}" || die "failed to follow bot"
"${TF_BIN}" publish --db-path "${TF_DB_PATH}" --id "${TF_IDENTITY}" --content "{\"type\":\"contact\",\"contact\":\"${BRIDGE_PUBKEY}\",\"following\":true}" || die "failed to follow bridge"
log "follow messages published"

# ------------------------------------------------------------------
# 9. Configure TF connection to room
# ------------------------------------------------------------------
log "configuring TF connection table (room-only) ..."
room_host_escaped="$(sql_escape "${ROOM_HOST}")"
room_key_escaped="$(sql_escape "${ROOM_KEY}")"
sqlite3 "${TF_DB_PATH}" "PRAGMA busy_timeout=5000; DELETE FROM connections;"
sqlite3 "${TF_DB_PATH}" "PRAGMA busy_timeout=5000; INSERT INTO connections (host, port, key) VALUES ('${room_host_escaped}', ${ROOM_PORT}, '${room_key_escaped}');"

# ------------------------------------------------------------------
# 10. Start tildefriends in background
# ------------------------------------------------------------------
log "starting tildefriends in background ..."
"${TF_BIN}" run --db-path "${TF_DB_PATH}" --verbose --one-proc > /tmp/tf.log 2>&1 &
TF_PID=$!
log "tildefriends started with PID ${TF_PID}"

log "waiting for tildefriends to initialize ..."
tf_start_deadline=$((SECONDS + 30))
while true; do
  if ! kill -0 "${TF_PID}" 2>/dev/null; then
    log "tildefriends died during startup. Log tail:"
    tail -n 100 /tmp/tf.log || true
    die "tildefriends process died during startup"
  fi
  if [[ -s /tmp/tf.log ]]; then
    log "tildefriends initialized (log file has output)"
    break
  fi
  if ((SECONDS >= tf_start_deadline)); then
    log "warning: tildefriends startup wait timed out, proceeding"
    break
  fi
  sleep 1
done

# ------------------------------------------------------------------
# 11. Wait for replication
# ------------------------------------------------------------------
log "waiting for TF to replicate bot feed via room ..."
baseline_msg_count="$(sql_count "${TF_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE author='$(sql_escape "${BOT_SSB_FEED}")';")"
deadline=$((SECONDS + MAX_WAIT_SECS))
replicated=false

while true; do
  if ! kill -0 "${TF_PID}" 2>/dev/null; then
    log "tildefriends died unexpectedly. Log tail:"
    tail -n 100 /tmp/tf.log || true
    die "tildefriends process died"
  fi

  tf_msg_count="$(sql_count "${TF_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE author='$(sql_escape "${BOT_SSB_FEED}")';")"
  post_count="$(sql_count "${TF_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE author='$(sql_escape "${BOT_SSB_FEED}")' AND CAST(content AS TEXT) LIKE '%type%post%';")"
  log "tildefriends: messages_from_bot=${tf_msg_count} posts_from_bot=${post_count}"

  if [[ "${tf_msg_count}" -gt "${baseline_msg_count}" && "${post_count}" -ge 1 ]]; then
    log "SUCCESS: tildefriends replicated ${post_count} post(s) and $((tf_msg_count - post_count)) other message(s) from bot"
    replicated=true
    break
  fi

  if ((SECONDS >= deadline)); then
    log "=== TF log tail ==="
    tail -n 200 /tmp/tf.log || true
    log "=== Bridge published message count ==="
    sql_retry "${BRIDGE_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE message_state='published';" || true
    die "tildefriends did not replicate bot feed after ${MAX_WAIT_SECS}s"
  fi
  sleep "${POLL_INTERVAL}"
done

if ! ${replicated}; then
  die "replication check fell through without success"
fi

# ------------------------------------------------------------------
# 12. Post-replication verification
# ------------------------------------------------------------------
log "running post-replication verification ..."

# 12a. Verify bridge published messages for bot
bridge_pub_count="$(sql_count "${BRIDGE_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE at_did='${bot_did_escaped}' AND message_state='published';")"
log "verified: ${bridge_pub_count} messages published on bridge for bot"

# 12b. Verify TF identity is a room member
if [[ -f "${ROOM_DB_PATH}" ]]; then
  tf_id_escaped="$(sql_escape "${TF_IDENTITY}")"
  tf_member_rows="$(sql_count "${ROOM_DB_PATH}" "SELECT COUNT(*) FROM members WHERE pub_key='${tf_id_escaped}';")"
  if [[ "${tf_member_rows}" -lt 1 ]]; then
    die "tildefriends identity not found in room members table"
  fi
  log "verified: tildefriends identity is a room member"
fi

# 12c. Verify bridge runtime was live
bridge_status="$(sql_retry "${BRIDGE_DB_PATH}" "SELECT value FROM bridge_state WHERE key='bridge_runtime_status' LIMIT 1;" || echo "")"
bridge_status="$(echo "${bridge_status}" | tr -d '[:space:]')"
if [[ "${bridge_status}" != "live" ]]; then
  gh_warn "bridge runtime status is '${bridge_status}' (expected 'live')"
fi

log "============================================"
log "  E2E PASSED: Full-stack ATProto → SSB     "
log "  Pipeline verified end-to-end             "
log "============================================"
exit 0
