package tool

import (
	"context"
	"log"
	"sync"

	"inner-companion/internal/audit"
	"inner-companion/internal/classify"
	"inner-companion/internal/protocol"
)

// ClassifiedBashTool wraps BashTool with command classification and policy enforcement.
type ClassifiedBashTool struct {
	inner        *BashTool
	classify     func(string) classify.ClassifyResult
	autonomy     string
	logger       audit.Logger
	mu           sync.Mutex
	l2Count      int
	l2FileCount  int
	l2LineCount  int
	workspaceDir string
	autoCommit   bool
}

// NewClassifiedBashTool creates a ClassifiedBashTool that enforces security policies.
func NewClassifiedBashTool(inner *BashTool, classifyFn func(string) classify.ClassifyResult, autonomy string, logger audit.Logger) *ClassifiedBashTool {
	return &ClassifiedBashTool{
		inner:    inner,
		classify: classifyFn,
		autonomy: autonomy,
		logger:   logger,
	}
}

// SetWorkspaceDir sets the workspace directory for change detection.
func (c *ClassifiedBashTool) SetWorkspaceDir(dir string) {
	c.workspaceDir = dir
}

// SetAutoCommit enables or disables automatic git commits after changes.
func (c *ClassifiedBashTool) SetAutoCommit(enabled bool) {
	c.autoCommit = enabled
}

func (c *ClassifiedBashTool) Name() string { return "bash" }

func (c *ClassifiedBashTool) Execute(ctx context.Context, args map[string]any) Result {
	cmdStr, ok := args["command"]
	if !ok {
		return Result{Output: "missing required argument: command", IsError: true}
	}
	command, ok := cmdStr.(string)
	if !ok || command == "" {
		return Result{Output: "command must be a non-empty string", IsError: true}
	}

	cr := c.classify(command)
	sessionID := protocol.SessionIDFromContext(ctx)

	switch {
	case cr.Level == classify.L3:
		c.logEvent(sessionID, "bash_denied", command, cr.Level, "L3 commands are not allowed")
		return Result{Output: "command denied: L3 commands are not allowed in Phase 1", IsError: true}
	case c.autonomy == "supervised" && cr.Level == classify.L2:
		c.mu.Lock()
		c.l2Count++
		count := c.l2Count
		fileCount := c.l2FileCount
		lineCount := c.l2LineCount
		c.mu.Unlock()
		if count > 20 || fileCount >= 10 || lineCount >= 500 {
			c.logEvent(sessionID, "bash_denied", command, cr.Level, "L2 limit exceeded")
			return Result{Output: "command denied: L2 command limit exceeded (20)", IsError: true}
		}
	}

	result := c.inner.Execute(ctx, args)

	c.logEvent(sessionID, "bash_executed", command, cr.Level, "")

	// Detect changes after bash execution
	if c.workspaceDir != "" {
		cs, err := DetectChanges(ctx, c.workspaceDir)
		if err == nil && len(cs.Files) > 0 {
			c.logChangeset(sessionID, command, cs)

			// Update L2 cumulative counters
			c.mu.Lock()
			c.l2FileCount += len(cs.Files)
			c.l2LineCount += cs.AddLines + cs.DelLines
			c.mu.Unlock()

			// Auto-commit if enabled
			if c.autoCommit {
				if commitErr := AutoCommit(ctx, c.workspaceDir, cs, command); commitErr != nil {
					log.Printf("auto-commit failed: %v", commitErr)
				}
			}
		}
	}

	return result
}

func (c *ClassifiedBashTool) logEvent(sessionID, event, command string, level classify.Level, reason string) {
	if c.logger == nil {
		return
	}
	detail := map[string]any{
		"command": command,
		"level":   int(level),
	}
	if reason != "" {
		detail["reason"] = reason
	}
	c.logger.Log(audit.Entry{
		SessionID: sessionID,
		Event:     event,
		Detail:    detail,
	})
}

func (c *ClassifiedBashTool) logChangeset(sessionID, command string, cs ChangeSet) {
	if c.logger == nil {
		return
	}
	checksums := make(map[string]string)
	for _, f := range cs.Files {
		checksums[f.Path] = f.Checksum
	}
	c.logger.Log(audit.Entry{
		SessionID: sessionID,
		Event:     "bash_changeset",
		Detail: map[string]any{
			"command":   command,
			"files":     len(cs.Files),
			"add_lines": cs.AddLines,
			"del_lines": cs.DelLines,
			"checksums": checksums,
		},
	})
}

// Ensure ClassifiedBashTool implements Executor.
var _ Executor = (*ClassifiedBashTool)(nil)
