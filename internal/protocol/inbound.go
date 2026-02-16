package protocol

import (
	"fmt"
)

// InboundUserMessage is the Phase 1 WebChat -> Gateway payload.
type InboundUserMessage struct {
	Type        string `json:"type"`
	SessionID   string `json:"session_id"`
	AgentID     string `json:"agent_id"`
	Text        string `json:"text"`
	ClientMsgID string `json:"client_msg_id"`
}

// ValidateInboundUserMessage validates required fields from the Phase 1 spec.
func ValidateInboundUserMessage(msg InboundUserMessage) error {
	if msg.Type != "user_message" {
		return fmt.Errorf("invalid type: %q", msg.Type)
	}

	fields := []requiredField{
		{name: "session_id", value: msg.SessionID},
		{name: "agent_id", value: msg.AgentID},
		{name: "text", value: msg.Text},
		{name: "client_msg_id", value: msg.ClientMsgID},
	}

	if err := validateRequired(fields...); err != nil {
		return err
	}

	return nil
}
