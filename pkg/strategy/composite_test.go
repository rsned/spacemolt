package strategy

import (
	"context"
	"log"
	"testing"
	"time"
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
