#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

if [[ "${LIVE_E2E_ENABLED:-}" != "1" ]]; then
  echo "[live-e2e] LIVE_E2E_ENABLED=1 is required"
  exit 1
fi

export GOCACHE="${GOCACHE:-/tmp/go-build-cache}"

echo "[live-e2e] running live relay + room interoperability test"
go test ./internal/livee2e -run TestBridgeLiveInterop -count=1

echo "[live-e2e] live interoperability test passed"
