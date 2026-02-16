package session

import (
	"context"

	"inner-companion/internal/protocol"
)

// SessionMeta holds metadata about a session.
type SessionMeta struct {
	SessionID     string
	AgentID       string
	MemorySummary string
}

// Store persists conversation history across requests.
type Store interface {
	CreateSession(ctx context.Context, sessionID, agentID string) error
	LoadHistory(ctx context.Context, sessionID string) ([]protocol.HistoryMessage, error)
	AppendMessages(ctx context.Context, sessionID string, msgs []protocol.HistoryMessage) error
	LoadMeta(ctx context.Context, sessionID string) (SessionMeta, error)
	SaveMemorySummary(ctx context.Context, sessionID, summary string) error
	MessageCount(ctx context.Context, sessionID string) (int, error)
	MaxSeq(ctx context.Context, sessionID string) (int64, error)
	DeleteMessagesBefore(ctx context.Context, sessionID string, seq int64) error
	Close() error
}

// EstimateTokens returns a rough token estimate for a slice of history messages.
// Uses a chars/4 heuristic.
func EstimateTokens(msgs []protocol.HistoryMessage) int {
	total := 0
	for _, m := range msgs {
		for _, b := range m.Content {
			total += len(b.Text) + len(b.Content) + len(b.Name)
		}
	}
	return total / 4
}
