package agent

import (
	"context"
	"strings"

	"inner-companion/internal/protocol"
)

// EchoRunner is a Phase 1 placeholder agent runner.
type EchoRunner struct{}

func (EchoRunner) Run(_ context.Context, req protocol.AgentRequest) (protocol.AgentResponse, error) {
	text := strings.TrimSpace(req.InputText)
	if text == "" {
		text = "(empty)"
	}

	return protocol.AgentResponse{
		RequestID:     req.RequestID,
		Status:        "ok",
		AssistantText: "echo: " + text,
		ToolCalls:     nil,
	}, nil
}
