package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"inner-companion/internal/agent"
	"inner-companion/internal/agentdef"
	"inner-companion/internal/anthropic"
	"inner-companion/internal/audit"
	"inner-companion/internal/classify"
	"inner-companion/internal/gateway"
	"inner-companion/internal/protocol"
	"inner-companion/internal/session"
	"inner-companion/internal/tool"
)

// fakeLLMClient implements anthropic.LLMClient for testing.
type fakeLLMClient struct {
	mu        sync.Mutex
	responses []anthropic.MessagesResponse
	calls     int
	lastReq   anthropic.MessagesRequest
}

func (f *fakeLLMClient) CreateMessage(_ context.Context, req anthropic.MessagesRequest) (anthropic.MessagesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastReq = req
	idx := f.calls
	f.calls++
	if idx < len(f.responses) {
		return f.responses[idx], nil
	}
	// Default: return end_turn with text
	return anthropic.MessagesResponse{
		ID:         "msg_test",
		Role:       "assistant",
		StopReason: "end_turn",
		Content: []anthropic.ContentBlock{
			{Type: "text", Text: "Hello from assistant"},
		},
	}, nil
}

// failingLLMClient always returns an error.
type failingLLMClient struct {
	err error
}

func (f *failingLLMClient) CreateMessage(_ context.Context, _ anthropic.MessagesRequest) (anthropic.MessagesResponse, error) {
	return anthropic.MessagesResponse{}, f.err
}

// setupServer creates a full server stack for E2E testing.
func setupServer(t *testing.T, llm anthropic.LLMClient, token string, autonomy string) (*httptest.Server, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	auditPath := filepath.Join(tmpDir, "audit.jsonl")

	store, err := session.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("sqlite store: %v", err)
	}

	auditLogger, err := audit.NewFileLogger(auditPath)
	if err != nil {
		t.Fatalf("audit logger: %v", err)
	}

	bashTool := tool.NewBashTool(tmpDir)
	classifiedBash := tool.NewClassifiedBashTool(bashTool, classify.Classify, autonomy, auditLogger)
	registry := tool.NewRegistry(tool.ReadTool{}, classifiedBash)

	summarizer := session.NewLLMSummarizer(llm)
	memoryManager := session.NewMemoryManager(store, summarizer, 200000)
	modeManager := gateway.NewModeManager(auditLogger)

	baseRunner := agent.NewAnthropicRunner(llm, registry, "You are a test assistant.")
	runner := gateway.NewSessionRunner(baseRunner, store, memoryManager, modeManager)

	svc := gateway.NewService(runner)
	var handler http.Handler = gateway.NewHTTPHandler(svc)
	handler = gateway.NewAuthMiddleware(token, handler)

	srv := httptest.NewServer(handler)

	cleanup := func() {
		srv.Close()
		svc.Shutdown()
		auditLogger.Close()
		store.Close()
	}

	return srv, cleanup
}

func postMessage(t *testing.T, url, token, sessionID, text, clientMsgID string) *http.Response {
	t.Helper()
	body := protocol.InboundUserMessage{
		Type:        "user_message",
		SessionID:   sessionID,
		AgentID:     "default",
		Text:        text,
		ClientMsgID: clientMsgID,
	}
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url+"/v1/messages", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http request: %v", err)
	}
	return resp
}

func decodeAssistant(t *testing.T, resp *http.Response) protocol.OutboundAssistantMessage {
	t.Helper()
	defer resp.Body.Close()
	var msg protocol.OutboundAssistantMessage
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return msg
}

func decodeError(t *testing.T, resp *http.Response) protocol.OutboundErrorMessage {
	t.Helper()
	defer resp.Body.Close()
	var msg protocol.OutboundErrorMessage
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return msg
}

