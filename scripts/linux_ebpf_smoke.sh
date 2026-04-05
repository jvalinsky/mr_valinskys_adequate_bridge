#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "[linux-ebpf-smoke] Linux is required"
  exit 1
fi

if ! command -v bpftrace >/dev/null 2>&1; then
  echo "[linux-ebpf-smoke] bpftrace is required in PATH"
  exit 1
fi

if [[ "$(id -u)" -ne 0 ]]; then
  echo "[linux-ebpf-smoke] run as root (or equivalent container privileges)"
  exit 1
fi

export GOFLAGS="${GOFLAGS:--mod=mod}"
export CGO_ENABLED="${CGO_ENABLED:-1}"

TARGET_BIN="/tmp/bridge-cli-ebpf-smoke"
DB_PATH="/tmp/bridge-ebpf-smoke.sqlite"
REPO_PATH="/tmp/bridge-ebpf-smoke-repo"
LOG_PATH="/tmp/bridge-ebpf-smoke.log"
BPFTRACE_LOG="/tmp/bridge-ebpftrace.log"

rm -f "$TARGET_BIN" "$DB_PATH" "$DB_PATH-wal" "$DB_PATH-shm" "$LOG_PATH" "$BPFTRACE_LOG"
rm -rf "$REPO_PATH"

echo "[linux-ebpf-smoke] building bridge-cli target"
go build -o "$TARGET_BIN" ./cmd/bridge-cli

if ! go tool nm "$TARGET_BIN" | grep -Eq 'runtime\.casgstatus'; then
  echo "[linux-ebpf-smoke] runtime.casgstatus symbol not found in built binary"
  exit 1
fi

echo "[linux-ebpf-smoke] starting short-lived bridge runtime target"
"$TARGET_BIN" \
  --db "$DB_PATH" \
  --bot-seed "ebpf-smoke-seed" \
  start \
  --firehose-enable=false \
  --room-enable=false \
  --repo-path "$REPO_PATH" \
  >"$LOG_PATH" 2>&1 &
TARGET_PID=$!

cleanup() {
  if kill -0 "$TARGET_PID" >/dev/null 2>&1; then
    kill "$TARGET_PID" >/dev/null 2>&1 || true
    wait "$TARGET_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

sleep 1
if ! kill -0 "$TARGET_PID" >/dev/null 2>&1; then
  echo "[linux-ebpf-smoke] bridge target exited early"
  cat "$LOG_PATH" || true
  exit 1
fi

echo "[linux-ebpf-smoke] running bpftrace casgstatus probe"
timeout 15s bpftrace -e "uprobe:$TARGET_BIN:runtime.casgstatus { @cnt = count(); } interval:s:5 { exit(); }" >"$BPFTRACE_LOG" 2>&1 &
BPFTRACE_PID=$!

# Drive a small amount of target activity while the probe is attached.
for _ in $(seq 1 200); do
  if ! kill -0 "$BPFTRACE_PID" >/dev/null 2>&1; then
    break
  fi
  "$TARGET_BIN" --db "$DB_PATH" stats >/dev/null 2>&1 || true
  sleep 0.01
done

if ! wait "$BPFTRACE_PID"; then
  echo "[linux-ebpf-smoke] bpftrace execution failed"
  cat "$BPFTRACE_LOG" || true
  exit 1
fi

count="$(grep -Eo '@cnt:[[:space:]]*[0-9]+' "$BPFTRACE_LOG" | awk '{print $2}' | tail -n 1)"
if [[ -z "$count" ]]; then
  echo "[linux-ebpf-smoke] probe did not report @cnt"
  cat "$BPFTRACE_LOG" || true
  exit 1
fi

if [[ "$count" -le 0 ]]; then
  echo "[linux-ebpf-smoke] expected casgstatus count > 0, got $count"
  cat "$BPFTRACE_LOG" || true
  exit 1
fi

echo "[linux-ebpf-smoke] success: runtime.casgstatus events captured (count=$count)"
