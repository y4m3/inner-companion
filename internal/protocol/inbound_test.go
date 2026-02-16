package protocol

import (
	"strings"
	"testing"
)

func TestValidateInboundUserMessage(t *testing.T) {
	t.Parallel()

	valid := InboundUserMessage{
		Type:        "user_message",
		SessionID:   "s-1",
		AgentID:     "a-1",
		Text:        "hello",
		ClientMsgID: "c-1",
	}

	if err := ValidateInboundUserMessage(valid); err != nil {
		t.Fatalf("expected valid message, got error: %v", err)
	}
}

func TestValidateInboundUserMessage_RequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		msg     InboundUserMessage
		errPart string
	}{
		{
			name: "type must be user_message",
			msg: InboundUserMessage{
				Type:        "assistant_message",
				SessionID:   "s-1",
				AgentID:     "a-1",
				Text:        "hello",
				ClientMsgID: "c-1",
			},
			errPart: "type",
		},
		{
			name: "session_id is required",
			msg: InboundUserMessage{
				Type:        "user_message",
				SessionID:   " ",
				AgentID:     "a-1",
				Text:        "hello",
				ClientMsgID: "c-1",
			},
			errPart: "session_id",
		},
		{
			name: "agent_id is required",
			msg: InboundUserMessage{
				Type:        "user_message",
				SessionID:   "s-1",
				AgentID:     "",
				Text:        "hello",
				ClientMsgID: "c-1",
			},
			errPart: "agent_id",
		},
		{
			name: "text is required",
			msg: InboundUserMessage{
				Type:        "user_message",
				SessionID:   "s-1",
				AgentID:     "a-1",
				Text:        "",
				ClientMsgID: "c-1",
			},
			errPart: "text",
		},
		{
			name: "client_msg_id is required",
			msg: InboundUserMessage{
				Type:        "user_message",
				SessionID:   "s-1",
				AgentID:     "a-1",
				Text:        "hello",
				ClientMsgID: "",
			},
			errPart: "client_msg_id",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateInboundUserMessage(tt.msg)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.errPart) {
				t.Fatalf("expected error to contain %q, got %q", tt.errPart, err.Error())
			}
		})
	}
}

func TestValidateInboundUserMessage_ErrorOrderIsDeterministic(t *testing.T) {
	t.Parallel()

	msg := InboundUserMessage{Type: "user_message"}
	err := ValidateInboundUserMessage(msg)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "session_id") {
		t.Fatalf("expected first missing field error to be session_id, got %q", err.Error())
	}
}
