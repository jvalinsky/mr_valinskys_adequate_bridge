#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${LOCAL_ATPROTO_ENV_FILE:-/tmp/mvab-local-atproto-live.env}"

"${ROOT_DIR}/scripts/local_atproto_up.sh"
"${ROOT_DIR}/scripts/local_atproto_bootstrap.sh" "${ENV_FILE}"

# shellcheck source=/dev/null
set -a
source "${ENV_FILE}"
set +a

cd "${ROOT_DIR}"
"${ROOT_DIR}/scripts/live_bridge_e2e.sh"
