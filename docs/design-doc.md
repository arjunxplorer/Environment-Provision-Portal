# Environment Provisioning Portal — Design Document

**Author:** Arjun Sharma
**Status:** Proposed
**Date:** 2026-07-13

---

## 1. Problem Statement

Developers at our organization frequently need isolated environments for feature development, testing, and QA. Today, creating an environment involves:

1. Filing a ticket with the platform team
2. Waiting 1-3 days for manual provisioning
3. Back-and-forth on configuration details
4. No visibility into what was provisioned or how to monitor it

This creates a bottleneck: the platform team spends time on repetitive requests, and developers wait for environments instead of building features.

**Goal:** Build a self-service tool that lets developers spin up a scoped environment (namespace + database + monitoring) in minutes, not days, with full observability and an AI-powered safety review of infrastructure changes.

---

## 2. Architecture Overview

```
Developer → CLI/API → Go Service → Terraform (provisions infra)
                                 → Git commit (manifests) → ArgoCD (syncs to cluster)
                                 → Prometheus/Grafana (auto-dashboard)
                                 → LLM (summarizes plan / flags risk)
```

**Core loop:** request → provision → deploy → observe → explain

### Component Responsibilities

| Component | Role |
|-----------|------|
| **Go API** | Accept requests, orchestrate provisioning pipeline, manage state |
| **Terraform** | Declare infrastructure (namespace, quotas, RBAC, Postgres, NetworkPolicy) |
| **ArgoCD** | GitOps deployment — git commit is the deployment trigger |
| **Prometheus/Grafana** | Auto-provisioned monitoring per environment |
| **LLM (Claude)** | Explain terraform plans in plain English, flag risks |

---

## 3. Key Design Decisions

### 3.1 Why Terraform Workspaces over Separate State Files?

**Alternatives considered:**
- Separate state files per environment
- Terraform Cloud workspaces
- Single state file with `for_each`

**Decision:** Terraform workspaces with local backend, one workspace per environment.

**Rationale:**
- Workspaces provide natural isolation without managing N state files
- Local backend is sufficient for a kind cluster (no S3/GCS needed)
- Easy to promote to remote backend later (just change the backend config)
- `terraform workspace select <env>` is clean for the runner

**Tradeoff:** All workspaces share the same backend. If the backend corrupts, all environments are affected. Mitigated by the fact that this targets ephemeral dev environments, not production.

### 3.2 Why ArgoCD over kubectl apply in CI?

**Alternatives considered:**
- `kubectl apply` directly from the Go API
- Flux CD
- ArgoCD

**Decision:** ArgoCD

**Rationale:**
- Git commit is the single source of truth — auditable, reviewable, rollback-able
- ArgoCD provides drift detection (self-heal) — if someone manually changes something, it reverts
- ArgoCD UI gives visibility into what's deployed where
- Industry standard for GitOps — demonstrates modern deploy patterns
- `kubectl apply` in CI/CD is an anti-pattern (no drift detection, no audit trail)

**Tradeoff:** Additional infrastructure to install and manage. Mitigated by ArgoCD being a single Helm install.

### 3.3 Why Go with os/exec over Terraform SDK?

**Alternatives considered:**
- HashiCorp's Terraform Go SDK (terraform-exec)
- Direct gRPC to Terraform
- Shell out with `os/exec`

**Decision:** `os/exec` wrapping the `terraform` CLI

**Rationale:**
- Simpler implementation — no SDK version coupling
- `terraform-exec` is essentially the same thing (it shells out too)
- Streaming output via stdout pipe is straightforward
- Easier to debug — can see exact commands being run
- Demonstrates practical Go skills (goroutines, channels, process management)

**Tradeoff:** Depends on `terraform` binary being in PATH. Mitigated by containerizing the service.

### 3.4 Why LLM as Human-in-the-Loop, Not Autonomous?

**Decision:** The LLM explains the Terraform plan *before* apply, but does NOT auto-approve or auto-reject. A human reviews the explanation.

**Rationale:**
- "Safe and reliable" AI adoption means augmenting human judgment, not replacing it
- Infrastructure changes are high-stakes — an LLM hallucination shouldn't delete production
- The explanation serves as a safety net: developers who don't read raw Terraform output get a plain-English summary
- Can evolve to auto-approve low-risk changes later (with confidence thresholds)

