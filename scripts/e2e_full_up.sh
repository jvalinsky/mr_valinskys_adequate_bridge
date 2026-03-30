#!/usr/bin/env bash
# e2e_full_up.sh — Run the full-stack E2E test (PLC, Relay, PDS, Bridge, Tildefriends).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/infra/e2e-full/docker-compose.yml"
PROJECT_NAME="mvab-e2e-full"

log() { echo "[e2e-full-up] $(date +%H:%M:%S) $*"; }

# Cleanup on exit if KEEP_E2E is not 1 OR if it failed (we want to stay alive on success)
STAY_ALIVE_ON_SUCCESS="${KEEP_E2E:-0}"

cleanup() {
  if [[ "${STAY_ALIVE_ON_SUCCESS}" == "1" ]]; then
    log "Environment preserved for inspection."
    log "To shut down: docker compose -p ${PROJECT_NAME} -f ${COMPOSE_FILE} down -v"
    return
  fi
  log "Cleaning up stack..."
  docker compose -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" down -v --remove-orphans >/dev/null 2>&1 || true
}

trap cleanup EXIT

log "building and starting e2e-full stack..."
docker compose -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" up \
  --build \
  --exit-code-from test-runner \
  --timeout 30

exit_code=$?

if [[ "${exit_code}" -eq 0 ]]; then
  log "============================================"
  log "  E2E SUCCESS!"
  log "============================================"
else
  log "============================================"
  log "  E2E FAILED (exit ${exit_code})"
  log "============================================"
  docker compose -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" logs
fi

exit "${exit_code}"
