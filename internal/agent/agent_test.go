package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"inner-companion/internal/anthropic"
	"inner-companion/internal/protocol"
	"inner-companion/internal/tool"
)

type fakeLLMClient struct {
	responses []anthropic.MessagesResponse
	errors    []error
	calls     []anthropic.MessagesRequest
	callIdx   int
}

func (f *fakeLLMClient) CreateMessage(_ context.Context, req anthropic.MessagesRequest) (anthropic.MessagesResponse, error) {
	f.calls = append(f.calls, req)
	idx := f.callIdx
	f.callIdx++
	if idx < len(f.errors) && f.errors[idx] != nil {
		return anthropic.MessagesResponse{}, f.errors[idx]
	}
	if idx < len(f.responses) {
		return f.responses[idx], nil
	}
	return anthropic.MessagesResponse{}, errors.New("no more responses configured")
}

type fakeExecutor struct {
	name    string
	results []tool.Result
	callIdx int
}

func (f *fakeExecutor) Name() string { return f.name }
func (f *fakeExecutor) Execute(_ context.Context, _ map[string]any) tool.Result {
	idx := f.callIdx
	f.callIdx++
	if idx < len(f.results) {
		return f.results[idx]
	}
	return tool.Result{Output: "no result configured", IsError: true}
}

func makeReq() protocol.AgentRequest {
	return protocol.AgentRequest{
		RequestID:    "req-1",
		SessionID:    "sess-1",
		AgentID:      "agent-1",
		InputText:    "hello",
		AllowedTools: []string{"read", "bash"},
		TimeoutSec:   60,
	}
}

func TestAnthropicRunner_TextOnly(t *testing.T) {
	client := &fakeLLMClient{
		responses: []anthropic.MessagesResponse{
			{
				StopReason: "end_turn",
				Content: []anthropic.ContentBlock{
					{Type: "text", Text: "Hello, world!"},
				},
			},
		},
	}
	registry := tool.NewRegistry()
	runner := NewAnthropicRunner(client, registry, "system prompt")

	res, err := runner.Run(context.Background(), makeReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "ok" {
		t.Fatalf("expected ok, got %s", res.Status)
	}
	if res.AssistantText != "Hello, world!" {
		t.Fatalf("expected 'Hello, world!', got %q", res.AssistantText)
	}
	if len(res.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(res.ToolCalls))
	}
}

func TestAnthropicRunner_SingleToolCall(t *testing.T) {
	client := &fakeLLMClient{
		responses: []anthropic.MessagesResponse{
			{
				StopReason: "tool_use",
				Content: []anthropic.ContentBlock{
					{Type: "text", Text: "Let me read that file."},
					{Type: "tool_use", ID: "tu1", Name: "read", Input: map[string]any{"path": "/tmp/f"}},
				},
			},
			{
				StopReason: "end_turn",
				Content: []anthropic.ContentBlock{
					{Type: "text", Text: "file contents: hello"},
				},
			},
		},
	}
	readExec := &fakeExecutor{
		name:    "read",
		results: []tool.Result{{Output: "hello", IsError: false}},
	}
	registry := tool.NewRegistry(readExec)
	runner := NewAnthropicRunner(client, registry, "")

	res, err := runner.Run(context.Background(), makeReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.AssistantText != "file contents: hello" {
		t.Fatalf("expected 'file contents: hello', got %q", res.AssistantText)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].ToolName != "read" {
		t.Fatalf("expected read, got %s", res.ToolCalls[0].ToolName)
	}
	if !res.ToolCalls[0].OK {
		t.Fatal("expected tool call OK")
	}

	// Verify tool result was sent back
	if len(client.calls) != 2 {
		t.Fatalf("expected 2 API calls, got %d", len(client.calls))
	}
	lastCall := client.calls[1]
	if len(lastCall.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(lastCall.Messages))
	}
	toolResultMsg := lastCall.Messages[2]
	if toolResultMsg.Role != "user" {
		t.Fatalf("expected user role for tool result, got %s", toolResultMsg.Role)
	}
	if toolResultMsg.Content[0].Type != "tool_result" {
		t.Fatalf("expected tool_result, got %s", toolResultMsg.Content[0].Type)
	}
}

