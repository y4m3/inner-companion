package protocol

import (
	"fmt"
	"strings"
)

var phase1Tools = map[string]struct{}{
	"read": {},
	"bash": {},
}

const maxAgentTimeoutSec = 300

type requiredField struct {
	name  string
	value string
}

// OutboundAssistantMessage is a Gateway -> WebChat assistant message.
type OutboundAssistantMessage struct {
	Type        string `json:"type"`
	SessionID   string `json:"session_id"`
	AgentID     string `json:"agent_id"`
	Text        string `json:"text"`
	ServerMsgID string `json:"server_msg_id"`
	InReplyTo   string `json:"in_reply_to"`
}

// OutboundToolResult is a Gateway -> WebChat tool result message.
type OutboundToolResult struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
	ToolName  string `json:"tool_name"`
	OK        bool   `json:"ok"`
	Summary   string `json:"summary"`
	DetailRef string `json:"detail_ref"`
}

// OutboundErrorMessage is a Gateway -> WebChat error message.
type OutboundErrorMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

// HistoryContentBlock represents a single content block within a history message.
type HistoryContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
}

// HistoryMessage represents a stored conversation message.
type HistoryMessage struct {
	Role    string                `json:"role"`
	Content []HistoryContentBlock `json:"content"`
}

// AgentRequest is a Gateway -> Agent request.
type AgentRequest struct {
	RequestID    string   `json:"request_id"`
	SessionID    string   `json:"session_id"`
	AgentID      string   `json:"agent_id"`
	InputText    string   `json:"input_text"`
	AllowedTools []string `json:"allowed_tools"`
	TimeoutSec   int      `json:"timeout_sec"`

	History      []HistoryMessage `json:"-"`
	SystemPrompt string           `json:"-"`
}

// AgentToolCall is a tool call in AgentResponse.
type AgentToolCall struct {
	ToolName      string         `json:"tool_name"`
	Args          map[string]any `json:"args"`
	ResultSummary string         `json:"result_summary"`
	OK            bool           `json:"ok"`
}

// AgentResponse is an Agent -> Gateway response.
type AgentResponse struct {
	RequestID     string          `json:"request_id"`
	Status        string          `json:"status"`
	AssistantText string          `json:"assistant_text"`
	ToolCalls     []AgentToolCall `json:"tool_calls"`

	NewMessages []HistoryMessage `json:"-"`
}

func ValidateOutboundAssistantMessage(msg OutboundAssistantMessage) error {
	if msg.Type != "assistant_message" {
		return fmt.Errorf("invalid type: %q", msg.Type)
	}
	if err := validateRequired(
		requiredField{name: "session_id", value: msg.SessionID},
		requiredField{name: "agent_id", value: msg.AgentID},
		requiredField{name: "text", value: msg.Text},
		requiredField{name: "server_msg_id", value: msg.ServerMsgID},
		requiredField{name: "in_reply_to", value: msg.InReplyTo},
	); err != nil {
		return err
	}
	return nil
}

func ValidateOutboundToolResult(msg OutboundToolResult) error {
	if msg.Type != "tool_result" {
		return fmt.Errorf("invalid type: %q", msg.Type)
	}
	if err := validateRequired(
		requiredField{name: "session_id", value: msg.SessionID},
		requiredField{name: "agent_id", value: msg.AgentID},
		requiredField{name: "tool_name", value: msg.ToolName},
		requiredField{name: "summary", value: msg.Summary},
		requiredField{name: "detail_ref", value: msg.DetailRef},
	); err != nil {
		return err
	}
	if !isPhase1Tool(msg.ToolName) {
		return fmt.Errorf("tool_name must be read|bash")
	}
	return nil
}

func ValidateOutboundErrorMessage(msg OutboundErrorMessage) error {
	if msg.Type != "error" {
		return fmt.Errorf("invalid type: %q", msg.Type)
	}
	if err := validateRequired(
		requiredField{name: "session_id", value: msg.SessionID},
		requiredField{name: "agent_id", value: msg.AgentID},
		requiredField{name: "code", value: msg.Code},
		requiredField{name: "message", value: msg.Message},
	); err != nil {
		return err
	}
	return nil
}

func ValidateAgentRequest(req AgentRequest) error {
	if err := validateRequired(
		requiredField{name: "request_id", value: req.RequestID},
		requiredField{name: "session_id", value: req.SessionID},
		requiredField{name: "agent_id", value: req.AgentID},
		requiredField{name: "input_text", value: req.InputText},
	); err != nil {
		return err
	}
	if req.TimeoutSec <= 0 {
		return fmt.Errorf("timeout_sec must be > 0")
	}
	if req.TimeoutSec > maxAgentTimeoutSec {
		return fmt.Errorf("timeout_sec must be <= %d", maxAgentTimeoutSec)
	}
	if len(req.AllowedTools) == 0 {
		return fmt.Errorf("allowed_tools is required")
	}
	seen := map[string]struct{}{}
	for _, tool := range req.AllowedTools {
		if _, ok := seen[tool]; ok {
			return fmt.Errorf("allowed_tools contains duplicate: %q", tool)
		}
		seen[tool] = struct{}{}
		if !isPhase1Tool(tool) {
			return fmt.Errorf("allowed_tools includes unsupported tool: %q", tool)
		}
	}
	return nil
}

func ValidateAgentResponse(res AgentResponse) error {
	if err := validateRequired(
		requiredField{name: "request_id", value: res.RequestID},
		requiredField{name: "status", value: res.Status},
	); err != nil {
		return err
	}
	if res.Status != "ok" && res.Status != "error" {
		return fmt.Errorf("status must be ok|error")
	}
	if res.Status == "ok" && strings.TrimSpace(res.AssistantText) == "" {
		return fmt.Errorf("assistant_text is required when status is ok")
	}
	for _, call := range res.ToolCalls {
		if !isPhase1Tool(call.ToolName) {
			return fmt.Errorf("tool_calls.tool_name must be read|bash")
		}
		if call.Args == nil {
			return fmt.Errorf("tool_calls.args is required")
		}
		if strings.TrimSpace(call.ResultSummary) == "" {
			return fmt.Errorf("tool_calls.result_summary is required")
		}
	}
	return nil
}

func validateRequired(fields ...requiredField) error {
	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}
	return nil
}

func isPhase1Tool(tool string) bool {
	_, ok := phase1Tools[tool]
	return ok
}
