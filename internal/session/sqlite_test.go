package session

import (
	"context"
	"path/filepath"
	"testing"

	"inner-companion/internal/protocol"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSQLiteStore_CreateSessionIdempotent(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.CreateSession(ctx, "s1", "a1"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// Second call with same session_id must not error
	if err := store.CreateSession(ctx, "s1", "a1"); err != nil {
		t.Fatalf("idempotent create: %v", err)
	}

	meta, err := store.LoadMeta(ctx, "s1")
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.SessionID != "s1" || meta.AgentID != "a1" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}

func TestSQLiteStore_AppendAndLoadHistory(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	store.CreateSession(ctx, "s1", "a1")

	msgs := []protocol.HistoryMessage{
		{Role: "user", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "hi"}}},
	}
	if err := store.AppendMessages(ctx, "s1", msgs); err != nil {
		t.Fatalf("AppendMessages: %v", err)
	}

	loaded, err := store.LoadHistory(ctx, "s1")
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(loaded))
	}
	if loaded[0].Role != "user" || loaded[0].Content[0].Text != "hello" {
		t.Fatalf("unexpected first message: %+v", loaded[0])
	}
	if loaded[1].Role != "assistant" || loaded[1].Content[0].Text != "hi" {
		t.Fatalf("unexpected second message: %+v", loaded[1])
	}
}

func TestSQLiteStore_OrderPreserved(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	store.CreateSession(ctx, "s1", "a1")

	for i := 0; i < 5; i++ {
		msg := protocol.HistoryMessage{
			Role:    "user",
			Content: []protocol.HistoryContentBlock{{Type: "text", Text: string(rune('A' + i))}},
		}
		store.AppendMessages(ctx, "s1", []protocol.HistoryMessage{msg})
	}

	loaded, err := store.LoadHistory(ctx, "s1")
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(loaded) != 5 {
		t.Fatalf("expected 5, got %d", len(loaded))
	}
	for i, m := range loaded {
		expected := string(rune('A' + i))
		if m.Content[0].Text != expected {
			t.Fatalf("message %d: expected %q, got %q", i, expected, m.Content[0].Text)
		}
	}
}

func TestSQLiteStore_MessageCount(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	store.CreateSession(ctx, "s1", "a1")

	count, err := store.MessageCount(ctx, "s1")
	if err != nil {
		t.Fatalf("MessageCount: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}

	store.AppendMessages(ctx, "s1", []protocol.HistoryMessage{
		{Role: "user", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "a"}}},
		{Role: "assistant", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "b"}}},
	})

	count, err = store.MessageCount(ctx, "s1")
	if err != nil {
		t.Fatalf("MessageCount: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2, got %d", count)
	}
}

func TestSQLiteStore_SaveMemorySummary(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	store.CreateSession(ctx, "s1", "a1")

	if err := store.SaveMemorySummary(ctx, "s1", "summary text"); err != nil {
		t.Fatalf("SaveMemorySummary: %v", err)
	}

	meta, err := store.LoadMeta(ctx, "s1")
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.MemorySummary != "summary text" {
		t.Fatalf("expected 'summary text', got %q", meta.MemorySummary)
	}
}

func TestSQLiteStore_MaxSeqAndDelete(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	store.CreateSession(ctx, "s1", "a1")

	store.AppendMessages(ctx, "s1", []protocol.HistoryMessage{
		{Role: "user", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "a"}}},
		{Role: "assistant", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "b"}}},
		{Role: "user", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "c"}}},
	})

	maxSeq, err := store.MaxSeq(ctx, "s1")
	if err != nil {
		t.Fatalf("MaxSeq: %v", err)
	}
	if maxSeq != 3 {
		t.Fatalf("expected maxSeq 3, got %d", maxSeq)
	}

	// Delete messages before seq 3 (i.e., delete seq 1 and 2)
	if err := store.DeleteMessagesBefore(ctx, "s1", 3); err != nil {
		t.Fatalf("DeleteMessagesBefore: %v", err)
	}

	loaded, err := store.LoadHistory(ctx, "s1")
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 message after delete, got %d", len(loaded))
	}
	if loaded[0].Content[0].Text != "c" {
		t.Fatalf("expected 'c', got %q", loaded[0].Content[0].Text)
	}
}

func TestEstimateTokens(t *testing.T) {
	t.Parallel()

	msgs := []protocol.HistoryMessage{
		{Role: "user", Content: []protocol.HistoryContentBlock{{Type: "text", Text: "abcdefgh"}}}, // 8 chars
	}
	tokens := EstimateTokens(msgs)
	if tokens != 2 { // 8/4 = 2
		t.Fatalf("expected 2, got %d", tokens)
	}
}
