#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

export LIVE_E2E_TEST_PATTERN="${LIVE_E2E_TEST_PATTERN:-TestBridgeLiveInteropReverseSSBClient}"
export LIVE_E2E_LABEL="${LIVE_E2E_LABEL:-live reverse interoperability test (repo ssb-client)}"

"${ROOT_DIR}/scripts/live_bridge_e2e.sh"
