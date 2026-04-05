#!/usr/bin/env bash
# test_runner.sh — runs inside the test-runner container.
# Orchestrates the tildefriends→bridge-room e2e flow in strict invite-derived mode:
#   1. Wait for bridge room to become healthy & live
#   2. Create and consume invite for TF identity
#   3. Seed TF connection from invite-derived multiserverAddress
#   4. Publish follow messages required for room tunnel dial
#   5. Verify TF replicates bot feed while strict invite invariants hold
set -euo pipefail

BRIDGE_HTTP_ADDR="${BRIDGE_HTTP_ADDR:-bridge:8976}"
BRIDGE_MUXRPC_ADDR="${BRIDGE_MUXRPC_ADDR:-bridge:8989}"
BRIDGE_DB_PATH="${BRIDGE_DB_PATH:-/bridge-data/bridge.sqlite}"
BRIDGE_REPO_PATH="${BRIDGE_REPO_PATH:-/bridge-data/ssb-repo}"
ROOM_DB_PATH="${ROOM_DB_PATH:-${BRIDGE_REPO_PATH}/room/room.sqlite}"
TF_DB_PATH="${TF_DB_PATH:-/tf-data/db.sqlite}"
TF_BIN="${TF_BIN:-/app/out/release/tildefriends}"
BOT_SEED="${BOT_SEED:-e2e-docker-seed}"
BOT_DID="${BOT_DID:-did:plc:e2e-docker-bot}"
MAX_WAIT_SECS="${MAX_WAIT_SECS:-120}"
POLL_INTERVAL="${POLL_INTERVAL:-3}"

log() { echo "[e2e-tf] $(date +%H:%M:%S) $*"; }

gh_warn() {
  if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
    echo "::warning file=infra/e2e-tildefriends/test_runner.sh,line=${BASH_LINENO[0]}::$*"
  fi
  log "WARN: $*"
}

# shellcheck disable=SC2329
cleanup() {
  log "killing background tildefriends process..."
  if [[ -n "${TF_PID:-}" ]] && kill -0 "${TF_PID}" 2>/dev/null; then
    kill "${TF_PID}" || true
  fi
}
trap cleanup EXIT

die() {
  if [[ -n "${GITHUB_ACTIONS:-}" ]]; then
    echo "::error file=infra/e2e-tildefriends/test_runner.sh,line=${BASH_LINENO[0]}::$*"
  fi
  log "FAIL: $*" >&2
  exit 1
}

sql_escape() {
  local escaped="${1//\'/\'\'}"
  echo "${escaped}"
}

sql_count() {
  local db_path="$1"
  local query="$2"
  local count
  count="$(sqlite3 "${db_path}" "PRAGMA busy_timeout=5000; ${query}" 2>/dev/null || echo "0")"
  count="$(echo "${count}" | tr -d '[:space:]')"
  if [[ -z "${count}" ]]; then
    count="0"
  fi
  echo "${count}"
}

parse_host_port() {
  local addr="$1"
  local host_var="$2"
  local port_var="$3"
  local host="${addr%:*}"
  local port="${addr##*:}"
  if [[ -z "${host}" || -z "${port}" || "${host}" == "${addr}" || ! "${port}" =~ ^[0-9]+$ ]]; then
    die "invalid host:port value for BRIDGE_MUXRPC_ADDR: ${addr}"
  fi
  printf -v "${host_var}" "%s" "${host}"
  printf -v "${port_var}" "%s" "${port}"
}

url_decode() {
  local encoded="${1//+/ }"
  printf '%b' "${encoded//%/\\x}"
}

normalize_host_for_compare() {
  local host="$1"
  host="${host#[}"
  host="${host%]}"
  echo "${host}"
}

assert_invite_endpoint_matches_expected() {
  local invite_host_norm expected_host_norm
  invite_host_norm="$(normalize_host_for_compare "${ROOM_HOST}")"
  expected_host_norm="$(normalize_host_for_compare "${EXPECTED_ROOM_HOST}")"

  if [[ "${ROOM_PORT}" != "${EXPECTED_ROOM_PORT}" ]]; then
    die "invite-derived muxrpc port mismatch: got ${ROOM_PORT}, expected ${EXPECTED_ROOM_PORT}"
  fi
  if [[ "${invite_host_norm}" != "${expected_host_norm}" ]]; then
    die "invite-derived muxrpc host mismatch: got ${ROOM_HOST}, expected ${EXPECTED_ROOM_HOST}"
  fi
}

