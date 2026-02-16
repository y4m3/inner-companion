package gateway

import (
	"context"
	"log"

	"inner-companion/internal/protocol"
	"inner-companion/internal/session"
)

// SessionRunner decorates an AgentRunner with session persistence.
type SessionRunner struct {
	inner   AgentRunner
	store   session.Store
	memory  *session.MemoryManager
}

// NewSessionRunner creates a SessionRunner that wraps an inner AgentRunner.
func NewSessionRunner(inner AgentRunner, store session.Store, memory *session.MemoryManager) *SessionRunner {
	return &SessionRunner{
		inner:  inner,
		store:  store,
		memory: memory,
	}
}

// Run implements AgentRunner by loading session history, delegating to the inner runner,
// and persisting new messages.
func (sr *SessionRunner) Run(ctx context.Context, req protocol.AgentRequest) (protocol.AgentResponse, error) {
	// Ensure session exists
	if err := sr.store.CreateSession(ctx, req.SessionID, req.AgentID); err != nil {
		return protocol.AgentResponse{}, err
	}

	// Load conversation history
	history, err := sr.store.LoadHistory(ctx, req.SessionID)
	if err != nil {
		return protocol.AgentResponse{}, err
	}
	req.History = history

	// Build system prompt with memory summary
	systemPrompt, err := sr.memory.BuildSystemPrompt(ctx, req.SessionID, "")
	if err != nil {
		return protocol.AgentResponse{}, err
	}
	if systemPrompt != "" {
		req.SystemPrompt = systemPrompt
	}

	// Delegate to inner runner
	res, err := sr.inner.Run(ctx, req)
	if err != nil {
		return protocol.AgentResponse{}, err
	}

	// Persist new messages (log errors, don't fail response)
	if len(res.NewMessages) > 0 {
		if appendErr := sr.store.AppendMessages(ctx, req.SessionID, res.NewMessages); appendErr != nil {
			log.Printf("session %s: failed to append messages: %v", req.SessionID, appendErr)
		}
	}

	// Check and flush memory (log errors, don't fail response)
	if flushErr := sr.memory.CheckAndFlush(ctx, req.SessionID); flushErr != nil {
		log.Printf("session %s: memory flush error: %v", req.SessionID, flushErr)
	}

	return res, nil
}
