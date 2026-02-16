package gateway

import (
	"context"
	"fmt"
	"sync"
	"time"

	"inner-companion/internal/protocol"
)

// AgentRunner executes an agent request and returns an agent response.
type AgentRunner interface {
	Run(ctx context.Context, req protocol.AgentRequest) (protocol.AgentResponse, error)
}

type result struct {
	res       protocol.AgentResponse
	err       error
	createdAt time.Time
}

type inflightCall struct {
	done chan struct{}
	res  protocol.AgentResponse
	err  error
}

const defaultAgentTimeoutSec = 120

// Service handles inbound user messages and dispatches to AgentRunner.
type Service struct {
	runner AgentRunner

	mu                    sync.Mutex
	idempRes              map[string]result
	idempOrder            []string
	maxIdempotencyEntries int
	idempotencyTTL        time.Duration
	nowFn                 func() time.Time
	inflight              map[string]*inflightCall
	runBaseCtx            context.Context
	runBaseCancel         context.CancelFunc
}

func NewService(runner AgentRunner) *Service {
	baseCtx, baseCancel := context.WithCancel(context.Background())
	return &Service{
		runner:                runner,
		idempRes:              map[string]result{},
		idempOrder:            []string{},
		maxIdempotencyEntries: 1024,
		idempotencyTTL:        5 * time.Minute,
		nowFn:                 time.Now,
		inflight:              map[string]*inflightCall{},
		runBaseCtx:            baseCtx,
		runBaseCancel:         baseCancel,
	}
}

// Shutdown cancels in-flight agent runs and makes this Service unusable for new requests.
func (s *Service) Shutdown() {
	if s.runBaseCancel != nil {
		s.runBaseCancel()
	}
}

func (s *Service) HandleUserMessage(ctx context.Context, msg protocol.InboundUserMessage) (protocol.AgentResponse, error) {
	if err := protocol.ValidateInboundUserMessage(msg); err != nil {
		return protocol.AgentResponse{}, fmt.Errorf("invalid inbound: %w", err)
	}

	reqID := makeRequestID(msg)
	now := s.nowFn()

	s.mu.Lock()
	if cached, ok := s.idempRes[reqID]; ok {
		if !s.isExpired(cached, now) {
			s.mu.Unlock()
			return cached.res, cached.err
		}
		delete(s.idempRes, reqID)
		s.removeFromOrder(reqID)
	}
	if call, ok := s.inflight[reqID]; ok {
		s.mu.Unlock()
		select {
		case <-call.done:
			return call.res, call.err
		case <-ctx.Done():
			return protocol.AgentResponse{}, ctx.Err()
		}
	}

	call := &inflightCall{done: make(chan struct{})}
	s.inflight[reqID] = call
	s.mu.Unlock()

	var (
		res protocol.AgentResponse
		err error
	)
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("agent run panicked: %v", recovered)
				res = protocol.AgentResponse{}
			}
		}()
		runCtx, cancel := context.WithTimeout(s.runBaseCtx, defaultAgentTimeoutSec*time.Second)
		defer cancel()
		res, err = s.runAgent(runCtx, msg, reqID)
	}()
	completedAt := s.nowFn()

	s.mu.Lock()
	if err == nil && res.Status == "ok" {
		s.storeIdempotentResult(reqID, result{res: res, err: nil, createdAt: completedAt})
	}
	call.res = res
	call.err = err
	delete(s.inflight, reqID)
	close(call.done)
	s.mu.Unlock()

	return res, err
}

func (s *Service) runAgent(ctx context.Context, msg protocol.InboundUserMessage, reqID string) (protocol.AgentResponse, error) {
	req := protocol.AgentRequest{
		RequestID:    reqID,
		SessionID:    msg.SessionID,
		AgentID:      msg.AgentID,
		InputText:    msg.Text,
		AllowedTools: []string{"read", "bash"},
		TimeoutSec:   defaultAgentTimeoutSec,
	}
	if err := protocol.ValidateAgentRequest(req); err != nil {
		return protocol.AgentResponse{}, fmt.Errorf("invalid agent request: %w", err)
	}

	res, err := s.runner.Run(ctx, req)
	if err != nil {
		return protocol.AgentResponse{}, fmt.Errorf("agent run failed: %w", err)
	}
	if res.RequestID == "" {
		res.RequestID = reqID
	}
	if err := protocol.ValidateAgentResponse(res); err != nil {
		return protocol.AgentResponse{}, fmt.Errorf("invalid agent response: %w", err)
	}
	return res, nil
}

func makeRequestID(msg protocol.InboundUserMessage) string {
	return msg.SessionID + ":" + msg.AgentID + ":" + msg.ClientMsgID
}

func (s *Service) storeIdempotentResult(reqID string, res result) {
	if _, exists := s.idempRes[reqID]; !exists {
		s.idempOrder = append(s.idempOrder, reqID)
	}
	s.idempRes[reqID] = res

	max := s.maxIdempotencyEntries
	if max <= 0 {
		max = 1
	}
	for len(s.idempOrder) > max {
		oldest := s.idempOrder[0]
		s.idempOrder = s.idempOrder[1:]
		delete(s.idempRes, oldest)
	}
}

func (s *Service) isExpired(res result, now time.Time) bool {
	if s.idempotencyTTL <= 0 {
		return false
	}
	if res.createdAt.IsZero() {
		return false
	}
	return now.Sub(res.createdAt) > s.idempotencyTTL
}

func (s *Service) removeFromOrder(reqID string) {
	for i, id := range s.idempOrder {
		if id == reqID {
			s.idempOrder = append(s.idempOrder[:i], s.idempOrder[i+1:]...)
			return
		}
	}
}
