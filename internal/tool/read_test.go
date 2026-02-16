package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadToolName(t *testing.T) {
	r := ReadTool{}
	if r.Name() != "read" {
		t.Fatalf("expected name 'read', got %q", r.Name())
	}
}

func TestReadMissingPathArg(t *testing.T) {
	r := ReadTool{}
	result := r.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Fatal("expected IsError for missing path arg")
	}
	if !strings.Contains(result.Output, "path") {
		t.Fatalf("expected error message to mention 'path', got %q", result.Output)
	}
}

func TestReadEmptyPathArg(t *testing.T) {
	r := ReadTool{}
	result := r.Execute(context.Background(), map[string]any{"path": ""})
	if !result.IsError {
		t.Fatal("expected IsError for empty path arg")
	}
	if !strings.Contains(result.Output, "empty") {
		t.Fatalf("expected error message to mention 'empty', got %q", result.Output)
	}
}

func TestReadNonExistentFile(t *testing.T) {
	r := ReadTool{}
	result := r.Execute(context.Background(), map[string]any{"path": "/tmp/inner-companion-test-nonexistent-file-abc123"})
	if !result.IsError {
		t.Fatal("expected IsError for non-existent file")
	}
	if !strings.Contains(strings.ToLower(result.Output), "not found") {
		t.Fatalf("expected error message to contain 'not found', got %q", result.Output)
	}
}

func TestReadDirectoryPath(t *testing.T) {
	dir := t.TempDir()
	r := ReadTool{}
	result := r.Execute(context.Background(), map[string]any{"path": dir})
	if !result.IsError {
		t.Fatal("expected IsError for directory path")
	}
	if !strings.Contains(strings.ToLower(result.Output), "directory") {
		t.Fatalf("expected error message to contain 'directory', got %q", result.Output)
	}
}

func TestReadFileTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")

	// Create a file just over 1 MiB.
	data := make([]byte, maxReadSize+1)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("failed to write large file: %v", err)
	}

	r := ReadTool{}
	result := r.Execute(context.Background(), map[string]any{"path": path})
	if !result.IsError {
		t.Fatal("expected IsError for file too large")
	}
	if !strings.Contains(strings.ToLower(result.Output), "too large") {
		t.Fatalf("expected error message to contain 'too large', got %q", result.Output)
	}
}

func TestReadNormalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	content := "hello, inner-companion!"

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	r := ReadTool{}
	result := r.Execute(context.Background(), map[string]any{"path": path})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Output)
	}
	if result.Output != content {
		t.Fatalf("expected output %q, got %q", content, result.Output)
	}
}

func TestReadWithCancelledContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ctx.txt")
	content := "context test"

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	r := ReadTool{}
	result := r.Execute(ctx, map[string]any{"path": path})
	// Read is fast and doesn't check context, so it should still succeed.
	if result.IsError {
		t.Fatalf("unexpected error with cancelled context: %s", result.Output)
	}
	if result.Output != content {
		t.Fatalf("expected output %q, got %q", content, result.Output)
	}
}
