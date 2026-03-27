#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
TESTNET_REPO_URL="${TESTNET_REPO_URL:-https://github.com/verdverm/testnet}"
TESTNET_REF="${TESTNET_REF:-7e862f5}"
TESTNET_DIR="${TESTNET_DIR:-/tmp/mvab-testnet}"
TESTNET_PROJECT_NAME="${TESTNET_PROJECT_NAME:-mvab-testnet}"
COMPOSE_FILE="${TESTNET_DIR}/dc/docker-compose.yaml"

for required in git docker openssl; do
  if ! command -v "${required}" >/dev/null 2>&1; then
    echo "[testnet-atproto] ${required} is required" >&2
    exit 1
  fi
done

if [[ ! -d "${TESTNET_DIR}/.git" ]]; then
  echo "[testnet-atproto] cloning ${TESTNET_REPO_URL} into ${TESTNET_DIR}"
  git clone "${TESTNET_REPO_URL}" "${TESTNET_DIR}"
fi

echo "[testnet-atproto] checking out ${TESTNET_REF}"
git -C "${TESTNET_DIR}" fetch origin --tags --prune
git -C "${TESTNET_DIR}" checkout --detach "${TESTNET_REF}"

rand_hex() {
  openssl rand -hex 32 | tr -d '\n'
}

relay_admin_password="$(rand_hex)"
pds_repo_key="$(rand_hex)"
pds_rot_key="$(rand_hex)"
pds_dpop="$(rand_hex)"
pds_jwt="$(rand_hex)"
pds_admin_password="$(rand_hex)"

cat > "${TESTNET_DIR}/dc/plc.env" <<VARS
DB_URL=postgres://plc:plc@plc_pg:5432/plc
DB_MIGRATE_URL=postgres://plc:plc@plc_pg:5432/plc
DEBUG_MODE=1
LOG_ENABLED=true
LOG_LEVEL=debug
ENABLE_MIGRATIONS=true
LOG_DESTINATION=1
VARS

cat > "${TESTNET_DIR}/dc/relay.env" <<VARS
RELAY_ADMIN_PASSWORD=${relay_admin_password}
RELAY_PLC_HOST=http://plc:3000
DATABASE_URL=postgres://relay:relay@relay_pg:5432/relay?sslmode=disable
RELAY_IP_BIND=:3000
RELAY_PERSIST_DIR=/data
RELAY_DISABLE_REQUEST_CRAWL=1
RELAY_INITIAL_SEQ_NUMBER=1
RELAY_TRUSTED_DOMAINS=
VARS

cat > "${TESTNET_DIR}/dc/jetstream.env" <<'VARS'
JETSTREAM_DATA_DIR=/data
JETSTREAM_LISTEN_ADDR=:7002
JETSTREAM_LIVENESS_TTL=86400s
VARS

cat > "${TESTNET_DIR}/dc/pds.env" <<VARS
PDS_HOSTNAME=localhost
PDS_PORT=3000
PDS_DATA_DIRECTORY=/app/data
PDS_BLOBSTORE_DISK_LOCATION=/app/blobs
PDS_REPO_SIGNING_KEY_K256_PRIVATE_KEY_HEX=${pds_repo_key}
PDS_PLC_ROTATION_KEY_K256_PRIVATE_KEY_HEX=${pds_rot_key}
PDS_DPOP_SECRET=${pds_dpop}
PDS_JWT_SECRET=${pds_jwt}
PDS_ADMIN_PASSWORD=${pds_admin_password}
PDS_DID_PLC_URL=http://plc:3000
PDS_CRAWLERS=http://relay:3000
PDS_SERVICE_HANDLE_DOMAINS=.test
PDS_OAUTH_PROVIDER_NAME=MVAB Local Testnet
PDS_SPICEDB_HOST=spicedb:50051
PDS_SPICEDB_TOKEN=testnet-spicedb
PDS_SPICEDB_INSECURE=1
SPICEDB_DATASTORE_ENGINE=postgres
SPICEDB_DATASTORE_CONN_URI=postgres://spicedb:spicedb@spicedb_pg:5432/spicedb?sslmode=disable
SPICEDB_POSTGRES_HOST=spicedb_pg
SPICEDB_POSTGRES_PORT=5432
SPICEDB_POSTGRES_DB=spicedb
SPICEDB_POSTGRES_USER=spicedb
SPICEDB_POSTGRES_PASSWORD=spicedb
PDS_DEV_MODE=1
NODE_TLS_REJECT_UNAUTHORIZED=0
LOG_ENABLED=0
LOG_LEVEL=info
PDS_INVITE_REQUIRED=0
PDS_DISABLE_SSRF_PROTECTION=0
VARS

if [[ ! -f "${COMPOSE_FILE}" ]]; then
  echo "[testnet-atproto] compose file missing: ${COMPOSE_FILE}" >&2
  exit 1
fi

echo "[testnet-atproto] starting stack via docker compose"
docker compose -f "${COMPOSE_FILE}" --project-name "${TESTNET_PROJECT_NAME}" up -d

"${ROOT_DIR}/scripts/testnet_atproto_wait.sh"
