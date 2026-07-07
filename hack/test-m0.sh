#!/usr/bin/env bash
# M0 acceptance test: cluster, registry, and ingress are functional.
set -euo pipefail
cd "$(dirname "$0")/.."
. hack/lib.sh

info "M0-1: kind cluster exists and node is Ready"
kind get clusters | grep -qx "${CLUSTER_NAME}" || fail "cluster '${CLUSTER_NAME}' not found"
kubectl wait --for=condition=Ready node --all --timeout=60s >/dev/null \
  || fail "node not Ready"
ok "node Ready"

info "M0-2: local registry answers on localhost:${REGISTRY_HOST_PORT}"
code=$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:${REGISTRY_HOST_PORT}/v2/")
[ "$code" = "200" ] || fail "registry /v2/ returned ${code}"
ok "registry /v2/ -> 200"

info "M0-3: cluster can pull from the local registry"
docker pull -q hashicorp/http-echo:1.0 >/dev/null
docker tag hashicorp/http-echo:1.0 "localhost:${REGISTRY_HOST_PORT}/m0-echo:test"
docker push -q "localhost:${REGISTRY_HOST_PORT}/m0-echo:test" >/dev/null
ok "pushed test image to local registry"

info "M0-4: echo app is reachable through ingress at m0-echo.localtest.me"
kubectl apply -f hack/testdata/echo.yaml >/dev/null
trap 'kubectl delete -f hack/testdata/echo.yaml --ignore-not-found >/dev/null 2>&1' EXIT
kubectl -n minato-m0-echo rollout status deploy/echo --timeout=120s >/dev/null

url="http://m0-echo.localtest.me$(ingress_port_suffix)"
body=""
for i in $(seq 1 30); do
  body=$(curl -fsS "$url" 2>/dev/null) && break
  sleep 2
done
echo "$body" | grep -q "m0-echo-ok" || fail "unexpected response from ${url}: '${body}'"
ok "GET ${url} -> 200 ('m0-echo-ok')"

info "M0 acceptance test passed"
