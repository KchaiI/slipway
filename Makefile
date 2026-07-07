SHELL := /bin/bash

.PHONY: cluster-up cluster-down cluster-purge test-m0 e2e

## Local environment ---------------------------------------------------------

cluster-up: ## Create kind cluster + registry + mirror + ingress-nginx
	hack/cluster-up.sh

cluster-down: ## Delete the kind cluster (registries are kept for cache)
	hack/cluster-down.sh

cluster-purge: ## Delete the cluster and the registry containers
	hack/cluster-down.sh --purge

## Acceptance tests ----------------------------------------------------------

test-m0: ## M0: cluster / registry / ingress smoke test
	hack/test-m0.sh
