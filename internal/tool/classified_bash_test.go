package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"inner-companion/internal/audit"
	"inner-companion/internal/classify"
	"inner-companion/internal/protocol"
)

// spyLogger captures audit entries for test assertions.
type spyLogger struct {
	mu      sync.Mutex
	entries []audit.Entry
}

func (s *spyLogger) Log(entry audit.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry)
	return nil
}

func (s *spyLogger) Close() error { return nil }

func (s *spyLogger) Entries() []audit.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]audit.Entry, len(s.entries))
	copy(cp, s.entries)
	return cp
}

func TestClassifiedBash_L1Allowed(t *testing.T) {
	spy := &spyLogger{}
	inner := newBashToolDirect(t.TempDir())
	cb := NewClassifiedBashTool(inner, classify.Classify, "supervised", spy)

	ctx := protocol.WithSessionID(context.Background(), "test-session")
	result := cb.Execute(ctx, map[string]any{"command": "ls"})

	if result.IsError {
		t.Errorf("L1 command should be allowed, got error: %s", result.Output)
	}
}

func TestClassifiedBash_L3Denied(t *testing.T) {
	spy := &spyLogger{}
	inner := newBashToolDirect(t.TempDir())
	cb := NewClassifiedBashTool(inner, classify.Classify, "full", spy)

	ctx := protocol.WithSessionID(context.Background(), "test-session")
	result := cb.Execute(ctx, map[string]any{"command": "rm -rf /"})

	if !result.IsError {
		t.Error("L3 command should be denied")
	}
	if result.Output == "" {
		t.Error("denied result should contain an error message")
	}
}

func TestClassifiedBash_SupervisedL2Limit(t *testing.T) {
	spy := &spyLogger{}
	inner := newBashToolDirect(t.TempDir())
	cb := NewClassifiedBashTool(inner, classify.Classify, "supervised", spy)
	ctx := protocol.WithSessionID(context.Background(), "test-session")

	// First 20 L2 commands should be allowed
	for i := 0; i < 20; i++ {
		result := cb.Execute(ctx, map[string]any{"command": "mkdir -p testdir"})
		if result.IsError {
			t.Fatalf("L2 command #%d should be allowed in supervised mode, got error: %s", i+1, result.Output)
		}
	}

	// 21st L2 command should be denied
	result := cb.Execute(ctx, map[string]any{"command": "mkdir -p testdir"})
	if !result.IsError {
		t.Error("L2 command #21 should be denied in supervised mode (limit 20)")
	}
}

func TestClassifiedBash_FullModeL2AlwaysAllowed(t *testing.T) {
	spy := &spyLogger{}
	inner := newBashToolDirect(t.TempDir())
	cb := NewClassifiedBashTool(inner, classify.Classify, "full", spy)
	ctx := protocol.WithSessionID(context.Background(), "test-session")

	// Even beyond 20, L2 should be allowed in full mode
	for i := 0; i < 25; i++ {
		result := cb.Execute(ctx, map[string]any{"command": "mkdir -p testdir"})
		if result.IsError {
			t.Fatalf("L2 command #%d should be allowed in full mode, got error: %s", i+1, result.Output)
		}
	}
}

func TestClassifiedBash_AuditLogEntries(t *testing.T) {
	spy := &spyLogger{}
	inner := newBashToolDirect(t.TempDir())
	cb := NewClassifiedBashTool(inner, classify.Classify, "supervised", spy)
	ctx := protocol.WithSessionID(context.Background(), "test-session")

	// Execute an L1 command
	cb.Execute(ctx, map[string]any{"command": "ls"})
	// Execute an L3 command (denied)
	cb.Execute(ctx, map[string]any{"command": "rm -rf /"})

	entries := spy.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 audit entries, got %d", len(entries))
	}

	if entries[0].Event != "bash_executed" {
		t.Errorf("first entry event = %q, want %q", entries[0].Event, "bash_executed")
	}
	if entries[0].SessionID != "test-session" {
		t.Errorf("first entry session_id = %q, want %q", entries[0].SessionID, "test-session")
	}

	if entries[1].Event != "bash_denied" {
		t.Errorf("second entry event = %q, want %q", entries[1].Event, "bash_denied")
	}
}

