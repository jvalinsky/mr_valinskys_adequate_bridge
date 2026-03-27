#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

ENV_FILE="${LIVE_ATPROTO_ENV_FILE:-${LIVE_ATPROTO_CONFIG_FILE:-}}"
if [[ -n "${ENV_FILE}" ]]; then
  if [[ ! -f "${ENV_FILE}" ]]; then
    echo "[live-e2e] env/config file not found: ${ENV_FILE}"
    exit 1
  fi
  # shellcheck source=/dev/null
  set -a
  source "${ENV_FILE}"
  set +a
  export LIVE_ATPROTO_CONFIG_FILE="${LIVE_ATPROTO_CONFIG_FILE:-${ENV_FILE}}"
fi

if [[ "${LIVE_E2E_ENABLED:-}" != "1" ]]; then
  echo "[live-e2e] LIVE_E2E_ENABLED=1 is required"
  exit 1
fi

export GOCACHE="${GOCACHE:-/tmp/go-build-cache}"

echo "[live-e2e] running live relay + room interoperability test"
go test ./internal/livee2e -run TestBridgeLiveInterop -count=1

echo "[live-e2e] live interoperability test passed"
