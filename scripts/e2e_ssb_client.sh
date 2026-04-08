#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/infra/e2e-full/docker-compose.yml"
PROJECT_NAME="mvab-e2e-ssb-client"

log() { echo "[e2e-ssb-client] $(date +%H:%M:%S) $*"; }

STAY_ALIVE_ON_SUCCESS="${KEEP_E2E:-0}"

cleanup() {
  if [[ "${STAY_ALIVE_ON_SUCCESS}" == "1" ]]; then
    log "Environment preserved for inspection."
    log "To shut down: docker compose -p ${PROJECT_NAME} -f ${COMPOSE_FILE} --profile ssbclient-e2e down -v"
    return
  fi
  log "Cleaning up stack..."
  docker compose -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" --profile ssbclient-e2e down -v --remove-orphans >/dev/null 2>&1 || true
}

trap cleanup EXIT

log "building and starting repo ssb-client e2e stack..."
docker compose -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" --profile ssbclient-e2e up \
  --build \
  -d \
  --timeout 30

set +e
docker compose -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" wait test-runner-ssbclient
exit_code=$?
set -e

if [[ "${exit_code}" -eq 0 ]]; then
  log "============================================"
  log "  E2E SUCCESS!"
  log "============================================"
else
  log "============================================"
  log "  E2E FAILED (exit ${exit_code})"
  log "============================================"
  docker compose -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" --profile ssbclient-e2e logs
fi

exit "${exit_code}"
