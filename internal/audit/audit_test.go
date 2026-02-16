package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileLogger_WritesValidJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	logger, err := NewFileLogger(path)
	if err != nil {
		t.Fatalf("NewFileLogger: %v", err)
	}
	defer logger.Close()

	entry := Entry{
		SessionID: "sess-1",
		AgentID:   "agent-1",
		Event:     "test_event",
		Detail:    map[string]any{"key": "value"},
	}
	if err := logger.Log(entry); err != nil {
		t.Fatalf("Log: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var decoded Entry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\ndata: %s", err, data)
	}
	if decoded.Event != "test_event" {
		t.Errorf("Event = %q, want %q", decoded.Event, "test_event")
	}
	if decoded.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", decoded.SessionID, "sess-1")
	}
}

func TestFileLogger_MultipleEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	logger, err := NewFileLogger(path)
	if err != nil {
		t.Fatalf("NewFileLogger: %v", err)
	}
	defer logger.Close()

	for i := 0; i < 3; i++ {
		if err := logger.Log(Entry{Event: "event"}); err != nil {
			t.Fatalf("Log[%d]: %v", i, err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	dec := json.NewDecoder(strings.NewReader(string(data)))
	count := 0
	for dec.More() {
		var e Entry
		if err := dec.Decode(&e); err != nil {
			t.Fatalf("Decode[%d]: %v", count, err)
		}
		count++
	}
	if count != 3 {
		t.Errorf("got %d entries, want 3", count)
	}
}

func TestFileLogger_TimestampAutoSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	logger, err := NewFileLogger(path)
	if err != nil {
		t.Fatalf("NewFileLogger: %v", err)
	}
	defer logger.Close()

	before := time.Now()
	if err := logger.Log(Entry{Event: "ts_test"}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	after := time.Now()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var decoded Entry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Timestamp.Before(before) || decoded.Timestamp.After(after) {
		t.Errorf("Timestamp %v not between %v and %v", decoded.Timestamp, before, after)
	}
}

func TestFileLogger_Close(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	logger, err := NewFileLogger(path)
	if err != nil {
		t.Fatalf("NewFileLogger: %v", err)
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Writing after close should fail
	err = logger.Log(Entry{Event: "after_close"})
	if err == nil {
		t.Error("expected error writing to closed logger, got nil")
	}
}

func TestNopLogger_Log(t *testing.T) {
	var l NopLogger
	if err := l.Log(Entry{Event: "test"}); err != nil {
		t.Errorf("NopLogger.Log returned error: %v", err)
	}
}

func TestNopLogger_Close(t *testing.T) {
	var l NopLogger
	if err := l.Close(); err != nil {
		t.Errorf("NopLogger.Close returned error: %v", err)
	}
}
