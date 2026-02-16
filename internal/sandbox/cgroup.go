package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
)

// CgroupAvailable returns true if systemd-run --user is available.
func CgroupAvailable() bool {
	err := exec.Command("systemd-run", "--user", "--scope", "--", "true").Run()
	return err == nil
}

// RunInCgroup runs a command inside a systemd user scope with resource limits.
func RunInCgroup(ctx context.Context, cfg Config, command string) (string, error) {
	self, err := exec.LookPath("bash")
	if err != nil {
		return "", fmt.Errorf("cgroup: bash not found: %w", err)
	}

	args := []string{
		"--user",
		"--scope",
		"--property=CPUQuota=100%",
		"--property=MemoryMax=512M",
		"--",
		self, "-c", command,
	}

	cmd := exec.CommandContext(ctx, "systemd-run", args...)
	cmd.Dir = cfg.WorkspaceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("cgroup: %s: %w", stderr.String(), err)
		}
		return "", fmt.Errorf("cgroup: %w", err)
	}

	return stdout.String(), nil
}

// cgroupEnabled is set once at package init time.
var cgroupEnabled bool

func init() {
	cgroupEnabled = CgroupAvailable()
	if cgroupEnabled {
		log.Printf("cgroup: systemd-run --user available, resource limits enabled")
	} else {
		log.Printf("cgroup: systemd-run --user not available, resource limits disabled")
	}
}
