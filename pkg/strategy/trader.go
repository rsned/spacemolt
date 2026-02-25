package strategy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// TraderStrategy implements autonomous trading operations.
// TODO: Extract full trade route logic from cmd/auto-trader when stabilized.
type TraderStrategy struct {
	status string
	mu     sync.RWMutex
}

func (s *TraderStrategy) Name() string        { return "trader" }
func (s *TraderStrategy) Description() string { return "Autonomous trading: buy/sell commodities, run trade routes" }

func (s *TraderStrategy) CurrentStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.status == "" {
		return "idle"
	}
	return s.status
}

func (s *TraderStrategy) setStatus(status string) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

// Run executes the trader strategy loop.
// This is currently a stub that monitors state and reports status.
// The full trade route logic from cmd/auto-trader will be integrated when stabilized.
func (s *TraderStrategy) Run(ctx context.Context, client *game.Client, cfg Config) error {
	logger := cfg.Logger
	logger.Printf("[%s] starting trader strategy (stub)", cfg.AgentID)
	s.setStatus("trading")

	for {
		select {
		case <-ctx.Done():
			s.setStatus("stopped")
			return nil
		case <-time.After(game.SleepTick):
		}

		state := client.GetState()
		status := fmt.Sprintf("trading at %s (credits: %.0f)", state.CurrentSystem, state.Credits)
		s.setStatus(status)
		if cfg.OnStatusUpdate != nil {
			cfg.OnStatusUpdate(status)
		}
	}
}
