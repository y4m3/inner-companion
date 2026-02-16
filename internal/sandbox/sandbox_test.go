package sandbox

import "testing"

func TestAvailable(t *testing.T) {
	// Available should return a boolean without crashing.
	result := Available()
	t.Logf("Available() = %v", result)
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("/tmp/workspace")
	if cfg.WorkspaceDir != "/tmp/workspace" {
		t.Errorf("WorkspaceDir = %q, want /tmp/workspace", cfg.WorkspaceDir)
	}
	if cfg.Limits.MaxProcs != 256 {
		t.Errorf("MaxProcs = %d, want 256", cfg.Limits.MaxProcs)
	}
	if cfg.Limits.MaxMemory != 512*1024*1024 {
		t.Errorf("MaxMemory = %d, want %d", cfg.Limits.MaxMemory, 512*1024*1024)
	}
	if cfg.Limits.MaxCPUSec != 120 {
		t.Errorf("MaxCPUSec = %d, want 120", cfg.Limits.MaxCPUSec)
	}
	if len(cfg.ReadOnlyDirs) == 0 {
		t.Error("ReadOnlyDirs should not be empty")
	}
}
