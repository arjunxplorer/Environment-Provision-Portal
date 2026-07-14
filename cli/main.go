package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"portal/api/models"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	baseURL := os.Getenv("PORTAL_API_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	client := &http.Client{Timeout: 30 * time.Second}

	switch os.Args[1] {
	case "create":
		cmdCreate(client, baseURL, os.Args[2:])
	case "status":
		cmdStatus(client, baseURL, os.Args[2:])
	case "list":
		cmdList(client, baseURL)
	case "destroy":
		cmdDestroy(client, baseURL, os.Args[2:])
	case "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Environment Provisioning Portal CLI

Usage:
  portal create <name> --owner <email> [--db]   Create a new environment
  portal status <name>                          Check environment status
  portal list                                   List all environments
  portal destroy <name>                         Destroy an environment
  portal help                                   Show this help

Environment variables:
  PORTAL_API_URL   API base URL (default: http://localhost:8080)`)
}

func cmdCreate(client *http.Client, baseURL string, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: portal create <name> --owner <email> [--db]")
		os.Exit(1)
	}

	name := args[0]
	owner := ""
	dbEnabled := false

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--owner":
			if i+1 < len(args) {
				owner = args[i+1]
				i++
			}
		case "--db":
			dbEnabled = true
		}
	}

	if owner == "" {
		fmt.Fprintln(os.Stderr, "error: --owner is required")
		os.Exit(1)
	}

	req := models.CreateEnvironmentRequest{
		Name:      name,
		Owner:     owner,
		DBEnabled: dbEnabled,
	}

	body, _ := json.Marshal(req)
	resp, err := client.Post(baseURL+"/environments", "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		printError(resp)
		os.Exit(1)
	}

	var env models.Environment
	json.NewDecoder(resp.Body).Decode(&env)

	fmt.Printf("✓ Environment %q provisioning started\n", env.Name)
	fmt.Printf("  Status:  %s\n", env.Status)
	fmt.Printf("  Owner:   %s\n", env.Owner)
	fmt.Printf("  DB:      %v\n", env.DBEnabled)
	fmt.Println()
	fmt.Printf("  Poll with: portal status %s\n", env.Name)
}

func cmdStatus(client *http.Client, baseURL string, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: portal status <name>")
		os.Exit(1)
	}

	resp, err := client.Get(baseURL + "/environments/" + args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		fmt.Fprintf(os.Stderr, "environment %q not found\n", args[0])
		os.Exit(1)
	}
	if resp.StatusCode != http.StatusOK {
		printError(resp)
		os.Exit(1)
	}

	var env models.Environment
	json.NewDecoder(resp.Body).Decode(&env)

	statusIcon := "⏳"
	switch env.Status {
	case models.StatusActive:
		statusIcon = "✅"
	case models.StatusError:
		statusIcon = "❌"
	case models.StatusDestroyed:
		statusIcon = "🗑️"
	}

	fmt.Printf("%s Environment: %s\n", statusIcon, env.Name)
	fmt.Printf("   Status:    %s\n", env.Status)
	fmt.Printf("   Owner:     %s\n", env.Owner)
	fmt.Printf("   Created:   %s\n", env.CreatedAt.Format(time.RFC3339))

	if env.Namespace != "" {
		fmt.Printf("   Namespace: %s\n", env.Namespace)
	}
	if env.DBHost != "" {
		fmt.Printf("   DB Host:   %s:%d\n", env.DBHost, env.DBPort)
	}
	if env.PlanExplanation != "" {
		fmt.Println()
		fmt.Println("   📋 AI Plan Explanation:")
		fmt.Println("   " + env.PlanExplanation)
	}
	if env.Error != "" {
		fmt.Printf("\n   ⚠️  Error: %s\n", env.Error)
	}
}

func cmdList(client *http.Client, baseURL string) {
	resp, err := client.Get(baseURL + "/environments")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var envs []models.Environment
	json.NewDecoder(resp.Body).Decode(&envs)

	if len(envs) == 0 {
		fmt.Println("No environments provisioned.")
		return
	}

	fmt.Printf("%-20s %-12s %-25s %-8s\n", "NAME", "STATUS", "OWNER", "DB")
	fmt.Println("─────────────────────────────────────────────────────────────────────")
	for _, env := range envs {
		dbStr := "no"
		if env.DBEnabled {
			dbStr = "yes"
		}
		fmt.Printf("%-20s %-12s %-25s %-8s\n", env.Name, env.Status, env.Owner, dbStr)
	}
}

func cmdDestroy(client *http.Client, baseURL string, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: portal destroy <name>")
		os.Exit(1)
	}

	req, _ := http.NewRequest("DELETE", baseURL+"/environments/"+args[0], nil)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		fmt.Fprintf(os.Stderr, "environment %q not found\n", args[0])
		os.Exit(1)
	}
	if resp.StatusCode != http.StatusOK {
		printError(resp)
		os.Exit(1)
	}

	fmt.Printf("✓ Environment %q destruction initiated\n", args[0])
}

func printError(resp *http.Response) {
	body, _ := io.ReadAll(resp.Body)
	fmt.Fprintf(os.Stderr, "API error (status %d): %s\n", resp.StatusCode, string(body))
}