func TestClassifiedBash_MissingCommand(t *testing.T) {
	spy := &spyLogger{}
	inner := newBashToolDirect(t.TempDir())
	cb := NewClassifiedBashTool(inner, classify.Classify, "supervised", spy)
	ctx := context.Background()

	result := cb.Execute(ctx, map[string]any{})
	if !result.IsError {
		t.Error("missing command should return error")
	}
}

func TestClassifiedBash_EmptyCommand(t *testing.T) {
	spy := &spyLogger{}
	inner := newBashToolDirect(t.TempDir())
	cb := NewClassifiedBashTool(inner, classify.Classify, "supervised", spy)
	ctx := context.Background()

	result := cb.Execute(ctx, map[string]any{"command": ""})
	if !result.IsError {
		t.Error("empty command should return error")
	}
}

func TestClassifiedBash_Name(t *testing.T) {
	inner := newBashToolDirect(t.TempDir())
	cb := NewClassifiedBashTool(inner, classify.Classify, "supervised", audit.NopLogger{})
	if cb.Name() != "bash" {
		t.Errorf("Name() = %q, want %q", cb.Name(), "bash")
	}
}

// --- Item 5: bash change detection + checksum audit ---

func TestClassifiedBash_ChangeDetection_LogsChangeset(t *testing.T) {
	dir := initGitRepo(t)
	spy := &spyLogger{}
	inner := newBashToolDirect(dir)
	cb := NewClassifiedBashTool(inner, classify.Classify, "supervised", spy)
	cb.SetWorkspaceDir(dir)

	ctx := protocol.WithSessionID(context.Background(), "test-session")

	// Execute a command that creates a file
	result := cb.Execute(ctx, map[string]any{"command": "echo hello > newfile.txt"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Output)
	}

	// Verify bash_changeset event was logged
	entries := spy.Entries()
	var changesetEntry *audit.Entry
	for i, e := range entries {
		if e.Event == "bash_changeset" {
			changesetEntry = &entries[i]
			break
		}
	}
	if changesetEntry == nil {
		t.Fatal("expected bash_changeset audit entry")
	}
	files, ok := changesetEntry.Detail["files"]
	if !ok {
		t.Fatal("expected files in changeset detail")
	}
	fileCount, ok := files.(int)
	if !ok || fileCount < 1 {
		t.Errorf("expected at least 1 changed file, got %v", files)
	}
}

// --- Item 6: git auto-commit ---

func TestClassifiedBash_AutoCommit_CreatesCommit(t *testing.T) {
	dir := initGitRepo(t)
	spy := &spyLogger{}
	inner := newBashToolDirect(dir)
	cb := NewClassifiedBashTool(inner, classify.Classify, "supervised", spy)
	cb.SetWorkspaceDir(dir)
	cb.SetAutoCommit(true)

	ctx := protocol.WithSessionID(context.Background(), "test-session")

	result := cb.Execute(ctx, map[string]any{"command": "echo autocommit > auto.txt"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Output)
	}

	// Verify auto-commit was created
	cmd := exec.Command("git", "log", "--oneline")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if !strings.Contains(string(out), "auto:") {
		t.Errorf("expected auto-commit in git log, got: %s", out)
	}

	// Verify working dir is clean
	cmd2 := exec.Command("git", "status", "--porcelain")
	cmd2.Dir = dir
	out2, _ := cmd2.Output()
	if strings.TrimSpace(string(out2)) != "" {
		t.Errorf("expected clean working dir after auto-commit, got: %s", out2)
	}
}