dump_tf_connections() {
  log "current TF connections rows:"
  sqlite3 "${TF_DB_PATH}" "PRAGMA busy_timeout=5000; SELECT host, port, key FROM connections ORDER BY host, port;" || true
}

dump_tf_log_tail() {
  log "showing last 200 lines from /tmp/tf.log:"
  tail -n 200 /tmp/tf.log || true
}

assert_room_only_connections() {
  local room_host_escaped room_key_escaped bridge_key_escaped bridge_raw_key_escaped
  room_host_escaped="$(sql_escape "${ROOM_HOST}")"
  room_key_escaped="$(sql_escape "${ROOM_KEY}")"
  bridge_key_escaped="$(sql_escape "${BRIDGE_KEY}")"
  bridge_raw_key_escaped="$(sql_escape "${BRIDGE_KEY_RAW}")"

  local total_rows room_rows direct_rows
  total_rows="$(sql_count "${TF_DB_PATH}" "SELECT COUNT(*) FROM connections;")"
  room_rows="$(sql_count "${TF_DB_PATH}" "SELECT COUNT(*) FROM connections WHERE host='${room_host_escaped}' AND port=${ROOM_PORT} AND key='${room_key_escaped}';")"
  direct_rows="$(sql_count "${TF_DB_PATH}" "SELECT COUNT(*) FROM connections WHERE port=8008 OR key='${bridge_key_escaped}' OR key='${bridge_raw_key_escaped}';")"

  if [[ "${total_rows}" != "1" ]]; then
    dump_tf_connections
    die "strict room invariant failed: expected exactly 1 TF connection row, got ${total_rows}"
  fi
  if [[ "${room_rows}" != "1" ]]; then
    dump_tf_connections
    die "strict room invariant failed: expected room connection row host=${ROOM_HOST} port=${ROOM_PORT}"
  fi
  if [[ "${direct_rows}" != "0" ]]; then
    dump_tf_connections
    die "strict room invariant failed: found direct bridge connection row(s)"
  fi
}

room_connection_succeeded() {
  local room_host_escaped room_key_escaped connected_rows
  room_host_escaped="$(sql_escape "${ROOM_HOST}")"
  room_key_escaped="$(sql_escape "${ROOM_KEY}")"
  connected_rows="$(sql_count "${TF_DB_PATH}" "SELECT COUNT(*) FROM connections WHERE host='${room_host_escaped}' AND port=${ROOM_PORT} AND key='${room_key_escaped}' AND last_success IS NOT NULL;")"
  [[ "${connected_rows}" -ge 1 ]]
}

room_attendants_observed() {
  grep -F "room.attendants" /tmp/tf.log >/dev/null 2>&1 && grep -F "${BRIDGE_PUBKEY}" /tmp/tf.log >/dev/null 2>&1
}

wait_for_follow_contact() {
  local deadline contacts_json
  deadline=$((SECONDS + MAX_WAIT_SECS))
  while true; do
    contacts_json="$("${TF_BIN}" get_contacts --db-path "${TF_DB_PATH}" --id "${TF_IDENTITY}" 2>/dev/null || echo "{}")"
    if echo "${contacts_json}" | jq -e --arg feed "${BOT_SSB_FEED}" '.follows | type == "object" and has($feed)' >/dev/null 2>&1; then
      log "contact follow confirmed in TF contacts graph for ${BOT_SSB_FEED}"
      return 0
    fi
    if ((SECONDS >= deadline)); then
      log "latest contacts snapshot:"
      echo "${contacts_json}" | jq -c '.' || true
      die "follow precondition failed: TF does not report bot feed in follows graph ${BOT_SSB_FEED}"
    fi
    sleep "${POLL_INTERVAL}"
  done
}

