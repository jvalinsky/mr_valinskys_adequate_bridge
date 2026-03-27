#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${TESTNET_ENV_FILE:-/tmp/mvab-testnet-live.env}"
KEEP_RUNNING="${TESTNET_KEEP_RUNNING:-0}"

cleanup() {
  if [[ "${KEEP_RUNNING}" == "1" ]]; then
    echo "[testnet-atproto] keeping testnet stack running"
    return
  fi
  "${ROOT_DIR}/scripts/testnet_atproto_down.sh" || true
}
trap cleanup EXIT

"${ROOT_DIR}/scripts/testnet_atproto_up.sh"
"${ROOT_DIR}/scripts/testnet_atproto_bootstrap.sh" "${ENV_FILE}"

# shellcheck source=/dev/null
set -a
source "${ENV_FILE}"
set +a

cd "${ROOT_DIR}"
"${ROOT_DIR}/scripts/live_bridge_e2e.sh"
