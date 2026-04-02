#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

export GOCACHE="${GOCACHE:-/tmp/mvab-go-build-cache}"
if [[ "${GOFLAGS:-}" != *"-mod="* ]]; then
  export GOFLAGS="-mod=mod ${GOFLAGS:-}"
fi

DEFAULT_DB_PATH="${ROOT_DIR}/bridge-live.sqlite"
DEFAULT_REPO_PATH="${ROOT_DIR}/.ssb-bridge-live"
DEFAULT_RELAY_URL="wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos"
DEFAULT_SSB_LISTEN_ADDR=":8008"
DEFAULT_ROOM_LISTEN_ADDR="127.0.0.1:8989"
DEFAULT_ROOM_HTTP_LISTEN_ADDR="127.0.0.1:8976"
DEFAULT_ROOM_MODE="community"
DEFAULT_UI_LISTEN_ADDR="127.0.0.1:8080"

usage() {
  cat <<'EOF'
Usage:
  scripts/setup_live_bridge.sh <command> [options]

Commands:
  setup       Add all DIDs from a file into the bridge DB
  backfill    Backfill all active bridged accounts
  start       Start the live bridge and embedded room
  status      Show configured accounts and bridge stats
  serve-ui    Serve the admin UI

Common options:
  --db PATH                 SQLite DB path
  --repo PATH               Shared SSB repo path
  --bot-seed SEED           Stable seed for deterministic SSB feed IDs
  --relay-url URL           Firehose relay URL

Setup options:
  --dids PATH               File containing one DID per line
  --allow-empty             Allow setup to proceed when no DIDs are found

Backfill options:
  --since VALUE             Optional backfill since value
  --publish-workers N       Publish worker count
  --xrpc-host URL           Optional fixed PDS host override for sync.getRepo

Start options:
  --publish-workers N       Publish worker count
  --xrpc-host URL           Optional read-host override for dependency/blob fetches
  --ssb-listen-addr ADDR    SSB listen address
  --room-listen-addr ADDR   Room muxrpc listen address
  --room-http-addr ADDR     Room HTTP listen address
  --room-mode MODE          open|community|restricted
  --room-https-domain NAME  Required when exposing room off-loopback
  --no-room                 Disable embedded room

Serve UI options:
  --listen-addr ADDR        UI listen address
  --ui-auth-user USER       HTTP basic auth user
  --ui-auth-pass-env ENV    Env var containing HTTP basic auth password

Environment:
  BRIDGE_BOT_SEED may be used instead of --bot-seed.

Examples:
  scripts/setup_live_bridge.sh setup --dids dids.txt --bot-seed 'secret-seed'
  scripts/setup_live_bridge.sh backfill --bot-seed 'secret-seed'
  scripts/setup_live_bridge.sh start --bot-seed 'secret-seed'
  scripts/setup_live_bridge.sh status
EOF
}

fail() {
  echo "[setup-live-bridge] $*" >&2
  exit 1
}

