package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetectChanges_NoChanges(t *testing.T) {
	t.Parallel()
	dir := initGitRepo(t)

	cs, err := DetectChanges(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cs.Files) != 0 {
		t.Errorf("expected 0 changed files, got %d", len(cs.Files))
	}
	if cs.AddLines != 0 || cs.DelLines != 0 {
		t.Errorf("expected 0 add/del lines, got +%d -%d", cs.AddLines, cs.DelLines)
	}
}

func TestDetectChanges_NewFile(t *testing.T) {
	t.Parallel()
	dir := initGitRepo(t)

	// Create a new file
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cs, err := DetectChanges(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cs.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(cs.Files))
	}
	if cs.Files[0].Path != "hello.txt" {
		t.Errorf("expected path 'hello.txt', got %q", cs.Files[0].Path)
	}
	if cs.Files[0].Checksum == "" {
		t.Error("expected non-empty checksum")
	}
	if cs.AddLines < 2 {
		t.Errorf("expected at least 2 added lines, got %d", cs.AddLines)
	}
}

func TestDetectChanges_ModifiedFile(t *testing.T) {
	t.Parallel()
	dir := initGitRepo(t)

	// Create and commit a file
	fpath := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(fpath, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitExec(t, dir, "add", "data.txt")
	gitExec(t, dir, "commit", "-m", "add data")

	// Modify the file
	if err := os.WriteFile(fpath, []byte("modified\nline2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cs, err := DetectChanges(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cs.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(cs.Files))
	}
	if cs.Files[0].Path != "data.txt" {
		t.Errorf("expected path 'data.txt', got %q", cs.Files[0].Path)
	}
}

func TestDetectChanges_DeletedFile(t *testing.T) {
	t.Parallel()
	dir := initGitRepo(t)

	// Create and commit a file
	fpath := filepath.Join(dir, "remove.txt")
	if err := os.WriteFile(fpath, []byte("to be removed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitExec(t, dir, "add", "remove.txt")
	gitExec(t, dir, "commit", "-m", "add remove.txt")

	// Delete the file
	os.Remove(fpath)

	cs, err := DetectChanges(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cs.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(cs.Files))
	}
	if cs.DelLines < 1 {
		t.Errorf("expected at least 1 deleted line, got %d", cs.DelLines)
	}
	// Deleted file should have empty checksum
	if cs.Files[0].Checksum != "" {
		t.Errorf("expected empty checksum for deleted file, got %q", cs.Files[0].Checksum)
	}
}

func TestDetectChanges_ChecksumIsSHA256(t *testing.T) {
	t.Parallel()
	dir := initGitRepo(t)

	if err := os.WriteFile(filepath.Join(dir, "check.txt"), []byte("checksum test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cs, err := DetectChanges(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cs.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(cs.Files))
	}
	// SHA256 hex string is 64 characters
	if len(cs.Files[0].Checksum) != 64 {
		t.Errorf("expected 64-char SHA256 hex, got %d chars: %q", len(cs.Files[0].Checksum), cs.Files[0].Checksum)
	}
}

func TestDetectChanges_NonGitDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, err := DetectChanges(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

// initGitRepo creates a temporary git repository with an initial commit.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitExec(t, dir, "init")
	gitExec(t, dir, "config", "user.email", "test@test.com")
	gitExec(t, dir, "config", "user.name", "Test")
	// Create initial commit
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	gitExec(t, dir, "add", ".gitkeep")
	gitExec(t, dir, "commit", "-m", "initial")
	return dir
}

// gitExec runs a git command in the given directory.
func gitExec(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
