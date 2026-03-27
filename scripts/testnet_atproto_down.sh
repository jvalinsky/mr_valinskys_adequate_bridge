#!/usr/bin/env bash
set -euo pipefail

TESTNET_DIR="${TESTNET_DIR:-/tmp/mvab-testnet}"
TESTNET_PROJECT_NAME="${TESTNET_PROJECT_NAME:-mvab-testnet}"
COMPOSE_FILE="${TESTNET_DIR}/dc/docker-compose.yaml"

if [[ ! -f "${COMPOSE_FILE}" ]]; then
  echo "[testnet-atproto] compose file missing: ${COMPOSE_FILE}" >&2
  exit 0
fi

echo "[testnet-atproto] stopping stack"
docker compose -f "${COMPOSE_FILE}" --project-name "${TESTNET_PROJECT_NAME}" down --remove-orphans
