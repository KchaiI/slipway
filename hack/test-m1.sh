#!/usr/bin/env bash
# M1 acceptance test: control plane creates apps, deploys a prebuilt image,
# generates Deployment/Service/Ingress, and destroys apps cleanly.
set -euo pipefail
cd "$(dirname "$0")/.."
. hack/lib.sh

APP=m1-demo
SUFFIX="$(ingress_port_suffix)"
export MINATO_API="http://minato.localtest.me${SUFFIX}"
MINATO=bin/minato

info "building CLI"
go build -o bin/minato ./cmd/minato

cleanup() { "${MINATO}" apps destroy "${APP}" >/dev/null 2>&1 || true; }
trap cleanup EXIT
cleanup

info "M1-1: server is healthy at ${MINATO_API}"
curl -fsS "${MINATO_API}/healthz" >/dev/null || fail "server /healthz unreachable"
ok "healthz -> 200"

info "M1-2: minato apps create ${APP} creates the namespace"
"${MINATO}" apps create "${APP}"
kubectl get ns "minato-app-${APP}" >/dev/null || fail "namespace not created"
kubectl -n "minato-app-${APP}" get configmap minato-releases >/dev/null \
  || fail "release configmap not initialized"
ok "namespace minato-app-${APP} + release store exist"

info "M1-3: duplicate create is rejected"
if "${MINATO}" apps create "${APP}" >/dev/null 2>&1; then
  fail "duplicate app create should fail"
fi
ok "duplicate rejected"

info "M1-4: deploying a prebuilt image yields Deployment/Service/Ingress and a live URL"
"${MINATO}" deploy-image mendhak/http-https-echo:37 -a "${APP}"
for kind_ in deploy service ingress; do
  kubectl -n "minato-app-${APP}" get "${kind_}" "${APP}" >/dev/null \
    || fail "${kind_}/${APP} not created"
done
url="http://${APP}.localtest.me${SUFFIX}"
code=""
for i in $(seq 1 30); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "$url") && [ "$code" = "200" ] && break
  sleep 2
done
[ "$code" = "200" ] || fail "GET ${url} returned ${code}"
ok "GET ${url} -> 200"

info "M1-5: apps list and status report the app"
# Capture output before grepping: `cmd | grep -q` + pipefail can report 141
# when grep exits before the CLI finishes writing (SIGPIPE).
list_out=$("${MINATO}" apps list)
echo "${list_out}" | grep -q "${APP}" || fail "app missing from apps list"
status_out=$("${MINATO}" status -a "${APP}")
echo "${status_out}" | grep -q "1/1 ready" || fail "status does not show 1/1 ready"
ok "list + status OK"

info "M1-6: apps destroy removes the namespace"
"${MINATO}" apps destroy "${APP}"
for i in $(seq 1 45); do
  kubectl get ns "minato-app-${APP}" >/dev/null 2>&1 || break
  sleep 2
done
kubectl get ns "minato-app-${APP}" >/dev/null 2>&1 && fail "namespace still present"
ok "namespace deleted"

info "M1 acceptance test passed"
