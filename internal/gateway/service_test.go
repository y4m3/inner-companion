package gateway

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"inner-companion/internal/protocol"
)

type fakeAgentRunner struct {
	calls int
	resp  protocol.AgentResponse
	err   error
	mu    sync.Mutex
	delay time.Duration
	runFn func(req protocol.AgentRequest) (protocol.AgentResponse, error)
}

type blockingCtxRunner struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockingCtxRunner) Run(ctx context.Context, req protocol.AgentRequest) (protocol.AgentResponse, error) {
	select {
	case <-r.started:
	default:
		close(r.started)
	}
	select {
	case <-ctx.Done():
		return protocol.AgentResponse{}, ctx.Err()
	case <-r.release:
		return protocol.AgentResponse{
			RequestID:     req.RequestID,
			Status:        "ok",
			AssistantText: "done",
		}, nil
	}
}

func (f *fakeAgentRunner) Run(_ context.Context, req protocol.AgentRequest) (protocol.AgentResponse, error) {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.runFn != nil {
		return f.runFn(req)
	}
	if f.resp.RequestID == "" {
		f.resp.RequestID = req.RequestID
	}
	return f.resp, f.err
}

func (f *fakeAgentRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestServiceHandleUserMessage(t *testing.T) {
	t.Parallel()

	runner := &fakeAgentRunner{resp: protocol.AgentResponse{Status: "ok", AssistantText: "done"}}
	svc := NewService(runner)

	res, err := svc.HandleUserMessage(context.Background(), protocol.InboundUserMessage{
		Type:        "user_message",
		SessionID:   "s-1",
		AgentID:     "a-1",
		Text:        "hello",
		ClientMsgID: "c-1",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if res.RequestID == "" {
		t.Fatalf("expected request_id")
	}
	if runner.callCount() != 1 {
		t.Fatalf("expected 1 call, got %d", runner.callCount())
	}
}

func TestServiceHandleUserMessage_IdempotentByClientMsgID(t *testing.T) {
	t.Parallel()

	runner := &fakeAgentRunner{resp: protocol.AgentResponse{Status: "ok", AssistantText: "done"}}
	svc := NewService(runner)

	msg := protocol.InboundUserMessage{
		Type:        "user_message",
		SessionID:   "s-1",
		AgentID:     "a-1",
		Text:        "hello",
		ClientMsgID: "c-1",
	}

	res1, err := svc.HandleUserMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	res2, err := svc.HandleUserMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if res1.RequestID != res2.RequestID {
		t.Fatalf("expected same request_id, got %q vs %q", res1.RequestID, res2.RequestID)
	}
	if runner.callCount() != 1 {
		t.Fatalf("expected idempotent single runner call, got %d", runner.callCount())
	}
}

func TestServiceHandleUserMessage_IdempotentConcurrent(t *testing.T) {
	t.Parallel()

	runner := &fakeAgentRunner{resp: protocol.AgentResponse{Status: "ok", AssistantText: "done"}, delay: 20 * time.Millisecond}
	svc := NewService(runner)

	msg := protocol.InboundUserMessage{
		Type:        "user_message",
		SessionID:   "s-1",
		AgentID:     "a-1",
		Text:        "hello",
		ClientMsgID: "c-1",
	}

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.HandleUserMessage(context.Background(), msg)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if runner.callCount() != 1 {
		t.Fatalf("expected a single runner call for concurrent duplicate requests, got %d", runner.callCount())
	}
}

func TestServiceHandleUserMessage_ErrorIsNotCached(t *testing.T) {
	t.Parallel()

	var n atomic.Int32
	runner := &fakeAgentRunner{
		runFn: func(req protocol.AgentRequest) (protocol.AgentResponse, error) {
			count := n.Add(1)
			if count == 1 {
				return protocol.AgentResponse{}, context.DeadlineExceeded
			}
			return protocol.AgentResponse{
				RequestID:     req.RequestID,
				Status:        "ok",
				AssistantText: "recovered",
			}, nil
		},
	}
	svc := NewService(runner)
	msg := protocol.InboundUserMessage{
		Type:        "user_message",
		SessionID:   "s-1",
		AgentID:     "a-1",
		Text:        "hello",
		ClientMsgID: "c-1",
	}

	_, err1 := svc.HandleUserMessage(context.Background(), msg)
	if err1 == nil {
		t.Fatalf("expected first error")
	}
	res2, err2 := svc.HandleUserMessage(context.Background(), msg)
	if err2 != nil {
		t.Fatalf("expected second call to retry and recover, got %v", err2)
	}
	if res2.AssistantText != "recovered" {
		t.Fatalf("expected recovered response, got %+v", res2)
	}
	if runner.callCount() != 2 {
		t.Fatalf("expected retry to invoke runner twice, got %d", runner.callCount())
	}
}

func TestServiceHandleUserMessage_AgentStatusErrorIsNotCached(t *testing.T) {
	t.Parallel()

	var n atomic.Int32
	runner := &fakeAgentRunner{
		runFn: func(req protocol.AgentRequest) (protocol.AgentResponse, error) {
			count := n.Add(1)
			if count == 1 {
				return protocol.AgentResponse{
					RequestID:     req.RequestID,
					Status:        "error",
					AssistantText: "temporary upstream error",
				}, nil
			}
			return protocol.AgentResponse{
				RequestID:     req.RequestID,
				Status:        "ok",
				AssistantText: "recovered",
			}, nil
		},
	}
	svc := NewService(runner)
	msg := protocol.InboundUserMessage{
		Type:        "user_message",
		SessionID:   "s-1",
		AgentID:     "a-1",
		Text:        "hello",
		ClientMsgID: "c-1",
	}

	res1, err1 := svc.HandleUserMessage(context.Background(), msg)
	if err1 != nil {
		t.Fatalf("first call error: %v", err1)
	}
	if res1.Status != "error" {
		t.Fatalf("expected first response status=error, got %q", res1.Status)
	}

	res2, err2 := svc.HandleUserMessage(context.Background(), msg)
	if err2 != nil {
		t.Fatalf("second call error: %v", err2)
	}
	if res2.Status != "ok" || res2.AssistantText != "recovered" {
		t.Fatalf("expected second call to rerun and recover, got %+v", res2)
	}
	if runner.callCount() != 2 {
		t.Fatalf("expected status=error result not cached, got %d calls", runner.callCount())
	}
}

func TestServiceHandleUserMessage_IdempotencyCacheEviction(t *testing.T) {
	t.Parallel()

	runner := &fakeAgentRunner{resp: protocol.AgentResponse{Status: "ok", AssistantText: "done"}}
	svc := NewService(runner)
	svc.maxIdempotencyEntries = 1

	msg1 := protocol.InboundUserMessage{Type: "user_message", SessionID: "s-1", AgentID: "a-1", Text: "hello", ClientMsgID: "c-1"}
	msg2 := protocol.InboundUserMessage{Type: "user_message", SessionID: "s-1", AgentID: "a-1", Text: "hello", ClientMsgID: "c-2"}

	if _, err := svc.HandleUserMessage(context.Background(), msg1); err != nil {
		t.Fatalf("first message error: %v", err)
	}
	if _, err := svc.HandleUserMessage(context.Background(), msg2); err != nil {
		t.Fatalf("second message error: %v", err)
	}
	if _, err := svc.HandleUserMessage(context.Background(), msg1); err != nil {
		t.Fatalf("third message error: %v", err)
	}

	if runner.callCount() != 3 {
		t.Fatalf("expected oldest cache entry eviction and rerun, got %d runner calls", runner.callCount())
	}
}

func TestServiceHandleUserMessage_IdempotencyCacheExpiresByTTL(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0)
	runner := &fakeAgentRunner{resp: protocol.AgentResponse{Status: "ok", AssistantText: "done"}}
	svc := NewService(runner)
	svc.idempotencyTTL = 30 * time.Second
	svc.nowFn = func() time.Time { return now }

	msg := protocol.InboundUserMessage{
		Type:        "user_message",
		SessionID:   "s-1",
		AgentID:     "a-1",
		Text:        "hello",
		ClientMsgID: "c-1",
	}

	if _, err := svc.HandleUserMessage(context.Background(), msg); err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if _, err := svc.HandleUserMessage(context.Background(), msg); err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if runner.callCount() != 1 {
		t.Fatalf("expected cache hit before ttl expiration, got %d calls", runner.callCount())
	}

	now = now.Add(31 * time.Second)

	if _, err := svc.HandleUserMessage(context.Background(), msg); err != nil {
		t.Fatalf("third call error: %v", err)
	}
	if runner.callCount() != 2 {
		t.Fatalf("expected rerun after ttl expiration, got %d calls", runner.callCount())
	}
}

func TestServiceHandleUserMessage_IdempotencyTTLStartsAtResponseTime(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0)
	var nowMu sync.Mutex
	runner := &fakeAgentRunner{
		runFn: func(req protocol.AgentRequest) (protocol.AgentResponse, error) {
			nowMu.Lock()
			now = now.Add(40 * time.Second) // emulate long agent execution
			nowMu.Unlock()
			return protocol.AgentResponse{
				RequestID:     req.RequestID,
				Status:        "ok",
				AssistantText: "done",
			}, nil
		},
	}
	svc := NewService(runner)
	svc.idempotencyTTL = 30 * time.Second
	svc.nowFn = func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	}

	msg := protocol.InboundUserMessage{
		Type:        "user_message",
		SessionID:   "s-1",
		AgentID:     "a-1",
		Text:        "hello",
		ClientMsgID: "c-1",
	}

	if _, err := svc.HandleUserMessage(context.Background(), msg); err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if _, err := svc.HandleUserMessage(context.Background(), msg); err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if runner.callCount() != 1 {
		t.Fatalf("expected cache hit right after first completion, got %d calls", runner.callCount())
	}
}

