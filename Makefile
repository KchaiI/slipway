SHELL := /bin/bash

.PHONY: build install-cli deploy-server cluster-up cluster-down cluster-purge test-m0 test-m1 e2e

## Build ----------------------------------------------------------------------

build: ## Build CLI and server binaries into bin/
	go build -o bin/minato ./cmd/minato
	go build -o bin/minato-server ./cmd/minato-server

install-cli: ## Install the minato CLI into GOPATH/bin
	go install ./cmd/minato

deploy-server: ## Build the server image and roll it out to the cluster
	hack/deploy-server.sh

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

test-m1: ## M1: control plane + CLI + image deploy
	hack/test-m1.sh

test-m2: ## M2: git push -> build -> deploy pipeline
	hack/test-m2.sh
