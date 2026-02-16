package sandbox

import "testing"

func TestLandlockAvailable(t *testing.T) {
	// landlockAvailable should return a bool without crashing.
	// It may return true or false depending on kernel support.
	result := landlockAvailable()
	t.Logf("landlockAvailable() = %v", result)
}
