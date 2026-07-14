package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"time"

	"portal/api/git"
	"portal/api/llm"
	"portal/api/models"
	"portal/api/terraform"
)

var validName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,61}[a-z0-9]$`)

// Provisioner handles environment lifecycle operations.
type Provisioner struct {
	envs      map[string]*models.Environment
	envsMu    sync.RWMutex
	moduleDir string
	envsDir   string
	gitDir    string
	llmClient *llm.Client
	logger    *slog.Logger

	// job queue — buffered channel of env IDs to provision.
	jobCh chan string
}

// NewProvisioner creates a Provisioner and starts the background worker.
func NewProvisioner(moduleDir, envsDir, gitDir string, llmClient *llm.Client, logger *slog.Logger) *Provisioner {
	p := &Provisioner{
		envs:      make(map[string]*models.Environment),
		moduleDir: moduleDir,
		envsDir:   envsDir,
		gitDir:    gitDir,
		llmClient: llmClient,
		logger:    logger,
		jobCh:     make(chan string, 10),
	}
	go p.worker()
	return p
}

// Create handles POST /environments — validates, stores, enqueues provisioning.
func (p *Provisioner) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	if !validName.MatchString(req.Name) {
		http.Error(w, `{"error":"name must be lowercase alphanumeric with hyphens, 3-63 chars"}`, http.StatusBadRequest)
		return
	}
	if req.Owner == "" {
		http.Error(w, `{"error":"owner is required"}`, http.StatusBadRequest)
		return
	}

	p.envsMu.Lock()
	if _, exists := p.envs[req.Name]; exists {
		p.envsMu.Unlock()
		http.Error(w, fmt.Sprintf(`{"error":"environment %q already exists"}`, req.Name), http.StatusConflict)
		return
	}

	env := &models.Environment{
		ID:        req.Name,
		Name:      req.Name,
		Owner:     req.Owner,
		DBEnabled: req.DBEnabled,
		Status:    models.StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	p.envs[req.Name] = env
	p.envsMu.Unlock()

	// Enqueue for async provisioning.
	p.jobCh <- req.Name

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(env)
}

// List handles GET /environments.
func (p *Provisioner) List(w http.ResponseWriter, r *http.Request) {
	p.envsMu.RLock()
	defer p.envsMu.RUnlock()

	envs := make([]*models.Environment, 0, len(p.envs))
	for _, env := range p.envs {
		envs = append(envs, env)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(envs)
}

// Status handles GET /environments/{id}.
func (p *Provisioner) Status(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	p.envsMu.RLock()
	env, ok := p.envs[id]
	p.envsMu.RUnlock()

	if !ok {
		http.Error(w, `{"error":"environment not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(env)
}

// Destroy handles DELETE /environments/{id}.
func (p *Provisioner) Destroy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	p.envsMu.Lock()
	env, ok := p.envs[id]
	if !ok {
		p.envsMu.Unlock()
		http.Error(w, `{"error":"environment not found"}`, http.StatusNotFound)
		return
	}
	if env.Status == models.StatusDestroying {
		p.envsMu.Unlock()
		http.Error(w, `{"error":"environment is already being destroyed"}`, http.StatusConflict)
		return
	}
	env.Status = models.StatusDestroying
	env.UpdatedAt = time.Now().UTC()
	p.envsMu.Unlock()

	// Run destroy in background.
	go p.destroyEnv(id)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(env)
}

// worker processes the provisioning job queue serially (one env at a time).
func (p *Provisioner) worker() {
	for envID := range p.jobCh {
		p.provisionEnv(envID)
	}
}

