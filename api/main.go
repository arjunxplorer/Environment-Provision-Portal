package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"portal/api/handlers"
	"portal/api/llm"
	"portal/api/middleware"
)

func main() {
	// --- 12-factor: structured logging to stdout ---
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// --- 12-factor: config from env vars ---
	port := envOrDefault("PORT", "8080")
	moduleDir := envOrDefault("TERRAFORM_MODULE_DIR", "../infra/modules/environment")
	envsDir := envOrDefault("TERRAFORM_ENVS_DIR", "../infra/envs")
	gitDir := envOrDefault("GIT_REPO_DIR", "..")

	// --- LLM client (nil if LLM_API_KEY not set) ---
	llmClient := llm.NewClient()
	if llmClient == nil {
		logger.Warn("LLM_API_KEY not set — AI plan explanations disabled")
	}

	// --- Provisioner ---
	prov := handlers.NewProvisioner(moduleDir, envsDir, gitDir, llmClient, logger)

	// --- Routes ---
	mux := http.NewServeMux()

	// Health check.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	// Environment CRUD.
	mux.HandleFunc("POST /environments", prov.Create)
	mux.HandleFunc("GET /environments", prov.List)
	mux.HandleFunc("GET /environments/{id}", prov.Status)
	mux.HandleFunc("DELETE /environments/{id}", prov.Destroy)

	// --- Middleware stack ---
	handler := middleware.CORS(
		middleware.RequestID(
			middleware.Recovery(logger)(
				middleware.Logger(logger)(mux),
			),
		),
	)

	// --- Start server ---
	logger.Info("starting Environment Provisioning Portal", "port", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
