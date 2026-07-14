output "namespace" {
  description = "The Kubernetes namespace created for this environment"
  value       = kubernetes_namespace.env.metadata[0].name
}

output "db_host" {
  description = "PostgreSQL headless service DNS name (empty if db_enabled=false)"
  value       = var.db_enabled ? "${kubernetes_service.postgres[0].metadata[0].name}.${local.namespace}.svc.cluster.local" : ""
}

output "db_port" {
  description = "PostgreSQL port (0 if db_enabled=false)"
  value       = var.db_enabled ? 5432 : 0
}

output "db_password_secret" {
  description = "Name of the Kubernetes Secret containing DB credentials"
  value       = var.db_enabled ? kubernetes_secret.db_credentials[0].metadata[0].name : ""
}

output "environment_name" {
  description = "The environment name (echo back for confirmation)"
  value       = var.name
}

output "owner" {
  description = "The environment owner (echo back for confirmation)"
  value       = var.owner
}