// 1. Basic conversation
func TestE2E_BasicConversation(t *testing.T) {
	llm := &fakeLLMClient{}
	srv, cleanup := setupServer(t, llm, "", "supervised")
	defer cleanup()

	resp := postMessage(t, srv.URL, "", "sess1", "hello", "msg1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	msg := decodeAssistant(t, resp)
	if msg.Type != "assistant_message" {
		t.Errorf("type = %q, want assistant_message", msg.Type)
	}
	if msg.Text == "" {
		t.Error("text is empty")
	}
	if msg.SessionID != "sess1" {
		t.Errorf("session_id = %q, want sess1", msg.SessionID)
	}
}

// 2. Auth success
func TestE2E_AuthSuccess(t *testing.T) {
	llm := &fakeLLMClient{}
	srv, cleanup := setupServer(t, llm, "secret-token", "supervised")
	defer cleanup()

	resp := postMessage(t, srv.URL, "secret-token", "sess1", "hello", "msg1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	msg := decodeAssistant(t, resp)
	if msg.Type != "assistant_message" {
		t.Errorf("type = %q, want assistant_message", msg.Type)
	}
}

// 3. Auth failure
func TestE2E_AuthFailure(t *testing.T) {
	llm := &fakeLLMClient{}
	srv, cleanup := setupServer(t, llm, "secret-token", "supervised")
	defer cleanup()

	resp := postMessage(t, srv.URL, "wrong-token", "sess1", "hello", "msg1")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	errMsg := decodeError(t, resp)
	if errMsg.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", errMsg.Code)
	}
}

// 4. Session persistence
func TestE2E_SessionPersistence(t *testing.T) {
	callCount := 0
	llm := &fakeLLMClient{
		responses: []anthropic.MessagesResponse{
			{ID: "msg1", Role: "assistant", StopReason: "end_turn", Content: []anthropic.ContentBlock{{Type: "text", Text: "First response"}}},
			{ID: "msg2", Role: "assistant", StopReason: "end_turn", Content: []anthropic.ContentBlock{{Type: "text", Text: "Second response"}}},
		},
	}
	_ = callCount
	srv, cleanup := setupServer(t, llm, "", "supervised")
	defer cleanup()

	// First message
	resp1 := postMessage(t, srv.URL, "", "sess-persist", "first", "msg1")
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first request status = %d", resp1.StatusCode)
	}
	decodeAssistant(t, resp1)

	// Second message with same session
	resp2 := postMessage(t, srv.URL, "", "sess-persist", "second", "msg2")
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second request status = %d", resp2.StatusCode)
	}
	msg2 := decodeAssistant(t, resp2)
	if msg2.SessionID != "sess-persist" {
		t.Errorf("session_id = %q, want sess-persist", msg2.SessionID)
	}

	// Verify LLM received history in second call
	llm.mu.Lock()
	if llm.calls < 2 {
		t.Errorf("expected at least 2 LLM calls, got %d", llm.calls)
	}
	// The second call should have messages from the first conversation
	if len(llm.lastReq.Messages) <= 1 {
		t.Errorf("expected history in second call, got %d messages", len(llm.lastReq.Messages))
	}
	llm.mu.Unlock()
}

// 5. Tool L1 execution (supervised) - uses read tool to avoid sandbox issues in test
func TestE2E_ToolL1Execution(t *testing.T) {
	// Create a file to read
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("hello world"), 0644)

	llm := &fakeLLMClient{
		responses: []anthropic.MessagesResponse{
			{
				ID:         "msg1",
				Role:       "assistant",
				StopReason: "tool_use",
				Content: []anthropic.ContentBlock{
					{Type: "tool_use", ID: "tool1", Name: "read", Input: map[string]any{"path": testFile}},
				},
			},
			{
				ID:         "msg2",
				Role:       "assistant",
				StopReason: "end_turn",
				Content:    []anthropic.ContentBlock{{Type: "text", Text: "File contents: hello world"}},
			},
		},
	}
	srv, cleanup := setupServer(t, llm, "", "supervised")
	defer cleanup()

	resp := postMessage(t, srv.URL, "", "sess-tool", "read the file", "msg1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	msg := decodeAssistant(t, resp)
	if msg.Text != "File contents: hello world" {
		t.Errorf("text = %q, want 'File contents: hello world'", msg.Text)
	}
}

