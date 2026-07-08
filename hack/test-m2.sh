#!/usr/bin/env bash
# M2 acceptance test: git push -> Kaniko build -> automatic deploy.
#   1. push of the sample app serves HTTP 200 within 90 seconds
#   2. a second push (code change) is reflected at the URL
#   3. a broken Dockerfile is reported as a failure and does not touch the
#      running version
set -euo pipefail
cd "$(dirname "$0")/.."
. hack/lib.sh

APP=m2-sample
SUFFIX="$(ingress_port_suffix)"
export MINATO_API="http://minato.localtest.me${SUFFIX}"
MINATO=bin/minato
WORK="$(mktemp -d)"
URL="http://${APP}.localtest.me${SUFFIX}"

go build -o bin/minato ./cmd/minato

cleanup() {
  "${MINATO}" apps destroy "${APP}" >/dev/null 2>&1 || true
  rm -rf "${WORK}"
}
trap cleanup EXIT
cleanup 2>/dev/null || true

info "M2-0: create app and prepare a sample repo"
"${MINATO}" apps create "${APP}" >/dev/null
cp -r examples/sample-app/. "${WORK}/"
git -C "${WORK}" init -q -b main
git -C "${WORK}" -c user.email=e2e@minato.local -c user.name=e2e commit -qm "init" --allow-empty
git -C "${WORK}" add -A
git -C "${WORK}" -c user.email=e2e@minato.local -c user.name=e2e commit -qm "sample app v1"
git -C "${WORK}" remote add minato "http://minato.localtest.me${SUFFIX}/git/${APP}.git"

info "M2-1: git push deploys and serves 200 within 90s"
start=$(date +%s)
git -C "${WORK}" push minato main 2>&1 | sed 's/^/    /'
body=""
while :; do
  body=$(curl -fsS --max-time 2 "${URL}" 2>/dev/null) && break
  [ $(( $(date +%s) - start )) -lt 90 ] || fail "no HTTP 200 at ${URL} within 90s"
  sleep 1
done
elapsed=$(( $(date +%s) - start ))
echo "${body}" | grep -q "rev 1" || fail "unexpected body: ${body}"
[ "${elapsed}" -le 90 ] || fail "took ${elapsed}s (> 90s)"
ok "push -> 200 in ${elapsed}s ('${body}')"

info "M2-2: second push updates the app"
sed -i '' 's/(rev 1)/(rev 2)/' "${WORK}/main.go"
git -C "${WORK}" -c user.email=e2e@minato.local -c user.name=e2e commit -qam "rev 2"
git -C "${WORK}" push minato main 2>&1 | sed 's/^/    /'
body=""
for i in $(seq 1 30); do
  body=$(curl -fsS --max-time 2 "${URL}" 2>/dev/null || true)
  echo "${body}" | grep -q "rev 2" && break
  sleep 2
done
echo "${body}" | grep -q "rev 2" || fail "rev 2 not served: '${body}'"
ok "second push live ('${body}')"

info "M2-3: broken Dockerfile fails the build and leaves rev 2 serving"
echo "RUN exit 1" >> "${WORK}/Dockerfile"
git -C "${WORK}" -c user.email=e2e@minato.local -c user.name=e2e commit -qam "break build"
push_out=$(git -C "${WORK}" push minato main 2>&1 || true)
echo "${push_out}" | sed 's/^/    /'
echo "${push_out}" | grep -qi "failed" || fail "push output does not report the build failure"
body=$(curl -fsS "${URL}")
echo "${body}" | grep -q "rev 2" || fail "previous release damaged: '${body}'"
releases_out=$("${MINATO}" releases -a "${APP}")
echo "${releases_out}" | grep -q "failed" || fail "releases does not record the failed build"
ok "failure reported; rev 2 still live"

info "M2 acceptance test passed"
