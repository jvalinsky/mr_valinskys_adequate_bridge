#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

export GOCACHE="${GOCACHE:-/tmp/go-build-cache}"

echo "[smoke] running deterministic bridge smoke test"
go test ./internal/smoke -run TestBridgeSmoke -count=1

echo "[smoke] bridge smoke test passed"
