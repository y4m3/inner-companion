package protocol

import (
	"context"
	"testing"
)

func TestWithSessionID_SetAndGet(t *testing.T) {
	ctx := context.Background()
	ctx = WithSessionID(ctx, "session-123")

	got := SessionIDFromContext(ctx)
	if got != "session-123" {
		t.Errorf("SessionIDFromContext = %q, want %q", got, "session-123")
	}
}

func TestSessionIDFromContext_Missing(t *testing.T) {
	ctx := context.Background()

	got := SessionIDFromContext(ctx)
	if got != "" {
		t.Errorf("SessionIDFromContext = %q, want empty string", got)
	}
}
