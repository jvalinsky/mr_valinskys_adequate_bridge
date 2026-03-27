#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PROFILE="${1:-${ATPROTO_HARNESS_PROFILE:-mini}}"

case "${PROFILE}" in
  mini|local)
    exec "${ROOT_DIR}/scripts/local_bridge_e2e.sh"
    ;;
  testnet)
    exec "${ROOT_DIR}/scripts/testnet_bridge_e2e.sh"
    ;;
  *)
    echo "Unknown harness profile: ${PROFILE}" >&2
    echo "Supported profiles: mini, testnet" >&2
    exit 1
    ;;
esac
