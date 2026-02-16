package sandbox

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func applyRlimits(limits ResourceLimits) error {
	// Set RLIMIT_NPROC
	if err := unix.Setrlimit(unix.RLIMIT_NPROC, &unix.Rlimit{
		Cur: limits.MaxProcs,
		Max: limits.MaxProcs,
	}); err != nil {
		return fmt.Errorf("setrlimit NPROC: %w", err)
	}

	// Set RLIMIT_AS (address space)
	if err := unix.Setrlimit(unix.RLIMIT_AS, &unix.Rlimit{
		Cur: limits.MaxMemory,
		Max: limits.MaxMemory,
	}); err != nil {
		return fmt.Errorf("setrlimit AS: %w", err)
	}

	// Set RLIMIT_CPU
	if err := unix.Setrlimit(unix.RLIMIT_CPU, &unix.Rlimit{
		Cur: limits.MaxCPUSec,
		Max: limits.MaxCPUSec,
	}); err != nil {
		return fmt.Errorf("setrlimit CPU: %w", err)
	}

	return nil
}
