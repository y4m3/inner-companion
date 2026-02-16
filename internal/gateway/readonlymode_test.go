package gateway

import (
	"context"
	"testing"
	"time"

	"inner-companion/internal/audit"
)

func TestModeManager_StartsInNormalMode(t *testing.T) {
	mm := NewModeManager(audit.NopLogger{})
	if got := mm.CurrentMode(); got != ModeNormal {
		t.Errorf("CurrentMode() = %q, want %q", got, ModeNormal)
	}
}

func TestModeManager_ThreeFailuresTransitionToReadonly(t *testing.T) {
	mm := NewModeManager(audit.NopLogger{})
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()

	if got := mm.CurrentMode(); got != ModeNormal {
		t.Errorf("after 2 failures: CurrentMode() = %q, want %q", got, ModeNormal)
	}

	mm.RecordLLMFailure()
	if got := mm.CurrentMode(); got != ModeReadonly {
		t.Errorf("after 3 failures: CurrentMode() = %q, want %q", got, ModeReadonly)
	}
}

func TestModeManager_SuccessResetsFailCount(t *testing.T) {
	mm := NewModeManager(audit.NopLogger{})
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()
	mm.RecordLLMSuccess()

	// After reset, 2 more failures should not trigger readonly
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()
	if got := mm.CurrentMode(); got != ModeNormal {
		t.Errorf("after reset + 2 failures: CurrentMode() = %q, want %q", got, ModeNormal)
	}
}

func TestModeManager_TwoAuthErrorsTransitionToReadonly(t *testing.T) {
	mm := NewModeManager(audit.NopLogger{})
	mm.RecordLLMAuthError()

	if got := mm.CurrentMode(); got != ModeNormal {
		t.Errorf("after 1 auth error: CurrentMode() = %q, want %q", got, ModeNormal)
	}

	mm.RecordLLMAuthError()
	if got := mm.CurrentMode(); got != ModeReadonly {
		t.Errorf("after 2 auth errors: CurrentMode() = %q, want %q", got, ModeReadonly)
	}
}

func TestModeManager_TryRecover_BeforeCooldown(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mm := NewModeManager(audit.NopLogger{})
	mm.nowFn = func() time.Time { return now }

	// Force readonly
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()

	// Still within cooldown
	now = now.Add(2 * time.Minute)
	if mm.TryRecover(context.Background()) {
		t.Error("TryRecover() = true before cooldown, want false")
	}
	if got := mm.CurrentMode(); got != ModeReadonly {
		t.Errorf("CurrentMode() = %q, want %q", got, ModeReadonly)
	}
}

func TestModeManager_TryRecover_AfterCooldown(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mm := NewModeManager(audit.NopLogger{})
	mm.nowFn = func() time.Time { return now }

	// Force readonly
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()

	// Past cooldown
	now = now.Add(6 * time.Minute)
	if !mm.TryRecover(context.Background()) {
		t.Error("TryRecover() = false after cooldown, want true")
	}
	if got := mm.CurrentMode(); got != ModeNormal {
		t.Errorf("CurrentMode() = %q, want %q", got, ModeNormal)
	}
}

func TestModeManager_TryRecover_HealthCheckTwoSuccesses(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mm := NewModeManager(audit.NopLogger{})
	mm.nowFn = func() time.Time { return now }

	// Force readonly
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()

	// Set healthCheck that always returns true
	mm.SetHealthCheck(func(ctx context.Context) bool {
		return true
	})

	// Past cooldown
	now = now.Add(6 * time.Minute)
	if !mm.TryRecover(context.Background()) {
		t.Error("TryRecover() = false, want true when health check succeeds twice")
	}
	if got := mm.CurrentMode(); got != ModeNormal {
		t.Errorf("CurrentMode() = %q, want %q", got, ModeNormal)
	}
}

func TestModeManager_TryRecover_HealthCheckFirstFails(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mm := NewModeManager(audit.NopLogger{})
	mm.nowFn = func() time.Time { return now }

	// Force readonly
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()

	// Set healthCheck that returns false on first call
	mm.SetHealthCheck(func(ctx context.Context) bool {
		return false
	})

	// Past cooldown
	now = now.Add(6 * time.Minute)
	if mm.TryRecover(context.Background()) {
		t.Error("TryRecover() = true, want false when first health check fails")
	}
	if got := mm.CurrentMode(); got != ModeReadonly {
		t.Errorf("CurrentMode() = %q, want %q", got, ModeReadonly)
	}
}

func TestModeManager_TryRecover_HealthCheckSecondFails(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mm := NewModeManager(audit.NopLogger{})
	mm.nowFn = func() time.Time { return now }

	// Force readonly
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()

	// Set healthCheck that returns true first, then false
	callCount := 0
	mm.SetHealthCheck(func(ctx context.Context) bool {
		callCount++
		return callCount <= 1 // true on first, false on second
	})

	// Past cooldown
	now = now.Add(6 * time.Minute)
	if mm.TryRecover(context.Background()) {
		t.Error("TryRecover() = true, want false when second health check fails")
	}
	if got := mm.CurrentMode(); got != ModeReadonly {
		t.Errorf("CurrentMode() = %q, want %q", got, ModeReadonly)
	}
}

func TestModeManager_TryRecover_NilHealthCheckSkips(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mm := NewModeManager(audit.NopLogger{})
	mm.nowFn = func() time.Time { return now }

	// Force readonly
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()

	// No healthCheck set (nil) - should skip health checks (backward compat)

	// Past cooldown
	now = now.Add(6 * time.Minute)
	if !mm.TryRecover(context.Background()) {
		t.Error("TryRecover() = false, want true when healthCheck is nil")
	}
	if got := mm.CurrentMode(); got != ModeNormal {
		t.Errorf("CurrentMode() = %q, want %q", got, ModeNormal)
	}
}

func TestModeManager_TransitionsAreLogged(t *testing.T) {
	var entries []audit.Entry
	logger := &captureLogger{entries: &entries}

	mm := NewModeManager(logger)
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()
	mm.RecordLLMFailure()

	if len(entries) != 1 {
		t.Fatalf("got %d log entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Event != "mode_transition" {
		t.Errorf("Event = %q, want %q", e.Event, "mode_transition")
	}
	if e.Detail["from"] != "normal" {
		t.Errorf("Detail[from] = %v, want %q", e.Detail["from"], "normal")
	}
	if e.Detail["to"] != "readonly" {
		t.Errorf("Detail[to] = %v, want %q", e.Detail["to"], "readonly")
	}
}

// captureLogger is a test helper that records logged entries.
type captureLogger struct {
	entries *[]audit.Entry
}

func (c *captureLogger) Log(entry audit.Entry) error {
	*c.entries = append(*c.entries, entry)
	return nil
}

func (c *captureLogger) Close() error { return nil }
