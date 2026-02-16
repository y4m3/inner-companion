package tool

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"time"

	"inner-companion/internal/sandbox"
)

const (
	bashTimeout   = 120 * time.Second
	maxOutputSize = 1 << 20 // 1 MiB
)

// CommandRunner executes a shell command and returns its output.
type CommandRunner func(ctx context.Context, command string) (string, error)

// BashTool executes shell commands, optionally inside a sandbox.
type BashTool struct {
	workspaceDir string
	runner       CommandRunner
}

// NewBashTool creates a BashTool with the given workspace directory.
// It uses sandbox execution if available, otherwise falls back to direct exec.
func NewBashTool(workspaceDir string) *BashTool {
	b := &BashTool{workspaceDir: workspaceDir}
	if sandbox.Available() {
		cfg := sandbox.DefaultConfig(workspaceDir)
		b.runner = func(ctx context.Context, command string) (string, error) {
			return sandbox.RunInSandbox(ctx, cfg, command)
		}
	} else {
		log.Printf("WARNING: sandbox not available, commands will execute directly")
		b.runner = b.directExec
	}
	return b
}

// newBashToolDirect creates a BashTool that always uses direct exec (for testing).
func newBashToolDirect(workspaceDir string) *BashTool {
	b := &BashTool{workspaceDir: workspaceDir}
	b.runner = b.directExec
	return b
}

func (b *BashTool) Name() string { return "bash" }

func (b *BashTool) Execute(ctx context.Context, args map[string]any) Result {
	cmdStr, ok := args["command"]
	if !ok {
		return Result{Output: "missing required argument: command", IsError: true}
	}
	command, ok := cmdStr.(string)
	if !ok || command == "" {
		return Result{Output: "command must be a non-empty string", IsError: true}
	}

	ctx, cancel := context.WithTimeout(ctx, bashTimeout)
	defer cancel()

	output, err := b.runner(ctx, command)
	if err != nil {
		errMsg := err.Error()
		if output != "" {
			errMsg = output + "\n" + errMsg
		}
		return Result{Output: errMsg, IsError: true}
	}

	if len(output) > maxOutputSize {
		output = output[:maxOutputSize] + fmt.Sprintf("\n... (output truncated at 1MiB)")
	}

	return Result{Output: output, IsError: false}
}

func (b *BashTool) directExec(ctx context.Context, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = b.workspaceDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
