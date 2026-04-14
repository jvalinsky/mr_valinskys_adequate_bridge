#!/usr/bin/env bash
# e2e_full_ssbclient_demo.sh — launch the Docker demo where ssb-client joins the bridge room
# and syncs messages from a bridged ATProto bot account.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/infra/e2e-full/docker-compose.yml"
PROJECT_NAME="${E2E_FULL_DEMO_PROJECT_NAME:-mvab-e2e-full-demo}"
KEEP_STACK="${KEEP_E2E:-1}"

log() { echo "[e2e-full-ssbclient-demo] $(date +%H:%M:%S) $*"; }

compose() {
  docker compose -p "${PROJECT_NAME}" -f "${COMPOSE_FILE}" --profile ssbclient-demo "$@"
}

DEMO_SERVICES=(
  bridge-admin-ui
  ssb-client-demo
  demo-runner-ssbclient
)

if ! command -v docker >/dev/null 2>&1; then
  log "docker is required"
  exit 1
fi

if ! docker image inspect blebbit/relay:latest >/dev/null 2>&1; then
  log "blebbit/relay:latest not found; building relay image ..."
  "${ROOT_DIR}/infra/local-atproto/build-relay.sh"
fi

cleanup() {
  if [[ "${KEEP_STACK}" == "1" ]]; then
    log "stack preserved for inspection"
    log "to shut down: docker compose -p ${PROJECT_NAME} -f ${COMPOSE_FILE} down -v --remove-orphans"
    return
  fi
  log "cleaning up stack ..."
  compose down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

log "building and starting ssbclient demo stack ..."
compose up --build -d --timeout 30 "${DEMO_SERVICES[@]}"

set +e
compose wait demo-runner-ssbclient
exit_code=$?
set -e

if [[ "${exit_code}" -ne 0 ]]; then
  log "demo runner failed (exit ${exit_code}); dumping key logs"
  compose logs --tail=200 bridge seeder ssb-client-demo demo-runner-ssbclient || true
  exit "${exit_code}"
fi

log "============================================"
log "  DEMO READY"
log "============================================"
log "bridge admin UI: http://127.0.0.1:8080 (admin / e2e-password)"
log "ssb-client UI:   http://127.0.0.1:8081 (demo / ssbclient-password)"
log "demo verifier logs: docker compose -p ${PROJECT_NAME} -f ${COMPOSE_FILE} logs demo-runner-ssbclient"
