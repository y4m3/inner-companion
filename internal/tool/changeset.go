package tool

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ChangeSet describes file changes detected after a bash command execution.
type ChangeSet struct {
	Files    []ChangedFile
	AddLines int
	DelLines int
}

// ChangedFile represents a single changed file with its path and checksum.
type ChangedFile struct {
	Path     string
	Checksum string // SHA256 hex, empty for deleted files
}

// DetectChanges detects file changes in a git workspace.
// It returns an error if the directory is not a git repository.
func DetectChanges(ctx context.Context, workspaceDir string) (ChangeSet, error) {
	if !isGitRepo(ctx, workspaceDir) {
		return ChangeSet{}, fmt.Errorf("not a git repository: %s", workspaceDir)
	}

	files, err := changedFiles(ctx, workspaceDir)
	if err != nil {
		return ChangeSet{}, err
	}

	var cs ChangeSet
	for _, f := range files {
		cf := ChangedFile{Path: f}
		absPath := filepath.Join(workspaceDir, f)
		if _, statErr := os.Stat(absPath); statErr == nil {
			checksum, hashErr := sha256File(absPath)
			if hashErr == nil {
				cf.Checksum = checksum
			}
		}
		cs.Files = append(cs.Files, cf)
	}

	add, del, err := diffStats(ctx, workspaceDir)
	if err == nil {
		cs.AddLines = add
		cs.DelLines = del
	}

	return cs, nil
}

func isGitRepo(ctx context.Context, dir string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func changedFiles(ctx context.Context, dir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}

	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// git status --porcelain format: XY filename
		if len(line) < 4 {
			continue
		}
		file := strings.TrimSpace(line[2:])
		// Handle renamed files: "R  old -> new"
		if idx := strings.Index(file, " -> "); idx >= 0 {
			file = file[idx+4:]
		}
		files = append(files, file)
	}
	return files, nil
}

func diffStats(ctx context.Context, dir string) (add, del int, err error) {
	// For unstaged changes
	cmd := exec.CommandContext(ctx, "git", "diff", "--numstat")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("git diff --numstat: %w", err)
	}
	a1, d1 := parseNumstat(string(out))

	// For untracked files, count lines directly
	cmd2 := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd2.Dir = dir
	out2, err := cmd2.Output()
	if err != nil {
		return a1, d1, nil
	}
	for _, line := range strings.Split(string(out2), "\n") {
		if strings.HasPrefix(line, "??") {
			file := strings.TrimSpace(line[3:])
			absPath := filepath.Join(dir, file)
			data, readErr := os.ReadFile(absPath)
			if readErr == nil {
				lineCount := strings.Count(string(data), "\n")
				if len(data) > 0 && data[len(data)-1] != '\n' {
					lineCount++
				}
				a1 += lineCount
			}
		}
	}

	// For staged changes
	cmd3 := exec.CommandContext(ctx, "git", "diff", "--cached", "--numstat")
	cmd3.Dir = dir
	out3, err := cmd3.Output()
	if err != nil {
		return a1, d1, nil
	}
	a2, d2 := parseNumstat(string(out3))

	return a1 + a2, d1 + d2, nil
}

func parseNumstat(output string) (add, del int) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// Binary files show '-' instead of numbers
		if fields[0] == "-" || fields[1] == "-" {
			continue
		}
		a, err1 := strconv.Atoi(fields[0])
		d, err2 := strconv.Atoi(fields[1])
		if err1 == nil && err2 == nil {
			add += a
			del += d
		}
	}
	return
}

func sha256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}
