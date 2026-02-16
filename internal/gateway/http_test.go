package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"inner-companion/internal/protocol"
)

type errReadCloser struct{}

func (errReadCloser) Read(_ []byte) (int, error) { return 0, errors.New("read failure") }
func (errReadCloser) Close() error               { return nil }

func TestHTTPHandler_PostMessages_Success(t *testing.T) {
	t.Parallel()

	runner := &fakeAgentRunner{resp: protocol.AgentResponse{Status: "ok", AssistantText: "done"}}
	svc := NewService(runner)
	t.Cleanup(svc.Shutdown)
	h := NewHTTPHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"type":"user_message","session_id":"s-1","agent_id":"a-1","text":"hello","client_msg_id":"c-1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "assistant_message") {
		t.Fatalf("expected assistant_message payload, got %s", rec.Body.String())
	}
}

func TestHTTPHandler_PostMessages_AgentStatusError(t *testing.T) {
	t.Parallel()

	runner := &fakeAgentRunner{resp: protocol.AgentResponse{Status: "error", AssistantText: "model failed"}}
	svc := NewService(runner)
	t.Cleanup(svc.Shutdown)
	h := NewHTTPHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"type":"user_message","session_id":"s-1","agent_id":"a-1","text":"hello","client_msg_id":"c-1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"type":"error"`) {
		t.Fatalf("expected error payload, got %s", rec.Body.String())
	}
}

func TestHTTPHandler_PostMessages_InvalidPayload(t *testing.T) {
	t.Parallel()

	svc := NewService(&fakeAgentRunner{})
	t.Cleanup(svc.Shutdown)
	h := NewHTTPHandler(svc)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"type":"assistant_message"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "error") {
		t.Fatalf("expected error payload, got %s", rec.Body.String())
	}

	var msg protocol.OutboundErrorMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if msg.SessionID == "" || msg.AgentID == "" {
		t.Fatalf("error payload must include session_id/agent_id, got %+v", msg)
	}
	if msg.Message != "invalid request payload" {
		t.Fatalf("expected generic bad request message, got %q", msg.Message)
	}
}

func TestHTTPHandler_PostMessages_UnsupportedMediaType(t *testing.T) {
	t.Parallel()

	svc := NewService(&fakeAgentRunner{})
	t.Cleanup(svc.Shutdown)
	h := NewHTTPHandler(svc)

	cases := []struct {
		name        string
		contentType string
	}{
		{"wrong type", "text/plain"},
		{"missing", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"type":"user_message"}`))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("expected 415, got %d", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), `"type":"error"`) {
				t.Fatalf("expected error payload, got %s", rec.Body.String())
			}
		})
	}
}

func TestHTTPHandler_PostMessages_ReadBodyFailureIsServerError(t *testing.T) {
	t.Parallel()

	svc := NewService(&fakeAgentRunner{})
	t.Cleanup(svc.Shutdown)
	h := NewHTTPHandler(svc)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", io.NopCloser(bytes.NewBuffer(nil)))
	req.Header.Set("Content-Type", "application/json")
	req.Body = errReadCloser{}
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestHTTPHandler_PostMessages_InternalErrorDoesNotLeakDetails(t *testing.T) {
	t.Parallel()

	runner := &fakeAgentRunner{err: errors.New("db timeout: secret-token-123")}
	svc := NewService(runner)
	t.Cleanup(svc.Shutdown)
	h := NewHTTPHandler(svc)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"type":"user_message","session_id":"s-1","agent_id":"a-1","text":"hello","client_msg_id":"c-1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret-token-123") {
		t.Fatalf("expected internal details to be masked, got %s", rec.Body.String())
	}
}

func TestHTTPHandler_PostMessages_TooLargePayload(t *testing.T) {
	t.Parallel()

	svc := NewService(&fakeAgentRunner{})
	t.Cleanup(svc.Shutdown)
	h := NewHTTPHandler(svc)
	large := strings.Repeat("a", maxRequestBodyBytes+1)
	payload := `{"type":"user_message","session_id":"s-1","agent_id":"a-1","text":"` + large + `","client_msg_id":"c-1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}
}

func TestHTTPHandler_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	svc := NewService(&fakeAgentRunner{})
	t.Cleanup(svc.Shutdown)
	h := NewHTTPHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != http.MethodPost {
		t.Fatalf("expected Allow=POST, got %q", allow)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected json content type, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"type":"error"`) {
		t.Fatalf("expected error payload, got %s", rec.Body.String())
	}
}