func TestServiceHandleUserMessage_InflightWaitHonorsContextCancel(t *testing.T) {
	t.Parallel()

	runner := &blockingCtxRunner{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc := NewService(runner)
	msg := protocol.InboundUserMessage{Type: "user_message", SessionID: "s-1", AgentID: "a-1", Text: "hello", ClientMsgID: "c-1"}

	ownerDone := make(chan struct{})
	go func() {
		_, _ = svc.HandleUserMessage(context.Background(), msg)
		close(ownerDone)
	}()
	<-runner.started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	begin := time.Now()
	_, err := svc.HandleUserMessage(ctx, msg)
	elapsed := time.Since(begin)

	if err == nil {
		t.Fatalf("expected context cancellation error")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected context canceled error, got %q", err.Error())
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("expected quick return on canceled context, took %v", elapsed)
	}

	close(runner.release)
	<-ownerDone
}

func TestServiceHandleUserMessage_RejectInvalidInbound(t *testing.T) {
	t.Parallel()

	svc := NewService(&fakeAgentRunner{})
	_, err := svc.HandleUserMessage(context.Background(), protocol.InboundUserMessage{Type: "assistant_message"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "invalid inbound") {
		t.Fatalf("expected invalid inbound error, got %q", err.Error())
	}
}

func TestServiceHandleUserMessage_RunnerPanicDoesNotLeakInflight(t *testing.T) {
	t.Parallel()

	var n atomic.Int32
	runner := &fakeAgentRunner{
		runFn: func(req protocol.AgentRequest) (protocol.AgentResponse, error) {
			count := n.Add(1)
			if count == 1 {
				panic("boom")
			}
			return protocol.AgentResponse{
				RequestID:     req.RequestID,
				Status:        "ok",
				AssistantText: "recovered",
			}, nil
		},
	}
	svc := NewService(runner)
	msg := protocol.InboundUserMessage{
		Type:        "user_message",
		SessionID:   "s-1",
		AgentID:     "a-1",
		Text:        "hello",
		ClientMsgID: "c-1",
	}

	_, err1 := svc.HandleUserMessage(context.Background(), msg)
	if err1 == nil {
		t.Fatalf("expected panic to be converted to error")
	}
	if !strings.Contains(err1.Error(), "panic") {
		t.Fatalf("expected panic-related error, got %q", err1.Error())
	}

	res2, err2 := svc.HandleUserMessage(context.Background(), msg)
	if err2 != nil {
		t.Fatalf("expected second call to recover, got %v", err2)
	}
	if res2.Status != "ok" || res2.AssistantText != "recovered" {
		t.Fatalf("unexpected second response: %+v", res2)
	}
	if runner.callCount() != 2 {
		t.Fatalf("expected second call to execute runner again, got %d calls", runner.callCount())
	}
}

func TestServiceHandleUserMessage_InflightOwnerCancelDoesNotFailWaiters(t *testing.T) {
	t.Parallel()

	runner := &blockingCtxRunner{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc := NewService(runner)
	msg := protocol.InboundUserMessage{
		Type:        "user_message",
		SessionID:   "s-1",
		AgentID:     "a-1",
		Text:        "hello",
		ClientMsgID: "c-1",
	}

	ownerCtx, ownerCancel := context.WithCancel(context.Background())
	ownerDone := make(chan error, 1)
	go func() {
		_, err := svc.HandleUserMessage(ownerCtx, msg)
		ownerDone <- err
	}()

	<-runner.started

	waiterDone := make(chan error, 1)
	go func() {
		_, err := svc.HandleUserMessage(context.Background(), msg)
		waiterDone <- err
	}()

	ownerCancel()
	close(runner.release)

	ownerErr := <-ownerDone
	waiterErr := <-waiterDone

	if ownerErr != nil {
		t.Fatalf("expected owner cancel not to fail shared inflight run, got %v", ownerErr)
	}
	if waiterErr != nil {
		t.Fatalf("expected waiter success, got %v", waiterErr)
	}
}

func TestServiceHandleUserMessage_ShutdownCancelsInflightRun(t *testing.T) {
	t.Parallel()

	runner := &blockingCtxRunner{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc := NewService(runner)
	msg := protocol.InboundUserMessage{
		Type:        "user_message",
		SessionID:   "s-1",
		AgentID:     "a-1",
		Text:        "hello",
		ClientMsgID: "c-1",
	}

	resultErr := make(chan error, 1)
	go func() {
		_, err := svc.HandleUserMessage(context.Background(), msg)
		resultErr <- err
	}()

	<-runner.started
	svc.Shutdown()

	err := <-resultErr
	if err == nil {
		t.Fatalf("expected shutdown to cancel inflight run")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected context canceled error, got %q", err.Error())
	}
}

func TestServiceHandleUserMessage_AfterShutdownRejectsNewRequests(t *testing.T) {
	t.Parallel()

	runner := &blockingCtxRunner{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc := NewService(runner)
	svc.Shutdown()

	msg := protocol.InboundUserMessage{
		Type:        "user_message",
		SessionID:   "s-1",
		AgentID:     "a-1",
		Text:        "hello",
		ClientMsgID: "c-1",
	}

	_, err := svc.HandleUserMessage(context.Background(), msg)
	if err == nil {
		t.Fatalf("expected error after shutdown")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected context canceled error, got %q", err.Error())
	}
}
