#!/usr/bin/env bash
# Bootstrap the local minato environment:
#   kind cluster + local image registry + docker.io pull-through mirror + ingress-nginx
set -euo pipefail
cd "$(dirname "$0")/.."
. hack/lib.sh

# --- 1. Pick the host port for ingress (default 80, fall back to 8080) -------
if [ -z "${MINATO_HTTP_PORT:-}" ]; then
  if nc -z 127.0.0.1 80 >/dev/null 2>&1; then
    MINATO_HTTP_PORT=8080
    info "port 80 is in use; falling back to ${MINATO_HTTP_PORT} (app URLs get :${MINATO_HTTP_PORT})"
  else
    MINATO_HTTP_PORT=80
  fi
fi

# --- 2. Local registry + docker.io pull-through mirror ------------------------
if ! container_running "${REGISTRY_NAME}"; then
  info "starting local registry ${REGISTRY_NAME} (localhost:${REGISTRY_HOST_PORT})"
  docker rm -f "${REGISTRY_NAME}" >/dev/null 2>&1 || true
  docker run -d --restart=always -p "127.0.0.1:${REGISTRY_HOST_PORT}:5000" \
    --name "${REGISTRY_NAME}" "${REGISTRY_IMAGE}" >/dev/null
fi

if ! container_running "${MIRROR_NAME}"; then
  info "starting docker.io pull-through mirror ${MIRROR_NAME}"
  docker rm -f "${MIRROR_NAME}" >/dev/null 2>&1 || true
  docker run -d --restart=always \
    -e REGISTRY_PROXY_REMOTEURL=https://registry-1.docker.io \
    --name "${MIRROR_NAME}" "${REGISTRY_IMAGE}" >/dev/null
fi

# --- 3. kind cluster -----------------------------------------------------------
if kind get clusters 2>/dev/null | grep -qx "${CLUSTER_NAME}"; then
  info "kind cluster '${CLUSTER_NAME}' already exists; skipping create"
else
  info "creating kind cluster '${CLUSTER_NAME}' (ingress on host port ${MINATO_HTTP_PORT})"
  sed "s/hostPort: 80$/hostPort: ${MINATO_HTTP_PORT}/" deploy/kind/kind-config.yaml \
    | kind create cluster --config -
fi

# --- 4. Let containerd resolve the registries over plain HTTP ------------------
for node in $(kind get nodes --name "${CLUSTER_NAME}"); do
  for reg in "${REGISTRY_NAME}:5000" "localhost:${REGISTRY_HOST_PORT}"; do
    docker exec "${node}" mkdir -p "/etc/containerd/certs.d/${reg}"
    printf '[host."http://%s:5000"]\n' "${REGISTRY_NAME}" \
      | docker exec -i "${node}" tee "/etc/containerd/certs.d/${reg}/hosts.toml" >/dev/null
  done
done

# --- 5. Attach registries to the kind docker network ---------------------------
for c in "${REGISTRY_NAME}" "${MIRROR_NAME}"; do
  if [ "$(docker inspect -f '{{json .NetworkSettings.Networks.kind}}' "${c}")" = "null" ]; then
    docker network connect kind "${c}"
  fi
done

# --- 6. Advertise the local registry (kind convention) --------------------------
kubectl apply -f - <<EOF >/dev/null
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${REGISTRY_HOST_PORT}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF

# --- 7. ingress-nginx ------------------------------------------------------------
info "deploying ingress-nginx"
kubectl apply -f deploy/ingress-nginx/deploy.yaml >/dev/null
info "waiting for ingress-nginx controller to become ready"
for i in $(seq 1 60); do
  if kubectl -n ingress-nginx get pod -l app.kubernetes.io/component=controller 2>/dev/null \
      | grep -q Running; then
    break
  fi
  sleep 2
done
kubectl -n ingress-nginx wait --for=condition=ready pod \
  -l app.kubernetes.io/component=controller --timeout=180s >/dev/null

info "cluster is up. ingress: http://<app>.localtest.me$(ingress_port_suffix)"
