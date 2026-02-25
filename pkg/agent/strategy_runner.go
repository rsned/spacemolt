package agent

import (
	"context"
	"log"
	"sync"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/strategy"
)

// StrategyRunner runs a Strategy instead of an LLM-based decision loop.
// It implements a similar lifecycle to Runner but delegates behavior to the strategy.
type StrategyRunner struct {
	agentID    string
	strategy   strategy.Strategy
	gameClient *game.Client
	kb         knowledge.Base
	params     map[string]string

	mu      sync.RWMutex
	running bool
	cancel  context.CancelFunc
	logger  *log.Logger
}

// StrategyRunnerConfig holds config for creating a StrategyRunner.
type StrategyRunnerConfig struct {
	AgentID    string
	Strategy   strategy.Strategy
	GameClient *game.Client
	KB         knowledge.Base
	Parameters map[string]string
	Logger     *log.Logger
}

// NewStrategyRunner creates a new strategy runner.
func NewStrategyRunner(cfg StrategyRunnerConfig) *StrategyRunner {
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	if cfg.Parameters == nil {
		cfg.Parameters = make(map[string]string)
	}
	return &StrategyRunner{
		agentID:    cfg.AgentID,
		strategy:   cfg.Strategy,
		gameClient: cfg.GameClient,
		kb:         cfg.KB,
		params:     cfg.Parameters,
		logger:     cfg.Logger,
	}
}

// Start begins running the strategy in a goroutine.
func (sr *StrategyRunner) Start(ctx context.Context) error {
	sr.mu.Lock()
	if sr.running {
		sr.mu.Unlock()
		return nil
	}
	sr.running = true

	stratCtx, cancel := context.WithCancel(ctx)
	sr.cancel = cancel
	sr.mu.Unlock()

	go func() {
		sr.logger.Printf("[%s] starting strategy %q", sr.agentID, sr.strategy.Name())

		cfg := strategy.Config{
			AgentID:       sr.agentID,
			Logger:        sr.logger,
			KnowledgeBase: sr.kb,
			Parameters:    sr.params,
			OnStatusUpdate: func(status string) {
				sr.logger.Printf("[%s] strategy status: %s", sr.agentID, status)
			},
		}

		if err := sr.strategy.Run(stratCtx, sr.gameClient, cfg); err != nil {
			sr.logger.Printf("[%s] strategy %q error: %v", sr.agentID, sr.strategy.Name(), err)
		}

		sr.mu.Lock()
		sr.running = false
		sr.mu.Unlock()

		sr.logger.Printf("[%s] strategy %q stopped", sr.agentID, sr.strategy.Name())
	}()

	return nil
}

// Stop cancels the running strategy.
func (sr *StrategyRunner) Stop() error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if sr.cancel != nil {
		sr.cancel()
	}
	sr.running = false
	return nil
}

// IsRunning returns whether the strategy is currently executing.
func (sr *StrategyRunner) IsRunning() bool {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	return sr.running
}

// Strategy returns the underlying strategy.
func (sr *StrategyRunner) Strategy() strategy.Strategy {
	return sr.strategy
}

// AgentID returns the agent ID.
func (sr *StrategyRunner) AgentID() string {
	return sr.agentID
}

// GameClient returns the underlying game client.
func (sr *StrategyRunner) GameClient() *game.Client {
	return sr.gameClient
}