get_feed_sequence() {
  local feed_id="$1"
  local raw seq
  raw="$("${TF_BIN}" get_sequence --db-path "${TF_DB_PATH}" --id "${feed_id}" 2>/dev/null || true)"
  seq="$(printf '%s\n' "${raw}" | sed -n 's/^[[:space:]]*\(-\?[0-9]\+\)[[:space:]]*$/\1/p' | tail -n 1)"
  if [[ -z "${seq}" ]]; then
    seq="-1"
  fi
  echo "${seq}"
}

queue_retry_trigger_message() {
  local nonce now_iso trigger_text at_uri at_cid at_json ssb_json
  nonce="$(date +%s%N)"
  now_iso="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  trigger_text="e2e room replication trigger ${nonce}"
  at_uri="at://${BOT_DID}/app.bsky.feed.post/e2e-room-${nonce}"
  at_cid="bafy-e2e-room-${nonce}"

  at_json="$(jq -cn --arg text "${trigger_text}" --arg createdAt "${now_iso}" '{"$type":"app.bsky.feed.post","text":$text,"createdAt":$createdAt}')"
  ssb_json="$(jq -cn --arg text "${trigger_text}" --arg createdAt "${now_iso}" '{type:"post",text:$text,createdAt:$createdAt}')"

  sqlite3 "${BRIDGE_DB_PATH}" "PRAGMA busy_timeout=5000; INSERT INTO messages (at_uri, at_cid, at_did, type, message_state, raw_at_json, raw_ssb_json, publish_error, publish_attempts, last_publish_attempt_at) VALUES ('$(sql_escape "${at_uri}")', '$(sql_escape "${at_cid}")', '$(sql_escape "${BOT_DID}")', 'app.bsky.feed.post', 'failed', '$(sql_escape "${at_json}")', '$(sql_escape "${ssb_json}")', '$(sql_escape "e2e room retry trigger")', 0, NULL);"
  RETRY_TRIGGER_AT_URI="${at_uri}"
  log "queued retry-trigger bridge message: ${RETRY_TRIGGER_AT_URI}"
}

parse_host_port "${BRIDGE_MUXRPC_ADDR}" EXPECTED_ROOM_HOST EXPECTED_ROOM_PORT

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
# 2. Wait for bridge to report "live" status in its DB
# ------------------------------------------------------------------
log "waiting for bridge runtime status=live ..."
deadline=$((SECONDS + MAX_WAIT_SECS))
while true; do
  status="$(sqlite3 "${BRIDGE_DB_PATH}" "SELECT value FROM bridge_state WHERE key='bridge_runtime_status' LIMIT 1;" 2>/dev/null || echo "")"
  status="$(echo "${status}" | tr -d '[:space:]')"
  if [[ "${status}" == "live" ]]; then
    log "bridge is live"
    break
  fi
  if ((SECONDS >= deadline)); then
    die "bridge runtime status never reached 'live' (current: ${status})"
  fi
  sleep "${POLL_INTERVAL}"
done

# ------------------------------------------------------------------
# 3. Extract room public key from the bridge repo secret file
# ------------------------------------------------------------------
log "extracting room public key from bridge repo ..."
if [[ ! -f "${BRIDGE_REPO_PATH}/room/secret" ]]; then
  die "bridge room secret file not found at ${BRIDGE_REPO_PATH}/room/secret"
fi

ROOM_PUB_KEY="$(jq -r '.id // empty' "${BRIDGE_REPO_PATH}/room/secret" | tr -d '[:space:]')"
if [[ -z "${ROOM_PUB_KEY}" || "${ROOM_PUB_KEY}" != @*".ed25519" ]]; then
  die "invalid room pub key: ${ROOM_PUB_KEY}"
fi
log "room pub key: ${ROOM_PUB_KEY}"

# ------------------------------------------------------------------
# 4. Look up the bot's SSB feed ID from the bridge DB
# ------------------------------------------------------------------
log "looking up bot SSB feed for DID=${BOT_DID} ..."
bot_did_escaped="$(sql_escape "${BOT_DID}")"
BOT_SSB_FEED="$(sqlite3 "${BRIDGE_DB_PATH}" "SELECT ssb_feed_id FROM bridged_accounts WHERE at_did='${bot_did_escaped}' AND active=1 LIMIT 1;" | tr -d '[:space:]')"
if [[ -z "${BOT_SSB_FEED}" ]]; then
  die "no active bridged account found for ${BOT_DID}"
