package protocol

import (
	"strings"
	"testing"
)

func TestValidateOutboundAssistantMessage(t *testing.T) {
	t.Parallel()

	msg := OutboundAssistantMessage{
		Type:        "assistant_message",
		SessionID:   "s-1",
		AgentID:     "a-1",
		Text:        "done",
		ServerMsgID: "m-1",
		InReplyTo:   "c-1",
	}
	if err := ValidateOutboundAssistantMessage(msg); err != nil {
		t.Fatalf("expected valid assistant message, got %v", err)
	}
}

func TestValidateOutboundToolResult(t *testing.T) {
	t.Parallel()

	msg := OutboundToolResult{
		Type:      "tool_result",
		SessionID: "s-1",
		AgentID:   "a-1",
		ToolName:  "read",
		OK:        true,
		Summary:   "read 10 lines",
		DetailRef: "log-1",
	}
	if err := ValidateOutboundToolResult(msg); err != nil {
		t.Fatalf("expected valid tool result, got %v", err)
	}
}

func TestValidateOutboundErrorMessage(t *testing.T) {
	t.Parallel()

	msg := OutboundErrorMessage{
		Type:      "error",
		SessionID: "s-1",
		AgentID:   "a-1",
		Code:      "bad_request",
		Message:   "invalid payload",
	}
	if err := ValidateOutboundErrorMessage(msg); err != nil {
		t.Fatalf("expected valid error message, got %v", err)
	}
}

func TestValidateOutboundContract_InvalidToolName(t *testing.T) {
	t.Parallel()

	msg := OutboundToolResult{
		Type:      "tool_result",
		SessionID: "s-1",
		AgentID:   "a-1",
		ToolName:  "write",
		OK:        true,
		Summary:   "x",
		DetailRef: "y",
	}
	err := ValidateOutboundToolResult(msg)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "tool_name") {
		t.Fatalf("expected tool_name error, got %q", err.Error())
	}
}

func TestValidateAgentRequest(t *testing.T) {
	t.Parallel()

	req := AgentRequest{
		RequestID:    "r-1",
		SessionID:    "s-1",
		AgentID:      "a-1",
		InputText:    "hello",
		AllowedTools: []string{"read", "bash"},
		TimeoutSec:   120,
	}
	if err := ValidateAgentRequest(req); err != nil {
		t.Fatalf("expected valid agent request, got %v", err)
	}
}

func TestValidateAgentRequest_DuplicateAllowedToolsRejected(t *testing.T) {
	t.Parallel()

	req := AgentRequest{
		RequestID:    "r-1",
		SessionID:    "s-1",
		AgentID:      "a-1",
		InputText:    "hello",
		AllowedTools: []string{"read", "read"},
		TimeoutSec:   120,
	}
	err := ValidateAgentRequest(req)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "allowed_tools") {
		t.Fatalf("expected allowed_tools error, got %q", err.Error())
	}
}

func TestValidateAgentRequest_TimeoutUpperBound(t *testing.T) {
	t.Parallel()

	req := AgentRequest{
		RequestID:    "r-1",
		SessionID:    "s-1",
		AgentID:      "a-1",
		InputText:    "hello",
		AllowedTools: []string{"read"},
		TimeoutSec:   10000,
	}
	err := ValidateAgentRequest(req)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "timeout_sec") {
		t.Fatalf("expected timeout_sec error, got %q", err.Error())
	}
}

func TestValidateAgentResponse(t *testing.T) {
	t.Parallel()

	res := AgentResponse{
		RequestID:     "r-1",
		Status:        "ok",
		AssistantText: "done",
		ToolCalls: []AgentToolCall{
			{ToolName: "read", Args: map[string]any{"path": "a.txt"}, ResultSummary: "ok", OK: true},
		},
	}
	if err := ValidateAgentResponse(res); err != nil {
		t.Fatalf("expected valid agent response, got %v", err)
	}
}

func TestValidateAgentResponse_OkRequiresAssistantText(t *testing.T) {
	t.Parallel()

	res := AgentResponse{RequestID: "r-1", Status: "ok", AssistantText: "   "}
	err := ValidateAgentResponse(res)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "assistant_text") {
		t.Fatalf("expected assistant_text error, got %q", err.Error())
	}
}

func TestValidateAgentResponse_ToolCallRequiresSummaryAndArgs(t *testing.T) {
	t.Parallel()

	res := AgentResponse{
		RequestID:     "r-1",
		Status:        "ok",
		AssistantText: "done",
		ToolCalls: []AgentToolCall{
			{ToolName: "read", Args: nil, ResultSummary: "ok", OK: true},
		},
	}
	err := ValidateAgentResponse(res)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "tool_calls.args") {
		t.Fatalf("expected tool_calls.args error, got %q", err.Error())
	}

	res.ToolCalls[0].Args = map[string]any{"path": "a.txt"}
	res.ToolCalls[0].ResultSummary = " "
	err = ValidateAgentResponse(res)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "tool_calls.result_summary") {
		t.Fatalf("expected tool_calls.result_summary error, got %q", err.Error())
	}
}

func TestValidateAgentRequest_InvalidAllowedTools(t *testing.T) {
	t.Parallel()

	req := AgentRequest{
		RequestID:    "r-1",
		SessionID:    "s-1",
		AgentID:      "a-1",
		InputText:    "hello",
		AllowedTools: []string{"read", "write"},
		TimeoutSec:   120,
	}
	err := ValidateAgentRequest(req)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "allowed_tools") {
		t.Fatalf("expected allowed_tools error, got %q", err.Error())
	}
}

func TestValidateAgentResponse_InvalidStatus(t *testing.T) {
	t.Parallel()

	res := AgentResponse{RequestID: "r-1", Status: "pending", AssistantText: ""}
	err := ValidateAgentResponse(res)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "status") {
		t.Fatalf("expected status error, got %q", err.Error())
	}
}
