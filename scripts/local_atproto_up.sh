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
mkdir -p "${LOCAL_ATPROTO_DATA_DIR}/cocoon-keys"

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
   VALUES ('${LOCAL_ATPROTO_PDS_SERVICE_HOST}', true, 'active', 10000, true, 0, 0, NOW(), NOW())
   ON CONFLICT (hostname) DO UPDATE SET status = 'active', no_ssl = true, account_limit = 10000, last_seq = 0, updated_at = NOW();" \
  >/dev/null

# Also reset any accounts that were marked as host-throttled during previous failed connections
docker exec local-atproto-relay_pg-1 psql -U relay -d relay -c \
  "UPDATE account SET status = 'active' WHERE status = 'host-throttled';" \
  >/dev/null

# Wait for pds-proxy to be fully ready (not just healthy, but serving requests on port 80)
echo "[local-atproto] waiting for pds-proxy to be ready..."
for i in {1..30}; do
  if docker exec local-atproto-relay-1 wget -q -O /dev/null "http://${LOCAL_ATPROTO_PDS_SERVICE_HOST}/" 2>/dev/null; then
    echo "[local-atproto] pds-proxy ready"
    break
  fi
  sleep 1
done

# Now restart relay to pick up the PDS connection with fresh DNS
docker restart local-atproto-relay-1 >/dev/null 2>&1
"${ROOT_DIR}/scripts/local_atproto_wait.sh"

# Wait for slurper to connect to PDS (check for "persisting cursors" logs AFTER restart)
echo "[local-atproto] waiting for relay slurper to connect to PDS..."
sleep 3  # Give slurper time to start connection attempts
for i in {1..30}; do
  if docker logs local-atproto-relay-1 --since 10s 2>&1 | grep -q "finished persisting cursors"; then
    echo "[local-atproto] relay connected to PDS"
    break
  fi
  # Also check for dialing success (connected to correct IP)
  if docker logs local-atproto-relay-1 --since 10s 2>&1 | grep -qE 'dial tcp 192\.168\.|dial tcp 10\.'; then
    echo "[local-atproto] relay dialing PDS (may be connecting...)"
  fi
  sleep 1
done
