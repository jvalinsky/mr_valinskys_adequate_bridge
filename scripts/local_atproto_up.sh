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

"${ROOT_DIR}/infra/local-atproto/build-relay.sh"

echo "[local-atproto] starting stack via docker compose"
if (( ${#BUILD_FLAG[@]} > 0 )); then
  docker compose -f "${COMPOSE_FILE}" up -d "${BUILD_FLAG[@]}"
else
  docker compose -f "${COMPOSE_FILE}" up -d
fi

"${ROOT_DIR}/scripts/local_atproto_wait.sh"

# Seed the relay database with the PDS host.  The relay's SSRF protection
# blocks Docker-internal addresses via requestCrawl, so we insert the host
# record directly and restart the relay so it picks up the subscription.
# We always reset last_seq to 0 because the PDS data directory may have been
# wiped between runs while the relay's PostgreSQL volume persists, which would
# leave last_seq pointing past the PDS's current sequence ("future cursor").
LOCAL_ATPROTO_PDS_SERVICE_HOST="${LOCAL_ATPROTO_PDS_SERVICE_HOST:-pds.test}"
echo "[local-atproto] seeding relay with PDS host (${LOCAL_ATPROTO_PDS_SERVICE_HOST})"
docker exec local-atproto-relay_pg-1 psql -U relay -d relay -c \
  "INSERT INTO host (hostname, no_ssl, status, account_limit, trusted, last_seq, account_count, created_at, updated_at)
   VALUES ('${LOCAL_ATPROTO_PDS_SERVICE_HOST}', true, 'active', 1000, true, 0, 0, NOW(), NOW())
   ON CONFLICT (hostname) DO UPDATE SET status = 'active', no_ssl = true, last_seq = 0, updated_at = NOW();" \
  >/dev/null
docker restart local-atproto-relay-1 >/dev/null 2>&1
"${ROOT_DIR}/scripts/local_atproto_wait.sh"
