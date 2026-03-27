#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/infra/local-atproto/docker-compose.yml"

export LOCAL_ATPROTO_DATA_DIR="${LOCAL_ATPROTO_DATA_DIR:-/tmp/mvab-local-atproto}"

echo "[local-atproto] stopping stack"
docker compose -f "${COMPOSE_FILE}" down --remove-orphans
