#!/bin/bash
# Debug script to capture muxrpc wire traffic between Tildefriends and bridge
# Usage: ./scripts/debug_muxrpc_capture.sh <bridge-host> <bridge-port>

set -e

BRIDGE_HOST="${1:-localhost}"
BRIDGE_PORT="${2:-8989}"

echo "=== MUXRPC Wire Capture ==="
echo "Target: $BRIDGE_HOST:$BRIDGE_PORT"
echo "Using socat to capture raw traffic..."
echo ""

# Create a temporary file for the capture
CAPTURE_FILE="/tmp/muxrpc_capture_$(date +%s).hex"

# Method 1: Using socat to capture hex dump
# Note: This will capture encrypted secret-stream traffic
echo "Starting capture (Ctrl+C to stop)..."
socat - TCP:"$BRIDGE_HOST:$BRIDGE_PORT" | hexdump -C > "$CAPTURE_FILE" &

SOCAT_PID=$!
echo "Capture started, PID: $SOCAT_PID"
echo "Output file: $CAPTURE_FILE"

# Give it a moment
sleep 1

# Send a simple whoami probe
echo "Sending whoami probe..."
echo -n -e '\x00\x00\x00\x27{"name":["whoami"],"type":"async","body":{}}\x00' | socat - TCP:"$BRIDGE_HOST:$BRIDGE_PORT"

sleep 2

# Kill the background socat
kill $SOCAT_PID 2>/dev/null || true

echo ""
echo "=== Capture Complete ==="
echo "Review with: cat $CAPTURE_FILE | head -100"
