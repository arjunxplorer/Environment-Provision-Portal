package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Client wraps an LLM API for generating Terraform plan explanations.
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// NewClient creates an LLM client from environment variables.
// Expects: LLM_API_KEY, LLM_BASE_URL (optional), LLM_MODEL (optional).
func NewClient() *Client {
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		return nil // LLM disabled — not an error.
	}

	baseURL := os.Getenv("LLM_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}

	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// ExplainPlan sends terraform plan output to the LLM and returns a plain-English
// explanation with risk flags. This is designed as a human-in-the-loop safety check.
func (c *Client) ExplainPlan(ctx context.Context, planOutput, envName string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("LLM client not configured")
	}

	prompt := fmt.Sprintf(`You are a platform engineering assistant reviewing a Terraform plan before it is applied.

Environment: %s

Below is the raw Terraform plan output. Your job:
1. Summarize what infrastructure will be created/modified/destroyed in plain English that a non-Terraform-fluent developer can understand.
2. Flag any risks:
   - Destructive changes (resource deletion, replacement)
   - Public exposure (open security groups, public IPs)
   - Missing resource limits (no CPU/memory limits, no storage caps)
   - Anything that looks unusual or potentially dangerous
3. End with a clear recommendation: SAFE TO APPLY or NEEDS REVIEW (with reasons).

Be concise. Use bullet points. No preamble.

TERRAFORM PLAN OUTPUT:
%s`, envName, planOutput)

	// Use Claude Messages API format.
	reqBody := map[string]interface{}{
		"model":      c.model,
		"max_tokens": 1024,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("LLM request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("LLM API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("LLM returned empty response")
	}

	return result.Content[0].Text, nil
}
