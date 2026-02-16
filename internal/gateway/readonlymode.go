package gateway

import (
	"context"
	"sync"
	"time"

	"inner-companion/internal/audit"
)

type Mode string

const (
	ModeNormal   Mode = "normal"
	ModeReadonly Mode = "readonly"
)

type ModeManager struct {
	mu             sync.Mutex
	mode           Mode
	failCount      int
	authErrorCount int
	logger         audit.Logger
	readonlySince  time.Time
	cooldown       time.Duration
	nowFn          func() time.Time
	healthCheck    func(ctx context.Context) bool
}

func NewModeManager(logger audit.Logger) *ModeManager {
	return &ModeManager{
		mode:     ModeNormal,
		logger:   logger,
		cooldown: 5 * time.Minute,
		nowFn:    time.Now,
	}
}

func (m *ModeManager) CurrentMode() Mode {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mode
}

func (m *ModeManager) RecordLLMFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failCount++
	if m.failCount >= 3 && m.mode == ModeNormal {
		m.transitionTo(ModeReadonly)
	}
}

func (m *ModeManager) RecordLLMAuthError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authErrorCount++
	if m.authErrorCount >= 2 && m.mode == ModeNormal {
		m.transitionTo(ModeReadonly)
	}
}

func (m *ModeManager) RecordLLMSuccess() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failCount = 0
	m.authErrorCount = 0
}

func (m *ModeManager) SetHealthCheck(fn func(ctx context.Context) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthCheck = fn
}

func (m *ModeManager) TryRecover(ctx context.Context) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mode != ModeReadonly {
		return false
	}
	if m.nowFn().Sub(m.readonlySince) < m.cooldown {
		return false
	}
	// Health check: 2 consecutive successes required
	if m.healthCheck != nil {
		for i := 0; i < 2; i++ {
			if !m.healthCheck(ctx) {
				return false
			}
		}
	}
	m.mode = ModeNormal
	m.failCount = 0
	m.authErrorCount = 0
	m.logTransition("readonly", "normal")
	return true
}

func (m *ModeManager) transitionTo(mode Mode) {
	old := string(m.mode)
	m.mode = mode
	m.readonlySince = m.nowFn()
	m.logTransition(old, string(mode))
}

func (m *ModeManager) logTransition(from, to string) {
	if m.logger == nil {
		return
	}
	m.logger.Log(audit.Entry{
		Event: "mode_transition",
		Detail: map[string]any{
			"from": from,
			"to":   to,
		},
	})
}
