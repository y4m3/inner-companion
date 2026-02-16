package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoCommit_CommitsChanges(t *testing.T) {
	t.Parallel()
	dir := initGitRepo(t)

	// Create a file
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cs := ChangeSet{
		Files:    []ChangedFile{{Path: "new.txt", Checksum: "abc123"}},
		AddLines: 1,
	}

	err := AutoCommit(context.Background(), dir, cs, "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify commit was created
	cmd := exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	logLine := strings.TrimSpace(string(out))
	if !strings.Contains(logLine, "auto:") {
		t.Errorf("expected commit message containing 'auto:', got %q", logLine)
	}

	// Verify working directory is clean
	cmd2 := exec.Command("git", "status", "--porcelain")
	cmd2.Dir = dir
	out2, _ := cmd2.Output()
	if strings.TrimSpace(string(out2)) != "" {
		t.Errorf("expected clean working directory, got: %s", out2)
	}
}

func TestAutoCommit_NoChanges(t *testing.T) {
	t.Parallel()
	dir := initGitRepo(t)

	cs := ChangeSet{}

	err := AutoCommit(context.Background(), dir, cs, "ls")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should still only have the initial commit
	cmd := exec.Command("git", "rev-list", "--count", "HEAD")
	cmd.Dir = dir
	out, _ := cmd.Output()
	if strings.TrimSpace(string(out)) != "1" {
		t.Errorf("expected 1 commit, got %s", strings.TrimSpace(string(out)))
	}
}

func TestAutoCommit_NonGitDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	cs := ChangeSet{
		Files: []ChangedFile{{Path: "file.txt", Checksum: "abc"}},
	}

	// Should skip without error
	err := AutoCommit(context.Background(), dir, cs, "echo test")
	if err != nil {
		t.Fatalf("expected nil error for non-git dir, got: %v", err)
	}
}

func TestAutoCommit_CommitMessageContainsSummary(t *testing.T) {
	t.Parallel()
	dir := initGitRepo(t)

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line1\nline2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cs := ChangeSet{
		Files:    []ChangedFile{{Path: "a.txt", Checksum: "def"}},
		AddLines: 2,
	}

	err := AutoCommit(context.Background(), dir, cs, "echo setup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := exec.Command("git", "log", "--format=%B", "-1")
	cmd.Dir = dir
	out, _ := cmd.Output()
	msg := strings.TrimSpace(string(out))
	if !strings.Contains(msg, "1 file") {
		t.Errorf("expected commit message to contain file count, got %q", msg)
	}
}
