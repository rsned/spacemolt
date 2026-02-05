package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// EventCallback is called when events occur during agent execution
type EventCallback func(agentID string, eventType string, data interface{})

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

	// History tracking
	history *History

	// Event callback for streaming
	eventCallback EventCallback

	// Logging
	logger *log.Logger
}

// RunnerConfig holds configuration for the agent runner
type RunnerConfig struct {
	// DecisionInterval is how often the agent makes decisions
	// Should be aligned with game tick rate (11 seconds for 10-second ticks to avoid edge cases)
	// Query commands can run anytime, but action commands are rate-limited by the server
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
		DecisionInterval: 11 * time.Second, // Slightly more than 10-second game tick
		MaxRetries:       10,                // Allow 10 consecutive failures
		ActionTimeout:    5 * time.Second,   // Wait 5 seconds for action result
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
		history:    NewHistory(1000), // Track last 1000 actions
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
	lastActionTime := r.lastActionTime
	r.mu.RUnlock()

	// Can act if either:
	// 1. The game tick has advanced (currentTick > lastActionTick)
	// 2. OR 12+ seconds have elapsed (fallback in case state update is delayed)
	timeSinceLastAction := time.Since(lastActionTime)
	tickAdvanced := currentTick > lastActionTick
	timeElapsed := timeSinceLastAction >= 12*time.Second || lastActionTime.IsZero()

	canAct := tickAdvanced || timeElapsed

	// Log throttling details for debugging
	if !canAct {
		r.logger.Printf("[%s] Throttle check: tick=%d, lastTick=%d, timeSince=%.1fs",
			r.agent.ID(), currentTick, lastActionTick, timeSinceLastAction.Seconds())
	}

	// Agent makes decision
	decision, err := r.agent.Decide(ctx, stateCopy)
	if err != nil {
		return fmt.Errorf("decision failed: %w", err)
	}

	// Emit decision event
	r.emitEvent("decision", map[string]interface{}{
		"action":     decision.Action,
		"target":     decision.Target,
		"confidence": decision.Confidence,
		"reasoning":  decision.Reasoning,
		"tick":       currentTick,
	})

	// Check if this is an action command (consumes tick)
	isAction := isActionCommand(decision.Action)

	// If it's an action command and we can't act yet, skip
	if isAction && !canAct {
		r.logger.Printf("[%s] Throttled: waiting for next tick (current: %d, last: %d, elapsed: %.1fs)",
			r.agent.ID(), currentTick, lastActionTick, timeSinceLastAction.Seconds())
		return nil
	}

	// Execute the decision
	if err := r.executeDecision(ctx, decision); err != nil {
		// Emit error event
		r.emitEvent("error", map[string]interface{}{
			"action":  decision.Action,
			"target":  decision.Target,
			"error":   err.Error(),
			"tick":    currentTick,
		})

		// Record failure in history
		r.recordAction(decision, "error", err)

		// Record failure in agent learning
		result := ActionResult{
			Action:   decision.Action,
			Target:   decision.Target,
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

	// Emit success event
	r.emitEvent("action", map[string]interface{}{
		"action":  decision.Action,
		"target":  decision.Target,
		"status":  "success",
		"tick":    currentTick,
	})

	// Record success in history
	r.recordAction(decision, "success", nil)

	// Record success in agent learning
	result := ActionResult{
		Action:   decision.Action,
		Target:   decision.Target,
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
	// DEBUG: Log full decision details
	r.logger.Printf("[%s] === Decision Execution Debug ===", r.agent.ID())
	r.logger.Printf("[%s] Decision.Action: '%s'", r.agent.ID(), decision.Action)
	r.logger.Printf("[%s] Decision.Target: '%s'", r.agent.ID(), decision.Target)
	r.logger.Printf("[%s] Decision.Confidence: %.1f%%", r.agent.ID(), decision.Confidence*100)
	r.logger.Printf("[%s] Decision.Reasoning: %s", r.agent.ID(), decision.Reasoning)

	// Create context with timeout for the action
	actionCtx, cancel := context.WithTimeout(ctx, r.config.ActionTimeout)
	defer cancel()

	switch decision.Action {
	case "undock":
		r.logger.Printf("[%s] -> Calling gameClient.Undock()", r.agent.ID())
		return r.gameClient.Undock(actionCtx)

	case "dock":
		r.logger.Printf("[%s] -> Calling gameClient.Dock()", r.agent.ID())
		return r.gameClient.Dock(actionCtx)

	case "travel":
		if decision.Target == "" {
			r.logger.Printf("[%s] ERROR: travel requires target POI but Target is empty", r.agent.ID())
			return fmt.Errorf("travel requires target POI")
		}
		r.logger.Printf("[%s] -> Calling gameClient.Travel('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.Travel(actionCtx, decision.Target)

	case "jump":
		if decision.Target == "" {
			r.logger.Printf("[%s] ERROR: jump requires target system but Target is empty", r.agent.ID())
			return fmt.Errorf("jump requires target system")
		}
		r.logger.Printf("[%s] -> Calling gameClient.Jump('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.Jump(actionCtx, decision.Target)

	case "mine":
		r.logger.Printf("[%s] -> Calling gameClient.Mine()", r.agent.ID())
		return r.gameClient.Mine(actionCtx)

	case "scan":
		r.logger.Printf("[%s] -> Calling gameClient.Scan()", r.agent.ID())
		err := r.gameClient.Scan(actionCtx)
		if err == nil {
			// Save POI knowledge after successful scan
			r.saveSystemKnowledge(ctx)
		}
		return err

	case "get_status":
		r.logger.Printf("[%s] -> Calling gameClient.GetStatus()", r.agent.ID())
		return r.gameClient.GetStatus(actionCtx)

	case "get_system":
		r.logger.Printf("[%s] -> Calling gameClient.GetSystem()", r.agent.ID())
		err := r.gameClient.GetSystem(actionCtx)
		if err == nil {
			// Save system and POI knowledge after successful get_system
			r.saveSystemKnowledge(ctx)
		}
		return err

	case "wait":
		// Deliberate wait - do nothing
		r.logger.Printf("[%s] -> Waiting (deliberate)", r.agent.ID())
		return nil

	default:
		r.logger.Printf("[%s] ERROR: Unknown action: '%s' (does not match any case)", r.agent.ID(), decision.Action)
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

// GetHistory returns the most recent N history entries
func (r *Runner) GetHistory(limit int) []HistoryEntry {
	return r.history.GetRecent(limit)
}

// SetEventCallback sets the callback function for streaming events
func (r *Runner) SetEventCallback(cb EventCallback) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventCallback = cb
}

// emitEvent publishes an event if a callback is set
func (r *Runner) emitEvent(eventType string, data interface{}) {
	r.mu.RLock()
	cb := r.eventCallback
	r.mu.RUnlock()

	if cb != nil {
		cb(r.agent.ID(), eventType, data)
	}
}

// recordAction adds an action to history
func (r *Runner) recordAction(decision Decision, result string, err error) {
	state := r.gameClient.GetState()
	currentTick := int64(0)
	if state != nil {
		currentTick = state.GetTick()
	}

	entry := HistoryEntry{
		Tick:       currentTick,
		Timestamp:  time.Now(),
		Action:     decision.Action,
		Target:     decision.Target,
		Confidence: decision.Confidence,
		Result:     result,
		Reasoning:  decision.Reasoning,
	}

	if err != nil {
		entry.Error = err.Error()
	}

	r.history.Add(entry)
}

// saveSystemKnowledge saves the current system and POI information to agent memory
func (r *Runner) saveSystemKnowledge(ctx context.Context) {
	state := r.gameClient.GetState()
	if state == nil {
		return
	}

	// Convert and save system information
	system := System{
		ID:            state.System.ID,
		Name:          state.System.Name,
		Position:      Position{X: state.System.Position.X, Y: state.System.Position.Y},
		SecurityLevel: getSecurityLevel(state.System.PoliceLevel),
		Faction:       state.System.Empire,
		Connections:   state.System.Connections,
		DiscoveredBy:  r.agent.Name(),
	}

	if err := r.agent.Memory().RememberSystem(ctx, system); err != nil {
		r.logger.Printf("[%s] Warning: failed to save system knowledge: %v", r.agent.ID(), err)
	}

	// Convert and save POI information
	for _, gamePOI := range state.System.POIs {
		// Extract full resource information
		resources := make([]ResourceInfo, 0, len(gamePOI.Resources))
		for _, res := range gamePOI.Resources {
			resources = append(resources, ResourceInfo{
				ResourceID: res.ResourceID,
				Richness:   res.Richness,
				Remaining:  res.Remaining,
			})
		}

		poi := POI{
			ID:           gamePOI.ID,
			SystemID:     gamePOI.SystemID,
			Name:         gamePOI.Name,
			Type:         gamePOI.Type,
			Position:     Position{X: gamePOI.Position.X, Y: gamePOI.Position.Y},
			Services:     []string{}, // Services are not in current game state
			Resources:    resources,
			DiscoveredBy: r.agent.Name(),
		}

		if err := r.agent.Memory().RememberPOI(ctx, poi); err != nil {
			r.logger.Printf("[%s] Warning: failed to save POI knowledge: %v", r.agent.ID(), err)
		}
	}

	// Save connections
	for _, conn := range state.System.Connections {
		if err := r.agent.Memory().RememberConnection(ctx, state.System.ID, conn); err != nil {
			r.logger.Printf("[%s] Warning: failed to save connection knowledge: %v", r.agent.ID(), err)
		}
	}
}

// getSecurityLevel converts police level to security level string
func getSecurityLevel(policeLevel int) string {
	switch policeLevel {
	case 0:
		return "None"
	case 1:
		return "Low"
	case 2:
		return "Medium"
	case 3:
		return "High"
	default:
		return "Unknown"
	}
}
