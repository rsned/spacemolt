package strategy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// FighterStrategy implements autonomous combat operations.
// TODO: Extract full combat loop from cmd/auto-fighter when stabilized.
type FighterStrategy struct {
	status string
	mu     sync.RWMutex
}

func (s *FighterStrategy) Name() string        { return "fighter" }
func (s *FighterStrategy) Description() string { return "Autonomous combat: hunt hostiles, loot wrecks, upgrade weapons" }

func (s *FighterStrategy) CurrentStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.status == "" {
		return "idle"
	}
	return s.status
}

func (s *FighterStrategy) setStatus(status string) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

// Run executes the fighter strategy loop.
// This is currently a stub that monitors state and reports status.
// The full combat loop from cmd/auto-fighter will be integrated when stabilized.
func (s *FighterStrategy) Run(ctx context.Context, client *game.Client, cfg Config) error {
	logger := cfg.Logger
	logger.Printf("[%s] starting fighter strategy (stub)", cfg.AgentID)
	s.setStatus("patrolling")

	for {
		select {
		case <-ctx.Done():
			s.setStatus("stopped")
			return nil
		case <-time.After(game.SleepTick):
		}

		state := client.GetState()
		status := fmt.Sprintf("patrolling %s", state.CurrentSystem)
		s.setStatus(status)
		if cfg.OnStatusUpdate != nil {
			cfg.OnStatusUpdate(status)
		}
	}
}
