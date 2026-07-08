#!/usr/bin/env bash
# Build minato-server, push it to the local registry, and roll it out.
set -euo pipefail
cd "$(dirname "$0")/.."
. hack/lib.sh

TAG="$(git rev-parse --short HEAD 2>/dev/null || echo dev)-$(date +%s)"
IMAGE_PUSH="localhost:${REGISTRY_HOST_PORT}/minato-server:${TAG}"
IMAGE_PULL="${REGISTRY_NAME}:5000/minato-server:${TAG}"

info "building minato-server image (${TAG})"
docker build -q -t "${IMAGE_PUSH}" . >/dev/null
docker push -q "${IMAGE_PUSH}" >/dev/null

info "applying manifests"
kubectl apply -f deploy/server/ >/dev/null

info "rolling out ${IMAGE_PULL}"
kubectl -n minato-system set env deploy/minato-server \
  "MINATO_INGRESS_PORT=$(ingress_host_port)" >/dev/null
kubectl -n minato-system set image deploy/minato-server "server=${IMAGE_PULL}" >/dev/null
kubectl -n minato-system rollout status deploy/minato-server --timeout=120s

info "minato-server is up: http://minato.localtest.me$(ingress_port_suffix)/healthz"
