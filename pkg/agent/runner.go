package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// Runner wraps an agent with its game client and runs the play loop
type Runner struct {
	agent      Agent
	gameClient game.GameClient
	config     RunnerConfig

	// State
	mu              sync.RWMutex
	running         bool
	lastActionTick  int64
	lastActionTime  time.Time
	crashCount      int
	stopCh          chan struct{}
	stopOnce        sync.Once

	// Logging
	logger *log.Logger
}

// RunnerConfig holds configuration for the agent runner
type RunnerConfig struct {
	// DecisionInterval is how often the agent makes decisions
	// Should be less than the action tick (e.g., 3-5 seconds for 10-second ticks)
	DecisionInterval time.Duration

	// MaxRetries is the maximum number of consecutive decision errors before stopping
	MaxRetries int

	// ActionTimeout is how long to wait for action result before considering it failed
	ActionTimeout time.Duration

	// Logger for runner events
	Logger *log.Logger
}

// DefaultRunnerConfig returns sensible defaults
func DefaultRunnerConfig() RunnerConfig {
	return RunnerConfig{
		DecisionInterval: 5 * time.Second,  // Check every 5 seconds
		MaxRetries:       10,                // Allow 10 consecutive failures
		ActionTimeout:    2 * time.Second,   // Wait 2 seconds for action result
		Logger:           log.Default(),
	}
}

// NewRunner creates a new agent runner
func NewRunner(agent Agent, gameClient game.GameClient, config RunnerConfig) *Runner {
	if config.Logger == nil {
		config.Logger = log.Default()
	}

	return &Runner{
		agent:      agent,
		gameClient: gameClient,
		config:     config,
		stopCh:     make(chan struct{}),
		logger:     config.Logger,
	}
}

// Start begins the agent's play loop
func (r *Runner) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return fmt.Errorf("runner already started")
	}
	r.running = true
	r.mu.Unlock()

	r.logger.Printf("[%s] Starting agent runner", r.agent.ID())

	// Start agent
	if err := r.agent.Start(ctx); err != nil {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
		return fmt.Errorf("failed to start agent: %w", err)
	}

	// Start play loop in goroutine
	go r.run(ctx)

	return nil
}

// Stop halts the agent runner
func (r *Runner) Stop() error {
	r.stopOnce.Do(func() {
		close(r.stopCh)
	})

	// Wait a moment for graceful shutdown
	time.Sleep(100 * time.Millisecond)

	r.mu.Lock()
	r.running = false
	r.mu.Unlock()

	// Stop agent
	if err := r.agent.Stop(); err != nil {
		return fmt.Errorf("failed to stop agent: %w", err)
	}

	r.logger.Printf("[%s] Agent runner stopped", r.agent.ID())
	return nil
}

// IsRunning returns whether the runner is currently running
func (r *Runner) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}

// HasCrashed returns whether the runner has crashed (exceeded max retries)
func (r *Runner) HasCrashed() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.crashCount >= r.config.MaxRetries
}

// run is the main play loop
func (r *Runner) run(ctx context.Context) {
	ticker := time.NewTicker(r.config.DecisionInterval)
	defer ticker.Stop()

	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	consecutiveErrors := 0

	for {
		select {
		case <-ctx.Done():
			r.logger.Printf("[%s] Context cancelled, stopping play loop", r.agent.ID())
			return
		case <-r.stopCh:
			r.logger.Printf("[%s] Stop signal received", r.agent.ID())
			return
		case <-ticker.C:
			// Execute one decision cycle
			if err := r.executeCycle(ctx); err != nil {
				consecutiveErrors++
				r.mu.Lock()
				r.crashCount = consecutiveErrors
				r.mu.Unlock()

				r.logger.Printf("[%s] Decision cycle error (%d/%d): %v",
					r.agent.ID(), consecutiveErrors, r.config.MaxRetries, err)

				if consecutiveErrors >= r.config.MaxRetries {
					r.logger.Printf("[%s] Max retries exceeded, stopping runner", r.agent.ID())
					return
				}
			} else {
				// Success - reset error counter
				consecutiveErrors = 0
			}
		}
	}
}

