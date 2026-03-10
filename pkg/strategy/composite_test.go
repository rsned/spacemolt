package strategy

import (
	"context"
	"log"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestCompositeStrategyInterface(t *testing.T) {
	primary := &mockStrategy{name: "primary", runDelay: 50 * time.Millisecond}
	background := &mockStrategy{name: "background", runDelay: 5 * time.Second}

	cs := NewCompositeStrategy(primary, background, CompositeConfig{
		CleanupTimeout: 1 * time.Second,
		Logger:         log.Default(),
	})

	if cs.Name() != "primary+background" {
		t.Errorf("Name() = %q, want %q", cs.Name(), "primary+background")
	}

	var _ Strategy = cs
}

func TestCompositeStrategyRunsPrimary(t *testing.T) {
	primary := &mockStrategy{name: "primary", runDelay: 100 * time.Millisecond}
	background := &mockStrategy{name: "bg", runDelay: 5 * time.Second}

	cs := NewCompositeStrategy(primary, background, CompositeConfig{
		CleanupTimeout: 1 * time.Second,
		Logger:         log.Default(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_ = cs.Run(ctx, nil, Config{AgentID: "test"})

	primary.mu.Lock()
	called := primary.runCalled
	primary.mu.Unlock()

	if !called {
		t.Error("expected primary strategy to have run")
	}
}

func TestCompositeStrategyStatus(t *testing.T) {
	primary := &mockStrategy{name: "primary", runDelay: 100 * time.Millisecond}
	background := &mockStrategy{name: "background", runDelay: 5 * time.Second}

	cs := NewCompositeStrategy(primary, background, CompositeConfig{
		CleanupTimeout: 1 * time.Second,
		Logger:         log.Default(),
	})

	status := cs.CurrentStatus()
	if status == "" {
		t.Error("expected non-empty status")
	}
}

// idleAwareMock simulates a primary strategy that enters idle windows
// and signals the composite strategy via NotifyIdle.
type idleAwareMock struct {
	name      string
	composite *CompositeStrategy
	cycles    int
	idleTime  time.Duration
	status    string
}

func (m *idleAwareMock) Name() string          { return m.name }
func (m *idleAwareMock) Description() string   { return m.name + " strategy" }
func (m *idleAwareMock) CurrentStatus() string { return m.status }

func (m *idleAwareMock) Run(ctx context.Context, _ game.GameClient, _ Config) error {
	for range m.cycles {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		m.status = "active"
		time.Sleep(20 * time.Millisecond)

		// Enter idle
		m.status = "idle"
		if m.composite != nil {
			m.composite.NotifyIdle(true)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.idleTime):
		}

		// Exit idle
		if m.composite != nil {
			m.composite.NotifyIdle(false)
		}
	}
	return nil
}

func TestCompositeStrategyIdleSignaling(t *testing.T) {
	var bgRunCount atomic.Int32
	bg := &mockStrategy{name: "bg-mine", runDelay: 5 * time.Second}

	// Wrap bg to count runs
	countingBg := &countingWrapStrategy{
		inner:    bg,
		runCount: &bgRunCount,
	}

	primary := &idleAwareMock{
		name:     "primary",
		cycles:   2,
		idleTime: 150 * time.Millisecond,
	}

	cs := NewCompositeStrategy(primary, countingBg, CompositeConfig{
		CleanupTimeout: 1 * time.Second,
		Logger:         log.Default(),
	})
	primary.composite = cs

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := cs.Run(ctx, nil, Config{AgentID: "idle-test"})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Background should have been started at least once during idle windows
	runs := bgRunCount.Load()
	if runs == 0 {
		t.Error("expected background to have been started during idle windows")
	}
}

// countingWrapStrategy wraps a strategy and counts how many times Run is called.
type countingWrapStrategy struct {
	inner    Strategy
	runCount *atomic.Int32
}

func (s *countingWrapStrategy) Name() string          { return s.inner.Name() }
func (s *countingWrapStrategy) Description() string   { return s.inner.Description() }
func (s *countingWrapStrategy) CurrentStatus() string { return s.inner.CurrentStatus() }

func (s *countingWrapStrategy) Run(ctx context.Context, client game.GameClient, cfg Config) error {
	s.runCount.Add(1)
	return s.inner.Run(ctx, client, cfg)
}