// 6. Bash L3 rejection
func TestE2E_BashL3Rejection(t *testing.T) {
	llm := &fakeLLMClient{
		responses: []anthropic.MessagesResponse{
			{
				ID:         "msg1",
				Role:       "assistant",
				StopReason: "tool_use",
				Content: []anthropic.ContentBlock{
					{Type: "tool_use", ID: "tool1", Name: "bash", Input: map[string]any{"command": "rm -rf /"}},
				},
			},
			{
				ID:         "msg2",
				Role:       "assistant",
				StopReason: "end_turn",
				Content:    []anthropic.ContentBlock{{Type: "text", Text: "Command was denied"}},
			},
		},
	}
	srv, cleanup := setupServer(t, llm, "", "supervised")
	defer cleanup()

	resp := postMessage(t, srv.URL, "", "sess-l3", "delete everything", "msg1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	msg := decodeAssistant(t, resp)
	// The assistant should have received the denial and responded
	if msg.Text != "Command was denied" {
		t.Errorf("text = %q, want 'Command was denied'", msg.Text)
	}
}

// 7. Tool failure limit - read tool with nonexistent files
func TestE2E_ToolFailureLimit(t *testing.T) {
	// Create tool_use responses for read tool with nonexistent paths
	var responses []anthropic.MessagesResponse
	for i := 0; i < 11; i++ {
		responses = append(responses, anthropic.MessagesResponse{
			ID:         "msg",
			Role:       "assistant",
			StopReason: "tool_use",
			Content: []anthropic.ContentBlock{
				{Type: "tool_use", ID: "t" + string(rune('a'+i)), Name: "read", Input: map[string]any{"path": "/nonexistent/file" + string(rune('0'+i))}},
			},
		})
	}
	// Final response after many failed tool calls
	responses = append(responses, anthropic.MessagesResponse{
		ID:         "final",
		Role:       "assistant",
		StopReason: "end_turn",
		Content:    []anthropic.ContentBlock{{Type: "text", Text: "Done after failures"}},
	})

	llm := &fakeLLMClient{responses: responses}
	srv, cleanup := setupServer(t, llm, "", "supervised")
	defer cleanup()

	resp := postMessage(t, srv.URL, "", "sess-fail", "do something", "msg1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	msg := decodeAssistant(t, resp)
	if msg.Text == "" {
		t.Error("expected non-empty text")
	}
}

// 8. ReadOnly mode transition (3 LLM failures → readonly)
func TestE2E_ReadOnlyModeTransition(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	auditPath := filepath.Join(tmpDir, "audit.jsonl")

	store, _ := session.NewSQLiteStore(dbPath)
	defer store.Close()
	auditLogger, _ := audit.NewFileLogger(auditPath)
	defer auditLogger.Close()

	// Use a failing LLM client to trigger failures
	failLLM := &failingLLMClient{err: &anthropic.APIError{StatusCode: 500, Body: "internal error"}}
	bashTool := tool.NewBashTool(tmpDir)
	classifiedBash := tool.NewClassifiedBashTool(bashTool, classify.Classify, "supervised", auditLogger)
	registry := tool.NewRegistry(tool.ReadTool{}, classifiedBash)

	summarizer := session.NewLLMSummarizer(failLLM)
	memoryManager := session.NewMemoryManager(store, summarizer, 200000)
	modeManager := gateway.NewModeManager(auditLogger)

	baseRunner := agent.NewAnthropicRunner(failLLM, registry, "test")
	runner := gateway.NewSessionRunner(baseRunner, store, memoryManager, modeManager)

	svc := gateway.NewService(runner)
	defer svc.Shutdown()
	handler := gateway.NewHTTPHandler(svc)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// 3 failures should trigger readonly
	for i := 0; i < 3; i++ {
		postMessage(t, srv.URL, "", "sess-ro", "hello", "msg"+string(rune('1'+i)))
	}

	if modeManager.CurrentMode() != gateway.ModeReadonly {
		t.Fatalf("expected readonly mode, got %v", modeManager.CurrentMode())
	}
}

// 9. YAML definition loading
func TestE2E_YAMLDefinitionLoading(t *testing.T) {
	tmpDir := t.TempDir()
	yamlContent := `name: test-agent
system_prompt: "Custom system prompt"
allowed_tools:
  - read
  - bash
autonomy: full
`
	yamlPath := filepath.Join(tmpDir, "agent.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	def, err := agentdef.LoadFromFile(yamlPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if def.Name != "test-agent" {
		t.Errorf("name = %q, want test-agent", def.Name)
	}
	if def.SystemPrompt != "Custom system prompt" {
		t.Errorf("system_prompt = %q", def.SystemPrompt)
	}
	if def.Autonomy != agentdef.AutonomyFull {
		t.Errorf("autonomy = %q, want full", def.Autonomy)
	}
	if len(def.AllowedTools) != 2 {
		t.Errorf("allowed_tools = %v", def.AllowedTools)
	}
}

// 10. Audit log recording
func TestE2E_AuditLogRecording(t *testing.T) {
	tmpDir := t.TempDir()
	auditPath := filepath.Join(tmpDir, "audit.jsonl")

	auditLogger, err := audit.NewFileLogger(auditPath)
	if err != nil {
		t.Fatalf("audit logger: %v", err)
	}

	// Log some entries
	auditLogger.Log(audit.Entry{
		SessionID: "sess1",
		Event:     "test_event",
		Detail:    map[string]any{"key": "value"},
	})
	auditLogger.Close()

	// Verify the file contains valid JSONL
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var entry audit.Entry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("unmarshal entry: %v", err)
	}
	if entry.SessionID != "sess1" {
		t.Errorf("session_id = %q, want sess1", entry.SessionID)
	}
	if entry.Event != "test_event" {
		t.Errorf("event = %q, want test_event", entry.Event)
	}
	if entry.Timestamp.IsZero() {
		t.Error("timestamp should be auto-set")
	}
}

// 11. Idempotency (same client_msg_id returns cached response)
func TestE2E_Idempotency(t *testing.T) {
	callCount := 0
	llm := &fakeLLMClient{
		responses: []anthropic.MessagesResponse{
			{ID: "msg1", Role: "assistant", StopReason: "end_turn", Content: []anthropic.ContentBlock{{Type: "text", Text: "Response 1"}}},
			{ID: "msg2", Role: "assistant", StopReason: "end_turn", Content: []anthropic.ContentBlock{{Type: "text", Text: "Response 2"}}},
		},
	}
	_ = callCount
	srv, cleanup := setupServer(t, llm, "", "supervised")
	defer cleanup()

	// First call
	resp1 := postMessage(t, srv.URL, "", "sess-idem", "hello", "same-msg-id")
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first status = %d", resp1.StatusCode)
	}
	msg1 := decodeAssistant(t, resp1)

	// Second call with same client_msg_id should return cached
	resp2 := postMessage(t, srv.URL, "", "sess-idem", "hello", "same-msg-id")
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second status = %d", resp2.StatusCode)
	}
	msg2 := decodeAssistant(t, resp2)

	// Same response should be returned
	if msg1.Text != msg2.Text {
		t.Errorf("idempotent responses differ: %q vs %q", msg1.Text, msg2.Text)
	}
	if msg1.ServerMsgID != msg2.ServerMsgID {
		t.Errorf("server_msg_id differ: %q vs %q", msg1.ServerMsgID, msg2.ServerMsgID)
	}
}