// executeCycle runs one decision-making cycle
func (r *Runner) executeCycle(ctx context.Context) error {
	// Get current game state
	state := r.gameClient.GetState()
	if state == nil {
		return fmt.Errorf("game state is nil")
	}

	// Clone state for safe concurrent access
	stateCopy := state.Clone()
	currentTick := stateCopy.GetTick()

	// Check if we can take an action this tick
	r.mu.RLock()
	lastActionTick := r.lastActionTick
	r.mu.RUnlock()

	canAct := currentTick > lastActionTick

	// Agent makes decision
	decision, err := r.agent.Decide(ctx, stateCopy)
	if err != nil {
		return fmt.Errorf("decision failed: %w", err)
	}

	// Check if this is an action command (consumes tick)
	isAction := isActionCommand(decision.Action)

	// If it's an action command and we can't act yet, skip
	if isAction && !canAct {
		r.logger.Printf("[%s] Throttled: waiting for next tick (current: %d, last action: %d)",
			r.agent.ID(), currentTick, lastActionTick)
		return nil
	}

	// Execute the decision
	if err := r.executeDecision(ctx, decision); err != nil {
		// Record failure in agent learning
		result := ActionResult{
			Success:  false,
			Message:  fmt.Sprintf("Action failed: %v", err),
			NewState: r.gameClient.GetState(),
			Error:    err,
		}
		_ = r.agent.Learn(result)

		// Note: Failed actions don't consume tick, so we don't update lastActionTick
		return fmt.Errorf("action execution failed: %w", err)
	}

	// Update last action tick/time if this was an action command
	// Note: We update optimistically here, but failed actions won't consume tick
	if isAction {
		r.mu.Lock()
		r.lastActionTick = currentTick
		r.lastActionTime = time.Now()
		r.mu.Unlock()
	}

	// Record success in agent learning
	result := ActionResult{
		Success:  true,
		Message:  fmt.Sprintf("%s: %s", decision.Action, decision.Reasoning),
		NewState: r.gameClient.GetState(),
		Reward:   1.0, // Basic reward for successful action
		Error:    nil,
	}
	if err := r.agent.Learn(result); err != nil {
		r.logger.Printf("[%s] Warning: failed to record learning: %v", r.agent.ID(), err)
	}

	return nil
}

// executeDecision converts an agent decision to game commands
func (r *Runner) executeDecision(ctx context.Context, decision Decision) error {
	r.logger.Printf("[%s] Executing: %s (confidence: %.1f%%)",
		r.agent.ID(), decision.Action, decision.Confidence*100)

	// Create context with timeout for the action
	actionCtx, cancel := context.WithTimeout(ctx, r.config.ActionTimeout)
	defer cancel()

	switch decision.Action {
	case "undock":
		return r.gameClient.Undock(actionCtx)

	case "dock":
		return r.gameClient.Dock(actionCtx)

	case "travel":
		if decision.Target == "" {
			return fmt.Errorf("travel requires target POI")
		}
		return r.gameClient.Travel(actionCtx, decision.Target)

	case "jump":
		if decision.Target == "" {
			return fmt.Errorf("jump requires target system")
		}
		return r.gameClient.Jump(actionCtx, decision.Target)

	case "mine":
		return r.gameClient.Mine(actionCtx)

	case "scan":
		return r.gameClient.Scan(actionCtx)

	case "get_status":
		return r.gameClient.GetStatus(actionCtx)

	case "get_system":
		return r.gameClient.GetSystem(actionCtx)

	case "wait":
		// Deliberate wait - do nothing
		r.logger.Printf("[%s] Waiting (deliberate)", r.agent.ID())
		return nil

	default:
		return fmt.Errorf("unknown action: %s", decision.Action)
	}
}

// isActionCommand returns true for commands that consume the 10-second action tick
// SOURCE: https://www.spacemolt.com/api (check regularly for updates!)
// IMPORTANT: Failed actions do NOT count against rate limit
func isActionCommand(action string) bool {
	// Query/info commands that do NOT consume ticks
	queryCommands := map[string]bool{
		// Player & Ship Info
		"get_status":      true,
		"get_ship":        true,
		"get_skills":      true,
		// World Info
		"get_system":      true,
		"get_poi":         true,
		"get_base":        true,
		"get_map":         true,
		"get_version":     true,
		"get_base_cost":   true,
		// Market & Trading
		"get_listings":    true,
		"get_trades":      true,
		"get_wrecks":      true,
		"get_base_wrecks": true,
		"get_recipes":     true,
		"get_notes":       true,
		// Raid Info
		"raid_status":     true,
		// Forum Browsing
		"forum_list":       true,
		"forum_get_thread": true,
		// Help
		"help": true,
		// Auth (special case - not rate limited but not gameplay)
		"register": true,
		"login":    true,
		"logout":   true,
		// Meta
		"wait": true, // Deliberate waiting is not an action
	}

	// If it's a query command, it does NOT consume action tick
	if queryCommands[action] {
		return false
	}

	// Everything else is an action command (consumes tick)
	// Including: travel, jump, dock, undock, attack, scan, cloak, mine,
	// buy, sell, list_item, cancel_list, buy_listing, trade_offer,
	// trade_accept, trade_decline, trade_cancel, loot_wreck, salvage_wreck,
	// loot_base_wreck, salvage_base_wreck, buy_ship, install_mod, uninstall_mod,
	// refuel, repair, craft, chat, create_faction, join_faction, leave_faction,
	// faction_invite, faction_kick, faction_promote, buy_insurance,
	// claim_insurance, set_home_base, set_status, set_colors, set_anonymous,
	// build_base, attack_base, create_map, use_map, create_note, write_note,
	// forum_create_thread, forum_reply, forum_upvote, forum_delete_thread,
	// forum_delete_reply
	return true
}

// GetAgent returns the wrapped agent
func (r *Runner) GetAgent() Agent {
	return r.agent
}

// GetGameClient returns the game client
func (r *Runner) GetGameClient() game.GameClient {
	return r.gameClient
}

// GetLastActionTick returns the tick of the last action
func (r *Runner) GetLastActionTick() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastActionTick
}

// GetLastActionTime returns the time of the last action
func (r *Runner) GetLastActionTime() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastActionTime
}

// GetCrashCount returns the number of consecutive errors
func (r *Runner) GetCrashCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.crashCount
}
