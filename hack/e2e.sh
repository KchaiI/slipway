#!/usr/bin/env bash
# End-to-end verification of the Definition of Done. Idempotent: brings the
# environment up if needed, deploys the current server, then runs the Go e2e
# suite in test/e2e.
set -euo pipefail
cd "$(dirname "$0")/.."
. hack/lib.sh

hack/cluster-up.sh
hack/deploy-server.sh

info "building CLI"
go build -o bin/minato ./cmd/minato

SUFFIX="$(ingress_port_suffix)"
export MINATO_API="http://minato.localtest.me${SUFFIX}"
export MINATO_URL_SUFFIX="${SUFFIX}"
export MINATO_BIN="$(pwd)/bin/minato"
export REPO_ROOT="$(pwd)"

info "running e2e suite"
go test -v -tags e2e -count=1 -timeout 20m ./test/e2e
