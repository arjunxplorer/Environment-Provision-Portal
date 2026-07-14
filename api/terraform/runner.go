package terraform

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Runner wraps the Terraform CLI for a single environment workspace.
type Runner struct {
	ModuleDir string // path to the TF module (infra/modules/environment)
	EnvDir    string // path to per-env tfvars dir (infra/envs/<name>)
	WorkDir   string // working directory for terraform commands
}

// NewRunner creates a Runner for the given environment name.
func NewRunner(envName, moduleDir, envsDir string) *Runner {
	envDir := filepath.Join(envsDir, envName)
	return &Runner{
		ModuleDir: moduleDir,
		EnvDir:    envDir,
		WorkDir:   envDir,
	}
}

// TFVar represents a single terraform variable assignment.
type TFVar struct {
	Name  string
	Value string
}

// WriteTFVars writes a .tfvars file for this environment.
func (r *Runner) WriteTFVars(vars []TFVar) error {
	if err := os.MkdirAll(r.EnvDir, 0o755); err != nil {
		return fmt.Errorf("create env dir: %w", err)
	}

	var buf bytes.Buffer
	for _, v := range vars {
		// Simple quoting — works for strings and bools.
		if v.Value == "true" || v.Value == "false" {
			fmt.Fprintf(&buf, "%s = %s\n", v.Name, v.Value)
		} else {
			fmt.Fprintf(&buf, "%s = %q\n", v.Name, v.Value)
		}
	}

	path := filepath.Join(r.EnvDir, "terraform.tfvars")
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// WriteBackendConfig writes a backend config file to isolate state per env.
func (r *Runner) WriteBackendConfig() error {
	if err := os.MkdirAll(r.EnvDir, 0o755); err != nil {
		return err
	}
	// Local backend with per-env state file — simple isolation for kind.
	cfg := fmt.Sprintf(`terraform {
  backend "local" {
    path = "terraform.tfstate"
  }
}
`)
	path := filepath.Join(r.EnvDir, "backend.tf")
	return os.WriteFile(path, []byte(cfg), 0o644)
}

// WriteMainModule generates a main.tf that calls the environment module.
func (r *Runner) WriteMainModule() error {
	if err := os.MkdirAll(r.EnvDir, 0o755); err != nil {
		return err
	}

	absModule, err := filepath.Abs(r.ModuleDir)
	if err != nil {
		return err
	}

	main := fmt.Sprintf(`terraform {
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
  source = %q

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
`, absModule)

	path := filepath.Join(r.EnvDir, "main.tf")
	return os.WriteFile(path, []byte(main), 0o644)
}

// LogLine is a single line of terraform output.
type LogLine struct {
	Line    string
	IsError bool
}

// RunResult captures the outcome of a terraform command.
type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Error    error
}

// Init runs `terraform init` in the env working directory.
func (r *Runner) Init(ctx context.Context) (*RunResult, error) {
	return r.run(ctx, nil, "init", "-input=false", "-no-color")
}

// Plan runs `terraform plan` and returns the output.
func (r *Runner) Plan(ctx context.Context) (string, *RunResult, error) {
	result, err := r.run(ctx, nil, "plan", "-input=false", "-no-color", "-detailed-exitcode")
	if err != nil {
		return "", result, err
	}
	return result.Stdout, result, nil
}

// Apply runs `terraform apply` with auto-approve.
func (r *Runner) Apply(ctx context.Context, logCh chan<- LogLine) (*RunResult, error) {
	return r.runStreaming(ctx, logCh, "apply", "-auto-approve", "-input=false", "-no-color")
}

// Destroy runs `terraform destroy` with auto-approve.
func (r *Runner) Destroy(ctx context.Context, logCh chan<- LogLine) (*RunResult, error) {
	return r.runStreaming(ctx, logCh, "destroy", "-auto-approve", "-input=false", "-no-color")
}

// Output reads a terraform output value by name.
func (r *Runner) Output(ctx context.Context, name string) (string, error) {
	result, err := r.run(ctx, nil, "output", "-raw", "-no-color", name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

// run executes a terraform command and captures all output.
func (r *Runner) run(ctx context.Context, logCh chan<- LogLine, args ...string) (*RunResult, error) {
	cmd := exec.CommandContext(ctx, "terraform", args...)
	cmd.Dir = r.WorkDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := &RunResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		result.Error = err
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("terraform %s: %w", args[0], err)
	}
	return result, nil
}

// runStreaming executes terraform and streams stdout lines to logCh.
func (r *Runner) runStreaming(ctx context.Context, logCh chan<- LogLine, args ...string) (*RunResult, error) {
	cmd := exec.CommandContext(ctx, "terraform", args...)
	cmd.Dir = r.WorkDir

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start terraform: %w", err)
	}

	// Stream stdout lines.
	var stdoutBuf bytes.Buffer
	var mu sync.Mutex
	scanner := bufio.NewScanner(stdoutPipe)
	for scanner.Scan() {
		line := scanner.Text()
		mu.Lock()
		stdoutBuf.WriteString(line + "\n")
		mu.Unlock()
		if logCh != nil {
			logCh <- LogLine{Line: line}
		}
	}

	err = cmd.Wait()
	result := &RunResult{
		Stderr: stderr.String(),
	}
	mu.Lock()
	result.Stdout = stdoutBuf.String()
	mu.Unlock()

	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		result.Error = err
		if logCh != nil {
			logCh <- LogLine{Line: fmt.Sprintf("terraform exited with code %d", result.ExitCode), IsError: true}
		}
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("terraform %s: %w", args[0], err)
	}
	return result, nil
}
