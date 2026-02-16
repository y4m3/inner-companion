package agent

import (
	"context"
	"testing"

	"inner-companion/internal/protocol"
)

func TestEchoRunnerRun(t *testing.T) {
	t.Parallel()

	r := EchoRunner{}
	res, err := r.Run(context.Background(), protocol.AgentRequest{
		RequestID:    "r-1",
		SessionID:    "s-1",
		AgentID:      "a-1",
		InputText:    "hello",
		AllowedTools: []string{"read", "bash"},
		TimeoutSec:   120,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.RequestID != "r-1" {
		t.Fatalf("expected request_id r-1, got %q", res.RequestID)
	}
	if res.Status != "ok" {
		t.Fatalf("expected status ok, got %q", res.Status)
	}
	if res.AssistantText == "" {
		t.Fatalf("expected non-empty assistant text")
	}
}
