package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

const sandboxArg = "__sandbox__"

// IsSandboxChild returns true if this process was launched as a sandbox child.
func IsSandboxChild() bool {
	return len(os.Args) > 1 && os.Args[1] == sandboxArg
}

// RunChild is called in the child process. It applies sandbox restrictions
// and then execs the given command via bash.
// This function does not return on success (it execs).
func RunChild() {
	// Args: self __sandbox__ <workspace_dir> <command>
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "sandbox: insufficient arguments\n")
		os.Exit(1)
	}

	workspaceDir := os.Args[2]
	command := os.Args[3]

	cfg := DefaultConfig(workspaceDir)

	// Apply restrictions in order: landlock, seccomp, rlimits
	if err := applyLandlock(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "sandbox: landlock: %v\n", err)
		os.Exit(1)
	}
	if err := applySeccomp(); err != nil {
		fmt.Fprintf(os.Stderr, "sandbox: seccomp: %v\n", err)
		os.Exit(1)
	}
	if err := applyRlimits(cfg.Limits); err != nil {
		fmt.Fprintf(os.Stderr, "sandbox: rlimits: %v\n", err)
		os.Exit(1)
	}

	// Exec bash -c command
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sandbox: bash not found: %v\n", err)
		os.Exit(1)
	}
	err = unix.Exec(bashPath, []string{"bash", "-c", command}, os.Environ())
	fmt.Fprintf(os.Stderr, "sandbox: exec: %v\n", err)
	os.Exit(1)
}

// RunInSandbox runs a command inside a sandbox by re-executing the current binary.
func RunInSandbox(ctx context.Context, cfg Config, command string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("sandbox: executable path: %w", err)
	}

	cmd := exec.CommandContext(ctx, self, sandboxArg, cfg.WorkspaceDir, command)
	cmd.Dir = cfg.WorkspaceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("sandbox: %s: %w", stderr.String(), err)
		}
		return "", fmt.Errorf("sandbox: %w", err)
	}

	return stdout.String(), nil
}