func TestClassifiedBash_AutoCommit_DisabledByDefault(t *testing.T) {
	dir := initGitRepo(t)
	spy := &spyLogger{}
	inner := newBashToolDirect(dir)
	cb := NewClassifiedBashTool(inner, classify.Classify, "supervised", spy)
	cb.SetWorkspaceDir(dir)
	// autoCommit NOT enabled

	ctx := protocol.WithSessionID(context.Background(), "test-session")

	cb.Execute(ctx, map[string]any{"command": "echo nocommit > nocommit.txt"})

	// Should still have uncommitted changes
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, _ := cmd.Output()
	if !strings.Contains(string(out), "nocommit.txt") {
		t.Error("expected uncommitted file when auto-commit is disabled")
	}
}

// --- Item 8: L2 cumulative threshold (file count / line count) ---

func TestClassifiedBash_L2FileCountThreshold(t *testing.T) {
	dir := initGitRepo(t)
	spy := &spyLogger{}
	inner := newBashToolDirect(dir)
	cb := NewClassifiedBashTool(inner, classify.Classify, "supervised", spy)
	cb.SetWorkspaceDir(dir)

	ctx := protocol.WithSessionID(context.Background(), "test-session")

	// Create files that exceed the 10-file threshold
	for i := 0; i < 10; i++ {
		fname := filepath.Join(dir, "file"+strings.Repeat("x", 1)+string(rune('a'+i))+".txt")
		if err := os.WriteFile(fname, []byte("data\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Commit so we have a clean baseline
	gitExec(t, dir, "add", "-A")
	gitExec(t, dir, "commit", "-m", "add files")

	// Execute L2 commands that each create a new file (total 10 files)
	for i := 0; i < 10; i++ {
		result := cb.Execute(ctx, map[string]any{
			"command": "echo data > changed_" + string(rune('a'+i)) + ".txt",
		})
		if result.IsError {
			// Might be denied after threshold
			break
		}
		// Reset the changes to avoid git issues (commit them)
		gitExec(t, dir, "add", "-A")
		gitExec(t, dir, "commit", "-m", "auto")
	}

	// Next L2 command should be denied due to file count threshold
	result := cb.Execute(ctx, map[string]any{"command": "mkdir -p newdir"})
	if !result.IsError {
		t.Error("expected L2 denial after file count threshold (10)")
	}
	if result.IsError && !strings.Contains(result.Output, "L2") {
		t.Errorf("expected L2-related denial message, got: %s", result.Output)
	}
}

func TestClassifiedBash_L2LineCountThreshold(t *testing.T) {
	dir := initGitRepo(t)
	spy := &spyLogger{}
	inner := newBashToolDirect(dir)
	cb := NewClassifiedBashTool(inner, classify.Classify, "supervised", spy)
	cb.SetWorkspaceDir(dir)

	ctx := protocol.WithSessionID(context.Background(), "test-session")

	// Create a file with many lines to exceed 500-line threshold in one shot
	var lines strings.Builder
	for i := 0; i < 501; i++ {
		lines.WriteString("line content here\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "bigfile.txt"), []byte(lines.String()), 0644); err != nil {
		t.Fatal(err)
	}
	gitExec(t, dir, "add", "-A")
	gitExec(t, dir, "commit", "-m", "add big file")

	// Execute L2 command that modifies the big file (replace all content)
	result := cb.Execute(ctx, map[string]any{
		"command": "seq 1 501 > bigfile.txt",
	})
	if result.IsError {
		t.Fatalf("first L2 command should succeed: %s", result.Output)
	}

	// Commit changes
	gitExec(t, dir, "add", "-A")
	gitExec(t, dir, "commit", "-m", "modify")

	// Next L2 command should be denied due to line count threshold
	result = cb.Execute(ctx, map[string]any{"command": "mkdir -p anotherdir"})
	if !result.IsError {
		t.Error("expected L2 denial after line count threshold (500)")
	}
}
