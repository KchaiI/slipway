# Shared helpers for minato hack scripts. Source this file; do not execute.

CLUSTER_NAME=minato
REGISTRY_NAME=kind-registry
REGISTRY_HOST_PORT=5001
MIRROR_NAME=kind-registry-mirror
REGISTRY_IMAGE=registry:2

info() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31mFAIL:\033[0m %s\n' "$*" >&2; exit 1; }
ok()   { printf '\033[1;32mok\033[0m %s\n' "$*"; }

# Host port that reaches ingress-nginx inside the cluster (whatever port 80 of
# the kind node is published on). Prints e.g. "80" or "8080".
ingress_host_port() {
  docker port "${CLUSTER_NAME}-control-plane" 80/tcp 2>/dev/null | head -1 | awk -F: '{print $NF}'
}

# Base URL suffix for app hostnames: ":8080" when non-default, "" for port 80.
ingress_port_suffix() {
  local p
  p="$(ingress_host_port)"
  [ "$p" = "80" ] && echo "" || echo ":${p}"
}

container_running() {
  [ "$(docker inspect -f '{{.State.Running}}' "$1" 2>/dev/null)" = "true" ]
}
