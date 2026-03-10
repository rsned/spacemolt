package strategy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

const (
	// idleDecisionInterval is how often the idle strategy asks the LLM for guidance.
	// Longer than the normal decision interval to reduce LLM load.
	idleDecisionInterval = 30 * time.Second
)

// SmartIdleStrategy asks the LLM at longer intervals what the agent should do
// given their personality and current state. This is the fallback when no
// explicit strategy is assigned.
type SmartIdleStrategy struct {
	status string
	mu     sync.RWMutex
}

func (s *SmartIdleStrategy) Name() string { return "idle" }
func (s *SmartIdleStrategy) Description() string {
	return "Smart idle: LLM-guided behavior at 30s intervals based on agent personality"
}

func (s *SmartIdleStrategy) CurrentStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.status == "" {
		return "idle"
	}
	return s.status
}

func (s *SmartIdleStrategy) setStatus(status string) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

// Run executes the idle strategy loop.
func (s *SmartIdleStrategy) Run(ctx context.Context, client game.GameClient, cfg Config) error {
	logger := cfg.Logger
	llmClient := cfg.LLMClient

	if llmClient == nil {
		// Without an LLM client, just sit idle and report status.
		logger.Printf("[%s] smart idle: no LLM client, running in passive mode", cfg.AgentID)
		return s.passiveIdle(ctx, client, cfg)
	}

	logger.Printf("[%s] starting smart idle strategy (LLM-guided, %v interval)", cfg.AgentID, idleDecisionInterval)
	s.setStatus("idle (LLM-guided)")

	for {
		select {
		case <-ctx.Done():
			s.setStatus("stopped")
			return nil
		case <-time.After(idleDecisionInterval):
		}

		state := client.GetState()

		// Build a simple prompt asking the LLM what this agent should do.
		prompt := fmt.Sprintf(
			"You are %s, a space agent. You are currently in system %s at %s. "+
				"Your hull is at %.0f%%, fuel at %.0f%%, credits: %.0f. "+
				"You are %s. What should you do next? "+
				"Reply with a single action: mine, explore, trade, dock, undock, travel, or wait.",
			cfg.AgentID,
			state.CurrentSystem,
			state.CurrentPOI,
			safePercent(state.Hull, state.MaxHull),
			safePercent(state.Fuel, state.MaxFuel),
			state.Credits,
			dockedStr(state.Doc),
		)

		resp, err := llmClient.Decide(ctx, prompt)
		if err != nil {
			logger.Printf("[%s] idle LLM decision error: %v", cfg.AgentID, err)
			s.setStatus("idle (LLM error)")
			continue
		}

		status := fmt.Sprintf("idle: LLM suggests %q", resp.Action)
		s.setStatus(status)
		if cfg.OnStatusUpdate != nil {
			cfg.OnStatusUpdate(status)
		}

		logger.Printf("[%s] idle LLM decision: %s (confidence: %.0f%%)",
			cfg.AgentID, resp.Action, resp.Confidence)
	}
}

// passiveIdle just monitors state without LLM decisions.
func (s *SmartIdleStrategy) passiveIdle(ctx context.Context, client game.GameClient, cfg Config) error {
	for {
		select {
		case <-ctx.Done():
			s.setStatus("stopped")
			return nil
		case <-time.After(idleDecisionInterval):
		}

		state := client.GetState()
		status := fmt.Sprintf("idle at %s (passive)", state.CurrentSystem)
		s.setStatus(status)
		if cfg.OnStatusUpdate != nil {
			cfg.OnStatusUpdate(status)
		}
	}
}

func safePercent(value, max float64) float64 {
	if max <= 0 {
		return 0
	}
	return value / max * 100
}

func dockedStr(docked bool) string {
	if docked {
		return "docked at a station"
	}
	return "in open space"
}
