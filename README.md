<p align="center">
  <img src="https://img.shields.io/badge/Go-1.22-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Terraform-1.15-7B42BC?style=for-the-badge&logo=terraform&logoColor=white" alt="Terraform">
  <img src="https://img.shields.io/badge/Kubernetes-1.36-326CE5?style=for-the-badge&logo=kubernetes&logoColor=white" alt="Kubernetes">
  <img src="https://img.shields.io/badge/ArgoCD-3.4-EF7B4D?style=for-the-badge&logo=argo&logoColor=white" alt="ArgoCD">
  <img src="https://img.shields.io/badge/Prometheus-E6522C?style=for-the-badge&logo=prometheus&logoColor=white" alt="Prometheus">
  <img src="https://img.shields.io/badge/Grafana-F46800?style=for-the-badge&logo=grafana&logoColor=white" alt="Grafana">
</p>

<h1 align="center">Environment Provisioning Portal</h1>

<p align="center">
  <strong>Self-service Kubernetes environment provisioning with GitOps deployment, auto-wired observability, and AI-powered infrastructure review.</strong>
</p>

<p align="center">
  A developer fills out a form or runs a CLI command. In under 30 seconds, they get an isolated namespace, a PostgreSQL database, a Prometheus/Grafana monitoring dashboard, and an ArgoCD GitOps pipeline — all provisioned, deployed, and observable with zero manual steps.
</p>

---

## Why This Exists

Platform engineering teams at companies like Chime spend significant time on repetitive environment provisioning requests. Developers file a ticket, wait 1–3 days, and get a manually configured environment with no monitoring and no visibility into what was created.

This tool eliminates that bottleneck. It encodes the full provisioning workflow — infrastructure, deployment, observability, and safety review — into an automated, self-service API that any developer can call.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Developer                                   │
│                    CLI  /  API  /  Web Form                         │
└──────────────────────────────┬──────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Go API Server (:8080)                          │
│                                                                     │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐   │
│  │ Provision │  │  Status  │  │  List    │  │    Destroy       │   │
│  │ Handler   │  │ Handler  │  │ Handler  │  │    Handler       │   │
│  └─────┬────┘  └──────────┘  └──────────┘  └──────────────────┘   │
│        │                                                           │
│        ▼                                                           │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │              Async Job Queue (buffered channel)              │  │
│  └─────┬────────────┬────────────┬────────────┬────────────────┘  │
│        │            │            │            │                     │
│        ▼            ▼            ▼            ▼                     │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐          │
│  │Terraform │ │  LLM     │ │  GitOps  │ │Observability │          │
│  │  Runner  │ │ Explain  │ │ Committer│ │   Wiring     │          │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ └──────┬───────┘          │
└───────┼────────────┼────────────┼───────────────┼──────────────────┘
        │            │            │               │
        ▼            ▼            ▼               ▼
┌──────────────┐ ┌─────────┐ ┌─────────┐ ┌──────────────┐
│  Kubernetes  │ │ Claude  │ │  ArgoCD │ │ Prometheus   │
│  (via kind)  │ │   API   │ │  Sync   │ │ + Grafana    │
└──────────────┘ └─────────┘ └─────────┘ └──────────────┘
```

**Core loop:** `request → provision → deploy → observe → explain`

---

## What Gets Provisioned Per Environment

| Resource | Purpose |
|:---------|:--------|
| **Kubernetes Namespace** | `env-<name>` with ownership labels and annotations |
| **Resource Quota** | CPU/memory caps to prevent runaway consumption |
| **Limit Range** | Default per-pod resource limits |
| **Network Policy** | Default deny ingress; allow intra-namespace + ArgoCD |
| **PostgreSQL** (optional) | StatefulSet with auto-generated credentials in K8s Secret |
| **ArgoCD Application** | GitOps manifest — git commit is the deployment trigger |
| **ServiceMonitor** | Prometheus auto-discovers and scrapes environment metrics |
| **Grafana Dashboard** | Per-environment CPU, memory, network, and quota panels |

---

## Quick Start

### Prerequisites

| Tool | Version | Install |
|:-----|:--------|:--------|
| [Go](https://go.dev/dl/) | 1.22+ | `brew install go` |
| [Docker](https://docs.docker.com/get-docker/) | Latest | Desktop or Engine |
| [kind](https://kind.sigs.k8s.io/) | 0.20+ | `brew install kind` |
| [Terraform](https://developer.hashicorp.com/terraform/install) | 1.5+ | `brew install hashicorp/tap/terraform` |
| [Helm](https://helm.sh/docs/intro/install/) | 3.x | `brew install helm` |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | Latest | `brew install kubectl` |

### Bootstrap

```bash
# 1. Clone the repository
git clone <repo-url> && cd environment-provisioning-portal

