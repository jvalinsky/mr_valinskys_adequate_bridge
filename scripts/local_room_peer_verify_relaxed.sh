#!/usr/bin/env bash
set -euo pipefail

ROOM_HTTP_ADDR="${LIVE_ROOM_HTTP_ADDR:-127.0.0.1:9876}"
BRIDGE_DB_PATH="${LIVE_BRIDGE_DB_PATH:-}"
SOURCE_DID="${LIVE_BRIDGE_SOURCE_DID:-}"
EXPECTED_URIS="${LIVE_BRIDGE_EXPECTED_URIS:-}"

if [[ -z "${BRIDGE_DB_PATH}" || -z "${SOURCE_DID}" ]]; then
  echo "[local-room-verify-relaxed] missing LIVE_BRIDGE_DB_PATH or LIVE_BRIDGE_SOURCE_DID" >&2
  exit 1
fi

for required in sqlite3 curl; do
  if ! command -v "${required}" >/dev/null 2>&1; then
    echo "[local-room-verify-relaxed] ${required} is required" >&2
    exit 1
  fi
done

HTTP_URL="http://${ROOM_HTTP_ADDR}/healthz"
echo "[local-room-verify-relaxed] checking room HTTP health: ${HTTP_URL}"
curl -sS -f "${HTTP_URL}" >/dev/null

sql_escape() {
  echo "$1" | sed "s/'/''/g"
}

source_did_escaped="$(sql_escape "${SOURCE_DID}")"

if [[ -z "${EXPECTED_URIS}" ]]; then
  published_count="$(sqlite3 "${BRIDGE_DB_PATH}" "SELECT COUNT(*) FROM messages WHERE at_did='${source_did_escaped}' AND message_state='published' AND ssb_msg_ref IS NOT NULL AND ssb_msg_ref <> '';")"
  published_count="$(echo "${published_count}" | tr -d '[:space:]')"
  if [[ -z "${published_count}" || "${published_count}" -lt 1 ]]; then
    echo "[local-room-verify-relaxed] no published messages found for source DID ${SOURCE_DID}" >&2
    exit 1
  fi
  echo "[local-room-verify-relaxed] found ${published_count} published records for source DID ${SOURCE_DID}"
  exit 0
fi

expected_count=0
while IFS= read -r at_uri; do
  [[ -z "${at_uri}" ]] && continue
  expected_count=$((expected_count + 1))
  at_uri_escaped="$(sql_escape "${at_uri}")"

  row="$(sqlite3 "${BRIDGE_DB_PATH}" "SELECT at_did || '|' || message_state || '|' || COALESCE(ssb_msg_ref, '') FROM messages WHERE at_uri='${at_uri_escaped}' LIMIT 1;")"
  if [[ -z "${row}" ]]; then
    echo "[local-room-verify-relaxed] missing message row for ${at_uri}" >&2
    exit 1
  fi

  row_did="${row%%|*}"
  rest="${row#*|}"
  row_state="${rest%%|*}"
  row_ref="${rest#*|}"

  if [[ "${row_did}" != "${SOURCE_DID}" ]]; then
    echo "[local-room-verify-relaxed] source DID mismatch for ${at_uri}: got ${row_did}, want ${SOURCE_DID}" >&2
    exit 1
  fi
  if [[ "${row_state}" != "published" ]]; then
    echo "[local-room-verify-relaxed] message not published for ${at_uri}: state=${row_state}" >&2
    exit 1
  fi
  if [[ -z "${row_ref//[[:space:]]/}" ]]; then
    echo "[local-room-verify-relaxed] missing ssb_msg_ref for ${at_uri}" >&2
    exit 1
  fi
done < <(echo "${EXPECTED_URIS}" | tr ',' '\n' | sed '/^\s*$/d')

if [[ "${expected_count}" -lt 1 ]]; then
  echo "[local-room-verify-relaxed] expected URI list resolved to zero items" >&2
  exit 1
fi

echo "[local-room-verify-relaxed] validated ${expected_count} published bridge records (relaxed mode)"
