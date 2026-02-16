package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"inner-companion/internal/protocol"
)

type fakeStore struct {
	history       []protocol.HistoryMessage
	meta          SessionMeta
	messageCount  int
	maxSeq        int64
	deletedBefore int64
	appendedMsgs  []protocol.HistoryMessage
	savedSummary  string
}

func (f *fakeStore) CreateSession(context.Context, string, string) error { return nil }
func (f *fakeStore) LoadHistory(_ context.Context, _ string) ([]protocol.HistoryMessage, error) {
	return f.history, nil
}
func (f *fakeStore) AppendMessages(_ context.Context, _ string, msgs []protocol.HistoryMessage) error {
	f.appendedMsgs = append(f.appendedMsgs, msgs...)
	return nil
}
func (f *fakeStore) LoadMeta(_ context.Context, _ string) (SessionMeta, error) {
	return f.meta, nil
}
func (f *fakeStore) SaveMemorySummary(_ context.Context, _ string, summary string) error {
	f.savedSummary = summary
	f.meta.MemorySummary = summary
	return nil
}
func (f *fakeStore) MessageCount(_ context.Context, _ string) (int, error) {
	return f.messageCount, nil
}
func (f *fakeStore) MaxSeq(_ context.Context, _ string) (int64, error) {
	return f.maxSeq, nil
}
func (f *fakeStore) DeleteMessagesBefore(_ context.Context, _ string, seq int64) error {
	f.deletedBefore = seq
	return nil
}
func (f *fakeStore) Close() error { return nil }

type fakeSummarizer struct {
	result string
	err    error
	called bool
}

func (f *fakeSummarizer) Summarize(_ context.Context, _ []protocol.HistoryMessage, _ string) (string, error) {
	f.called = true
	return f.result, f.err
}

func TestMemoryManager_CheckAndFlush_BelowThreshold(t *testing.T) {
	t.Parallel()

	store := &fakeStore{messageCount: 5, maxSeq: 5}
	summarizer := &fakeSummarizer{result: "summary"}
	mm := NewMemoryManager(store, summarizer, 200000) // very high limit

	err := mm.CheckAndFlush(context.Background(), "s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summarizer.called {
		t.Fatal("summarizer should not have been called below threshold")
	}
}

func TestMemoryManager_CheckAndFlush_AboveThreshold(t *testing.T) {
	t.Parallel()

	// Create messages that exceed 70% of a small token limit
	bigText := strings.Repeat("x", 400) // 400 chars = ~100 tokens
	store := &fakeStore{
		history: []protocol.HistoryMessage{
			{Role: "user", Content: []protocol.HistoryContentBlock{{Type: "text", Text: bigText}}},
			{Role: "assistant", Content: []protocol.HistoryContentBlock{{Type: "text", Text: bigText}}},
		},
		messageCount: 2,
		maxSeq:       2,
	}
	summarizer := &fakeSummarizer{result: "compressed summary"}
	mm := NewMemoryManager(store, summarizer, 100) // 100 tokens limit, 70% = 70, actual ~200

	err := mm.CheckAndFlush(context.Background(), "s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !summarizer.called {
		t.Fatal("summarizer should have been called")
	}
	if store.savedSummary != "compressed summary" {
		t.Fatalf("expected 'compressed summary', got %q", store.savedSummary)
	}
	if store.deletedBefore == 0 {
		t.Fatal("expected messages to be deleted")
	}
}

func TestMemoryManager_CheckAndFlush_SummarizerError(t *testing.T) {
	t.Parallel()

	bigText := strings.Repeat("x", 400)
	store := &fakeStore{
		history: []protocol.HistoryMessage{
			{Role: "user", Content: []protocol.HistoryContentBlock{{Type: "text", Text: bigText}}},
		},
		messageCount: 1,
		maxSeq:       1,
	}
	summarizer := &fakeSummarizer{err: errors.New("api down")}
	mm := NewMemoryManager(store, summarizer, 100)

	err := mm.CheckAndFlush(context.Background(), "s1")
	if err == nil {
		t.Fatal("expected error from summarizer")
	}
	if !strings.Contains(err.Error(), "api down") {
		t.Fatalf("expected 'api down' in error, got %v", err)
	}
}

func TestMemoryManager_BuildSystemPrompt_NoSummary(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		meta: SessionMeta{MemorySummary: ""},
	}
	mm := NewMemoryManager(store, nil, 200000)

	prompt, err := mm.BuildSystemPrompt(context.Background(), "s1", "base prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "base prompt" {
		t.Fatalf("expected base prompt unchanged, got %q", prompt)
	}
}

func TestMemoryManager_BuildSystemPrompt_WithSummary(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		meta: SessionMeta{MemorySummary: "User asked about Go testing."},
	}
	mm := NewMemoryManager(store, nil, 200000)

	prompt, err := mm.BuildSystemPrompt(context.Background(), "s1", "base prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(prompt, "base prompt") {
		t.Fatal("expected base prompt in result")
	}
	if !strings.Contains(prompt, "User asked about Go testing.") {
		t.Fatal("expected memory summary in result")
	}
}
