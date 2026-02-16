package sandbox

import (
	"testing"
)

func TestCgroupAvailable(t *testing.T) {
	// Just verify it returns a bool and doesn't panic
	result := CgroupAvailable()
	t.Logf("CgroupAvailable() = %v", result)
}
