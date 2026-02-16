package gateway

import (
	"strings"
	"testing"
)

func TestDecodeInboundUserMessage(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"type":"user_message","session_id":"s-1","agent_id":"a-1","text":"hello","client_msg_id":"c-1"}`)
	msg, err := DecodeInboundUserMessage(payload)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if msg.SessionID != "s-1" {
		t.Fatalf("expected session_id s-1, got %q", msg.SessionID)
	}
}

func TestDecodeInboundUserMessage_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := DecodeInboundUserMessage([]byte(`{"type"`))
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "invalid json") {
		t.Fatalf("expected invalid json error, got %q", err.Error())
	}
}

func TestDecodeInboundUserMessage_ContractViolation(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"type":"assistant_message","session_id":"s-1","agent_id":"a-1","text":"hello","client_msg_id":"c-1"}`)
	_, err := DecodeInboundUserMessage(payload)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "invalid inbound message") {
		t.Fatalf("expected contract error, got %q", err.Error())
	}
}