log() {
  echo "[setup-live-bridge] $*"
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

require_file() {
  local path="$1"
  [[ -f "$path" ]] || fail "file not found: $path"
}

resolve_path() {
  local path="$1"
  if [[ "$path" = /* ]]; then
    printf '%s\n' "$path"
  else
    printf '%s\n' "${ROOT_DIR}/${path}"
  fi
}

BOT_SEED="${BRIDGE_BOT_SEED:-}"
DB_PATH="${DEFAULT_DB_PATH}"
REPO_PATH="${DEFAULT_REPO_PATH}"
RELAY_URL="${DEFAULT_RELAY_URL}"

COMMAND="${1:-}"
if [[ -z "${COMMAND}" ]]; then
  usage
  exit 1
fi
shift

DIDS_FILE=""
ALLOW_EMPTY=0
SINCE_VALUE=""
PUBLISH_WORKERS="1"
XRPC_HOST=""
SSB_LISTEN_ADDR="${DEFAULT_SSB_LISTEN_ADDR}"
ROOM_LISTEN_ADDR="${DEFAULT_ROOM_LISTEN_ADDR}"
ROOM_HTTP_ADDR="${DEFAULT_ROOM_HTTP_LISTEN_ADDR}"
ROOM_MODE="${DEFAULT_ROOM_MODE}"
ROOM_HTTPS_DOMAIN=""
ROOM_ENABLE=1
UI_LISTEN_ADDR="${DEFAULT_UI_LISTEN_ADDR}"
UI_AUTH_USER=""
UI_AUTH_PASS_ENV=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --db)
      [[ $# -ge 2 ]] || fail "--db requires a value"
      DB_PATH="$(resolve_path "$2")"
      shift 2
      ;;
    --repo)
      [[ $# -ge 2 ]] || fail "--repo requires a value"
      REPO_PATH="$(resolve_path "$2")"
      shift 2
      ;;
    --bot-seed)
      [[ $# -ge 2 ]] || fail "--bot-seed requires a value"
      BOT_SEED="$2"
      shift 2
      ;;
    --relay-url)
      [[ $# -ge 2 ]] || fail "--relay-url requires a value"
      RELAY_URL="$2"
      shift 2
      ;;
    --dids)
      [[ $# -ge 2 ]] || fail "--dids requires a value"
      DIDS_FILE="$(resolve_path "$2")"
      shift 2
      ;;
    --allow-empty)
      ALLOW_EMPTY=1
      shift
      ;;
    --since)
      [[ $# -ge 2 ]] || fail "--since requires a value"
      SINCE_VALUE="$2"
      shift 2
      ;;
    --publish-workers)
      [[ $# -ge 2 ]] || fail "--publish-workers requires a value"
      PUBLISH_WORKERS="$2"
      shift 2
      ;;
    --xrpc-host)
      [[ $# -ge 2 ]] || fail "--xrpc-host requires a value"
      XRPC_HOST="$2"
      shift 2
      ;;
    --ssb-listen-addr)
      [[ $# -ge 2 ]] || fail "--ssb-listen-addr requires a value"
      SSB_LISTEN_ADDR="$2"
      shift 2
      ;;
    --room-listen-addr)
      [[ $# -ge 2 ]] || fail "--room-listen-addr requires a value"
      ROOM_LISTEN_ADDR="$2"
      shift 2
      ;;
    --room-http-addr)
      [[ $# -ge 2 ]] || fail "--room-http-addr requires a value"
      ROOM_HTTP_ADDR="$2"
      shift 2
      ;;
    --room-mode)
      [[ $# -ge 2 ]] || fail "--room-mode requires a value"
      ROOM_MODE="$2"
      shift 2
      ;;
    --room-https-domain)
      [[ $# -ge 2 ]] || fail "--room-https-domain requires a value"
      ROOM_HTTPS_DOMAIN="$2"
      shift 2
      ;;
    --no-room)
      ROOM_ENABLE=0
      shift
      ;;
    --listen-addr)
      [[ $# -ge 2 ]] || fail "--listen-addr requires a value"
      UI_LISTEN_ADDR="$2"
      shift 2
      ;;
    --ui-auth-user)
      [[ $# -ge 2 ]] || fail "--ui-auth-user requires a value"
      UI_AUTH_USER="$2"
      shift 2
      ;;
    --ui-auth-pass-env)
      [[ $# -ge 2 ]] || fail "--ui-auth-pass-env requires a value"
      UI_AUTH_PASS_ENV="$2"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

require_cmd go

bridge_cli() {
  go run ./cmd/bridge-cli "$@"
}

require_seed() {
  [[ -n "${BOT_SEED}" ]] || fail "missing bot seed; pass --bot-seed or set BRIDGE_BOT_SEED"
}

collect_dids() {
  local path="$1"
  require_file "$path"

  local raw line trimmed existing
  DID_VALUES=()
  while IFS= read -r raw || [[ -n "$raw" ]]; do
    line="${raw%%#*}"
    trimmed="$(printf '%s' "$line" | tr -d '\r' | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')"
    [[ -n "$trimmed" ]] || continue
    [[ "$trimmed" == did:plc:* ]] || fail "invalid DID entry in ${path}: ${trimmed}"
    existing=0
    for did in "${DID_VALUES[@]:-}"; do
      if [[ "$did" == "$trimmed" ]]; then
        existing=1
        break
      fi
    done
    if [[ "$existing" == "0" ]]; then
      DID_VALUES+=("$trimmed")
    fi
  done < "$path"
}

run_setup() {
  require_seed
  [[ -n "${DIDS_FILE}" ]] || fail "setup requires --dids PATH"

  collect_dids "${DIDS_FILE}"
  if [[ ${#DID_VALUES[@]} -eq 0 && "${ALLOW_EMPTY}" != "1" ]]; then
    fail "no DIDs found in ${DIDS_FILE}; use --allow-empty to bypass"
  fi

  mkdir -p "$(dirname "$DB_PATH")" "$REPO_PATH"
  log "db=${DB_PATH}"
  log "repo=${REPO_PATH}"
  log "relay=${RELAY_URL}"
  log "loading ${#DID_VALUES[@]} DID(s) from ${DIDS_FILE}"

  local did
  for did in "${DID_VALUES[@]}"; do
    log "ensuring account ${did}"
    bridge_cli --db "$DB_PATH" --bot-seed "$BOT_SEED" account add "$did"
  done

  log "setup complete"
  bridge_cli --db "$DB_PATH" account list
}

run_backfill() {
  require_seed
  mkdir -p "$(dirname "$DB_PATH")" "$REPO_PATH"

  local args=(
    --db "$DB_PATH"
    --relay-url "$RELAY_URL"
    --bot-seed "$BOT_SEED"
    backfill
    --repo-path "$REPO_PATH"
    --active-accounts
    --publish-workers "$PUBLISH_WORKERS"
  )
  if [[ -n "${SINCE_VALUE}" ]]; then
    args+=(--since "$SINCE_VALUE")
  fi
  if [[ -n "${XRPC_HOST}" ]]; then
    args+=(--xrpc-host "$XRPC_HOST")
  fi

  log "starting backfill for active accounts"
  bridge_cli "${args[@]}"
}

run_start() {
  require_seed
  mkdir -p "$(dirname "$DB_PATH")" "$REPO_PATH"

  local args=(
    --db "$DB_PATH"
    --relay-url "$RELAY_URL"
    --bot-seed "$BOT_SEED"
    start
    --repo-path "$REPO_PATH"
    --publish-workers "$PUBLISH_WORKERS"
    --ssb-listen-addr "$SSB_LISTEN_ADDR"
  )
  if [[ "${ROOM_ENABLE}" == "1" ]]; then
    args+=(
      --room-enable
      --room-listen-addr "$ROOM_LISTEN_ADDR"
      --room-http-listen-addr "$ROOM_HTTP_ADDR"
      --room-mode "$ROOM_MODE"
    )
    if [[ -n "${ROOM_HTTPS_DOMAIN}" ]]; then
      args+=(--room-https-domain "$ROOM_HTTPS_DOMAIN")
    fi
  else
    args+=(--room-enable=false)
  fi
  if [[ -n "${XRPC_HOST}" ]]; then
    args+=(--xrpc-host "$XRPC_HOST")
  fi

  log "starting live bridge"
  bridge_cli "${args[@]}"
}

run_status() {
  log "accounts"
  bridge_cli --db "$DB_PATH" account list
  log "stats"
  bridge_cli --db "$DB_PATH" stats
}

run_serve_ui() {
  local args=(
    --db "$DB_PATH"
    serve-ui
    --listen-addr "$UI_LISTEN_ADDR"
  )
  if [[ -n "${UI_AUTH_USER}" || -n "${UI_AUTH_PASS_ENV}" ]]; then
    [[ -n "${UI_AUTH_USER}" ]] || fail "--ui-auth-user is required when --ui-auth-pass-env is set"
    [[ -n "${UI_AUTH_PASS_ENV}" ]] || fail "--ui-auth-pass-env is required when --ui-auth-user is set"
    args+=(--ui-auth-user "$UI_AUTH_USER" --ui-auth-pass-env "$UI_AUTH_PASS_ENV")
  fi

  log "serving admin UI on ${UI_LISTEN_ADDR}"
  bridge_cli "${args[@]}"
}

case "${COMMAND}" in
  setup)
    run_setup
    ;;
  backfill)
    run_backfill
    ;;
  start)
    run_start
    ;;
  status)
    run_status
    ;;
  serve-ui)
    run_serve_ui
    ;;
  --help|-h|help)
    usage
    ;;
  *)
    fail "unknown command: ${COMMAND}"
    ;;
esac
