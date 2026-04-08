#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${LOCAL_ATPROTO_ENV_FILE:-/tmp/mvab-local-atproto-live.env}"

"${ROOT_DIR}/scripts/local_atproto_up.sh"
"${ROOT_DIR}/scripts/local_atproto_bootstrap.sh" "${ENV_FILE}"

if [[ ! -f "${ENV_FILE}" ]]; then
  echo "[local-reverse-e2e] env/config file not found: ${ENV_FILE}"
  exit 1
fi

cd "${ROOT_DIR}"
export LIVE_ATPROTO_ENV_FILE="${LIVE_ATPROTO_ENV_FILE:-${ENV_FILE}}"
"${ROOT_DIR}/scripts/live_reverse_ssb_client_e2e.sh"
