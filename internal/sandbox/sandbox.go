package sandbox

import "log"

// Config holds sandbox configuration.
type Config struct {
	WorkspaceDir string
	ReadOnlyDirs []string // e.g., /usr, /lib, /bin
	Limits       ResourceLimits
}

// ResourceLimits holds resource limit configuration.
type ResourceLimits struct {
	MaxProcs  uint64 // RLIMIT_NPROC (default 256)
	MaxMemory uint64 // RLIMIT_AS in bytes (default 512MB)
	MaxCPUSec uint64 // RLIMIT_CPU in seconds (default 120)
}

// DefaultConfig returns a default sandbox configuration.
func DefaultConfig(workspaceDir string) Config {
	return Config{
		WorkspaceDir: workspaceDir,
		ReadOnlyDirs: []string{"/usr", "/lib", "/lib64", "/bin", "/sbin"},
		Limits: ResourceLimits{
			MaxProcs:  256,
			MaxMemory: 512 * 1024 * 1024, // 512 MiB
			MaxCPUSec: 120,
		},
	}
}

// Available returns true if all sandbox mechanisms are available on this system.
// Returns false on non-Linux or if kernel features are missing.
func Available() bool {
	return landlockAvailable() && seccompAvailable()
}

// logWarning logs a warning message. Extracted for testability.
func logWarning(format string, args ...any) {
	log.Printf("WARNING: "+format, args...)
}