# 2. Create local Kubernetes cluster + install ArgoCD + Prometheus/Grafana
make cluster-install

# 3. Build and start the API server
make dev-up
```

The API server starts on `http://localhost:8080`.

### Create Your First Environment

```bash
# Via CLI
./bin/portal create feature-auth --owner you@company.com --db

# Via API
curl -X POST http://localhost:8080/environments \
  -H 'Content-Type: application/json' \
  -d '{"name": "feature-auth", "owner": "you@company.com", "db_enabled": true}'

# Poll status (provisioning takes ~20-30 seconds)
./bin/portal status feature-auth
```

**Response:**
```json
{
  "id": "feature-auth",
  "name": "feature-auth",
  "owner": "you@company.com",
  "db_enabled": true,
  "status": "active",
  "namespace": "env-feature-auth",
  "db_host": "feature-auth-postgres.env-feature-auth.svc.cluster.local",
  "db_port": 5432,
  "plan_explanation": "This plan creates a new isolated environment..."
}
```

### List and Destroy

```bash
./bin/portal list                    # All environments
./bin/portal destroy feature-auth    # Tears down via terraform destroy
```

---

## CLI Reference

```
portal create <name> --owner <email> [--db]   Create a new environment
portal status <name>                          Check environment status
portal list                                   List all environments
portal destroy <name>                         Destroy an environment
portal help                                   Show help
```

| Flag | Description |
|:-----|:------------|
| `--owner` | **(required)** Email or handle of the requesting developer |
| `--db` | Provision a PostgreSQL StatefulSet alongside the namespace |

| Env Var | Default | Description |
|:--------|:--------|:------------|
| `PORTAL_API_URL` | `http://localhost:8080` | API server base URL |

---

## API Reference

### `POST /environments`

Create a new environment. Returns `202 Accepted` and begins async provisioning.

**Request:**
```json
{
  "name": "feature-auth",
  "owner": "dev@company.com",
  "db_enabled": true
}
```

**Validation rules:**
- `name`: lowercase alphanumeric with hyphens, 3–63 characters
- `owner`: required, non-empty string
- `db_enabled`: boolean (default: `false`)

### `GET /environments`

List all provisioned environments. Returns `200 OK` with an array.

### `GET /environments/{id}`

Get environment details including status, namespace, DB connection info, and AI plan explanation. Returns `200 OK` or `404 Not Found`.

### `DELETE /environments/{id}`

Destroy an environment. Runs `terraform destroy` asynchronously. Returns `200 OK` or `404 Not Found`.

### `GET /healthz`

Health check. Returns `{"status": "ok"}`.

---

## AI-Powered Plan Review

Before applying any Terraform change, the service captures the `terraform plan` output and sends it to Claude with a structured prompt:

> *"Explain this infrastructure change in plain English for a non-Terraform-fluent reviewer, and flag anything risky — destructive changes, public exposure, missing resource limits."*

The response is stored alongside the environment and returned in the status API. This is designed as a **human-in-the-loop safety check** — the LLM explains, a human decides.

**To enable:**
```bash
export LLM_API_KEY=your-anthropic-api-key
export LLM_MODEL=claude-sonnet-4-20250514   # optional, this is the default
```

When `LLM_API_KEY` is not set, provisioning continues without AI explanation (non-fatal).

---

## Observability

### Prometheus

- Auto-discovers `ServiceMonitor` CRDs across all namespaces
- Scrapes metrics from any pod exposing a `/metrics` endpoint
- 7-day retention with resource-bounded storage

### Grafana

- **URL:** `http://localhost:30090`
- **Credentials:** `admin` / `portal-admin`
- Dashboards auto-provisioned per environment via ConfigMap sidecar
- Panels: Pod CPU, Pod Memory, Network I/O, Resource Quota Usage

### Structured Logging

All API logs are emitted as structured JSON to stdout (12-factor compliant):

```json
{
  "time": "2026-07-14T01:38:37Z",
  "level": "INFO",
  "msg": "environment provisioned successfully",
  "environment": "feature-auth",
  "request_id": "a1b2c3d4-..."
}
```

---

## Configuration

All configuration via environment variables:

| Variable | Default | Description |
|:---------|:--------|:------------|
| `PORT` | `8080` | API server listen port |
| `TERRAFORM_MODULE_DIR` | `../infra/modules/environment` | Path to Terraform module |
| `TERRAFORM_ENVS_DIR` | `../infra/envs` | Per-environment tfvars directory |
| `GIT_REPO_DIR` | `..` | Git repo root for ArgoCD manifest commits |
| `LLM_API_KEY` | _(none)_ | Anthropic API key (disables AI if unset) |
| `LLM_BASE_URL` | `https://api.anthropic.com/v1` | LLM API endpoint |
| `LLM_MODEL` | `claude-sonnet-4-20250514` | LLM model identifier |

