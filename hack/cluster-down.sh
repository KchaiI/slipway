#!/usr/bin/env bash
# Tear down the kind cluster. Registry containers are kept so their caches
# survive cluster rebuilds; pass --purge to remove them as well.
set -euo pipefail
cd "$(dirname "$0")/.."
. hack/lib.sh

kind delete cluster --name "${CLUSTER_NAME}" 2>/dev/null || true

if [ "${1:-}" = "--purge" ]; then
  info "removing registry containers"
  docker rm -f "${REGISTRY_NAME}" "${MIRROR_NAME}" >/dev/null 2>&1 || true
fi

info "cluster '${CLUSTER_NAME}' deleted"
