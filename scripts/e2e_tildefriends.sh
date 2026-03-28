#!/usr/bin/env bash
# e2e_tildefriends.sh — Run the tildefriends↔bridge-room Docker e2e test.
#
# Usage:
#   ./scripts/e2e_tildefriends.sh
#
# Environment:
#   E2E_TF_ENV_FILE=/path/file.env  — optional env/config file to source
#   E2E_TF_NO_CACHE=1               — pass --no-cache to docker compose build
#   E2E_TF_KEEP=1                   — skip cleanup on exit (debugging)
#   E2E_TF_SCENARIO=positive|broken-room
#   E2E_TF_EXPECT=pass|fail
#   E2E_TF_EXPOSE_PORTS=0|1         — include debug port override when set to 1
#   E2E_TF_BRIDGE_MUXRPC_ADDR=host:port
#   E2E_TF_BRIDGE_HTTP_ADDR=host:port
#   E2E_TF_FIREHOSE_MODE=off|external
#   E2E_TF_RELAY_URL=ws://host:port/xrpc/com.atproto.sync.subscribeRepos (required when mode=external)
#   E2E_TF_MAX_WAIT_SECS=<seconds>
#   E2E_TF_POLL_INTERVAL=<seconds>
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/infra/e2e-tildefriends/docker-compose.e2e-tildefriends.yml"
COMPOSE_PORTS_FILE="${ROOT_DIR}/infra/e2e-tildefriends/docker-compose.e2e-tildefriends.debug-ports.yml"
PROJECT_NAME="mvab-e2e-tf"
SCENARIO="${E2E_TF_SCENARIO:-positive}"
EXPECT="${E2E_TF_EXPECT:-}"
EXPOSE_PORTS="${E2E_TF_EXPOSE_PORTS:-0}"
FIREHOSE_MODE="${E2E_TF_FIREHOSE_MODE:-off}"
RELAY_URL="${E2E_TF_RELAY_URL:-${E2E_TF_BRIDGE_RELAY_URL:-}}"
ENV_FILE="${E2E_TF_ENV_FILE:-}"

log() { echo "[e2e-tf] $(date +%H:%M:%S) $*"; }
die() { log "FAIL: $*" >&2; exit 1; }

if [[ -n "${ENV_FILE}" ]]; then
  if [[ ! -f "${ENV_FILE}" ]]; then
    die "env/config file not found: ${ENV_FILE}"
  fi
  set -a
  # shellcheck source=/dev/null
  source "${ENV_FILE}"
  set +a
fi

case "${SCENARIO}" in
  positive|broken-room)
    ;;
  *)
    die "unsupported scenario: ${SCENARIO} (expected positive or broken-room)"
    ;;
esac

if [[ -z "${EXPECT}" ]]; then
  if [[ "${SCENARIO}" == "broken-room" ]]; then
    EXPECT="fail"
  else
    EXPECT="pass"
  fi
fi
case "${EXPECT}" in
  pass|fail)
    ;;
  *)
    die "unsupported expectation: ${EXPECT} (expected pass or fail)"
    ;;
esac

case "${EXPOSE_PORTS}" in
  0|1)
    ;;
  *)
    die "E2E_TF_EXPOSE_PORTS must be 0 or 1 (got ${EXPOSE_PORTS})"
    ;;
esac

case "${FIREHOSE_MODE}" in
  off|external)
    ;;
  *)
    die "unsupported E2E_TF_FIREHOSE_MODE: ${FIREHOSE_MODE} (expected off or external)"
    ;;
esac

if [[ "${FIREHOSE_MODE}" == "external" ]]; then
  if [[ -z "${RELAY_URL}" ]]; then
    die "E2E_TF_RELAY_URL is required when E2E_TF_FIREHOSE_MODE=external"
  fi
  export E2E_TF_BRIDGE_FIREHOSE_ENABLE="1"
  export E2E_TF_BRIDGE_RELAY_URL="${RELAY_URL}"
else
  export E2E_TF_BRIDGE_FIREHOSE_ENABLE="0"
  export E2E_TF_BRIDGE_RELAY_URL=""
fi

if [[ "${SCENARIO}" == "broken-room" ]]; then
  export E2E_TF_BRIDGE_MUXRPC_ADDR="${E2E_TF_BRIDGE_MUXRPC_ADDR:-bridge:39999}"
fi
export E2E_TF_BRIDGE_HTTP_ADDR="${E2E_TF_BRIDGE_HTTP_ADDR:-bridge:8976}"

compose_args=(
  -p "${PROJECT_NAME}"
  -f "${COMPOSE_FILE}"
)
if [[ "${EXPOSE_PORTS}" == "1" ]]; then
  compose_args+=(-f "${COMPOSE_PORTS_FILE}")
fi

compose() {
  docker compose "${compose_args[@]}" "$@"
}

# shellcheck disable=SC2329
cleanup() {
  if [[ "${E2E_TF_KEEP:-}" == "1" ]]; then
    log "E2E_TF_KEEP=1 — skipping cleanup"
    return
  fi
  log "cleaning up ..."
  compose down -v --remove-orphans 2>/dev/null || true
}
trap cleanup EXIT

dump_logs() {
  log "dumping container logs ..."
  compose logs --tail=200 bridge test-runner 2>/dev/null || true
}

log "scenario=${SCENARIO} expect=${EXPECT} expose_ports=${EXPOSE_PORTS} firehose_mode=${FIREHOSE_MODE}"

# ------------------------------------------------------------------
# Build
# ------------------------------------------------------------------
build_args=()
if [[ "${E2E_TF_NO_CACHE:-}" == "1" ]]; then
  build_args+=(--no-cache)
fi

log "initializing submodules for tildefriends build ..."
git -C "${ROOT_DIR}/reference/tildefriends" submodule update --init --recursive

log "building Docker images ..."
if [ ${#build_args[@]} -eq 0 ]; then
  compose build
else
  compose build "${build_args[@]}"
fi

# ------------------------------------------------------------------
# Run
# ------------------------------------------------------------------
log "starting e2e test ..."
set +e
compose up \
  --abort-on-container-exit \
  --exit-code-from test-runner \
  --timeout 30
exit_code=$?
set -e

if [[ "${exit_code}" -ne 0 ]]; then
  dump_logs
fi

result="fail"
if [[ "${EXPECT}" == "pass" && "${exit_code}" -eq 0 ]]; then
  result="pass"
elif [[ "${EXPECT}" == "fail" && "${exit_code}" -ne 0 ]]; then
  result="pass"
fi

if [[ "${result}" == "pass" ]]; then
  log "============================================"
  log "  E2E PASSED: scenario=${SCENARIO} expect=${EXPECT} compose_exit=${exit_code}"
  log "============================================"
  exit 0
fi

log "============================================"
log "  E2E FAILED: scenario=${SCENARIO} expect=${EXPECT} compose_exit=${exit_code}"
log "============================================"
if [[ "${exit_code}" -eq 0 ]]; then
  dump_logs
fi
exit 1
