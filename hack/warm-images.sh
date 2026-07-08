#!/usr/bin/env bash
# Warm the caches that dominate first-build latency:
#   - preload the kaniko executor and busybox onto the kind node
#   - seed the docker.io pull-through mirror with common base images
# Everything here is an optimization; builds work (slower) without it.
set -euo pipefail
cd "$(dirname "$0")/.."
. hack/lib.sh

info "preloading build images onto the kind node"
docker pull -q "${KANIKO_IMAGE}" >/dev/null
docker pull -q "${BUSYBOX_IMAGE}" >/dev/null
kind load docker-image --name "${CLUSTER_NAME}" "${KANIKO_IMAGE}" "${BUSYBOX_IMAGE}" >/dev/null

info "seeding the docker.io mirror with common base images"
for img in library/golang:1.26-alpine library/alpine:3.22; do
  # Pulling through localhost:${MIRROR_HOST_PORT} populates the proxy cache.
  docker pull -q "localhost:${MIRROR_HOST_PORT}/${img}" >/dev/null
  docker rmi "localhost:${MIRROR_HOST_PORT}/${img}" >/dev/null 2>&1 || true
done

info "warm-up done"