func TestAnthropicRunner_MultiTurn(t *testing.T) {
	client := &fakeLLMClient{
		responses: []anthropic.MessagesResponse{
			{
				StopReason: "tool_use",
				Content: []anthropic.ContentBlock{
					{Type: "tool_use", ID: "tu1", Name: "read", Input: map[string]any{"path": "/a"}},
				},
			},
			{
				StopReason: "tool_use",
				Content: []anthropic.ContentBlock{
					{Type: "tool_use", ID: "tu2", Name: "bash", Input: map[string]any{"command": "ls"}},
				},
			},
			{
				StopReason: "end_turn",
				Content: []anthropic.ContentBlock{
					{Type: "text", Text: "done"},
				},
			},
		},
	}
	readExec := &fakeExecutor{name: "read", results: []tool.Result{{Output: "data"}}}
	bashExec := &fakeExecutor{name: "bash", results: []tool.Result{{Output: "ok"}}}
	registry := tool.NewRegistry(readExec, bashExec)
	runner := NewAnthropicRunner(client, registry, "")

	res, err := runner.Run(context.Background(), makeReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(res.ToolCalls))
	}
	if res.AssistantText != "done" {
		t.Fatalf("expected 'done', got %q", res.AssistantText)
	}
}

func TestAnthropicRunner_DisallowedTool(t *testing.T) {
	client := &fakeLLMClient{
		responses: []anthropic.MessagesResponse{
			{
				StopReason: "tool_use",
				Content: []anthropic.ContentBlock{
					{Type: "tool_use", ID: "tu1", Name: "bash", Input: map[string]any{"command": "rm -rf /"}},
				},
			},
			{
				StopReason: "end_turn",
				Content: []anthropic.ContentBlock{
					{Type: "text", Text: "ok"},
				},
			},
		},
	}
	bashExec := &fakeExecutor{name: "bash", results: []tool.Result{{Output: "should not run"}}}
	registry := tool.NewRegistry(bashExec)

	req := makeReq()
	req.AllowedTools = []string{"read"} // bash not allowed

	runner := NewAnthropicRunner(client, registry, "")
	res, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].OK {
		t.Fatal("expected tool call NOT ok")
	}
	if !strings.Contains(res.ToolCalls[0].ResultSummary, "not allowed") {
		t.Fatalf("expected 'not allowed', got %q", res.ToolCalls[0].ResultSummary)
	}
	// The executor should not have been called
	if bashExec.callIdx != 0 {
		t.Fatal("bash executor should not have been called")
	}
}

func TestAnthropicRunner_MaxIterations(t *testing.T) {
	// Build enough responses to exceed maxIterations
	responses := make([]anthropic.MessagesResponse, maxIterations+1)
	for i := range responses {
		responses[i] = anthropic.MessagesResponse{
			StopReason: "tool_use",
			Content: []anthropic.ContentBlock{
				{Type: "tool_use", ID: "tu", Name: "read", Input: map[string]any{"path": "/a"}},
			},
		}
	}
	client := &fakeLLMClient{responses: responses}
	readExec := &fakeExecutor{
		name:    "read",
		results: make([]tool.Result, maxIterations+1),
	}
	registry := tool.NewRegistry(readExec)
	runner := NewAnthropicRunner(client, registry, "")

	_, err := runner.Run(context.Background(), makeReq())
	if err == nil {
		t.Fatal("expected error for max iterations")
	}
	if !strings.Contains(err.Error(), "exceeded maximum iterations") {
		t.Fatalf("expected max iterations error, got: %v", err)
	}
}

func TestAnthropicRunner_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := &fakeLLMClient{}
	registry := tool.NewRegistry()
	runner := NewAnthropicRunner(client, registry, "")

	_, err := runner.Run(ctx, makeReq())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestAnthropicRunner_APIError(t *testing.T) {
	client := &fakeLLMClient{
		errors: []error{errors.New("connection refused")},
	}
	registry := tool.NewRegistry()
	runner := NewAnthropicRunner(client, registry, "")

	_, err := runner.Run(context.Background(), makeReq())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "anthropic api") {
		t.Fatalf("expected 'anthropic api' in error, got: %v", err)
	}
}

func TestAnthropicRunner_HistoryPrepended(t *testing.T) {
	client := &fakeLLMClient{
		responses: []anthropic.MessagesResponse{
			{
				StopReason: "end_turn",
				Content:    []anthropic.ContentBlock{{Type: "text", Text: "continued"}},
			},
		},
	}
	registry := tool.NewRegistry()
	runner := NewAnthropicRunner(client, registry, "system prompt")

	req := makeReq()
	req.History = []protocol.HistoryMessage{
		{Role: "user", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "previous question"}}},
		{Role: "assistant", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "previous answer"}}},
	}

	res, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.AssistantText != "continued" {
		t.Fatalf("expected 'continued', got %q", res.AssistantText)
	}

	// Verify history was prepended: history (2) + current user message (1) = 3
	if len(client.calls) != 1 {
		t.Fatalf("expected 1 API call, got %d", len(client.calls))
	}
	msgs := client.calls[0].Messages
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content[0].Text != "previous question" {
		t.Fatalf("expected history user msg first, got %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content[0].Text != "previous answer" {
		t.Fatalf("expected history assistant msg second, got %+v", msgs[1])
	}
	if msgs[2].Role != "user" || msgs[2].Content[0].Text != "hello" {
		t.Fatalf("expected current user msg third, got %+v", msgs[2])
	}
}

