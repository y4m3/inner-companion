package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"inner-companion/internal/audit"
	"inner-companion/internal/protocol"
	"inner-companion/internal/session"
)

type fakeSessionStore struct {
	sessions      map[string]*fakeSessionData
	appendedMsgs  []protocol.HistoryMessage
	savedSummary  string
	createErr     error
	loadHistErr   error
	appendErr     error
}

type fakeSessionData struct {
	agentID       string
	history       []protocol.HistoryMessage
	memorySummary string
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{sessions: map[string]*fakeSessionData{}}
}

func (f *fakeSessionStore) CreateSession(_ context.Context, sid, aid string) error {
	if f.createErr != nil {
		return f.createErr
	}
	if _, ok := f.sessions[sid]; !ok {
		f.sessions[sid] = &fakeSessionData{agentID: aid}
	}
	return nil
}

func (f *fakeSessionStore) LoadHistory(_ context.Context, sid string) ([]protocol.HistoryMessage, error) {
	if f.loadHistErr != nil {
		return nil, f.loadHistErr
	}
	if d, ok := f.sessions[sid]; ok {
		return d.history, nil
	}
	return nil, nil
}

func (f *fakeSessionStore) AppendMessages(_ context.Context, sid string, msgs []protocol.HistoryMessage) error {
	if f.appendErr != nil {
		return f.appendErr
	}
	f.appendedMsgs = append(f.appendedMsgs, msgs...)
	if d, ok := f.sessions[sid]; ok {
		d.history = append(d.history, msgs...)
	}
	return nil
}

func (f *fakeSessionStore) LoadMeta(_ context.Context, sid string) (session.SessionMeta, error) {
	if d, ok := f.sessions[sid]; ok {
		return session.SessionMeta{
			SessionID:     sid,
			AgentID:       d.agentID,
			MemorySummary: d.memorySummary,
		}, nil
	}
	return session.SessionMeta{}, nil
}

func (f *fakeSessionStore) SaveMemorySummary(_ context.Context, sid, summary string) error {
	f.savedSummary = summary
	if d, ok := f.sessions[sid]; ok {
		d.memorySummary = summary
	}
	return nil
}

func (f *fakeSessionStore) MessageCount(_ context.Context, sid string) (int, error) {
	if d, ok := f.sessions[sid]; ok {
		return len(d.history), nil
	}
	return 0, nil
}

func (f *fakeSessionStore) MaxSeq(_ context.Context, sid string) (int64, error) {
	if d, ok := f.sessions[sid]; ok {
		return int64(len(d.history)), nil
	}
	return 0, nil
}

func (f *fakeSessionStore) DeleteMessagesBefore(_ context.Context, _ string, _ int64) error {
	return nil
}

func (f *fakeSessionStore) Close() error { return nil }

type fakeInnerRunner struct {
	response protocol.AgentResponse
	err      error
	lastReq  protocol.AgentRequest
}

func (f *fakeInnerRunner) Run(_ context.Context, req protocol.AgentRequest) (protocol.AgentResponse, error) {
	f.lastReq = req
	return f.response, f.err
}

type fakeSummarizer struct {
	result string
	err    error
	called bool
}

func (f *fakeSummarizer) Summarize(_ context.Context, _ []protocol.HistoryMessage, _ string) (string, error) {
	f.called = true
	return f.result, f.err
}

func TestSessionRunner_Run_BasicFlow(t *testing.T) {
	t.Parallel()

	store := newFakeSessionStore()
	inner := &fakeInnerRunner{
		response: protocol.AgentResponse{
			RequestID:     "r1",
			Status:        "ok",
			AssistantText: "hi",
			NewMessages: []protocol.HistoryMessage{
				{Role: "user", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "hello"}}},
				{Role: "assistant", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "hi"}}},
			},
		},
	}
	summarizer := &fakeSummarizer{result: "summary"}
	mm := session.NewMemoryManager(store, summarizer, 200000)
	sr := NewSessionRunner(inner, store, mm, nil)

	req := protocol.AgentRequest{
		RequestID:    "r1",
		SessionID:    "s1",
		AgentID:      "a1",
		InputText:    "hello",
		AllowedTools: []string{"read", "bash"},
		TimeoutSec:   60,
	}

	res, err := sr.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "ok" {
		t.Fatalf("expected ok, got %s", res.Status)
	}
	if res.AssistantText != "hi" {
		t.Fatalf("expected 'hi', got %q", res.AssistantText)
	}

	// Verify session was created
	if _, ok := store.sessions["s1"]; !ok {
		t.Fatal("session not created")
	}

	// Verify new messages were appended
	if len(store.appendedMsgs) != 2 {
		t.Fatalf("expected 2 appended messages, got %d", len(store.appendedMsgs))
	}
}

