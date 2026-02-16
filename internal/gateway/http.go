package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"inner-companion/internal/protocol"
)

const maxRequestBodyBytes = 1 << 20 // 1 MiB

type httpHandler struct {
	svc *Service
}

// NewHTTPHandler returns a Phase 1 HTTP handler.
func NewHTTPHandler(svc *Service) http.Handler {
	return &httpHandler{svc: svc}
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/messages" {
		writeError(w, "unknown", "unknown", http.StatusNotFound, "not_found", "route not found")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, "unknown", "unknown", http.StatusMethodNotAllowed, "method_not_allowed", "method must be POST")
		return
	}
	ct := strings.TrimSpace(r.Header.Get("Content-Type"))
	if ct == "" {
		writeError(w, "unknown", "unknown", http.StatusUnsupportedMediaType, "unsupported_media_type", "content-type must be application/json")
		return
	}
	if mediaType, _, err := mime.ParseMediaType(ct); err != nil || mediaType != "application/json" {
		writeError(w, "unknown", "unknown", http.StatusUnsupportedMediaType, "unsupported_media_type", "content-type must be application/json")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, "unknown", "unknown", http.StatusRequestEntityTooLarge, "payload_too_large", "request body too large")
			return
		}
		writeError(w, "unknown", "unknown", http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	inbound, err := DecodeInboundUserMessage(payload)
	if err != nil {
		writeError(w, "unknown", "unknown", http.StatusBadRequest, "bad_request", "invalid request payload")
		return
	}

	res, err := h.svc.HandleUserMessage(r.Context(), inbound)
	if err != nil {
		writeError(w, inbound.SessionID, inbound.AgentID, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	if res.Status == "error" {
		msg := strings.TrimSpace(res.AssistantText)
		if msg == "" {
			msg = "agent returned error"
		}
		writeError(w, inbound.SessionID, inbound.AgentID, http.StatusBadGateway, "agent_error", msg)
		return
	}

	assistant := protocol.OutboundAssistantMessage{
		Type:        "assistant_message",
		SessionID:   inbound.SessionID,
		AgentID:     inbound.AgentID,
		Text:        res.AssistantText,
		ServerMsgID: res.RequestID,
		InReplyTo:   inbound.ClientMsgID,
	}
	if err := protocol.ValidateOutboundAssistantMessage(assistant); err != nil {
		writeError(w, inbound.SessionID, inbound.AgentID, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(assistant)
}

func writeError(w http.ResponseWriter, sessionID, agentID string, statusCode int, code, message string) {
	if sessionID == "" {
		sessionID = "unknown"
	}
	if agentID == "" {
		agentID = "unknown"
	}

	out := protocol.OutboundErrorMessage{
		Type:      "error",
		SessionID: sessionID,
		AgentID:   agentID,
		Code:      code,
		Message:   message,
	}
	if err := protocol.ValidateOutboundErrorMessage(out); err != nil {
		out = protocol.OutboundErrorMessage{
			Type:      "error",
			SessionID: "unknown",
			AgentID:   "unknown",
			Code:      "internal_error",
			Message:   "failed to encode error response",
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(out)
}
