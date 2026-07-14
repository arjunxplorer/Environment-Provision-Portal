terraform {
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

provider "kubernetes" {
  config_path    = pathexpand("~/.kube/config")
  config_context = "kind-portal"
}

provider "null" {}

provider "random" {}

module "environment" {
  source = "/Users/arjunsharma/Desktop/Environment Provisioning Portal/infra/modules/environment"

  name      = var.name
  owner     = var.owner
  db_enabled = var.db_enabled
}

variable "name" {
  type = string
}

variable "owner" {
  type = string
}

variable "db_enabled" {
  type    = bool
  default = false
}

output "namespace" {
  value = module.environment.namespace
}

output "db_host" {
  value = module.environment.db_host
}

output "db_port" {
  value = module.environment.db_port
}
