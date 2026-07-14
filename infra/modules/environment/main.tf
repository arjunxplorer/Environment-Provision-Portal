terraform {
  required_version = ">= 1.5"

  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.25"
    }
    null = {
      source  = "hashicorp/null"
      version = "~> 3.2"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

locals {
  namespace = "env-${var.name}"
  # Sanitize owner for use in Kubernetes labels (no @ or special chars)
  owner_label = replace(var.owner, "/[^A-Za-z0-9._-]/", "_")
  common_labels = {
    "app.kubernetes.io/managed-by" = "environment-provisioning-portal"
    "portal/environment"           = var.name
    "portal/owner"                 = local.owner_label
  }
}

# ---------------------------------------------------------------------------
# Namespace
# ---------------------------------------------------------------------------
resource "kubernetes_namespace" "env" {
  metadata {
    name   = local.namespace
    labels = local.common_labels

    annotations = {
      "portal/owner"       = var.owner
      "portal/provisioned" = timestamp()
    }
  }
}

# ---------------------------------------------------------------------------
# Resource Quota — caps total resource consumption in the namespace
# ---------------------------------------------------------------------------
resource "kubernetes_resource_quota" "env" {
  metadata {
    name      = "${var.name}-quota"
    namespace = kubernetes_namespace.env.metadata[0].name
  }

  spec {
    hard = {
      "requests.cpu"    = var.cpu_request
      "requests.memory" = var.memory_request
      "limits.cpu"      = var.cpu_limit
      "limits.memory"   = var.memory_limit
      "pods"            = "20"
      "services"        = "10"
    }
  }
}

# ---------------------------------------------------------------------------
# Network Policy — default deny ingress, allow from same namespace + argocd
# ---------------------------------------------------------------------------
resource "kubernetes_network_policy" "default_deny" {
  metadata {
    name      = "${var.name}-default-deny"
    namespace = kubernetes_namespace.env.metadata[0].name
  }

  spec {
    pod_selector {}
    policy_types = ["Ingress"]

    ingress {
      from {
        namespace_selector {
          match_labels = {
            "kubernetes.io/metadata.name" = local.namespace
          }
        }
      }
    }

    # Allow ArgoCD to sync into this namespace
    ingress {
      from {
        namespace_selector {
          match_labels = {
            "kubernetes.io/metadata.name" = var.argocd_namespace
          }
        }
      }
    }
  }
}

# ---------------------------------------------------------------------------
# Limit Range — default per-pod limits so no pod runs unbounded
# ---------------------------------------------------------------------------
resource "kubernetes_limit_range" "env" {
  metadata {
    name      = "${var.name}-limits"
    namespace = kubernetes_namespace.env.metadata[0].name
  }

  spec {
    limit {
      type = "Container"
      default = {
        cpu    = "500m"
        memory = "256Mi"
      }
      default_request = {
        cpu    = "100m"
        memory = "128Mi"
      }
    }
  }
}

# ---------------------------------------------------------------------------
# PostgreSQL (optional) — single-replica StatefulSet + Service
# ---------------------------------------------------------------------------
resource "kubernetes_secret" "db_credentials" {
  count = var.db_enabled ? 1 : 0

  metadata {
    name      = "${var.name}-db-credentials"
    namespace = kubernetes_namespace.env.metadata[0].name
  }

  data = {
    POSTGRES_USER     = "app"
    POSTGRES_PASSWORD = random_password.db_password[0].result
    POSTGRES_DB       = "appdb"
  }
}

resource "random_password" "db_password" {
  count   = var.db_enabled ? 1 : 0
  length  = 24
  special = false
}

resource "kubernetes_stateful_set" "postgres" {
  count = var.db_enabled ? 1 : 0

  metadata {
    name      = "${var.name}-postgres"
    namespace = kubernetes_namespace.env.metadata[0].name
    labels    = merge(local.common_labels, { "portal/component" = "database" })
  }

  spec {
    service_name = kubernetes_service.postgres[0].metadata[0].name
    replicas     = 1

    selector {
      match_labels = {
        "app" = "${var.name}-postgres"
      }
    }

    template {
      metadata {
        labels = {
          "app" = "${var.name}-postgres"
        }
      }

      spec {
        container {
          name  = "postgres"
          image = "postgres:16-alpine"

          port {
            container_port = 5432
          }

          env_from {
            secret_ref {
              name = kubernetes_secret.db_credentials[0].metadata[0].name
            }
          }

          volume_mount {
            name       = "data"
            mount_path = "/var/lib/postgresql/data"
            sub_path   = "pgdata"
          }

          resources {
            requests = {
              cpu    = "100m"
              memory = "256Mi"
            }
            limits = {
              cpu    = "500m"
              memory = "512Mi"
            }
          }

          liveness_probe {
            exec {
              command = ["pg_isready", "-U", "app"]
            }
            initial_delay_seconds = 10
            period_seconds        = 10
          }

          readiness_probe {
            exec {
              command = ["pg_isready", "-U", "app"]
            }
            initial_delay_seconds = 5
            period_seconds        = 5
          }
        }
      }
    }

    volume_claim_template {
      metadata {
        name = "data"
      }

      spec {
        access_modes       = ["ReadWriteOnce"]
        storage_class_name = "standard"

        resources {
          requests = {
            storage = var.db_storage_size
          }
        }
      }
    }
  }
}

resource "kubernetes_service" "postgres" {
  count = var.db_enabled ? 1 : 0

  metadata {
    name      = "${var.name}-postgres"
    namespace = kubernetes_namespace.env.metadata[0].name
    labels    = merge(local.common_labels, { "portal/component" = "database" })
  }

  spec {
    selector = {
      "app" = "${var.name}-postgres"
    }

    port {
      port        = 5432
      target_port = 5432
    }

    cluster_ip = "None" # Headless for StatefulSet DNS
  }
}

# ---------------------------------------------------------------------------
# ServiceMonitor — lets Prometheus scrape pods in this namespace
# Uses null_resource + kubectl because kubernetes_manifest needs special
# provider config for CRDs.
# ---------------------------------------------------------------------------
resource "null_resource" "service_monitor" {
  triggers = {
    namespace = kubernetes_namespace.env.metadata[0].name
    name      = var.name
  }

  provisioner "local-exec" {
    command = <<-EOT
      kubectl apply -n ${local.namespace} -f - <<'EOF'
      apiVersion: monitoring.coreos.com/v1
      kind: ServiceMonitor
      metadata:
        name: ${var.name}-monitor
        namespace: ${local.namespace}
        labels:
          app.kubernetes.io/managed-by: environment-provisioning-portal
          portal/environment: ${var.name}
          portal/owner: ${local.owner_label}
      spec:
        namespaceSelector:
          matchNames:
            - ${local.namespace}
        selector:
          matchLabels:
            portal/environment: ${var.name}
        endpoints:
          - port: metrics
            interval: 30s
            path: /metrics
      EOF
    EOT
  }

  depends_on = [kubernetes_namespace.env]
}
