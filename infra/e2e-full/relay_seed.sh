#!/bin/sh
set -e
until psql -h relay_pg -U relay -d relay -c "SELECT 1 FROM host LIMIT 1" >/dev/null 2>&1; do
  echo "waiting for relay host table..."
  sleep 2
done
echo "Relay host table found. Seeding PDS hosts..."
# Register internal 'pds' name for crawl requests
psql -h relay_pg -U relay -d relay -c "
INSERT INTO host (hostname, no_ssl, status, account_limit, trusted, last_seq, account_count, created_at, updated_at)
VALUES ('pds', true, 'active', 1000, true, 0, 0, NOW(), NOW())
ON CONFLICT (hostname) DO UPDATE SET status = 'active', no_ssl = true, last_seq = 0, updated_at = NOW();"

# Register external-style 'pds.test' for handles
psql -h relay_pg -U relay -d relay -c "
INSERT INTO host (hostname, no_ssl, status, account_limit, trusted, last_seq, account_count, created_at, updated_at)
VALUES ('pds.test', false, 'active', 1000, true, 0, 0, NOW(), NOW())
ON CONFLICT (hostname) DO UPDATE SET status = 'active', no_ssl = false, last_seq = 0, updated_at = NOW();"
echo "Relay seeding complete."
