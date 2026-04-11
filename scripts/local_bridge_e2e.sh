#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${LOCAL_ATPROTO_ENV_FILE:-/tmp/mvab-local-atproto-live.env}"

"${ROOT_DIR}/scripts/local_atproto_up.sh"
"${ROOT_DIR}/scripts/local_atproto_bootstrap.sh" "${ENV_FILE}"

# Reset any accounts that were marked as host-throttled during initial relay startup
# (relay may have tried to connect before PDS was fully ready)
echo "[local-atproto] resetting relay account statuses..."
docker exec local-atproto-relay_pg-1 psql -U relay -d relay -c \
  "UPDATE account SET status = 'active' WHERE status = 'host-throttled';" \
  >/dev/null 2>&1 || true

if [[ ! -f "${ENV_FILE}" ]]; then
  echo "[local-e2e] env/config file not found: ${ENV_FILE}"
  exit 1
fi

cd "${ROOT_DIR}"
export LIVE_ATPROTO_ENV_FILE="${LIVE_ATPROTO_ENV_FILE:-${ENV_FILE}}"
"${ROOT_DIR}/scripts/live_bridge_e2e.sh"
