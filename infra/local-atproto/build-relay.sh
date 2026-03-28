#!/usr/bin/env bash
# Build the blebbit/relay:latest Docker image from bluesky-social/indigo source.
# The image is not published to any registry; the verdverm/testnet project builds
# it locally and we do the same here.
set -euo pipefail

IMAGE="blebbit/relay:latest"
INDIGO_REF="${INDIGO_REF:-main}"
INDIGO_DIR="${INDIGO_DIR:-/tmp/mvab-indigo-relay-build}"

if docker image inspect "${IMAGE}" >/dev/null 2>&1; then
  echo "[build-relay] ${IMAGE} already exists, skipping build (delete it to rebuild)"
  exit 0
fi

echo "[build-relay] cloning bluesky-social/indigo (ref=${INDIGO_REF}) ..."
if [[ -d "${INDIGO_DIR}/.git" ]]; then
  git -C "${INDIGO_DIR}" fetch origin --tags --prune
  git -C "${INDIGO_DIR}" checkout "${INDIGO_REF}" -- 2>/dev/null \
    || git -C "${INDIGO_DIR}" checkout --detach "origin/${INDIGO_REF}"
else
  git clone --depth 1 --branch "${INDIGO_REF}" \
    https://github.com/bluesky-social/indigo "${INDIGO_DIR}"
fi

echo "[build-relay] building ${IMAGE} ..."
docker build \
  -f "${INDIGO_DIR}/cmd/relay/Dockerfile" \
  -t "${IMAGE}" \
  "${INDIGO_DIR}"

echo "[build-relay] done: ${IMAGE}"
