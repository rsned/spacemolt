package strategy

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// mockStrategy records calls for testing.
type mockStrategy struct {
	name      string
	runCalled bool
	runErr    error
	runDelay  time.Duration
	status    string
	mu        sync.Mutex
}

func (m *mockStrategy) Name() string        { return m.name }
func (m *mockStrategy) Description() string { return m.name + " strategy" }
func (m *mockStrategy) CurrentStatus() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *mockStrategy) Run(ctx context.Context, _ game.GameClient, _ Config) error {
	m.mu.Lock()
	m.runCalled = true
	m.status = "running"
	m.mu.Unlock()

	if m.runDelay > 0 {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			m.status = "interrupted"
			m.mu.Unlock()
			return ctx.Err()
		case <-time.After(m.runDelay):
		}
	}

	m.mu.Lock()
	m.status = "completed"
	m.mu.Unlock()
	return m.runErr
}

func TestBackgroundRunnerStartStop(t *testing.T) {
	mock := &mockStrategy{name: "test-bg", runDelay: 5 * time.Second}
	bgr := NewBackgroundRunner(mock, BackgroundRunnerConfig{
		CleanupTimeout: 2 * time.Second,
	})

	cfg := Config{AgentID: "test-agent"}
	bgr.Start(context.Background(), nil, cfg)

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	if !bgr.IsRunning() {
		t.Error("expected background runner to be running")
	}

	cp := bgr.Interrupt()
	if bgr.IsRunning() {
		t.Error("expected background runner to be stopped after interrupt")
	}

	// Checkpoint should be empty for a strategy that doesn't save state
	_ = cp
}

func TestBackgroundRunnerNaturalCompletion(t *testing.T) {
	mock := &mockStrategy{name: "test-bg", runDelay: 50 * time.Millisecond}
	bgr := NewBackgroundRunner(mock, BackgroundRunnerConfig{
		CleanupTimeout: 2 * time.Second,
	})

	cfg := Config{AgentID: "test-agent"}
	bgr.Start(context.Background(), nil, cfg)

	// Wait for natural completion
	time.Sleep(200 * time.Millisecond)

	if bgr.IsRunning() {
		t.Error("expected background runner to have completed naturally")
	}
}
