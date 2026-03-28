#!/usr/bin/env bash
# test_runner.sh — runs inside the test-runner container for full-stack E2E.
set -euo pipefail

BRIDGE_HTTP_ADDR="${BRIDGE_HTTP_ADDR:-bridge:8976}"
BRIDGE_MUXRPC_ADDR="${BRIDGE_MUXRPC_ADDR:-bridge:8989}"
BRIDGE_DB_PATH="${BRIDGE_DB_PATH:-/bridge-data/bridge.sqlite}"
BRIDGE_REPO_PATH="${BRIDGE_REPO_PATH:-/bridge-data/ssb-repo}"
ROOM_DB_PATH="${ROOM_DB_PATH:-${BRIDGE_REPO_PATH}/room/roomdb}"
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

sql_count() {
  local db_path="$1"
  local query="$2"
  local count
  count="$(sqlite3 "${db_path}" "${query}" 2>/dev/null || echo "0")"
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
BOT_SSB_FEED="$(sqlite3 "${BRIDGE_DB_PATH}" "SELECT ssb_feed_id FROM bridged_accounts WHERE at_did='$(sql_escape "${BOT_DID}")' AND active=1 LIMIT 1;")"
if [[ -z "${BOT_SSB_FEED}" ]]; then
  # We should use the bridge-cli to add it to be safe (it derives the key)
  log "adding bridged account via bridge-cli..."
  # Use the binary from the bridge build if available, or just go run it from repo (which is mounted or in build)
  # Actually, the test-runner container is built from TF but we can mount the bridge repo
  # Wait, test-runner build: context ../../reference/tildefriends, dockerfile: ../../infra/e2e-tildefriends/Dockerfile.tildefriends
  # That Dockerfile probably doesn't have the bridge-cli.
  # Let's hope the seeder added it or we just use SQL with the same derivation logic (which is hard).
  # BUT the atproto-seed tool already ensured the account! Let's check its code.
  # Yes: if err := database.AddBridgedAccount(c.Context, db.BridgedAccount{...
  # So it should be there.
  BOT_SSB_FEED="$(sqlite3 "${BRIDGE_DB_PATH}" "SELECT ssb_feed_id FROM bridged_accounts WHERE at_did='$(sql_escape "${BOT_DID}")' AND active=1 LIMIT 1;")"
fi

if [[ -z "${BOT_SSB_FEED}" ]]; then
  die "no active bridged account found for bot DID=${BOT_DID} in ${BRIDGE_DB_PATH}"
fi
log "bot SSB feed: ${BOT_SSB_FEED}"

# ------------------------------------------------------------------
# 3. Tildefriends setup
# ------------------------------------------------------------------
log "getting tildefriends identity ..."
TF_IDENTITY="$("${TF_BIN}" get_identity --db-path "${TF_DB_PATH}" 2>/dev/null | grep -oE '@[A-Za-z0-9+/=]*\.ed25519' | head -n1)"
log "tildefriends identity: ${TF_IDENTITY}"

# Add TF to room members
sqlite3 "${ROOM_DB_PATH}" "INSERT OR IGNORE INTO members (role, pub_key) VALUES (1, '$(sql_escape "${TF_IDENTITY}")');"

# Get room pub key
ROOM_PUB_KEY="$(jq -r '.id // empty' "${BRIDGE_REPO_PATH}/room/secret" | tr -d '[:space:]')"

# Configure TF connection
sqlite3 "${TF_DB_PATH}" "INSERT OR IGNORE INTO connections (host, port, key) VALUES ('bridge', 8989, '$(sql_escape "${ROOM_PUB_KEY}")');"

# ------------------------------------------------------------------
# 4. Explicit Follows for UI Visibility
# ------------------------------------------------------------------
BRIDGE_PUBKEY="$(jq -r '.id // empty' "${BRIDGE_REPO_PATH}/secret" | tr -d '[:space:]')"
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
  msg_count="$(sql_count "${TF_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE author='$(sql_escape "${BOT_SSB_FEED}")';")"
  following_count="$(sql_count "${TF_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE author='$(sql_escape "${TF_IDENTITY}")' AND content LIKE '%\"type\":\"contact\"%';")"
  log "tildefriends status: following=${following_count} messages_from_bot=${msg_count}"
  if [[ "${msg_count}" -ge 1 ]]; then
    msg_content="$(sqlite3 "${TF_DB_PATH}" "SELECT content FROM messages WHERE author='$(sql_escape "${BOT_SSB_FEED}")' LIMIT 1;")"
    msg_text="$(echo "${msg_content}" | jq -r '.text // empty')"
    log "SUCCESS: tildefriends replicated ${msg_count} message(s) from bot"
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
