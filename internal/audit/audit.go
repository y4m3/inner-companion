package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type Entry struct {
	Timestamp time.Time      `json:"timestamp"`
	SessionID string         `json:"session_id"`
	AgentID   string         `json:"agent_id,omitempty"`
	Event     string         `json:"event"`
	Detail    map[string]any `json:"detail,omitempty"`
}

type Logger interface {
	Log(entry Entry) error
	Close() error
}

type FileLogger struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
}

func NewFileLogger(path string) (*FileLogger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("audit: %w", err)
	}
	return &FileLogger{
		file: f,
		enc:  json.NewEncoder(f),
	}, nil
}

func (l *FileLogger) Log(entry Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	return l.enc.Encode(entry)
}

func (l *FileLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

type NopLogger struct{}

func (NopLogger) Log(Entry) error { return nil }
func (NopLogger) Close() error    { return nil }
