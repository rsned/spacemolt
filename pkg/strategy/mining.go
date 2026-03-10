package strategy

import (
	"context"
	"fmt"
	"sync"

	"github.com/rsned/spacemolt/pkg/game"
)

// MiningStrategy wraps game.MiningLoop as a reusable strategy.
type MiningStrategy struct {
	status string
	mu     sync.RWMutex
}

func (s *MiningStrategy) Name() string        { return "mining" }
func (s *MiningStrategy) Description() string { return "Autonomous mining: mine resources, sell/craft at station, upgrade ship" }

func (s *MiningStrategy) CurrentStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.status == "" {
		return "idle"
	}
	return s.status
}

func (s *MiningStrategy) setStatus(status string) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

func (s *MiningStrategy) Run(ctx context.Context, client game.GameClient, cfg Config) error {
	logger := cfg.Logger

	// Parse station action strategy from parameters.
	stationAction := game.StationActionSellAll
	if sa, ok := cfg.Parameters["station_action"]; ok {
		switch sa {
		case "sell":
			stationAction = game.StationActionSellAll
		case "craft-sell":
			stationAction = game.StationActionCraftAndSell
		case "craft-deposit":
			stationAction = game.StationActionCraftAndDeposit
		default:
			logger.Printf("[%s] unknown station_action %q, defaulting to sell", cfg.AgentID, sa)
		}
	}

	s.setStatus("mining")
	if cfg.OnStatusUpdate != nil {
		cfg.OnStatusUpdate("mining")
	}

	miningCfg := &game.MiningLoopConfig{
		AgentID:              cfg.AgentID,
		UpgradeCheckInterval: 500,
		Tier1Threshold:       300.0,
		ReserveCredits:       50.0,
		UseBulkSell:          true,
		OnStationActions:     stationAction,
		OnUpgradeCheck: func() bool {
			return false
		},
		OnRunComplete: func(runNum int, creditsEarned float64, _ float64) {
			status := fmt.Sprintf("mining run #%d (earned %.0f credits)", runNum, creditsEarned)
			s.setStatus(status)
			if cfg.OnStatusUpdate != nil {
				cfg.OnStatusUpdate(status)
			}
		},
	}

	result, err := game.MiningLoop(client, logger, ctx, miningCfg)
	if err != nil {
		s.setStatus("error: " + err.Error())
		return err
	}

	s.setStatus(fmt.Sprintf("stopped: %s (%d runs, %.0f credits)",
		result.StoppedReason, result.RunsCompleted, result.TotalCreditsEarned))
	return nil
}
