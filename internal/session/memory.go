package session

import (
	"context"
	"fmt"

	"inner-companion/internal/anthropic"
	"inner-companion/internal/protocol"
)

// Summarizer compresses conversation history into a summary string.
type Summarizer interface {
	Summarize(ctx context.Context, msgs []protocol.HistoryMessage, existingSummary string) (string, error)
}

// MemoryManager handles memory flush and system prompt injection.
type MemoryManager struct {
	store      Store
	summarizer Summarizer
	tokenLimit int
}

// NewMemoryManager creates a MemoryManager with the given token limit.
func NewMemoryManager(store Store, summarizer Summarizer, tokenLimit int) *MemoryManager {
	return &MemoryManager{
		store:      store,
		summarizer: summarizer,
		tokenLimit: tokenLimit,
	}
}

// CheckAndFlush checks if the session's history exceeds the token threshold
// and, if so, summarizes old messages and deletes them.
func (m *MemoryManager) CheckAndFlush(ctx context.Context, sessionID string) error {
	history, err := m.store.LoadHistory(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load history: %w", err)
	}

	tokens := EstimateTokens(history)
	threshold := m.tokenLimit * 70 / 100

	if tokens <= threshold {
		return nil
	}

	meta, err := m.store.LoadMeta(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load meta: %w", err)
	}

	summary, err := m.summarizer.Summarize(ctx, history, meta.MemorySummary)
	if err != nil {
		return fmt.Errorf("summarize: %w", err)
	}

	if err := m.store.SaveMemorySummary(ctx, sessionID, summary); err != nil {
		return fmt.Errorf("save summary: %w", err)
	}

	maxSeq, err := m.store.MaxSeq(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("max seq: %w", err)
	}

	// Delete all messages except the most recent one
	if err := m.store.DeleteMessagesBefore(ctx, sessionID, maxSeq); err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}

	return nil
}

// BuildSystemPrompt returns the base system prompt with memory summary injected if present.
func (m *MemoryManager) BuildSystemPrompt(ctx context.Context, sessionID, basePrompt string) (string, error) {
	meta, err := m.store.LoadMeta(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("load meta: %w", err)
	}

	if meta.MemorySummary == "" {
		return basePrompt, nil
	}

	return basePrompt + "\n\n<conversation_summary>\n" + meta.MemorySummary + "\n</conversation_summary>", nil
}

// LLMSummarizer uses an Anthropic LLM client to summarize conversation history.
type LLMSummarizer struct {
	client anthropic.LLMClient
}

// NewLLMSummarizer creates a new LLMSummarizer.
func NewLLMSummarizer(client anthropic.LLMClient) *LLMSummarizer {
	return &LLMSummarizer{client: client}
}

const summarizePrompt = `Summarize the following conversation concisely, preserving key facts, decisions, and context needed for continuation. If an existing summary is provided, merge new information into it.`

// Summarize compresses conversation history into a summary string.
func (s *LLMSummarizer) Summarize(ctx context.Context, msgs []protocol.HistoryMessage, existingSummary string) (string, error) {
	userContent := "Existing summary:\n" + existingSummary + "\n\nConversation:\n"
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == "text" && b.Text != "" {
				userContent += m.Role + ": " + b.Text + "\n"
			}
		}
	}

	resp, err := s.client.CreateMessage(ctx, anthropic.MessagesRequest{
		System: summarizePrompt,
		Messages: []anthropic.Message{
			{
				Role: "user",
				Content: []anthropic.ContentBlock{
					{Type: "text", Text: userContent},
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("llm summarize: %w", err)
	}

	for _, b := range resp.Content {
		if b.Type == "text" {
			return b.Text, nil
		}
	}
	return "", fmt.Errorf("llm summarize: no text in response")
}
