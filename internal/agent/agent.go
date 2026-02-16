package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"inner-companion/internal/anthropic"
	"inner-companion/internal/protocol"
	"inner-companion/internal/tool"
)

const maxIterations = 20

// AnthropicRunner implements gateway.AgentRunner using the Anthropic Messages API.
type AnthropicRunner struct {
	client   anthropic.LLMClient
	registry *tool.Registry
	system   string
}

// NewAnthropicRunner creates a new AnthropicRunner.
func NewAnthropicRunner(client anthropic.LLMClient, registry *tool.Registry, system string) *AnthropicRunner {
	return &AnthropicRunner{
		client:   client,
		registry: registry,
		system:   system,
	}
}

func (a *AnthropicRunner) Run(ctx context.Context, req protocol.AgentRequest) (protocol.AgentResponse, error) {
	toolDefs := a.buildToolDefs()

	// Convert history to anthropic messages and prepend
	var messages []anthropic.Message
	for _, hm := range req.History {
		messages = append(messages, toAnthropicMessage(hm))
	}

	// Track the index where new messages start
	newMsgStart := len(messages)

	// Add current user message
	messages = append(messages, anthropic.Message{
		Role: "user",
		Content: []anthropic.ContentBlock{
			{Type: "text", Text: req.InputText},
		},
	})

	// Determine system prompt
	system := a.system
	if req.SystemPrompt != "" {
		system = req.SystemPrompt
	}

	var allToolCalls []protocol.AgentToolCall

	for i := 0; i < maxIterations; i++ {
		if ctx.Err() != nil {
			return protocol.AgentResponse{}, ctx.Err()
		}

		resp, err := a.client.CreateMessage(ctx, anthropic.MessagesRequest{
			System:   system,
			Messages: messages,
			Tools:    toolDefs,
		})
		if err != nil {
			return protocol.AgentResponse{}, fmt.Errorf("anthropic api: %w", err)
		}

		if resp.StopReason == "end_turn" || resp.StopReason != "tool_use" {
			text := extractText(resp.Content)
			if text == "" {
				text = "(no response)"
			}

			// Add final assistant message for history
			messages = append(messages, anthropic.Message{
				Role:    "assistant",
				Content: resp.Content,
			})

			return protocol.AgentResponse{
				RequestID:     req.RequestID,
				Status:        "ok",
				AssistantText: text,
				ToolCalls:     allToolCalls,
				NewMessages:   toHistoryMessages(messages[newMsgStart:]),
			}, nil
		}

		// tool_use: add assistant message, execute tools, add results
		messages = append(messages, anthropic.Message{
			Role:    "assistant",
			Content: resp.Content,
		})

		var toolResults []anthropic.ContentBlock
		for _, block := range resp.Content {
			if block.Type != "tool_use" {
				continue
			}

			result := a.executeTool(ctx, block.Name, block.Input, req.AllowedTools)

			summary := result.Output
			if len(summary) > 200 {
				summary = summary[:200] + "..."
			}
			allToolCalls = append(allToolCalls, protocol.AgentToolCall{
				ToolName:      block.Name,
				Args:          block.Input,
				ResultSummary: summary,
				OK:            !result.IsError,
			})

			toolResults = append(toolResults, anthropic.ContentBlock{
				Type:      "tool_result",
				ToolUseID: block.ID,
				Content:   result.Output,
				IsError:   result.IsError,
			})
		}

		messages = append(messages, anthropic.Message{
			Role:    "user",
			Content: toolResults,
		})
	}

	return protocol.AgentResponse{}, fmt.Errorf("exceeded maximum iterations (%d)", maxIterations)
}

func toAnthropicMessage(hm protocol.HistoryMessage) anthropic.Message {
	blocks := make([]anthropic.ContentBlock, len(hm.Content))
	for i, b := range hm.Content {
		blocks[i] = anthropic.ContentBlock{
			Type:      b.Type,
			Text:      b.Text,
			ID:        b.ID,
			Name:      b.Name,
			Input:     b.Input,
			ToolUseID: b.ToolUseID,
			Content:   b.Content,
			IsError:   b.IsError,
		}
	}
	return anthropic.Message{Role: hm.Role, Content: blocks}
}

func toHistoryMessages(msgs []anthropic.Message) []protocol.HistoryMessage {
	result := make([]protocol.HistoryMessage, len(msgs))
	for i, m := range msgs {
		blocks := make([]protocol.HistoryContentBlock, len(m.Content))
		for j, b := range m.Content {
			blocks[j] = protocol.HistoryContentBlock{
				Type:      b.Type,
				Text:      b.Text,
				ID:        b.ID,
				Name:      b.Name,
				Input:     b.Input,
				ToolUseID: b.ToolUseID,
				Content:   b.Content,
				IsError:   b.IsError,
			}
		}
		result[i] = protocol.HistoryMessage{Role: m.Role, Content: blocks}
	}
	return result
}

func (a *AnthropicRunner) executeTool(ctx context.Context, name string, input map[string]any, allowed []string) tool.Result {
	for _, t := range allowed {
		if t == name {
			if executor, ok := a.registry.Get(name); ok {
				return executor.Execute(ctx, input)
			}
			return tool.Result{Output: fmt.Sprintf("tool %q not found", name), IsError: true}
		}
	}
	return tool.Result{Output: fmt.Sprintf("tool %q is not allowed", name), IsError: true}
}

func extractText(blocks []anthropic.ContentBlock) string {
	for _, b := range blocks {
		if b.Type == "text" {
			return b.Text
		}
	}
	return ""
}

func (a *AnthropicRunner) buildToolDefs() []anthropic.ToolDef {
	names := a.registry.Names()
	defs := make([]anthropic.ToolDef, 0, len(names))
	for _, name := range names {
		defs = append(defs, makeToolDef(name))
	}
	return defs
}

func makeToolDef(name string) anthropic.ToolDef {
	switch name {
	case "read":
		return anthropic.ToolDef{
			Name:        "read",
			Description: "Read the contents of a file at the given path.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"The file path to read"}},"required":["path"]}`),
		}
	case "bash":
		return anthropic.ToolDef{
			Name:        "bash",
			Description: "Execute a bash command.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"The bash command to execute"}},"required":["command"]}`),
		}
	default:
		return anthropic.ToolDef{
			Name:        name,
			Description: fmt.Sprintf("Tool: %s", name),
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		}
	}
}