---

## Project Structure

```
├── api/                            Go backend service
│   ├── main.go                     HTTP server, routes, middleware stack
│   ├── handlers/
│   │   └── provision.go            Async provisioning pipeline + CRUD handlers
│   ├── terraform/
│   │   └── runner.go               Terraform CLI wrapper (init, plan, apply, destroy)
│   ├── git/
│   │   └── committer.go            ArgoCD Application YAML generator + git commit
│   ├── llm/
│   │   └── explain.go              Claude API client for plan explanations
│   ├── models/
│   │   └── environment.go          Data types, request/response structs
│   └── middleware/
│       └── middleware.go            Request ID, structured logging, CORS, panic recovery
│
├── cli/                            CLI wrapper over the API
│   └── main.go                     portal create/status/list/destroy
│
├── infra/
│   ├── modules/environment/        Reusable Terraform module
│   │   ├── main.tf                 Namespace, quotas, NetworkPolicy, Postgres, ServiceMonitor
│   │   ├── variables.tf            Input variables with validation
│   │   └── outputs.tf              Namespace, DB host/port, credentials secret
│   └── envs/                       Generated per-environment workspaces (gitignored)
│
├── gitops/
│   └── apps/                       Auto-generated ArgoCD Application manifests
│
├── observability/
│   ├── prometheus-values.yaml      Helm values for kube-prometheus-stack
│   └── grafana-dashboards/
│       └── environment-template.json   Templated Grafana dashboard per environment
│
├── docs/
│   └── design-doc.md               RFC-style design document
│
├── Makefile                        One-command bootstrap and build
├── kind-config.yaml                Kind cluster configuration (3 nodes)
└── README.md                       This file
```

---

## Design Decisions

Full write-up in [`docs/design-doc.md`](docs/design-doc.md), covering:

| Decision | Why |
|:---------|:----|
| **Terraform workspaces** over separate state files | Natural isolation, easy promotion to remote backend later |
| **ArgoCD** over `kubectl apply` in CI | Git as source of truth, drift detection, audit trail, industry standard |
| **`os/exec`** over Terraform Go SDK | Simpler, no SDK version coupling, demonstrates process management |
| **LLM as human-in-the-loop** | Augment, don't replace — infrastructure changes are high-stakes |
| **Async job queue** | Buffered Go channel — API responds immediately, provisioning runs in background |

---

## Makefile Targets

| Target | Description |
|:-------|:------------|
| `make help` | Show all available targets |
| `make cluster-install` | Create kind cluster + install ArgoCD + Prometheus/Grafana |
| `make dev-up` | Build binaries and start the API server |
| `make build` | Compile API server and CLI to `bin/` |
| `make run` | Start the API server |
| `make test` | Run Go tests |
| `make lint` | Run golangci-lint |
| `make tf-validate` | Validate the Terraform module |
| `make clean` | Remove build artifacts and terraform state |
| `make cluster-delete` | Destroy the kind cluster |

---

## 12-Factor Compliance

| Factor | Implementation |
|:-------|:---------------|
| I. Codebase | Single repo, tracked in git |
| II. Dependencies | Go modules, explicit in `go.mod` |
| III. Config | Environment variables (`PORT`, `LLM_API_KEY`, etc.) |
| IV. Backing services | Postgres as K8s service, address via DNS |
| V. Build/Release/Run | `make build` produces binary; deploy to K8s |
| VI. Processes | Stateless API; state in Terraform files |
| VII. Port binding | `http.ListenAndServe` on `$PORT` |
| VIII. Concurrency | Scale via K8s replicas; job queue per instance |
| IX. Disposability | Fast startup, graceful shutdown |
| X. Dev/prod parity | Kind cluster mirrors production topology |
| XI. Logs | Structured JSON to stdout via `slog` |
| XII. Admin | CLI tool for admin operations |

---

## V2 Roadmap

- **Multi-tenancy** — per-team namespaces with RBAC isolation and cost quotas
- **Drift detection** — periodic `terraform plan` to detect manual changes
- **Cost tracking** — resource usage metrics per environment with auto-destroy for idle envs
- **Template library** — pre-built environment types (API-only, full-stack, ML workload)
- **Web UI** — TanStack Form-based interface with real-time WebSocket status
- **CI/CD integration** — PR-triggered preview environments, auto-destroy on merge

---

## License

MIT
