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

	messages := []anthropic.Message{
		{
			Role: "user",
			Content: []anthropic.ContentBlock{
				{Type: "text", Text: req.InputText},
			},
		},
	}

	var allToolCalls []protocol.AgentToolCall

	for i := 0; i < maxIterations; i++ {
		if ctx.Err() != nil {
			return protocol.AgentResponse{}, ctx.Err()
		}

		resp, err := a.client.CreateMessage(ctx, anthropic.MessagesRequest{
			System:   a.system,
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
			return protocol.AgentResponse{
				RequestID:     req.RequestID,
				Status:        "ok",
				AssistantText: text,
				ToolCalls:     allToolCalls,
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
