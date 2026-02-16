package anthropic

import (
	"context"
	"errors"
	"time"
)

// RetryClient wraps an LLMClient with retry logic.
type RetryClient struct {
	inner    LLMClient
	maxRetry int
	sleepFn  func(time.Duration)
}

// NewRetryClient creates a RetryClient with default settings:
// maxRetry=3, sleepFn=time.Sleep.
func NewRetryClient(inner LLMClient) *RetryClient {
	return &RetryClient{
		inner:    inner,
		maxRetry: 3,
		sleepFn:  time.Sleep,
	}
}

// CreateMessage sends a request, retrying on retryable errors with exponential backoff.
// Backoff durations: 1s, 2s, 4s. Non-retryable errors and context cancellation
// return immediately.
func (r *RetryClient) CreateMessage(ctx context.Context, req MessagesRequest) (MessagesResponse, error) {
	var lastErr error

	for attempt := 0; attempt < r.maxRetry; attempt++ {
		// Check context before each attempt.
		if ctx.Err() != nil {
			return MessagesResponse{}, ctx.Err()
		}

		resp, err := r.inner.CreateMessage(ctx, req)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		// Check if the error is a retryable APIError.
		var apiErr *APIError
		if !errors.As(err, &apiErr) || !apiErr.IsRetryable() {
			return MessagesResponse{}, err
		}

		// Sleep with exponential backoff before retrying, unless this is the last attempt.
		if attempt < r.maxRetry-1 {
			backoff := time.Second * (1 << uint(attempt)) // 1s, 2s, 4s
			r.sleepFn(backoff)
		}
	}

	return MessagesResponse{}, lastErr
}