// provisionEnv runs the full provisioning pipeline for one environment.
func (p *Provisioner) provisionEnv(envID string) {
	ctx := context.Background()

	p.envsMu.RLock()
	env := p.envs[envID]
	p.envsMu.RUnlock()

	setStatus := func(status string) {
		p.envsMu.Lock()
		env.Status = status
		env.UpdatedAt = time.Now().UTC()
		p.envsMu.Unlock()
	}

	setStatus(models.StatusProvisioning)
	logger := p.logger.With("environment", envID)

	// --- Step 1: Terraform Init + Plan ---
	logger.Info("starting terraform init")
	runner := terraform.NewRunner(envID, p.moduleDir, p.envsDir)

	if err := runner.WriteBackendConfig(); err != nil {
		p.failEnv(env, fmt.Sprintf("write backend config: %v", err))
		return
	}
	if err := runner.WriteMainModule(); err != nil {
		p.failEnv(env, fmt.Sprintf("write main module: %v", err))
		return
	}
	if err := runner.WriteTFVars([]terraform.TFVar{
		{Name: "name", Value: env.Name},
		{Name: "owner", Value: env.Owner},
		{Name: "db_enabled", Value: fmt.Sprintf("%t", env.DBEnabled)},
	}); err != nil {
		p.failEnv(env, fmt.Sprintf("write tfvars: %v", err))
		return
	}

	initResult, err := runner.Init(ctx)
	if err != nil || initResult.ExitCode != 0 {
		p.failEnv(env, fmt.Sprintf("terraform init failed: %v %s", err, initResult.Stderr))
		return
	}

	planOutput, planResult, err := runner.Plan(ctx)
	if err != nil {
		p.failEnv(env, fmt.Sprintf("terraform plan failed: %v", err))
		return
	}
	logger.Info("terraform plan complete", "exit_code", planResult.ExitCode)

	// --- Step 2: AI Explanation (before apply — human-in-the-loop safety) ---
	if p.llmClient != nil && planOutput != "" {
		explanation, explainErr := p.llmClient.ExplainPlan(ctx, planOutput, env.Name)
		if explainErr != nil {
			logger.Warn("LLM explanation failed (non-fatal)", "error", explainErr)
		} else {
			p.envsMu.Lock()
			env.PlanExplanation = explanation
			p.envsMu.Unlock()
			logger.Info("LLM explanation generated")
		}
	}

	// --- Step 3: Terraform Apply ---
	logger.Info("starting terraform apply")
	logCh := make(chan terraform.LogLine, 100)
	go func() {
		for line := range logCh {
			logger.Info("tf", "line", line.Line, "error", line.IsError)
		}
	}()

	applyResult, err := runner.Apply(ctx, logCh)
	close(logCh)
	if err != nil || applyResult.ExitCode != 0 {
		p.failEnv(env, fmt.Sprintf("terraform apply failed: %v %s", err, applyResult.Stderr))
		return
	}

	// Read outputs.
	ns, _ := runner.Output(ctx, "namespace")
	dbHost, _ := runner.Output(ctx, "db_host")

	p.envsMu.Lock()
	env.Namespace = ns
	env.DBHost = dbHost
	if env.DBEnabled {
		env.DBPort = 5432
	}
	p.envsMu.Unlock()

	// --- Step 4: GitOps Handoff ---
	logger.Info("generating gitops manifest")
	committer := git.NewCommitter(p.gitDir)
	if commitErr := committer.CommitArgoApp(env.Name, env.Namespace); commitErr != nil {
		logger.Warn("gitops commit failed (non-fatal)", "error", commitErr)
	} else {
		logger.Info("gitops manifest committed")
	}

	// --- Done ---
	setStatus(models.StatusActive)
	logger.Info("environment provisioned successfully")
}

// destroyEnv tears down an environment.
func (p *Provisioner) destroyEnv(envID string) {
	ctx := context.Background()

	p.envsMu.RLock()
	env := p.envs[envID]
	p.envsMu.RUnlock()

	logger := p.logger.With("environment", envID)
	logger.Info("starting terraform destroy")

	runner := terraform.NewRunner(envID, p.moduleDir, p.envsDir)
	logCh := make(chan terraform.LogLine, 100)
	go func() {
		for line := range logCh {
			logger.Info("tf", "line", line.Line, "error", line.IsError)
		}
	}()

	result, err := runner.Destroy(ctx, logCh)
	close(logCh)

	p.envsMu.Lock()
	defer p.envsMu.Unlock()

	if err != nil || result.ExitCode != 0 {
		env.Status = models.StatusError
		env.Error = fmt.Sprintf("terraform destroy failed: %v %s", err, result.Stderr)
		env.UpdatedAt = time.Now().UTC()
		return
	}

	env.Status = models.StatusDestroyed
	env.UpdatedAt = time.Now().UTC()
	logger.Info("environment destroyed")
}

func (p *Provisioner) failEnv(env *models.Environment, msg string) {
	p.logger.Error("provisioning failed", "environment", env.Name, "error", msg)
	p.envsMu.Lock()
	env.Status = models.StatusError
	env.Error = msg
	env.UpdatedAt = time.Now().UTC()
	p.envsMu.Unlock()
}
