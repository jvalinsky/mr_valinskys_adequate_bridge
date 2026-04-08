#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

ENV_FILE="${LIVE_ATPROTO_ENV_FILE:-${LIVE_ATPROTO_CONFIG_FILE:-}}"
if [[ -n "${ENV_FILE}" ]]; then
  if [[ ! -f "${ENV_FILE}" ]]; then
    echo "[live-e2e] env/config file not found: ${ENV_FILE}"
    exit 1
  fi
  set -a
  # shellcheck source=/dev/null
  source "${ENV_FILE}"
  set +a
  export LIVE_ATPROTO_CONFIG_FILE="${LIVE_ATPROTO_CONFIG_FILE:-${ENV_FILE}}"
fi

EXPECT="${E2E_EXPECT:-pass}"
TEST_PATTERN="${LIVE_E2E_TEST_PATTERN:-TestBridgeLiveInterop}"
TEST_LABEL="${LIVE_E2E_LABEL:-live relay + room interoperability tests}"
case "${EXPECT}" in
  pass|fail)
    ;;
  *)
    echo "[live-e2e] unsupported expectation: ${EXPECT} (expected pass or fail)"
    exit 1
    ;;
esac

if [[ "${LIVE_E2E_ENABLED:-}" != "1" ]]; then
  echo "[live-e2e] LIVE_E2E_ENABLED=1 is required"
  exit 1
fi

export GOCACHE="${GOCACHE:-/tmp/go-build-cache}"
if [[ "${GOFLAGS:-}" != *"-mod="* ]]; then
  export GOFLAGS="-mod=mod ${GOFLAGS:-}"
fi

echo "[live-e2e] running ${TEST_LABEL} (pattern=${TEST_PATTERN} expect=${EXPECT})"
set +e
go test ./internal/livee2e -run "${TEST_PATTERN}" -count=1
test_exit=$?
set -e

if [[ "${EXPECT}" == "pass" && "${test_exit}" -eq 0 ]]; then
  echo "[live-e2e] E2E PASSED: live interoperability test passed"
  exit 0
fi
if [[ "${EXPECT}" == "fail" && "${test_exit}" -ne 0 ]]; then
  echo "[live-e2e] E2E PASSED: live interoperability test failed as expected (exit=${test_exit})"
  exit 0
fi

echo "[live-e2e] E2E FAILED: expectation=${EXPECT} go_test_exit=${test_exit}"
exit 1
