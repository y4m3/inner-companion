package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateMessage_Headers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers.
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key = %q, want %q", got, "test-key")
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("anthropic-version = %q, want %q", got, "2023-06-01")
		}
		if got := r.Header.Get("content-type"); got != "application/json" {
			t.Errorf("content-type = %q, want %q", got, "application/json")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MessagesResponse{
			ID:   "msg_test",
			Role: "assistant",
		})
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{
		APIKey:    "test-key",
		BaseURL:   srv.URL,
		Model:     "claude-test",
		MaxTokens: 1024,
	})

	_, err := c.CreateMessage(context.Background(), MessagesRequest{
		Messages: []Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hello"}}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateMessage_RequestBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}

		var req MessagesRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}

		// Verify model and max_tokens are overridden by client config.
		if req.Model != "claude-test" {
			t.Errorf("model = %q, want %q", req.Model, "claude-test")
		}
		if req.MaxTokens != 2048 {
			t.Errorf("max_tokens = %d, want %d", req.MaxTokens, 2048)
		}
		if req.System != "be helpful" {
			t.Errorf("system = %q, want %q", req.System, "be helpful")
		}
		if len(req.Messages) != 1 {
			t.Fatalf("messages len = %d, want 1", len(req.Messages))
		}
		if req.Messages[0].Role != "user" {
			t.Errorf("message role = %q, want %q", req.Messages[0].Role, "user")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MessagesResponse{ID: "msg_test"})
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{
		APIKey:    "test-key",
		BaseURL:   srv.URL,
		Model:     "claude-test",
		MaxTokens: 2048,
	})

	_, err := c.CreateMessage(context.Background(), MessagesRequest{
		Model:     "should-be-overridden",
		MaxTokens: 9999,
		System:    "be helpful",
		Messages:  []Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hello"}}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateMessage_SuccessResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := MessagesResponse{
			ID:         "msg_abc123",
			Type:       "message",
			Role:       "assistant",
			Model:      "claude-test",
			StopReason: "end_turn",
			Content: []ContentBlock{
				{Type: "text", Text: "Hello there!"},
			},
			Usage: Usage{
				InputTokens:  10,
				OutputTokens: 5,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "claude-test",
	})

	resp, err := c.CreateMessage(context.Background(), MessagesRequest{
		Messages: []Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.ID != "msg_abc123" {
		t.Errorf("id = %q, want %q", resp.ID, "msg_abc123")
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q, want %q", resp.StopReason, "end_turn")
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "Hello there!" {
		t.Errorf("content = %+v, want text 'Hello there!'", resp.Content)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("input_tokens = %d, want 10", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("output_tokens = %d, want 5", resp.Usage.OutputTokens)
	}
}

func TestCreateMessage_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"invalid request"}}`))
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "claude-test",
	})

	_, err := c.CreateMessage(context.Background(), MessagesRequest{
		Messages: []Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hi"}}}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("status code = %d, want 400", apiErr.StatusCode)
	}
	if apiErr.Body == "" {
		t.Error("expected non-empty body")
	}
}

func TestAPIError_IsRetryable(t *testing.T) {
	tests := []struct {
		code      int
		retryable bool
	}{
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
	}

	for _, tt := range tests {
		e := &APIError{StatusCode: tt.code}
		if got := e.IsRetryable(); got != tt.retryable {
			t.Errorf("IsRetryable(%d) = %v, want %v", tt.code, got, tt.retryable)
		}
	}
}
