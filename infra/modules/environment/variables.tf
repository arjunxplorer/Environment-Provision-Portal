variable "name" {
  description = "Environment name, used as namespace suffix and resource prefix"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,61}[a-z0-9]$", var.name))
    error_message = "Name must be lowercase alphanumeric with hyphens, 3-63 chars."
  }
}

variable "owner" {
  description = "Email or handle of the developer requesting this environment"
  type        = string
}

variable "db_enabled" {
  description = "Provision a PostgreSQL StatefulSet alongside the namespace"
  type        = bool
  default     = false
}

variable "db_storage_size" {
  description = "Persistent volume size for PostgreSQL (when db_enabled=true)"
  type        = string
  default     = "1Gi"
}

variable "cpu_request" {
  description = "CPU request for the namespace resource quota"
  type        = string
  default     = "500m"
}

variable "memory_request" {
  description = "Memory request for the namespace resource quota"
  type        = string
  default     = "512Mi"
}

variable "cpu_limit" {
  description = "CPU limit for the namespace resource quota"
  type        = string
  default     = "2"
}

variable "memory_limit" {
  description = "Memory limit for the namespace resource quota"
  type        = string
  default     = "2Gi"
}

variable "grafana_dashboard_endpoint" {
  description = "Grafana API endpoint for auto-provisioning dashboards (empty to skip)"
  type        = string
  default     = ""
}

variable "argocd_namespace" {
  description = "Namespace where ArgoCD is installed (for Application CRD targeting)"
  type        = string
  default     = "argocd"
}
