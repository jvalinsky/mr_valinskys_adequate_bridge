#!/usr/bin/env bash
# test_runner.sh — runs inside the test-runner container for full-stack E2E.
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

log() { echo "[e2e-tf-full] $(date +%H:%M:%S) $*"; }

die() { log "FAIL: $*" >&2; exit 1; }

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
    result="$(sqlite3 "${db_path}" "${query}" 2>/dev/null)" && { echo "${result}"; return 0; }
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

# ------------------------------------------------------------------
# 1. Wait for bridge room and seeder
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
# 2. Register bot DID in bridge if not already present
# ------------------------------------------------------------------
log "ensuring bridged account for bot in bridge DB ..."
# The bridge CLI may have already started or it may be starting. We can talk to the DB directly.
BOT_SSB_FEED="$(sql_retry "${BRIDGE_DB_PATH}" "SELECT ssb_feed_id FROM bridged_accounts WHERE at_did='$(sql_escape "${BOT_DID}")' AND active=1 LIMIT 1;" || true)"
if [[ -z "${BOT_SSB_FEED}" ]]; then
  log "waiting for bridged account to appear (seeder may still be running)..."
  sleep 5
  BOT_SSB_FEED="$(sql_retry "${BRIDGE_DB_PATH}" "SELECT ssb_feed_id FROM bridged_accounts WHERE at_did='$(sql_escape "${BOT_DID}")' AND active=1 LIMIT 1;" || true)"
fi

if [[ -z "${BOT_SSB_FEED}" ]]; then
  die "no active bridged account found for bot DID=${BOT_DID} in ${BRIDGE_DB_PATH}"
fi
log "bot SSB feed: ${BOT_SSB_FEED}"

# ------------------------------------------------------------------
# 2b. Wait for firehose to deliver commits and bridge to publish SSB messages
# ------------------------------------------------------------------
log "waiting for firehose to deliver commits and bridge to publish SSB messages..."
MAX_FIREHOSE_WAIT=60
FIREHOSE_WAIT=0
while true; do
  PUBLISHED_COUNT="$(sql_count "${BRIDGE_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE published=1;")"
  if [[ "${PUBLISHED_COUNT}" -gt 0 ]]; then
    log "firehose delivered commits, bridge has ${PUBLISHED_COUNT} published SSB messages"
    break
  fi
  if ((FIREHOSE_WAIT >= MAX_FIREHOSE_WAIT)); then
    log "Warning: firehose wait timed out after ${MAX_FIREHOSE_WAIT}s, proceeding anyway..."
    break
  fi
  log "waiting for firehose... (${FIREHOSE_WAIT}s/${MAX_FIREHOSE_WAIT}s) published=${PUBLISHED_COUNT}"
  sleep 2
  FIREHOSE_WAIT=$((FIREHOSE_WAIT + 2))
done

# ------------------------------------------------------------------
# 3. Tildefriends setup
# ------------------------------------------------------------------
log "getting tildefriends identity ..."
TF_IDENTITY="$("${TF_BIN}" get_identity --db-path "${TF_DB_PATH}" 2>/dev/null | grep -oE '@[A-Za-z0-9+/=]*\.ed25519' | head -n1)"
log "tildefriends identity: ${TF_IDENTITY}"

# Add TF to room members
sqlite3 "${ROOM_DB_PATH}" "INSERT OR IGNORE INTO members (role, pub_key) VALUES (1, '$(sql_escape "${TF_IDENTITY}")');"

# Get room pub key (FeedRef serializes as {} due to unexported fields, use .public instead)
ROOM_PUB_KEY="@$(jq -r '.public // empty' "${BRIDGE_REPO_PATH}/room/secret" | tr -d '[:space:]')"

# Configure TF connection
sqlite3 "${TF_DB_PATH}" "INSERT OR IGNORE INTO connections (host, port, key) VALUES ('bridge', 8989, '$(sql_escape "${ROOM_PUB_KEY}")');"

# ------------------------------------------------------------------
# 4. Explicit Follows for UI Visibility
# ------------------------------------------------------------------
BRIDGE_PUBKEY="@$(jq -r '.public // empty' "${BRIDGE_REPO_PATH}/secret" | tr -d '[:space:]')"
log "bridge identity: ${BRIDGE_PUBKEY}"

log "publishing follow messages (TF -> bot, TF -> bridge) ..."
"${TF_BIN}" publish --db-path "${TF_DB_PATH}" --id "${TF_IDENTITY}" --content "{\"type\":\"contact\",\"contact\":\"${BOT_SSB_FEED}\",\"following\":true}" || die "failed to follow bot"
"${TF_BIN}" publish --db-path "${TF_DB_PATH}" --id "${TF_IDENTITY}" --content "{\"type\":\"contact\",\"contact\":\"${BRIDGE_PUBKEY}\",\"following\":true}" || die "failed to follow bridge"
log "follow messages published"

log "waiting for Tildefriends to index follow messages ..."
sleep 10

# ------------------------------------------------------------------
# 5. Replication and Verification
# ------------------------------------------------------------------
log "starting tildefriends natively in background (port 12345) ..."
"${TF_BIN}" run --db-path "${TF_DB_PATH}" --verbose --one-proc --args "http_port=12345,http_local_only=false" > /tmp/tf.log 2>&1 &
TF_PID=$!

log "waiting for replication from bot ${BOT_SSB_FEED} ..."
deadline=$((SECONDS + MAX_WAIT_SECS))
while true; do
  # TF stores content as CBOR blobs — cast to TEXT and use simple substring matching
  msg_count="$(sql_count "${TF_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE author='$(sql_escape "${BOT_SSB_FEED}")';")"
  post_count="$(sql_count "${TF_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE author='$(sql_escape "${BOT_SSB_FEED}")' AND CAST(content AS TEXT) LIKE '%type%post%';")"
  following_count="$(sql_count "${TF_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE author='$(sql_escape "${TF_IDENTITY}")' AND CAST(content AS TEXT) LIKE '%type%contact%';")"
  log "tildefriends status: following=${following_count} messages_from_bot=${msg_count} posts_from_bot=${post_count}"

  if [[ "${post_count}" -ge 1 ]]; then
    msg_content="$(sqlite3 "${TF_DB_PATH}" "SELECT CAST(content AS TEXT) FROM messages WHERE author='$(sql_escape "${BOT_SSB_FEED}")' AND CAST(content AS TEXT) LIKE '%type%post%' LIMIT 1;")"
    # Extract text between 'dtext' CBOR key and next field — CBOR text has readable strings
    msg_text="$(echo "${msg_content}" | grep -oP '(?<=text).+?(?=type|$)' | head -1 || echo "(CBOR content)")"
    log "SUCCESS: tildefriends replicated ${post_count} post(s) and $((msg_count - post_count)) other message(s) from bot"
    log "replicated message text: ${msg_text}"
    break
  fi
  if ((SECONDS >= deadline)); then
    tail -n 100 /tmp/tf.log
    die "tildefriends did not replicate bot feed after ${MAX_WAIT_SECS}s"
  fi
  sleep "${POLL_INTERVAL}"
done

log "test passed! Keeping Tildefriends alive on port 12345..."
wait "${TF_PID}"
exit 0
