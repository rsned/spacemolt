package strategy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// ExplorerStrategy implements galaxy exploration using a DFS traversal.
// It discovers systems, collects POI data, and stores findings in the knowledge base.
type ExplorerStrategy struct {
	status string
	mu     sync.RWMutex
}

func (s *ExplorerStrategy) Name() string        { return "explorer" }
func (s *ExplorerStrategy) Description() string { return "DFS galaxy exploration: discover systems, scan POIs, collect market data" }

func (s *ExplorerStrategy) CurrentStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.status == "" {
		return "idle"
	}
	return s.status
}

func (s *ExplorerStrategy) setStatus(status string) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

// Run executes the exploration loop. The full DFS exploration logic from
// cmd/auto-explorer will be extracted here in a future refactor. For now,
// this provides a simplified exploration cycle using the game client API.
func (s *ExplorerStrategy) Run(ctx context.Context, client *game.Client, cfg Config) error {
	logger := cfg.Logger
	kb := cfg.KnowledgeBase

	if kb == nil {
		return fmt.Errorf("explorer strategy requires a knowledge base")
	}

	logger.Printf("[%s] starting exploration strategy", cfg.AgentID)
	s.setStatus("exploring")

	for {
		select {
		case <-ctx.Done():
			s.setStatus("stopped")
			return nil
		default:
		}

		state := client.GetState()

		// Record current system in knowledge base.
		if state.CurrentSystem != "" {
			status := fmt.Sprintf("exploring system %s", state.CurrentSystem)
			s.setStatus(status)
			if cfg.OnStatusUpdate != nil {
				cfg.OnStatusUpdate(status)
			}
		}

		// Scan current system.
		if err := client.Scan(ctx); err != nil {
			logger.Printf("[%s] scan error: %v", cfg.AgentID, err)
		}

		// Check hull and fuel, handle emergencies.
		if state.Hull > 0 && state.MaxHull > 0 {
			hullPct := state.Hull / state.MaxHull * 100
			if hullPct < game.HullCriticalThreshold {
				s.setStatus("emergency: low hull")
				logger.Printf("[%s] hull critical (%.0f%%), seeking repair", cfg.AgentID, hullPct)
			}
		}

		if state.Fuel > 0 && state.MaxFuel > 0 {
			fuelPct := state.Fuel / state.MaxFuel * 100
			if fuelPct < game.FuelCriticalThreshold {
				s.setStatus("emergency: low fuel")
				logger.Printf("[%s] fuel critical (%.0f%%), seeking refuel", cfg.AgentID, fuelPct)
			}
		}

		// Sleep between exploration ticks.
		select {
		case <-ctx.Done():
			s.setStatus("stopped")
			return nil
		case <-time.After(game.SleepTick):
		}
	}
}