func TestSessionRunner_Run_HistoryLoaded(t *testing.T) {
	t.Parallel()

	store := newFakeSessionStore()
	store.sessions["s1"] = &fakeSessionData{
		agentID: "a1",
		history: []protocol.HistoryMessage{
			{Role: "user", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "old msg"}}},
			{Role: "assistant", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "old reply"}}},
		},
	}

	inner := &fakeInnerRunner{
		response: protocol.AgentResponse{
			RequestID:     "r1",
			Status:        "ok",
			AssistantText: "continued",
			NewMessages: []protocol.HistoryMessage{
				{Role: "user", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "new msg"}}},
				{Role: "assistant", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "continued"}}},
			},
		},
	}
	summarizer := &fakeSummarizer{result: "summary"}
	mm := session.NewMemoryManager(store, summarizer, 200000)
	sr := NewSessionRunner(inner, store, mm, nil)

	req := protocol.AgentRequest{
		RequestID:    "r1",
		SessionID:    "s1",
		AgentID:      "a1",
		InputText:    "new msg",
		AllowedTools: []string{"read", "bash"},
		TimeoutSec:   60,
	}

	sr.Run(context.Background(), req)

	// Verify inner runner received history
	if len(inner.lastReq.History) != 2 {
		t.Fatalf("expected 2 history messages, got %d", len(inner.lastReq.History))
	}
	if inner.lastReq.History[0].Content[0].Text != "old msg" {
		t.Fatalf("expected 'old msg', got %q", inner.lastReq.History[0].Content[0].Text)
	}
}

func TestSessionRunner_Run_SystemPromptWithMemory(t *testing.T) {
	t.Parallel()

	store := newFakeSessionStore()
	store.sessions["s1"] = &fakeSessionData{
		agentID:       "a1",
		memorySummary: "User likes Go.",
	}

	inner := &fakeInnerRunner{
		response: protocol.AgentResponse{
			RequestID:     "r1",
			Status:        "ok",
			AssistantText: "ok",
		},
	}
	summarizer := &fakeSummarizer{result: "summary"}
	mm := session.NewMemoryManager(store, summarizer, 200000)
	sr := NewSessionRunner(inner, store, mm, nil)

	req := protocol.AgentRequest{
		RequestID:    "r1",
		SessionID:    "s1",
		AgentID:      "a1",
		InputText:    "hello",
		AllowedTools: []string{"read", "bash"},
		TimeoutSec:   60,
	}

	sr.Run(context.Background(), req)

	// Verify system prompt includes memory summary
	if !strings.Contains(inner.lastReq.SystemPrompt, "User likes Go.") {
		t.Fatalf("expected memory in system prompt, got %q", inner.lastReq.SystemPrompt)
	}
}

func TestSessionRunner_Run_InnerError(t *testing.T) {
	t.Parallel()

	store := newFakeSessionStore()
	inner := &fakeInnerRunner{err: errors.New("boom")}
	summarizer := &fakeSummarizer{}
	mm := session.NewMemoryManager(store, summarizer, 200000)
	sr := NewSessionRunner(inner, store, mm, nil)

	req := protocol.AgentRequest{
		RequestID:    "r1",
		SessionID:    "s1",
		AgentID:      "a1",
		InputText:    "hello",
		AllowedTools: []string{"read", "bash"},
		TimeoutSec:   60,
	}

	_, err := sr.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected 'boom', got %v", err)
	}

	// No messages should have been appended
	if len(store.appendedMsgs) != 0 {
		t.Fatalf("expected 0 appended messages, got %d", len(store.appendedMsgs))
	}
}

