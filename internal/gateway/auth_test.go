package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"inner-companion/internal/protocol"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	handler := NewAuthMiddleware("secret-token", okHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "ok")
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	handler := NewAuthMiddleware("secret-token", okHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	var errMsg protocol.OutboundErrorMessage
	if err := json.NewDecoder(rec.Body).Decode(&errMsg); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errMsg.Code != "unauthorized" {
		t.Errorf("error code = %q, want %q", errMsg.Code, "unauthorized")
	}
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	handler := NewAuthMiddleware("secret-token", okHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_EmptyTokenSkipsAuth(t *testing.T) {
	handler := NewAuthMiddleware("", okHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "ok")
	}
}
