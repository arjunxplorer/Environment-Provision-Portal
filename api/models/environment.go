package models

import "time"

// Environment represents a provisioned environment.
type Environment struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Owner     string    `json:"owner"`
	DBEnabled bool      `json:"db_enabled"`
	Status    string    `json:"status"` // pending, provisioning, active, destroying, destroyed, error
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Populated once provisioning completes.
	Namespace  string `json:"namespace,omitempty"`
	DBHost     string `json:"db_host,omitempty"`
	DBPort     int    `json:"db_port,omitempty"`
	ArgoAppURL string `json:"argocd_app_url,omitempty"`

	// AI-generated summary of the Terraform plan.
	PlanExplanation string `json:"plan_explanation,omitempty"`

	// Error message if status == "error".
	Error string `json:"error,omitempty"`
}

// CreateEnvironmentRequest is the payload for POST /environments.
type CreateEnvironmentRequest struct {
	Name      string `json:"name"`
	Owner     string `json:"owner"`
	DBEnabled bool   `json:"db_enabled"`
}

// EnvironmentStatusEvent is a log entry emitted during provisioning.
type EnvironmentStatusEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	Environment string    `json:"environment"`
	Stage       string    `json:"stage"` // plan, apply, gitops, observability, done
	Message     string    `json:"message"`
	Error       string    `json:"error,omitempty"`
}

// Valid statuses.
const (
	StatusPending      = "pending"
	StatusProvisioning = "provisioning"
	StatusActive       = "active"
	StatusDestroying   = "destroying"
	StatusDestroyed    = "destroyed"
	StatusError        = "error"
)