func TestSessionRunner_Run_AppendErrorDoesNotFailResponse(t *testing.T) {
	t.Parallel()

	store := newFakeSessionStore()
	store.appendErr = errors.New("db write fail")

	inner := &fakeInnerRunner{
		response: protocol.AgentResponse{
			RequestID:     "r1",
			Status:        "ok",
			AssistantText: "hi",
			NewMessages: []protocol.HistoryMessage{
				{Role: "user", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "hello"}}},
			},
		},
	}
	summarizer := &fakeSummarizer{}
	mm := session.NewMemoryManager(store, summarizer, 200000)
	sr := NewSessionRunner(inner, store, mm, nil)

	req := protocol.AgentRequest{
		RequestID:    "r1",
		SessionID:    "s1",
		AgentID:      "a1",
		InputText:    "hello",
		AllowedTools: []string{"read", "bash"},
		TimeoutSec:   60,
	}

	// AppendMessages error should be logged but not returned
	res, err := sr.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "ok" {
		t.Fatalf("expected ok, got %s", res.Status)
	}
}

func TestSessionRunner_Run_ReadonlyModeLimitsTools(t *testing.T) {
	t.Parallel()

	store := newFakeSessionStore()
	inner := &fakeInnerRunner{
		response: protocol.AgentResponse{
			RequestID:     "r1",
			Status:        "ok",
			AssistantText: "ok",
		},
	}
	summarizer := &fakeSummarizer{}
	mm := session.NewMemoryManager(store, summarizer, 200000)
	mode := NewModeManager(audit.NopLogger{})

	// Force readonly mode
	mode.RecordLLMFailure()
	mode.RecordLLMFailure()
	mode.RecordLLMFailure()

	sr := NewSessionRunner(inner, store, mm, mode)

	req := protocol.AgentRequest{
		RequestID:    "r1",
		SessionID:    "s1",
		AgentID:      "a1",
		InputText:    "hello",
		AllowedTools: []string{"read", "bash"},
		TimeoutSec:   60,
	}

	sr.Run(context.Background(), req)

	// In readonly mode, AllowedTools should be restricted to ["read"]
	if len(inner.lastReq.AllowedTools) != 1 || inner.lastReq.AllowedTools[0] != "read" {
		t.Fatalf("expected AllowedTools=[read], got %v", inner.lastReq.AllowedTools)
	}
}

func TestSessionRunner_Run_RecordsLLMFailure(t *testing.T) {
	t.Parallel()

	store := newFakeSessionStore()
	inner := &fakeInnerRunner{err: errors.New("llm error")}
	summarizer := &fakeSummarizer{}
	mm := session.NewMemoryManager(store, summarizer, 200000)
	mode := NewModeManager(audit.NopLogger{})

	sr := NewSessionRunner(inner, store, mm, mode)

	req := protocol.AgentRequest{
		RequestID:    "r1",
		SessionID:    "s1",
		AgentID:      "a1",
		InputText:    "hello",
		AllowedTools: []string{"read", "bash"},
		TimeoutSec:   60,
	}

	// 3 failures should trigger readonly
	sr.Run(context.Background(), req)
	sr.Run(context.Background(), req)
	sr.Run(context.Background(), req)

	if mode.CurrentMode() != ModeReadonly {
		t.Fatalf("expected readonly mode after 3 failures, got %v", mode.CurrentMode())
	}
}

func TestSessionRunner_ToolFailureLimit_AllowsUpTo10(t *testing.T) {
	t.Parallel()

	store := newFakeSessionStore()
	// Inner runner returns 10 failed tool calls in a single response
	inner := &fakeInnerRunner{
		response: protocol.AgentResponse{
			RequestID:     "r1",
			Status:        "ok",
			AssistantText: "done",
			ToolCalls: []protocol.AgentToolCall{
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
			},
		},
	}
	summarizer := &fakeSummarizer{result: "summary"}
	mm := session.NewMemoryManager(store, summarizer, 200000)
	sr := NewSessionRunner(inner, store, mm, nil)

	req := protocol.AgentRequest{
		RequestID:    "r1",
		SessionID:    "s1",
		AgentID:      "a1",
		InputText:    "hello",
		AllowedTools: []string{"read", "bash"},
		TimeoutSec:   60,
	}

	// First request: 10 failures accumulated. Should succeed.
	_, err := sr.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error with 10 failures, got: %v", err)
	}

	// Second request: cumulative count is 10, which is NOT > 10, so should still succeed
	inner.response.ToolCalls = nil // no new failures
	_, err = sr.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error at exactly 10 cumulative failures, got: %v", err)
	}
}

