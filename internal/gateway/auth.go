package gateway

import (
	"encoding/json"
	"net/http"
	"strings"

	"inner-companion/internal/protocol"
)

func NewAuthMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != token {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(protocol.OutboundErrorMessage{
				Type:      "error",
				SessionID: "unknown",
				AgentID:   "unknown",
				Code:      "unauthorized",
				Message:   "unauthorized",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
