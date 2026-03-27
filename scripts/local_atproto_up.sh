#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/infra/local-atproto/docker-compose.yml"

if ! command -v docker >/dev/null 2>&1; then
  echo "[local-atproto] docker is required" >&2
  exit 1
fi

BUILD_FLAG=()
if [[ "${1:-}" == "--build" ]]; then
  BUILD_FLAG+=(--build)
fi

export LOCAL_ATPROTO_DATA_DIR="${LOCAL_ATPROTO_DATA_DIR:-/tmp/mvab-local-atproto}"
mkdir -p "${LOCAL_ATPROTO_DATA_DIR}"

echo "[local-atproto] starting stack via docker compose"
if (( ${#BUILD_FLAG[@]} > 0 )); then
  docker compose -f "${COMPOSE_FILE}" up -d "${BUILD_FLAG[@]}"
else
  docker compose -f "${COMPOSE_FILE}" up -d
fi

"${ROOT_DIR}/scripts/local_atproto_wait.sh"