func TestSessionRunner_ToolFailureLimit_DeniesAfter10(t *testing.T) {
	t.Parallel()

	store := newFakeSessionStore()
	inner := &fakeInnerRunner{
		response: protocol.AgentResponse{
			RequestID:     "r1",
			Status:        "ok",
			AssistantText: "done",
			ToolCalls: []protocol.AgentToolCall{
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
			},
		},
	}
	summarizer := &fakeSummarizer{result: "summary"}
	mm := session.NewMemoryManager(store, summarizer, 200000)
	sr := NewSessionRunner(inner, store, mm, nil)

	req := protocol.AgentRequest{
		RequestID:    "r1",
		SessionID:    "s1",
		AgentID:      "a1",
		InputText:    "hello",
		AllowedTools: []string{"read", "bash"},
		TimeoutSec:   60,
	}

	// First request: 11 failures accumulated
	_, err := sr.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("first request should succeed even with 11 tool failures: %v", err)
	}

	// Second request: cumulative count is 11 (> 10), should be rejected
	_, err = sr.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected error after exceeding 10 tool failures")
	}
	if !strings.Contains(err.Error(), "tool failure limit exceeded") {
		t.Fatalf("expected 'tool failure limit exceeded', got: %v", err)
	}
}

func TestSessionRunner_ToolFailureLimit_CountsOnlyFailures(t *testing.T) {
	t.Parallel()

	store := newFakeSessionStore()
	inner := &fakeInnerRunner{
		response: protocol.AgentResponse{
			RequestID:     "r1",
			Status:        "ok",
			AssistantText: "done",
			ToolCalls: []protocol.AgentToolCall{
				{ToolName: "read", Args: map[string]any{"path": "/a"}, ResultSummary: "ok", OK: true},
				{ToolName: "read", Args: map[string]any{"path": "/b"}, ResultSummary: "ok", OK: true},
				{ToolName: "bash", Args: map[string]any{"command": "x"}, ResultSummary: "fail", OK: false},
				{ToolName: "read", Args: map[string]any{"path": "/c"}, ResultSummary: "ok", OK: true},
			},
		},
	}
	summarizer := &fakeSummarizer{result: "summary"}
	mm := session.NewMemoryManager(store, summarizer, 200000)
	sr := NewSessionRunner(inner, store, mm, nil)

	req := protocol.AgentRequest{
		RequestID:    "r1",
		SessionID:    "s1",
		AgentID:      "a1",
		InputText:    "hello",
		AllowedTools: []string{"read", "bash"},
		TimeoutSec:   60,
	}

	// Run 10 requests, each with 1 failure and 3 successes
	for i := 0; i < 10; i++ {
		_, err := sr.Run(context.Background(), req)
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i+1, err)
		}
	}

	// Cumulative failures: 10 (not > 10), next request should still work
	inner.response.ToolCalls = []protocol.AgentToolCall{
		{ToolName: "read", Args: map[string]any{"path": "/d"}, ResultSummary: "ok", OK: true},
	}
	_, err := sr.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error (only 10 failures from OK:false), got: %v", err)
	}
}

func TestSessionRunner_Run_RecordsLLMSuccess(t *testing.T) {
	t.Parallel()

	store := newFakeSessionStore()
	failInner := &fakeInnerRunner{err: errors.New("llm error")}
	summarizer := &fakeSummarizer{}
	mm := session.NewMemoryManager(store, summarizer, 200000)
	mode := NewModeManager(audit.NopLogger{})

	sr := NewSessionRunner(failInner, store, mm, mode)

	req := protocol.AgentRequest{
		RequestID:    "r1",
		SessionID:    "s1",
		AgentID:      "a1",
		InputText:    "hello",
		AllowedTools: []string{"read", "bash"},
		TimeoutSec:   60,
	}

	// 2 failures
	sr.Run(context.Background(), req)
	sr.Run(context.Background(), req)

	// Swap to a succeeding inner runner
	successInner := &fakeInnerRunner{
		response: protocol.AgentResponse{
			RequestID:     "r1",
			Status:        "ok",
			AssistantText: "ok",
		},
	}
	sr.inner = successInner
	sr.Run(context.Background(), req)

	// Success should reset, so 2 more failures should not trigger readonly
	sr.inner = failInner
	sr.Run(context.Background(), req)
	sr.Run(context.Background(), req)

	if mode.CurrentMode() != ModeNormal {
		t.Fatalf("expected normal mode after success reset, got %v", mode.CurrentMode())
	}
}
