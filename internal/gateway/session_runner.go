package gateway

import (
	"context"
	"fmt"
	"log"
	"sync"

	"inner-companion/internal/protocol"
	"inner-companion/internal/session"
)

// SessionRunner decorates an AgentRunner with session persistence.
type SessionRunner struct {
	inner      AgentRunner
	store      session.Store
	memory     *session.MemoryManager
	mode       *ModeManager
	mu         sync.Mutex
	failCounts map[string]int // sessionID → cumulative tool failure count
}

// NewSessionRunner creates a SessionRunner that wraps an inner AgentRunner.
func NewSessionRunner(inner AgentRunner, store session.Store, memory *session.MemoryManager, mode *ModeManager) *SessionRunner {
	return &SessionRunner{
		inner:      inner,
		store:      store,
		memory:     memory,
		mode:       mode,
		failCounts: make(map[string]int),
	}
}

// Run implements AgentRunner by loading session history, delegating to the inner runner,
// and persisting new messages.
func (sr *SessionRunner) Run(ctx context.Context, req protocol.AgentRequest) (protocol.AgentResponse, error) {
	// Check if tool failure limit already exceeded
	sr.mu.Lock()
	if sr.failCounts[req.SessionID] > 10 {
		sr.mu.Unlock()
		return protocol.AgentResponse{}, fmt.Errorf("tool failure limit exceeded (10 per session)")
	}
	sr.mu.Unlock()

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

	// Try to recover from readonly mode
	if sr.mode != nil && sr.mode.CurrentMode() == ModeReadonly {
		sr.mode.TryRecover(ctx)
	}

	// Check read-only mode
	if sr.mode != nil && sr.mode.CurrentMode() == ModeReadonly {
		req.AllowedTools = []string{"read"}
	}

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
		if sr.mode != nil {
			sr.mode.RecordLLMFailure()
		}
		return protocol.AgentResponse{}, err
	}
	if sr.mode != nil {
		sr.mode.RecordLLMSuccess()
	}

	// Count tool failures from response
	failCount := 0
	for _, tc := range res.ToolCalls {
		if !tc.OK {
			failCount++
		}
	}
	if failCount > 0 {
		sr.mu.Lock()
		sr.failCounts[req.SessionID] += failCount
		sr.mu.Unlock()
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
