#!/usr/bin/env bash
# M3 acceptance test: operational CLI — scale, realtime logs, rollback.
set -euo pipefail
cd "$(dirname "$0")/.."
. hack/lib.sh

APP=m3-ops
SUFFIX="$(ingress_port_suffix)"
export MINATO_API="http://minato.localtest.me${SUFFIX}"
MINATO=bin/minato
WORK="$(mktemp -d)"
URL="http://${APP}.localtest.me${SUFFIX}"
GIT="git -C ${WORK} -c user.email=e2e@minato.local -c user.name=e2e"

go build -o bin/minato ./cmd/minato

cleanup() {
  [ -n "${LOGS_PID:-}" ] && kill "${LOGS_PID}" 2>/dev/null || true
  "${MINATO}" apps destroy "${APP}" >/dev/null 2>&1 || true
  rm -rf "${WORK}"
}
trap cleanup EXIT
"${MINATO}" apps destroy "${APP}" >/dev/null 2>&1 || true

info "M3-0: deploy rev 1 and rev 2 of the sample app"
"${MINATO}" apps create "${APP}" >/dev/null
cp -r examples/sample-app/. "${WORK}/"
git -C "${WORK}" init -q -b main
${GIT} add -A && ${GIT} commit -qm "rev 1"
git -C "${WORK}" remote add minato "http://minato.localtest.me${SUFFIX}/git/${APP}.git"
git -C "${WORK}" push -q minato main 2>/dev/null
sed -i '' 's/(rev 1)/(rev 2)/' "${WORK}/main.go"
${GIT} commit -qam "rev 2"
git -C "${WORK}" push -q minato main 2>/dev/null
curl -fsS "${URL}" | grep -q "rev 2" || fail "rev 2 not live after setup"
ok "v1 and v2 deployed; rev 2 live"

info "M3-1: minato scale web=3 yields 3 ready pods"
scale_out=$("${MINATO}" scale web=3 -a "${APP}")
echo "    ${scale_out}"
echo "${scale_out}" | grep -q "3/3 ready" || fail "scale output: ${scale_out}"
ready=$(kubectl -n "minato-app-${APP}" get pods --field-selector=status.phase=Running \
  -l minato.dev/app="${APP}" --no-headers | grep -c "1/1" || true)
[ "${ready}" = "3" ] || fail "expected 3 ready pods, got ${ready}"
ok "3/3 pods ready"

info "M3-2: minato logs -f streams a request within 5 seconds"
LOGFILE="${WORK}/logs.txt"
"${MINATO}" logs -f -a "${APP}" > "${LOGFILE}" 2>&1 &
LOGS_PID=$!
sleep 2   # let the stream attach
marker="/m3-marker-$$"
curl -fsS "${URL}${marker}" >/dev/null
found=""
for i in $(seq 1 10); do
  grep -q "${marker}" "${LOGFILE}" && found=yes && break
  sleep 0.5
done
{ kill "${LOGS_PID}" && wait "${LOGS_PID}"; } 2>/dev/null || true
LOGS_PID=""
[ -n "${found}" ] || { cat "${LOGFILE}"; fail "request log did not appear within 5s"; }
grep -q "^\[${APP}-" "${LOGFILE}" || fail "log lines are not pod-prefixed"
ok "realtime log line arrived (pod-prefixed)"

info "M3-3: rollback serves rev 1 again and records v3"
rollback_out=$("${MINATO}" rollback -a "${APP}")
echo "    ${rollback_out}"
echo "${rollback_out}" | grep -q "released v3 (rollback to v1)" || fail "unexpected: ${rollback_out}"
body=""
for i in $(seq 1 15); do
  body=$(curl -fsS "${URL}")
  echo "${body}" | grep -q "rev 1" && break
  sleep 2
done
echo "${body}" | grep -q "rev 1" || fail "rollback did not restore rev 1: '${body}'"
releases_out=$("${MINATO}" releases -a "${APP}")
echo "${releases_out}" | sed 's/^/    /'
echo "${releases_out}" | grep -E "^v3\s+live" >/dev/null || fail "v3 not live in releases"
echo "${releases_out}" | grep -E "^v2\s+superseded" >/dev/null || fail "v2 not superseded"
ok "rollback -> rev 1 live as v3"

info "M3 acceptance test passed"
