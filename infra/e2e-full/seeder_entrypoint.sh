#!/usr/bin/env bash
# Seeder entrypoint: seeds ATProto data for e2e testing
# Phase 1: Create bot account and publish 10 posts
# Phase 2: Publish likes and follows targeting the bot

set -e

PDS_HOST="${PDS_HOST:-http://pds:80}"
BOT_SEED="${BOT_SEED:-e2e-full-seed}"
DB_PATH="${DB_PATH:-/data/bridge.sqlite}"

# Wait for bridge to connect to firehose by checking for firehose_connected in bridge DB
echo "[seeder] Waiting for bridge to connect to firehose..."
MAX_WAIT=60
WAIT_COUNT=0
while [ $WAIT_COUNT -lt $MAX_WAIT ]; do
    FIREHOSE_CONNECTED=$(sqlite3 "$DB_PATH" "SELECT value FROM bridge_state WHERE key = 'firehose_connected';" 2>/dev/null || echo "")
    if [ "$FIREHOSE_CONNECTED" = "1" ]; then
        echo "[seeder] Bridge connected to firehose"
        break
    fi
    echo "[seeder] Waiting for firehose connection... (${WAIT_COUNT}s/${MAX_WAIT}s)"
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
done

if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
    echo "[seeder] Warning: Bridge firehose connection not detected after ${MAX_WAIT}s, proceeding anyway..."
fi

echo "[seeder] Phase 1: Creating bot account with 10 posts..."
atproto-seed \
    --host "$PDS_HOST" \
    --db "$DB_PATH" \
    --bot-seed "$BOT_SEED" \
    --post-count 10

BOT_DID=$(cat /data/atproto-seed-complete 2>/dev/null || echo "")
if [[ -z "$BOT_DID" ]]; then
    echo "[seeder] Error: Failed to get bot DID"
    exit 1
fi

echo "[seeder] Bot DID: $BOT_DID"

# Manually register DID in relay so it starts slurping
echo "[seeder] Registering bot DID in relay..."
PGPASSWORD=relay psql -h relay_pg -U relay -d relay -c "
INSERT INTO account (did, host_id, status)
SELECT '$BOT_DID', id, 'active' FROM host WHERE hostname = 'pds.test'
ON CONFLICT (did) DO NOTHING;"

echo "[seeder] Phase 2: Publishing likes and follows targeting bot..."
atproto-seed \
    --host "$PDS_HOST" \
    --db "$DB_PATH" \
    --bot-seed "${BOT_SEED}-phase2" \
    --target-did "$BOT_DID"

echo "[seeder] Seeding complete!"

# Write completion marker for both phases
echo "complete" > /data/seeder-complete

# Keep container alive for inspection
tail -f /dev/null