---

## 4. Failure Modes and Mitigations

| Failure | Impact | Mitigation |
|---------|--------|------------|
| Terraform apply fails mid-way | Partially provisioned env | Terraform state tracks what succeeded; re-apply is idempotent |
| Git commit fails | No ArgoCD sync | Non-fatal warning; manual gitops commit can be retried |
| LLM API unavailable | No plan explanation | Non-fatal warning; provisioning continues without explanation |
| ArgoCD sync fails | Deployed manifests don't match | ArgoCD retries automatically; self-heal reverts manual changes |
| Kind cluster dies | Everything down | Ephemeral dev environment; `make cluster-install` rebuilds |
| Concurrent requests for same name | Conflict | Mutex-guarded map + conflict detection returns 409 |
| Terraform state corruption | Can't plan/apply/destroy | Low risk with local backend; can delete workspace and re-provision |

---

## 5. 12-Factor Compliance

| Factor | Implementation |
|--------|---------------|
| I. Codebase | One repo, tracked in git |
| II. Dependencies | Go modules, explicit in go.mod |
| III. Config | Environment variables (`PORT`, `LLM_API_KEY`, etc.) |
| IV. Backing services | Postgres is a backing service, address via K8s service DNS |
| V. Build/Release/Run | `make build` produces binary; deploy to K8s |
| VI. Processes | Stateless API server; state in Terraform files on disk |
| VII. Port binding | `http.ListenAndServe` on `$PORT` |
| VIII. Concurrency | Scale via K8s replicas; job queue is per-instance |
| IX. Disposability | Fast startup, graceful shutdown pattern |
| X. Dev/prod parity | Kind cluster mirrors production topology |
| XI. Logs | Structured JSON to stdout via `slog` |
| XII. Admin processes | CLI tool for admin operations |

---

## 6. V2 Roadmap

### 6.1 Multi-Tenancy
- Per-team namespaces with RBAC isolation
- Cost quotas per team (track via Prometheus metrics)
- Team-scoped Terraform workspaces

### 6.2 Drift Detection
- Periodic `terraform plan` to detect manual changes
- Alert via Slack/PagerDuty when drift is detected
- Auto-remediation or manual approval workflow

### 6.3 Cost Tracking
- Resource usage metrics per environment
- Cost estimation in the LLM explanation
- Auto-destroy idle environments (no pods running for N hours)

### 6.4 Template Library
- Pre-built environment templates (API-only, Full-stack, ML-workload)
- Template marketplace for teams to share configurations
- Custom resource add-ons (Redis, S3 buckets, etc.)

### 6.5 Web UI
- TanStack Form-based web interface
- Real-time provisioning status via WebSocket
- Visual environment topology diagram
- One-click destroy with confirmation

### 6.6 CI/CD Integration
- GitHub Actions workflow for environment lifecycle
- PR-triggered preview environments
- Auto-destroy on PR merge

---

## 7. Security Considerations

- **NetworkPolicy**: Default deny ingress per namespace, allow only intra-namespace and ArgoCD
- **ResourceQuota**: Prevent runaway resource consumption
- **LimitRange**: Default container limits to prevent unbounded pods
- **Postgres credentials**: Stored in K8s Secrets, auto-generated passwords
- **LLM API key**: Never logged, read from environment variable
- **No public exposure**: No LoadBalancer services, no Ingress (cluster-internal only)

---

## 8. Testing Strategy

- **Unit tests**: Handler logic, TFVar generation, template rendering
- **Integration tests**: Terraform init/plan against kind cluster
- **E2E test**: Full provisioning cycle (create → verify namespace → verify ArgoCD sync → destroy)
- **Manual validation**: `make dev-up`, use CLI to create/destroy environments

---

## Appendix A: API Contract

### POST /environments
```json
{
  "name": "feature-auth",
  "owner": "dev@company.com",
  "db_enabled": true
}
```
Response: 202 Accepted with Environment object.

### GET /environments/{id}
Response: 200 OK with Environment object (includes status, plan explanation).

### DELETE /environments/{id}
Response: 200 OK, triggers async destroy.

### GET /environments
Response: 200 OK with array of Environment objects.

### GET /healthz
Response: 200 OK `{"status": "ok"}`.
