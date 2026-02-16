package tool

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// AutoCommit stages and commits all changes in the workspace.
// It is a no-op if the directory is not a git repo or if there are no changes.
func AutoCommit(ctx context.Context, workspaceDir string, changes ChangeSet, cmdSummary string) error {
	if len(changes.Files) == 0 {
		return nil
	}

	if !isGitRepo(ctx, workspaceDir) {
		return nil
	}

	// Stage all changes
	cmd := exec.CommandContext(ctx, "git", "add", "-A")
	cmd.Dir = workspaceDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %s: %w", out, err)
	}

	// Build commit message
	msg := buildCommitMessage(changes, cmdSummary)

	cmd2 := exec.CommandContext(ctx, "git", "commit", "-m", msg)
	cmd2.Dir = workspaceDir
	if out, err := cmd2.CombinedOutput(); err != nil {
		outStr := strings.TrimSpace(string(out))
		if strings.Contains(outStr, "nothing to commit") {
			return nil
		}
		return fmt.Errorf("git commit: %s: %w", out, err)
	}

	return nil
}

func buildCommitMessage(changes ChangeSet, cmdSummary string) string {
	// Truncate command summary for commit message
	if len(cmdSummary) > 60 {
		cmdSummary = cmdSummary[:60] + "..."
	}

	fileCount := len(changes.Files)
	lineInfo := fmt.Sprintf("+%d -%d", changes.AddLines, changes.DelLines)

	return fmt.Sprintf("auto: %s\n\n%d file(s) changed, %s", cmdSummary, fileCount, lineInfo)
}
