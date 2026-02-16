package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// LLMClient is the interface for Anthropic API calls.
type LLMClient interface {
	CreateMessage(ctx context.Context, req MessagesRequest) (MessagesResponse, error)
}

// APIError represents an Anthropic API error response.
type APIError struct {
	StatusCode int
	Body       string
}

// Error returns a string representation of the API error.
func (e *APIError) Error() string {
	return fmt.Sprintf("anthropic api error: status=%d body=%s", e.StatusCode, e.Body)
}

// IsRetryable returns true for 429, 500, 502, 503.
func (e *APIError) IsRetryable() bool {
	switch e.StatusCode {
	case 429, 500, 502, 503:
		return true
	default:
		return false
	}
}

// ClientConfig holds configuration for the Anthropic API client.
type ClientConfig struct {
	APIKey     string
	BaseURL    string
	Model      string
	MaxTokens  int
	HTTPClient *http.Client
}

// Client implements LLMClient using net/http.
type Client struct {
	cfg    ClientConfig
	httpDo func(*http.Request) (*http.Response, error)
}

// NewClient creates a new Anthropic API client with the given configuration.
// Default BaseURL is "https://api.anthropic.com". Default MaxTokens is 4096.
// The caller must provide cfg.Model.
func NewClient(cfg ClientConfig) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 4096
	}
	httpClient := http.DefaultClient
	if cfg.HTTPClient != nil {
		httpClient = cfg.HTTPClient
	}
	return &Client{
		cfg:    cfg,
		httpDo: httpClient.Do,
	}
}

// CreateMessage sends a Messages API request to Anthropic and returns the response.
func (c *Client) CreateMessage(ctx context.Context, req MessagesRequest) (MessagesResponse, error) {
	// Override model and max_tokens from client config.
	req.Model = c.cfg.Model
	req.MaxTokens = c.cfg.MaxTokens

	body, err := json.Marshal(req)
	if err != nil {
		return MessagesResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return MessagesResponse{}, fmt.Errorf("create http request: %w", err)
	}

	httpReq.Header.Set("x-api-key", c.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("content-type", "application/json")

	resp, err := c.httpDo(httpReq)
	if err != nil {
		return MessagesResponse{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return MessagesResponse{}, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return MessagesResponse{}, &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	var result MessagesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return MessagesResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}

	return result, nil
}
