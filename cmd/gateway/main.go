package main

import (
	"log"
	"net/http"
	"os"

	"inner-companion/internal/agent"
	"inner-companion/internal/anthropic"
	"inner-companion/internal/gateway"
	"inner-companion/internal/sandbox"
	"inner-companion/internal/tool"
)

const systemPrompt = `You are a helpful coding assistant. You have access to tools for reading files and executing bash commands. Use them to help the user with their requests.`

func main() {
	// Handle sandbox child process
	if sandbox.IsSandboxChild() {
		sandbox.RunChild()
		return // RunChild calls os.Exit or unix.Exec; this line is unreachable
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = "claude-sonnet-4-5-20250929"
	}

	workspaceDir := os.Getenv("WORKSPACE_DIR")
	if workspaceDir == "" {
		workspaceDir = "."
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Build Anthropic client with retry
	llmClient := anthropic.NewRetryClient(
		anthropic.NewClient(anthropic.ClientConfig{
			APIKey: apiKey,
			Model:  model,
		}),
	)

	// Build tool registry
	registry := tool.NewRegistry(
		tool.ReadTool{},
		tool.NewBashTool(workspaceDir),
	)

	// Build agent runner
	runner := agent.NewAnthropicRunner(llmClient, registry, systemPrompt)

	svc := gateway.NewService(runner)
	h := gateway.NewHTTPHandler(svc)

	addr := "127.0.0.1:" + port
	log.Printf("gateway listening on %s", addr)
	if err := http.ListenAndServe(addr, h); err != nil {
		log.Fatal(err)
	}
}
