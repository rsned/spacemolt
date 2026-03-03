package strategy

import (
	"context"
	"testing"
	"time"
)

func TestChainStrategyName(t *testing.T) {
	a := &mockStrategy{name: "mine"}
	b := &mockStrategy{name: "sell"}
	chain := NewChainStrategy("mine+sell", a, b)

	if got := chain.Name(); got != "mine+sell" {
		t.Errorf("Name() = %q, want %q", got, "mine+sell")
	}
}

func TestChainStrategyRunsAllSteps(t *testing.T) {
	a := &mockStrategy{name: "step-a", runDelay: 10 * time.Millisecond}
	b := &mockStrategy{name: "step-b", runDelay: 10 * time.Millisecond}
	c := &mockStrategy{name: "step-c", runDelay: 10 * time.Millisecond}

	chain := NewChainStrategy("test-chain", a, b, c)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := chain.Run(ctx, nil, Config{AgentID: "test"})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	for _, s := range []*mockStrategy{a, b, c} {
		s.mu.Lock()
		called := s.runCalled
		s.mu.Unlock()
		if !called {
			t.Errorf("expected %s to have run", s.name)
		}
	}
}

func TestChainStrategyCancellation(t *testing.T) {
	slow := &mockStrategy{name: "slow", runDelay: 5 * time.Second}
	fast := &mockStrategy{name: "fast", runDelay: 10 * time.Millisecond}

	chain := NewChainStrategy("cancel-test", slow, fast)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_ = chain.Run(ctx, nil, Config{AgentID: "test"})

	fast.mu.Lock()
	fastCalled := fast.runCalled
	fast.mu.Unlock()

	if fastCalled {
		t.Error("fast step should NOT have run after cancellation")
	}
}

func TestChainStrategyImplementsInterface(t *testing.T) {
	var _ Strategy = &ChainStrategy{}
}
