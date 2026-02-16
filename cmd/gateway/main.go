package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"inner-companion/internal/agentdef"
	"inner-companion/internal/agent"
	"inner-companion/internal/anthropic"
	"inner-companion/internal/audit"
	"inner-companion/internal/classify"
	"inner-companion/internal/gateway"
	"inner-companion/internal/sandbox"
	"inner-companion/internal/session"
	"inner-companion/internal/tool"
)

const defaultSystemPrompt = `You are a helpful coding assistant. You have access to tools for reading files and executing bash commands. Use them to help the user with their requests.`

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

	sessionDB := os.Getenv("SESSION_DB")
	if sessionDB == "" {
		sessionDB = "sessions.db"
	}

	auditLogPath := os.Getenv("AUDIT_LOG")
	if auditLogPath == "" {
		auditLogPath = "audit.jsonl"
	}

	gatewayToken := os.Getenv("GATEWAY_TOKEN")

	// Load agent definition if specified
	systemPrompt := defaultSystemPrompt
	allowedTools := []string{"read", "bash"}
	autonomy := string(agentdef.AutonomySupervised)

	if agentDefPath := os.Getenv("AGENT_DEF"); agentDefPath != "" {
		def, err := agentdef.LoadFromFile(agentDefPath)
		if err != nil {
			log.Fatalf("agent def: %v", err)
		}
		if err := def.Validate(); err != nil {
			log.Fatalf("agent def: %v", err)
		}
		if def.SystemPrompt != "" {
			systemPrompt = def.SystemPrompt
		}
		if len(def.AllowedTools) > 0 {
			allowedTools = def.AllowedTools
		}
		autonomy = string(def.Autonomy)
		log.Printf("loaded agent definition: %s (autonomy=%s)", def.Name, def.Autonomy)
	}

	// Build audit logger
	var auditLogger audit.Logger
	auditLogger, err := audit.NewFileLogger(auditLogPath)
	if err != nil {
		log.Printf("WARNING: audit logger: %v, using nop logger", err)
		auditLogger = audit.NopLogger{}
	}
	defer auditLogger.Close()

	// Build restricted HTTP transport
	networkAllowlist := os.Getenv("NETWORK_ALLOWLIST")
	if networkAllowlist == "" {
		networkAllowlist = "api.anthropic.com"
	}
	hosts := strings.Split(networkAllowlist, ",")
	for i := range hosts {
		hosts[i] = strings.TrimSpace(hosts[i])
	}
	transport := anthropic.NewRestrictedTransport(hosts)
	httpClient := &http.Client{Transport: transport}
	log.Printf("network allowlist: %v", hosts)

	// Build Anthropic client with retry
	llmClient := anthropic.NewRetryClient(
		anthropic.NewClient(anthropic.ClientConfig{
			APIKey:     apiKey,
			Model:      model,
			HTTPClient: httpClient,
		}),
	)

	// Build tool registry with classified bash
	bashTool := tool.NewBashTool(workspaceDir)
	classifiedBash := tool.NewClassifiedBashTool(bashTool, classify.Classify, autonomy, auditLogger)
	classifiedBash.SetWorkspaceDir(workspaceDir)
	if os.Getenv("AUTO_COMMIT") == "true" {
		classifiedBash.SetAutoCommit(true)
		log.Printf("auto-commit enabled")
	}

	var executors []tool.Executor
	for _, t := range allowedTools {
		switch t {
		case "read":
			executors = append(executors, tool.ReadTool{})
		case "bash":
			executors = append(executors, classifiedBash)
		}
	}
	registry := tool.NewRegistry(executors...)

	// Build session store
	store, storeErr := session.NewSQLiteStore(sessionDB)
	if storeErr != nil {
		log.Fatalf("session store: %v", storeErr)
	}
	defer store.Close()

	// Build memory manager with LLM summarizer
	summarizer := session.NewLLMSummarizer(llmClient)
	memoryManager := session.NewMemoryManager(store, summarizer, 100000)

	// Build mode manager with health check
	modeManager := gateway.NewModeManager(auditLogger)
	modeManager.SetHealthCheck(func(ctx context.Context) bool {
		_, err := llmClient.CreateMessage(ctx, anthropic.MessagesRequest{
			System:    "ping",
			Messages:  []anthropic.Message{{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: "ping"}}}},
			MaxTokens: 1,
		})
		return err == nil
	})

	// Build agent runner with session persistence
	baseRunner := agent.NewAnthropicRunner(llmClient, registry, systemPrompt)
	runner := gateway.NewSessionRunner(baseRunner, store, memoryManager, modeManager)

	svc := gateway.NewService(runner)
	var handler http.Handler = gateway.NewHTTPHandler(svc)

	// Wrap with auth middleware
	handler = gateway.NewAuthMiddleware(gatewayToken, handler)

	// Serve static web files
	mux := http.NewServeMux()
	mux.Handle("/web/", http.StripPrefix("/web/", http.FileServer(http.Dir("web/static"))))
	mux.Handle("/", handler)

	addr := "127.0.0.1:" + port
	log.Printf("gateway listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
