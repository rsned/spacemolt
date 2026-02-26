package agent

import (
	"context"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/strategy"
)

// mockStrategy implements strategy.Strategy for testing.
type mockStrategy struct {
	name        string
	description string
	runFn       func(ctx context.Context, client *game.Client, cfg strategy.Config) error

	mu     sync.Mutex
	status string
}

func (m *mockStrategy) Name() string        { return m.name }
func (m *mockStrategy) Description() string  { return m.description }
func (m *mockStrategy) CurrentStatus() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *mockStrategy) Run(ctx context.Context, client *game.Client, cfg strategy.Config) error {
	m.mu.Lock()
	m.status = "running"
	m.mu.Unlock()

	if m.runFn != nil {
		return m.runFn(ctx, client, cfg)
	}
	// Default: just wait for cancellation
	<-ctx.Done()

	m.mu.Lock()
	m.status = "stopped"
	m.mu.Unlock()
	return nil
}

func TestNewStrategyRunner(t *testing.T) {
	strat := &mockStrategy{name: "test-strat", description: "A test strategy"}

	sr := NewStrategyRunner(StrategyRunnerConfig{
		AgentID:  "agent-1",
		Strategy: strat,
	})

	if sr.AgentID() != "agent-1" {
		t.Errorf("AgentID() = %q, want %q", sr.AgentID(), "agent-1")
	}
	if sr.Strategy() != strat {
		t.Error("Strategy() returned wrong strategy")
	}
	if sr.GameClient() != nil {
		t.Error("GameClient() should be nil when not set")
	}
	if sr.IsRunning() {
		t.Error("should not be running initially")
	}
}

func TestNewStrategyRunner_DefaultLogger(t *testing.T) {
	sr := NewStrategyRunner(StrategyRunnerConfig{
		AgentID:  "agent-1",
		Strategy: &mockStrategy{name: "test"},
	})

	// Should not panic - logger should be set to default
	if sr.logger == nil {
		t.Error("logger should not be nil")
	}
}

func TestNewStrategyRunner_DefaultParameters(t *testing.T) {
	sr := NewStrategyRunner(StrategyRunnerConfig{
		AgentID:  "agent-1",
		Strategy: &mockStrategy{name: "test"},
	})

	if sr.params == nil {
		t.Error("params should not be nil")
	}
}

func TestStrategyRunner_StartAndStop(t *testing.T) {
	strat := &mockStrategy{name: "test-strat"}
	logger := log.Default()

	sr := NewStrategyRunner(StrategyRunnerConfig{
		AgentID:  "agent-1",
		Strategy: strat,
		Logger:   logger,
	})

	ctx := context.Background()

	if err := sr.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Give the goroutine time to start
	time.Sleep(50 * time.Millisecond)

	if !sr.IsRunning() {
		t.Error("should be running after Start()")
	}

	if err := sr.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if sr.IsRunning() {
		t.Error("should not be running after Stop()")
	}
}

func TestStrategyRunner_DoubleStart(t *testing.T) {
	strat := &mockStrategy{name: "test-strat"}

	sr := NewStrategyRunner(StrategyRunnerConfig{
		AgentID:  "agent-1",
		Strategy: strat,
	})

	ctx := context.Background()

	if err := sr.Start(ctx); err != nil {
		t.Fatalf("first Start() error: %v", err)
	}
	defer func() { _ = sr.Stop() }()

	// Second start should be a no-op
	if err := sr.Start(ctx); err != nil {
		t.Fatalf("second Start() should not error, got: %v", err)
	}
}

func TestStrategyRunner_StopWithoutStart(t *testing.T) {
	strat := &mockStrategy{name: "test-strat"}

	sr := NewStrategyRunner(StrategyRunnerConfig{
		AgentID:  "agent-1",
		Strategy: strat,
	})

	// Should not panic
	if err := sr.Stop(); err != nil {
		t.Fatalf("Stop() without Start() error: %v", err)
	}
}

func TestStrategyRunner_ContextCancellation(t *testing.T) {
	strat := &mockStrategy{name: "test-strat"}

	sr := NewStrategyRunner(StrategyRunnerConfig{
		AgentID:  "agent-1",
		Strategy: strat,
	})

	ctx, cancel := context.WithCancel(context.Background())

	if err := sr.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Cancel the parent context
	cancel()

	// Give time for the strategy goroutine to finish
	time.Sleep(100 * time.Millisecond)

	// The runner should mark itself as not running once the strategy returns
	if sr.IsRunning() {
		t.Error("should not be running after context cancellation")
	}
}

func TestStrategyRunner_StrategyError(t *testing.T) {
	stratErr := make(chan struct{})
	strat := &mockStrategy{
		name: "error-strat",
		runFn: func(_ context.Context, _ *game.Client, _ strategy.Config) error {
			close(stratErr)
			return context.Canceled // Simulate an error
		},
	}

	sr := NewStrategyRunner(StrategyRunnerConfig{
		AgentID:  "agent-1",
		Strategy: strat,
	})

	if err := sr.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for strategy to error out
	<-stratErr
	time.Sleep(50 * time.Millisecond)

	if sr.IsRunning() {
		t.Error("should not be running after strategy error")
	}
}

func TestStrategyRunner_ConcurrentIsRunning(t *testing.T) {
	strat := &mockStrategy{name: "test-strat"}

	sr := NewStrategyRunner(StrategyRunnerConfig{
		AgentID:  "agent-1",
		Strategy: strat,
	})

	if err := sr.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer func() { _ = sr.Stop() }()

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sr.IsRunning()
		}()
	}
	wg.Wait()
}

func TestStrategyRunner_WithParameters(t *testing.T) {
	var mu sync.Mutex
	var receivedParams map[string]string
	done := make(chan struct{})

	strat := &mockStrategy{
		name: "param-strat",
		runFn: func(_ context.Context, _ *game.Client, cfg strategy.Config) error {
			mu.Lock()
			receivedParams = cfg.Parameters
			mu.Unlock()
			close(done)
			return nil
		},
	}

	params := map[string]string{
		"station_action": "craft-sell",
		"target_system":  "sol",
	}

	sr := NewStrategyRunner(StrategyRunnerConfig{
		AgentID:    "agent-1",
		Strategy:   strat,
		Parameters: params,
	})

	if err := sr.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for strategy to run and finish
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for strategy to run")
	}

	mu.Lock()
	defer mu.Unlock()

	if receivedParams == nil {
		t.Fatal("strategy did not receive parameters")
	}
	if receivedParams["station_action"] != "craft-sell" {
		t.Errorf("station_action = %q, want %q", receivedParams["station_action"], "craft-sell")
	}
	if receivedParams["target_system"] != "sol" {
		t.Errorf("target_system = %q, want %q", receivedParams["target_system"], "sol")
	}
}
