#!/bin/bash
# Debug script to check EBT state and diagnose replication issues
# Run inside the bridge container

set -e

echo "=== EBT Replication Debug Report ==="
echo "Timestamp: $(date -Iseconds)"
echo ""

# Check if we can query the bridge state
DB_PATH="${DB_PATH:-/data/bridge.sqlite}"
REPO_PATH="${REPO_PATH:-/data/ssb-repo}"

echo "=== 1. Bridge Database State ==="
if [ -f "$DB_PATH" ]; then
    echo "Accounts in bridge:"
    sqlite3 "$DB_PATH" "SELECT did, status, created_at FROM bridged_accounts ORDER BY created_at;" 2>/dev/null || echo "  (no accounts table or empty)"
    
    echo ""
    echo "Message counts by status:"
    sqlite3 "$DB_PATH" "SELECT status, COUNT(*) FROM messages GROUP BY status;" 2>/dev/null || echo "  (no messages table)"
    
    echo ""
    echo "Bridge state:"
    sqlite3 "$DB_PATH" "SELECT key, value FROM bridge_state;" 2>/dev/null || echo "  (no bridge_state table)"
else
    echo "  DB not found at $DB_PATH"
fi

echo ""
echo "=== 2. SSB Repo Feeds ==="
if [ -d "$REPO_PATH" ]; then
    echo "SSB repo path: $REPO_PATH"
    
    if [ -f "$REPO_PATH/flume.sqlite" ]; then
        echo "Flume log feeds:"
        sqlite3 "$REPO_PATH/flume.sqlite" "SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'log_%';" 2>/dev/null | head -20 || echo "  (error reading flume)"
        
        echo ""
        echo "Sample messages from first feed:"
        FEED_TABLE=$(sqlite3 "$REPO_PATH/flume.sqlite" "SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'log_%' LIMIT 1;" 2>/dev/null)
        if [ -n "$FEED_TABLE" ]; then
            sqlite3 "$REPO_PATH/flume.sqlite" "SELECT key, author, sequence FROM $FEED_TABLE ORDER BY sequence DESC LIMIT 5;" 2>/dev/null
        fi
    else
        echo "  Flume DB not found"
    fi
else
    echo "  Repo not found at $REPO_PATH"
fi

echo ""
echo "=== 3. EBT State Files ==="
if [ -d "$REPO_PATH/ebt-state" ]; then
    echo "EBT state directory exists"
    ls -la "$REPO_PATH/ebt-state/" 2>/dev/null || echo "  (cannot list)"
else
    echo "  No EBT state directory (will be created on first EBT exchange)"
fi

echo ""
echo "=== 4. Running Process Check ==="
ps aux | grep -E "bridge|ssb" | grep -v grep || echo "No bridge processes found"

echo ""
echo "=== 5. Recent Bridge Logs ==="
# Try to find bridge logs
for logfile in /var/log/bridge.log /tmp/bridge.log /data/bridge.log; do
    if [ -f "$logfile" ]; then
        echo "Logs from $logfile (last 50 lines with EBT/ebt):"
        tail -100 "$logfile" | grep -i "ebt\|replicate\|replication" | tail -50 || echo "  (no EBT-related logs)"
        break
    fi
done

echo ""
echo "=== 6. Network Connections ==="
netstat -tln 2>/dev/null | grep -E "8989|8976|8008" || ss -tln 2>/dev/null | grep -E "8989|8976|8008" || echo "Cannot check network"

echo ""
echo "=== Debug Report Complete ==="
