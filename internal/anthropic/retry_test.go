package anthropic

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeLLMClient is a test double for LLMClient.
type fakeLLMClient struct {
	calls     int
	responses []fakeLLMResponse
}

type fakeLLMResponse struct {
	resp MessagesResponse
	err  error
}

func (f *fakeLLMClient) CreateMessage(_ context.Context, _ MessagesRequest) (MessagesResponse, error) {
	idx := f.calls
	f.calls++
	if idx >= len(f.responses) {
		return MessagesResponse{}, errors.New("unexpected call")
	}
	return f.responses[idx].resp, f.responses[idx].err
}

func newRetryClientWithFake(fake *fakeLLMClient, sleeps *[]time.Duration) *RetryClient {
	rc := NewRetryClient(fake)
	rc.sleepFn = func(d time.Duration) {
		*sleeps = append(*sleeps, d)
	}
	return rc
}

func TestRetryClient_NonRetryableError(t *testing.T) {
	fake := &fakeLLMClient{
		responses: []fakeLLMResponse{
			{err: &APIError{StatusCode: 400, Body: "bad request"}},
		},
	}
	var sleeps []time.Duration
	rc := newRetryClientWithFake(fake, &sleeps)

	_, err := rc.CreateMessage(context.Background(), MessagesRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("status = %d, want 400", apiErr.StatusCode)
	}
	if fake.calls != 1 {
		t.Errorf("calls = %d, want 1 (no retry)", fake.calls)
	}
	if len(sleeps) != 0 {
		t.Errorf("sleeps = %d, want 0", len(sleeps))
	}
}

func TestRetryClient_RetryableExhausted(t *testing.T) {
	fake := &fakeLLMClient{
		responses: []fakeLLMResponse{
			{err: &APIError{StatusCode: 429, Body: "rate limit"}},
			{err: &APIError{StatusCode: 429, Body: "rate limit"}},
			{err: &APIError{StatusCode: 429, Body: "rate limit"}},
		},
	}
	var sleeps []time.Duration
	rc := newRetryClientWithFake(fake, &sleeps)

	_, err := rc.CreateMessage(context.Background(), MessagesRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 429 {
		t.Errorf("status = %d, want 429", apiErr.StatusCode)
	}
	if fake.calls != 3 {
		t.Errorf("calls = %d, want 3", fake.calls)
	}
	// Verify backoff pattern: 1s, 2s (only 2 sleeps for 3 attempts).
	if len(sleeps) != 2 {
		t.Fatalf("sleeps = %d, want 2", len(sleeps))
	}
	if sleeps[0] != 1*time.Second {
		t.Errorf("sleep[0] = %v, want 1s", sleeps[0])
	}
	if sleeps[1] != 2*time.Second {
		t.Errorf("sleep[1] = %v, want 2s", sleeps[1])
	}
}

func TestRetryClient_RetryThenSuccess(t *testing.T) {
	fake := &fakeLLMClient{
		responses: []fakeLLMResponse{
			{err: &APIError{StatusCode: 503, Body: "unavailable"}},
			{resp: MessagesResponse{ID: "msg_ok", StopReason: "end_turn"}},
		},
	}
	var sleeps []time.Duration
	rc := newRetryClientWithFake(fake, &sleeps)

	resp, err := rc.CreateMessage(context.Background(), MessagesRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "msg_ok" {
		t.Errorf("id = %q, want %q", resp.ID, "msg_ok")
	}
	if fake.calls != 2 {
		t.Errorf("calls = %d, want 2", fake.calls)
	}
	if len(sleeps) != 1 {
		t.Fatalf("sleeps = %d, want 1", len(sleeps))
	}
	if sleeps[0] != 1*time.Second {
		t.Errorf("sleep[0] = %v, want 1s", sleeps[0])
	}
}

func TestRetryClient_ContextCancelled(t *testing.T) {
	fake := &fakeLLMClient{
		responses: []fakeLLMResponse{
			{err: &APIError{StatusCode: 500, Body: "server error"}},
			{err: &APIError{StatusCode: 500, Body: "server error"}},
			{err: &APIError{StatusCode: 500, Body: "server error"}},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	var sleeps []time.Duration
	rc := newRetryClientWithFake(fake, &sleeps)

	// Cancel context after the first sleep.
	rc.sleepFn = func(d time.Duration) {
		sleeps = append(sleeps, d)
		cancel()
	}

	_, err := rc.CreateMessage(ctx, MessagesRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRetryClient_BackoffPattern(t *testing.T) {
	// Use 4 retries to verify 1s, 2s, 4s backoff pattern.
	fake := &fakeLLMClient{
		responses: []fakeLLMResponse{
			{err: &APIError{StatusCode: 502, Body: "bad gateway"}},
			{err: &APIError{StatusCode: 502, Body: "bad gateway"}},
			{err: &APIError{StatusCode: 502, Body: "bad gateway"}},
			{err: &APIError{StatusCode: 502, Body: "bad gateway"}},
		},
	}
	var sleeps []time.Duration
	rc := newRetryClientWithFake(fake, &sleeps)
	rc.maxRetry = 4

	_, err := rc.CreateMessage(context.Background(), MessagesRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if fake.calls != 4 {
		t.Errorf("calls = %d, want 4", fake.calls)
	}
	// 3 sleeps for 4 attempts.
	if len(sleeps) != 3 {
		t.Fatalf("sleeps = %d, want 3", len(sleeps))
	}
	expected := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	for i, want := range expected {
		if sleeps[i] != want {
			t.Errorf("sleep[%d] = %v, want %v", i, sleeps[i], want)
		}
	}
}
