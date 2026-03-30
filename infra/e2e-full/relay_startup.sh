#!/bin/sh
# Relay startup script: wait for PLC proxy CA cert and install it

echo "[relay-startup] Waiting for PLC proxy CA cert..."
MAX_WAIT=30
WAIT_COUNT=0
while [ $WAIT_COUNT -lt $MAX_WAIT ]; do
    if [ -f /certs/caddy/pki/authorities/local/root.crt ]; then
        echo "[relay-startup] Found CA cert at /certs/caddy/pki/authorities/local/root.crt"
        break
    fi
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
done

if [ -f /certs/caddy/pki/authorities/local/root.crt ]; then
    echo "[relay-startup] Installing CA cert..."
    cp /certs/caddy/pki/authorities/local/root.crt /usr/local/share/ca-certificates/pds.test.crt
    update-ca-certificates 2>/dev/null || echo "[relay-startup] update-ca-certificates failed, trying alternative..."
    
    # If update-ca-certificates failed, try adding to Go's cert pool via env
    export SSL_CERT_FILE=/certs/caddy/pki/authorities/local/root.crt
    export SSL_CERT_DIR=/certs/caddy/pki/authorities/local
    echo "[relay-startup] Set SSL_CERT_FILE=/certs/caddy/pki/authorities/local/root.crt"
else
    echo "[relay-startup] WARNING: CA cert not found after ${MAX_WAIT}s"
fi

echo "[relay-startup] Starting relay..."
exec /relay serve "$@"
