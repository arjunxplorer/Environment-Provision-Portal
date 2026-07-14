.PHONY: help dev-up build run test lint clean cluster-install monitor-install

# Default target
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------------------
# Local development bootstrap
# ---------------------------------------------------------------------------

cluster-install: ## Install kind cluster + ArgoCD + Prometheus/Grafana
	@echo "==> Creating kind cluster..."
	kind create cluster --name portal --config kind-config.yaml || true
	@echo "==> Installing ArgoCD..."
	kubectl create namespace argocd || true
	kubectl apply --server-side --force-conflicts -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
	@echo "==> Installing Prometheus + Grafana..."
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts || true
	helm repo update
	helm install monitoring prometheus-community/kube-prometheus-stack \
		-f observability/prometheus-values.yaml \
		-n monitoring --create-namespace
	@echo "==> Done. ArgoCD and monitoring are installing."
	@echo "    ArgoCD: kubectl port-forward svc/argocd-server -n argocd 8443:443"
	@echo "    Grafana: http://localhost:30090 (admin/portal-admin)"

dev-up: build run ## Build and start the API server

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

build: ## Build the API server and CLI
	@echo "==> Building API server..."
	cd api && go build -o ../bin/portal-api .
	@echo "==> Building CLI..."
	cd cli && go build -o ../bin/portal .
	@echo "==> Binaries in bin/"

run: ## Run the API server
	./bin/portal-api

# ---------------------------------------------------------------------------
# Quality
# ---------------------------------------------------------------------------

test: ## Run tests
	cd api && go test ./...

lint: ## Run linters (requires golangci-lint)
	cd api && golangci-lint run ./...
	cd cli && golangci-lint run ./...

# ---------------------------------------------------------------------------
# Terraform
# ---------------------------------------------------------------------------

tf-validate: ## Validate the Terraform module
	cd infra/modules/environment && terraform init -backend=false && terraform validate

# ---------------------------------------------------------------------------
# Cleanup
# ---------------------------------------------------------------------------

clean: ## Remove build artifacts
	rm -rf bin/
	rm -rf infra/envs/*/terraform.tfstate*
	rm -rf infra/envs/*/.terraform

cluster-delete: ## Delete the kind cluster
	kind delete cluster --name portal
