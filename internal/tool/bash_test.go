package tool

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBashToolName(t *testing.T) {
	b := newBashToolDirect(t.TempDir())
	if b.Name() != "bash" {
		t.Fatalf("expected bash, got %s", b.Name())
	}
}

func TestBashMissingCommand(t *testing.T) {
	b := newBashToolDirect(t.TempDir())
	res := b.Execute(context.Background(), map[string]any{})
	if !res.IsError {
		t.Fatal("expected error for missing command")
	}
	if !strings.Contains(res.Output, "command") {
		t.Fatalf("expected error about command, got: %s", res.Output)
	}
}

func TestBashEmptyCommand(t *testing.T) {
	b := newBashToolDirect(t.TempDir())
	res := b.Execute(context.Background(), map[string]any{"command": ""})
	if !res.IsError {
		t.Fatal("expected error for empty command")
	}
}

func TestBashEcho(t *testing.T) {
	b := newBashToolDirect(t.TempDir())
	res := b.Execute(context.Background(), map[string]any{"command": "echo hello"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if strings.TrimSpace(res.Output) != "hello" {
		t.Fatalf("expected hello, got: %q", res.Output)
	}
}

func TestBashExitCode(t *testing.T) {
	b := newBashToolDirect(t.TempDir())
	res := b.Execute(context.Background(), map[string]any{"command": "exit 42"})
	if !res.IsError {
		t.Fatal("expected error for non-zero exit")
	}
}

func TestBashTimeout(t *testing.T) {
	b := newBashToolDirect(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	res := b.Execute(ctx, map[string]any{"command": "sleep 10"})
	if !res.IsError {
		t.Fatal("expected error for timeout")
	}
}

func TestBashOutputTruncation(t *testing.T) {
	b := newBashToolDirect(t.TempDir())
	// Generate output larger than 1MiB
	res := b.Execute(context.Background(), map[string]any{
		"command": "dd if=/dev/zero bs=1024 count=1100 2>/dev/null | tr '\\0' 'A'",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Output, "truncated") {
		t.Fatal("expected truncation message for large output")
	}
}