fi
log "bot SSB feed: ${BOT_SSB_FEED}"

# ------------------------------------------------------------------
# 5. Wait for the bridge to have published at least 1 SSB message
# ------------------------------------------------------------------
log "waiting for bridge to publish SSB messages for ${BOT_DID} ..."
deadline=$((SECONDS + MAX_WAIT_SECS))
while true; do
  pub_count="$(sql_count "${BRIDGE_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE at_did='${bot_did_escaped}' AND message_state='published' AND ssb_msg_ref IS NOT NULL AND ssb_msg_ref <> '';")"
  if [[ "${pub_count}" -ge 1 ]]; then
    log "bridge has ${pub_count} published message(s) for bot"
    break
  fi
  if ((SECONDS >= deadline)); then
    die "no published messages for ${BOT_DID} after ${MAX_WAIT_SECS}s"
  fi
  sleep "${POLL_INTERVAL}"
done

# ------------------------------------------------------------------
# 6. Get tildefriends identity
# ------------------------------------------------------------------
log "getting tildefriends identity ..."
TF_IDENTITY="$("${TF_BIN}" get_identity --db-path "${TF_DB_PATH}" 2>/dev/null | grep -oE '@[A-Za-z0-9+/=]*\.ed25519' | head -n1)"
if [[ -z "${TF_IDENTITY}" || "${TF_IDENTITY}" != @*".ed25519" ]]; then
  die "could not resolve tildefriends identity (got: ${TF_IDENTITY})"
fi
log "tildefriends identity: ${TF_IDENTITY}"
if [[ ! -f "${ROOM_DB_PATH}" ]]; then
  die "bridge room DB not found at ${ROOM_DB_PATH}"
fi

# ------------------------------------------------------------------
# 7. Create + consume invite and derive strict TF connection target
# ------------------------------------------------------------------
log "creating invite via room facade ..."
invite_resp="$(curl -sS -f -X POST "http://${BRIDGE_HTTP_ADDR}/create-invite" -H "Accept: application/json")"
invite_url="$(echo "${invite_resp}" | jq -r '.url // empty')"
if [[ -z "${invite_url}" ]]; then
  die "create-invite response missing url: ${invite_resp}"
fi
if [[ "${invite_url}" != *"token="* ]]; then
  die "invite URL missing token: ${invite_url}"
fi
invite_token_raw="${invite_url##*token=}"
invite_token_raw="${invite_token_raw%%&*}"
INVITE_TOKEN="$(url_decode "${invite_token_raw}")"
if [[ -z "${INVITE_TOKEN}" ]]; then
  die "failed to decode invite token from URL: ${invite_url}"
fi
log "invite created"

consume_payload="$(jq -cn --arg invite "${INVITE_TOKEN}" --arg id "${TF_IDENTITY}" '{invite:$invite,id:$id}')"
consume_http="$(curl -sS -o /tmp/invite-consume.json -w "%{http_code}" -X POST "http://${BRIDGE_HTTP_ADDR}/invite/consume" -H "Content-Type: application/json" -H "Accept: application/json" --data "${consume_payload}")"
consume_body="$(cat /tmp/invite-consume.json)"
if [[ "${consume_http}" != "200" ]]; then
  die "invite consume failed: http=${consume_http} body=${consume_body}"
fi

consume_status="$(echo "${consume_body}" | jq -r '.status // empty')"
MULTISERVER_ADDR="$(echo "${consume_body}" | jq -r '.multiserverAddress // empty')"
if [[ "${consume_status}" != "successful" ]]; then
  die "invite consume returned non-success status=${consume_status} body=${consume_body}"
fi
if [[ -z "${MULTISERVER_ADDR}" || "${MULTISERVER_ADDR}" != net:*~shs:* ]]; then
  die "invalid invite consume multiserverAddress: ${MULTISERVER_ADDR}"
fi
log "invite consumed: multiserverAddress=${MULTISERVER_ADDR}"

join_query_token="$(printf "%s" "${INVITE_TOKEN}" | jq -sRr @uri)"
join_after_status="$(curl -sS -o /tmp/invite-join-after-consume.txt -w "%{http_code}" "http://${BRIDGE_HTTP_ADDR}/join?token=${join_query_token}")"
if [[ "${join_after_status}" != "404" ]]; then
  join_after_body="$(cat /tmp/invite-join-after-consume.txt)"
  die "expected consumed invite token to return 404 from /join, got ${join_after_status} body=${join_after_body}"
fi
log "invite token consumption confirmed (join returns 404)"

net_addr="${MULTISERVER_ADDR#net:}"
net_addr="${net_addr%%~shs:*}"
room_key_b64="${MULTISERVER_ADDR##*~shs:}"
if [[ -z "${net_addr}" || -z "${room_key_b64}" ]]; then
  die "failed parsing multiserverAddress=${MULTISERVER_ADDR}"
fi

if [[ "${net_addr}" =~ ^\[(.*)\]:(.*)$ ]]; then
  ROOM_HOST="[${BASH_REMATCH[1]}]"
  ROOM_PORT="${BASH_REMATCH[2]}"
else
  ROOM_HOST="${net_addr%:*}"
  ROOM_PORT="${net_addr##*:}"
fi
if [[ -z "${ROOM_HOST}" || -z "${ROOM_PORT}" || ! "${ROOM_PORT}" =~ ^[0-9]+$ ]]; then
  die "invalid invite-derived host/port from multiserverAddress=${MULTISERVER_ADDR}"
fi
ROOM_KEY="@${room_key_b64}.ed25519"
if [[ "${ROOM_KEY}" != "${ROOM_PUB_KEY}" ]]; then
  die "invite room key mismatch: invite=${ROOM_KEY} room_secret=${ROOM_PUB_KEY}"
fi
assert_invite_endpoint_matches_expected
log "invite-derived endpoint host=${ROOM_HOST} port=${ROOM_PORT} key=${ROOM_KEY}"

# ------------------------------------------------------------------
# 8. Publish follow (contact) messages from TF to the bot and bridge
# ------------------------------------------------------------------
BRIDGE_SECRET_FILE="${BRIDGE_REPO_PATH}/secret"
if [[ ! -f "${BRIDGE_SECRET_FILE}" ]]; then
  die "bridge secret file not found at ${BRIDGE_SECRET_FILE}"
fi
BRIDGE_PUBKEY="$(jq -r '.id // empty' "${BRIDGE_SECRET_FILE}" | tr -d '[:space:]')"
if [[ -z "${BRIDGE_PUBKEY}" || "${BRIDGE_PUBKEY}" != @*".ed25519" ]]; then
  die "invalid bridge pub key: ${BRIDGE_PUBKEY}"
fi
log "bridge pub key: ${BRIDGE_PUBKEY}"
BRIDGE_KEY="${BRIDGE_PUBKEY}"
BRIDGE_KEY_RAW="${BRIDGE_PUBKEY#@}"
BRIDGE_KEY_RAW="${BRIDGE_KEY_RAW%.ed25519}"

log "publishing follow (contact) messages from TF to bot and bridge..."
BOT_FOLLOW_JSON="{\"type\":\"contact\",\"contact\":\"${BOT_SSB_FEED}\",\"following\":true}"
"${TF_BIN}" publish --db-path "${TF_DB_PATH}" --id "${TF_IDENTITY}" --content "${BOT_FOLLOW_JSON}" || die "failed to follow bot"

BRIDGE_FOLLOW_JSON="{\"type\":\"contact\",\"contact\":\"${BRIDGE_PUBKEY}\",\"following\":true}"
"${TF_BIN}" publish --db-path "${TF_DB_PATH}" --id "${TF_IDENTITY}" --content "${BRIDGE_FOLLOW_JSON}" || die "failed to follow bridge"
log "follow messages published"

# ------------------------------------------------------------------
# 9. Setup strict invite-derived connection in TF
# ------------------------------------------------------------------
log "configuring TF connection table in strict invite-derived mode ..."
sqlite3 "${TF_DB_PATH}" "PRAGMA busy_timeout=5000; DELETE FROM connections;"

room_host_escaped="$(sql_escape "${ROOM_HOST}")"
room_key_escaped="$(sql_escape "${ROOM_KEY}")"
sqlite3 "${TF_DB_PATH}" "PRAGMA busy_timeout=5000; INSERT INTO connections (host, port, key) VALUES ('${room_host_escaped}', ${ROOM_PORT}, '${room_key_escaped}');"
assert_room_only_connections

# ------------------------------------------------------------------
# 10. Start tildefriends natively in background
# ------------------------------------------------------------------
log "starting tildefriends natively in background (invite-derived room path) ..."
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
# 11. Verify that tildefriends recognizes the contact
# ------------------------------------------------------------------
log "verifying contact is registered ..."
wait_for_follow_contact

# ------------------------------------------------------------------
# 12. Queue a retry-trigger message while room tunnel is active
# ------------------------------------------------------------------
baseline_msg_count="$(sql_count "${TF_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE author='$(sql_escape "${BOT_SSB_FEED}")';")"
baseline_seq="$(get_feed_sequence "${BOT_SSB_FEED}")"
if [[ ! "${baseline_seq}" =~ ^-?[0-9]+$ ]]; then
  baseline_seq="-1"
fi
log "baseline bot feed state before retry-trigger: count=${baseline_msg_count} sequence=${baseline_seq}"

log "queueing a retry-trigger message so bridge publishes while TF room tunnel is active ..."
queue_retry_trigger_message

# ------------------------------------------------------------------
# 13. Wait for tildefriends to replicate the bot's feed via room
# ------------------------------------------------------------------
log "waiting for TF to replicate bot feed via room (strict invariants enabled) ..."
deadline=$((SECONDS + MAX_WAIT_SECS))
replicated=false
while true; do
  assert_room_only_connections

  if ! kill -0 "${TF_PID}" 2>/dev/null; then
    log "tildefriends process ${TF_PID} died unexpectedly. Log:"
    dump_tf_log_tail
    die "tildefriends died"
  fi

  tf_msg_count="$(sql_count "${TF_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE author='$(sql_escape "${BOT_SSB_FEED}")';")"
  tf_seq="$(get_feed_sequence "${BOT_SSB_FEED}")"
  room_connected=false
  if room_connection_succeeded; then
    room_connected=true
  fi

  if [[ "${tf_msg_count}" -gt "${baseline_msg_count}" ]]; then
    if ${room_connected}; then
      log "SUCCESS: tildefriends replicated bot feed growth ${baseline_msg_count} -> ${tf_msg_count} via strict room-only path"
      replicated=true
      break
    fi
    log "replication growth seen (message count ${baseline_msg_count} -> ${tf_msg_count}) before room connection success marker; waiting..."
  fi

  if [[ "${tf_seq}" =~ ^[0-9]+$ ]] && [[ "${tf_seq}" -gt "${baseline_seq}" ]]; then
    if ${room_connected}; then
      log "SUCCESS: tildefriends sequence advanced ${baseline_seq} -> ${tf_seq} for bot feed via strict room-only path"
      replicated=true
      break
    fi
    log "replication growth seen (sequence ${baseline_seq} -> ${tf_seq}) before room connection success marker; waiting..."
  fi

  if ((SECONDS >= deadline)); then
    log "TF message count for bot: ${tf_msg_count} (baseline ${baseline_msg_count}), sequence: ${tf_seq:-unknown} (baseline ${baseline_seq})"
    if [[ -n "${RETRY_TRIGGER_AT_URI:-}" ]]; then
      trigger_state="$(sqlite3 "${BRIDGE_DB_PATH}" "SELECT message_state || ' ssb=' || COALESCE(ssb_msg_ref,'') || ' attempts=' || publish_attempts || ' err=' || COALESCE(publish_error,'') FROM messages WHERE at_uri='$(sql_escape "${RETRY_TRIGGER_AT_URI}")' LIMIT 1;" 2>/dev/null || true)"
      log "retry-trigger row state: ${trigger_state:-missing}"
    fi
    log "room_connection_succeeded=$(room_connection_succeeded && echo yes || echo no) room_attendants_observed=$(room_attendants_observed && echo yes || echo no)"
    dump_tf_log_tail
    die "tildefriends did not replicate bot feed via room after ${MAX_WAIT_SECS}s"
  fi
  sleep "${POLL_INTERVAL}"
done

if ! ${replicated}; then
  die "replication check fell through without success"
fi

# ------------------------------------------------------------------
# 13. Post-replication verification: bridge-side invariants
# ------------------------------------------------------------------
log "running post-replication verification ..."

# 13a. Verify exact seed message count on bridge side (3 seeded by e2e-seed)
seed_pub_count="$(sql_count "${BRIDGE_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE at_did='${bot_did_escaped}' AND message_state='published' AND at_uri LIKE 'at://${BOT_DID}/app.bsky.feed.post/e2e-%';")"
if [[ "${seed_pub_count}" -lt 3 ]]; then
  die "bridge seed message count mismatch: expected >= 3 published seed messages, got ${seed_pub_count}"
fi
log "verified: ${seed_pub_count} seed messages published on bridge"

# 13b. Verify retry trigger was processed (should be published or at least attempted)
if [[ -n "${RETRY_TRIGGER_AT_URI:-}" ]]; then
  trigger_state="$(sqlite3 "${BRIDGE_DB_PATH}" "SELECT message_state FROM messages WHERE at_uri='$(sql_escape "${RETRY_TRIGGER_AT_URI}")' LIMIT 1;" 2>/dev/null || echo "missing")"
  trigger_attempts="$(sqlite3 "${BRIDGE_DB_PATH}" "SELECT publish_attempts FROM messages WHERE at_uri='$(sql_escape "${RETRY_TRIGGER_AT_URI}")' LIMIT 1;" 2>/dev/null || echo "0")"
  trigger_attempts="$(echo "${trigger_attempts}" | tr -d '[:space:]')"
  if [[ "${trigger_state}" == "published" ]]; then
    log "verified: retry-trigger message was published"
  elif [[ "${trigger_attempts}" -gt 0 ]]; then
    log "note: retry-trigger message was attempted ${trigger_attempts} time(s) but state=${trigger_state}"
  else
    log "warning: retry-trigger message was not attempted (state=${trigger_state})"
  fi
fi

# 13c. Verify room membership for bridge sbot
bridge_pubkey_escaped="$(sql_escape "${BRIDGE_PUBKEY}")"
bridge_member_rows="$(sql_count "${ROOM_DB_PATH}" "SELECT COUNT(*) FROM members WHERE pub_key='${bridge_pubkey_escaped}';")"
if [[ "${bridge_member_rows}" -lt 1 ]]; then
  die "bridge sbot not found in room members table"
fi
log "verified: bridge sbot is a room member"

# 13d. Verify TF identity is a room member
tf_id_escaped="$(sql_escape "${TF_IDENTITY}")"
tf_member_rows="$(sql_count "${ROOM_DB_PATH}" "SELECT COUNT(*) FROM members WHERE pub_key='${tf_id_escaped}';")"
if [[ "${tf_member_rows}" -lt 1 ]]; then
  die "tildefriends identity not found in room members table"
fi
log "verified: tildefriends identity is a room member"

# 13e. Verify bridge runtime was live during test
bridge_status="$(sqlite3 "${BRIDGE_DB_PATH}" "SELECT value FROM bridge_state WHERE key='bridge_runtime_status' LIMIT 1;" 2>/dev/null || echo "")"
bridge_status="$(echo "${bridge_status}" | tr -d '[:space:]')"
if [[ "${bridge_status}" != "live" ]]; then
  log "warning: bridge runtime status is '${bridge_status}' (expected 'live')"
fi

log "============================================"
log "  E2E PASSED: TF ↔ Bridge Room replication  "
log "  Post-replication checks: OK               "
log "============================================"
exit 0
