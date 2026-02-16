package gateway

import (
	"encoding/json"
	"fmt"

	"inner-companion/internal/protocol"
)

// DecodeInboundUserMessage decodes and validates a Phase 1 inbound payload.
func DecodeInboundUserMessage(payload []byte) (protocol.InboundUserMessage, error) {
	var msg protocol.InboundUserMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return protocol.InboundUserMessage{}, fmt.Errorf("invalid json: %w", err)
	}
	if err := protocol.ValidateInboundUserMessage(msg); err != nil {
		return protocol.InboundUserMessage{}, fmt.Errorf("invalid inbound message: %w", err)
	}
	return msg, nil
}