func TestAnthropicRunner_SystemPromptOverride(t *testing.T) {
	client := &fakeLLMClient{
		responses: []anthropic.MessagesResponse{
			{
				StopReason: "end_turn",
				Content:    []anthropic.ContentBlock{{Type: "text", Text: "ok"}},
			},
		},
	}
	registry := tool.NewRegistry()
	runner := NewAnthropicRunner(client, registry, "default system")

	req := makeReq()
	req.SystemPrompt = "overridden system"

	runner.Run(context.Background(), req)

	if client.calls[0].System != "overridden system" {
		t.Fatalf("expected overridden system prompt, got %q", client.calls[0].System)
	}
}

func TestAnthropicRunner_NewMessages(t *testing.T) {
	client := &fakeLLMClient{
		responses: []anthropic.MessagesResponse{
			{
				StopReason: "end_turn",
				Content:    []anthropic.ContentBlock{{Type: "text", Text: "reply"}},
			},
		},
	}
	registry := tool.NewRegistry()
	runner := NewAnthropicRunner(client, registry, "")

	res, err := runner.Run(context.Background(), makeReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// NewMessages should contain the user message and assistant response
	if len(res.NewMessages) != 2 {
		t.Fatalf("expected 2 new messages, got %d", len(res.NewMessages))
	}
	if res.NewMessages[0].Role != "user" {
		t.Fatalf("expected user role, got %s", res.NewMessages[0].Role)
	}
	if res.NewMessages[0].Content[0].Text != "hello" {
		t.Fatalf("expected 'hello', got %q", res.NewMessages[0].Content[0].Text)
	}
	if res.NewMessages[1].Role != "assistant" {
		t.Fatalf("expected assistant role, got %s", res.NewMessages[1].Role)
	}
	if res.NewMessages[1].Content[0].Text != "reply" {
		t.Fatalf("expected 'reply', got %q", res.NewMessages[1].Content[0].Text)
	}
}

func TestAnthropicRunner_NewMessagesWithToolUse(t *testing.T) {
	client := &fakeLLMClient{
		responses: []anthropic.MessagesResponse{
			{
				StopReason: "tool_use",
				Content: []anthropic.ContentBlock{
					{Type: "text", Text: "reading"},
					{Type: "tool_use", ID: "tu1", Name: "read", Input: map[string]any{"path": "/x"}},
				},
			},
			{
				StopReason: "end_turn",
				Content:    []anthropic.ContentBlock{{Type: "text", Text: "done"}},
			},
		},
	}
	readExec := &fakeExecutor{name: "read", results: []tool.Result{{Output: "data"}}}
	registry := tool.NewRegistry(readExec)
	runner := NewAnthropicRunner(client, registry, "")

	res, err := runner.Run(context.Background(), makeReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// NewMessages: user + assistant(tool_use) + user(tool_result) + assistant(final)
	if len(res.NewMessages) != 4 {
		t.Fatalf("expected 4 new messages, got %d", len(res.NewMessages))
	}
	if res.NewMessages[0].Role != "user" {
		t.Fatalf("msg 0: expected user, got %s", res.NewMessages[0].Role)
	}
	if res.NewMessages[1].Role != "assistant" {
		t.Fatalf("msg 1: expected assistant, got %s", res.NewMessages[1].Role)
	}
	if res.NewMessages[2].Role != "user" {
		t.Fatalf("msg 2: expected user (tool_result), got %s", res.NewMessages[2].Role)
	}
	if res.NewMessages[3].Role != "assistant" {
		t.Fatalf("msg 3: expected assistant, got %s", res.NewMessages[3].Role)
	}
}

// slowLLMClient simulates a slow LLM that blocks until context is cancelled.
type slowLLMClient struct{}

func (s *slowLLMClient) CreateMessage(ctx context.Context, _ anthropic.MessagesRequest) (anthropic.MessagesResponse, error) {
	<-ctx.Done()
	return anthropic.MessagesResponse{}, ctx.Err()
}

func TestAnthropicRunner_RequestTimeout(t *testing.T) {
	client := &slowLLMClient{}
	registry := tool.NewRegistry()
	runner := NewAnthropicRunner(client, registry, "system prompt")
	runner.requestTimeout = 10 * time.Millisecond

	start := time.Now()
	_, err := runner.Run(context.Background(), makeReq())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error due to request timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
	if elapsed > 1*time.Second {
		t.Fatalf("expected fast timeout, but took %v", elapsed)
	}
}
