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
type EventCallback func(agentID string, eventType string, data any)

// ToTContext carries additional context for the Tree-of-Thought evaluator.
type ToTContext struct {
	RecentActions []HistoryEntry
	Goal          *Goal
	Priority      *Priority
}

// ToTEvaluator is an optional evaluator that uses Tree-of-Thought reasoning
// for richer decision making. Implemented by pkg/tot.Evaluator via an adapter.
type ToTEvaluator interface {
	// EvaluateToT runs the Tree-of-Thought pipeline and returns the best decision.
	// The second return value is the full thought tree for event emission.
	EvaluateToT(ctx context.Context, personality Personality, es EnrichedState, totCtx *ToTContext) (Decision, any, error)
}

// Runner wraps an agent with its game client and runs the play loop
type Runner struct {
	agent      Agent
	gameClient game.GameClient
	config     RunnerConfig

	// State
	mu             sync.RWMutex
	running        bool
	lastActionTick int64
	lastActionTime time.Time
	// lastPassiveSkillCheck is the wall-clock time of the most recent
	// runner-injected get_skills poll. Used to throttle passive XP
	// detection to game.PassiveSkillCheckInterval.
	lastPassiveSkillCheck time.Time
	crashCount            int
	stopCh                chan struct{}
	stopOnce              sync.Once

	// System tracking for route-home auto-update
	lastKnownSystem string

	// History tracking
	history *History

	// Event callback for streaming
	eventCallback EventCallback

	// Tree-of-Thought evaluator (nil if not enabled)
	totEvaluator ToTEvaluator

	// Pause state — when paused, the runner skips decision/ToT cycles
	paused bool

	// Enriched state — if set, provides KB enrichment and action-space
	// evaluation each cycle. Created externally (e.g., via agentstate.New)
	// and injected via SetEnrichedState.
	enrichedState EnrichedState

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
		MaxRetries:       10,               // Allow 10 consecutive failures
		ActionTimeout:    15 * time.Second, // Must exceed game tick (10s) for pending actions
		Logger:           log.Default(),
	}
}

// NewRunner creates a new agent runner
func NewRunner(agent Agent, gameClient game.GameClient, config RunnerConfig) *Runner {
	if config.Logger == nil {
		config.Logger = log.Default()
	}

	return &Runner{
		agent:                 agent,
		gameClient:            gameClient,
		config:                config,
		stopCh:                make(chan struct{}),
		logger:                config.Logger,
		history:               NewHistory(1000), // Track last 1000 actions
		lastPassiveSkillCheck: time.Now(),
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
	// Skip cycle entirely if paused
	r.mu.RLock()
	paused := r.paused
	r.mu.RUnlock()
	if paused {
		return nil
	}

	// Periodic passive-skill check. get_skills has no tick cost, so this
	// can run independent of tick advancement. The XPCallback wired into
	// the game client will record any non-zero deltas with
	// source="passive_skill" via XPTracker.
	r.mu.RLock()
	lastPassiveCheck := r.lastPassiveSkillCheck
	r.mu.RUnlock()
	if time.Since(lastPassiveCheck) >= game.PassiveSkillCheckInterval {
		r.logger.Printf("[%s] -> GetSkills() (passive XP check)", r.agent.ID())
		if err := r.gameClient.GetSkills(ctx); err != nil {
			r.logger.Printf("[%s] passive get_skills failed: %v", r.agent.ID(), err)
		}
		r.mu.Lock()
		r.lastPassiveSkillCheck = time.Now()
		r.mu.Unlock()
	}

	// Get current game state
	state := r.gameClient.GetState()
	if state == nil {
		return fmt.Errorf("game state is nil")
	}

	// Build enriched state for this cycle. If a full EnrichedState (backed
	// by agentstate.AgentState) was injected, refresh its enrichment layers.
	// Otherwise fall back to a raw wrapper around a cloned game state.
	var es EnrichedState
	if r.enrichedState != nil {
		r.enrichedState.Refresh(ctx)
		es = r.enrichedState
	}

	// Clone state for safe concurrent reads (tick checking, route updates).
	stateCopy := state.Clone()
	currentTick := stateCopy.GetTick()

	if es == nil {
		es = NewRawEnrichedState(stateCopy)
	}

	// Auto-update route home on system change
	r.maybeUpdateRouteHome(ctx, stateCopy)

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

	// If tick hasn't advanced, use get_notifications to refresh tick and
	// check for new notifications. This is cheaper than get_status.
	if !canAct {
		if err := r.gameClient.GetNotifications(ctx); err != nil {
			r.logger.Printf("[%s] get_notifications failed: %v", r.agent.ID(), err)
		}
		// Re-check tick after refresh
		state = r.gameClient.GetState()
		if state != nil {
			stateCopy = state.Clone()
			currentTick = stateCopy.GetTick()
			canAct = currentTick > lastActionTick
		}
	}

	// Skip decision-making entirely if we still can't act after refresh.
	// This prevents wasting LLM calls (especially the multi-call ToT pipeline)
	// when the game tick hasn't advanced.
	if !canAct {
		r.logger.Printf("[%s] Waiting for tick (current: %d, last: %d, elapsed: %.1fs)",
			r.agent.ID(), currentTick, lastActionTick, timeSinceLastAction.Seconds())
		return nil
	}

	// Try to use queued action first, fall back to LLM decision
	var decision Decision
	var err error

	if queuedAction, ok := r.agent.DequeueAction(); ok {
		// Use queued action
		r.logger.Printf("[%s] Using queued action [%d]: %s (queue has %d remaining)",
			r.agent.ID(), queuedAction.Sequence, queuedAction.Action, len(r.agent.GetActionQueue()))

		decision = Decision{
			Action:    queuedAction.Action,
			Target:    queuedAction.Target,
			Reasoning: fmt.Sprintf("[QUEUED #%d] %s", queuedAction.Sequence, queuedAction.Reasoning),
		}

		r.agent.SetUsingQueuedAction(true)
	} else {
		// No queued actions - get fresh decision
		if r.totEvaluator != nil && r.agent.Personality().DecisionMode == "tot" {
			// Tree-of-Thought pipeline — pass recent history and goal context
			totCtx := &ToTContext{
				RecentActions: r.history.GetRecent(5),
			}
			var tree any
			decision, tree, err = r.totEvaluator.EvaluateToT(ctx, r.agent.Personality(), es, totCtx)
			if err != nil {
				r.logger.Printf("[%s] ToT failed, falling back to single-call: %v", r.agent.ID(), err)
				decision, err = r.agent.Decide(ctx, es)
				if err != nil {
					return fmt.Errorf("decision failed: %w", err)
				}
			} else {
				r.emitEvent("thought_tree", tree)
			}
		} else {
			// Standard single-call LLM decision
			decision, err = r.agent.Decide(ctx, es)
			if err != nil {
				return fmt.Errorf("decision failed: %w", err)
			}
		}

		// Save any planned actions to the queue
		if len(decision.PlannedActions) > 0 {
			r.agent.EnqueueActions(decision.PlannedActions)
			r.logger.Printf("[%s] Enqueued %d planned actions", r.agent.ID(), len(decision.PlannedActions))
		}

		r.agent.SetUsingQueuedAction(false)
	}

	// Emit decision event
	r.emitEvent("decision", map[string]any{
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
		r.emitEvent("error", map[string]any{
			"action": decision.Action,
			"target": decision.Target,
			"error":  err.Error(),
			"tick":   currentTick,
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

		// Clear action queue on failures (plan assumptions invalid)
		r.agent.ClearActionQueue("action_failed")

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
	r.emitEvent("action", map[string]any{
		"action": decision.Action,
		"target": decision.Target,
		"status": "success",
		"tick":   currentTick,
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

	// Create context with timeout for the action.
	// Travel and jump compute their own timeouts based on arrival_tick,
	// so they get a generous 2-minute ceiling. Other actions use the
	// configured ActionTimeout (15s).
	timeout := r.config.ActionTimeout
	switch decision.Action {
	case "travel", "jump":
		timeout = 2 * time.Minute
	}
	actionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch decision.Action {
	// ---- Navigation ----
	case "undock":
		r.logger.Printf("[%s] -> Undock()", r.agent.ID())
		return r.gameClient.Undock(actionCtx)

	case "dock":
		r.logger.Printf("[%s] -> Dock()", r.agent.ID())
		return r.gameClient.Dock(actionCtx)

	case "travel":
		if decision.Target == "" {
			return fmt.Errorf("travel requires target POI")
		}
		r.logger.Printf("[%s] -> Travel('%s')", r.agent.ID(), decision.Target)
		_, err := r.gameClient.Travel(actionCtx, decision.Target)
		return err

	case "jump":
		if decision.Target == "" {
			return fmt.Errorf("jump requires target system")
		}
		r.logger.Printf("[%s] -> Jump('%s')", r.agent.ID(), decision.Target)
		_, err := r.gameClient.Jump(actionCtx, decision.Target)
		return err

	// ---- Mining & Scanning ----
	case "mine":
		r.logger.Printf("[%s] -> Mine()", r.agent.ID())
		return r.gameClient.Mine(actionCtx)

	case "scan":
		r.logger.Printf("[%s] -> Scan()", r.agent.ID())
		return r.gameClient.Scan(actionCtx)

	// ---- Combat ----
	case "attack":
		if decision.Target == "" {
			return fmt.Errorf("attack requires target ID")
		}
		r.logger.Printf("[%s] -> Attack('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.Attack(actionCtx, decision.Target)

	case "cloak":
		r.logger.Printf("[%s] -> Cloak(true)", r.agent.ID())
		return r.gameClient.Cloak(actionCtx, true)

	case "uncloak":
		r.logger.Printf("[%s] -> Cloak(false)", r.agent.ID())
		return r.gameClient.Cloak(actionCtx, false)

	// ---- Commerce ----
	case "sell":
		// Sell all cargo via bulk API (simpler than per-item when LLM doesn't specify quantity)
		r.logger.Printf("[%s] -> SellAllBulk()", r.agent.ID())
		return r.gameClient.SellAllBulk(actionCtx, nil)

	case "buy":
		if decision.Target == "" {
			return fmt.Errorf("buy requires target item ID")
		}
		r.logger.Printf("[%s] -> Buy('%s', 1)", r.agent.ID(), decision.Target)
		return r.gameClient.Buy(actionCtx, decision.Target, 1)

	case "get_listings":
		r.logger.Printf("[%s] -> GetListings()", r.agent.ID())
		return r.gameClient.GetListings(actionCtx)

	case "get_trades":
		r.logger.Printf("[%s] -> GetTrades()", r.agent.ID())
		return r.gameClient.GetTrades(actionCtx)

	// ---- Crafting ----
	case "craft":
		if decision.Target == "" {
			return fmt.Errorf("craft requires target recipe ID")
		}
		r.logger.Printf("[%s] -> CraftWithQuantity('%s', 1)", r.agent.ID(), decision.Target)
		return r.gameClient.CraftWithQuantity(actionCtx, decision.Target, 1)

	case "recycle":
		if decision.Target == "" {
			return fmt.Errorf("recycle requires target recipe ID")
		}
		r.logger.Printf("[%s] -> Recycle('%s', 1)", r.agent.ID(), decision.Target)
		return r.gameClient.Recycle(actionCtx, decision.Target, 1)

	case "get_recipes":
		r.logger.Printf("[%s] -> GetRecipes()", r.agent.ID())
		return r.gameClient.GetRecipes(actionCtx)

	// ---- Ship Maintenance ----
	case "refuel":
		r.logger.Printf("[%s] -> Refuel()", r.agent.ID())
		return r.gameClient.Refuel(actionCtx)

	case "repair":
		r.logger.Printf("[%s] -> Repair()", r.agent.ID())
		return r.gameClient.Repair(actionCtx)

	case "install", "install_mod":
		if decision.Target == "" {
			return fmt.Errorf("install requires target module ID")
		}
		r.logger.Printf("[%s] -> InstallMod('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.InstallMod(actionCtx, decision.Target)

	case "uninstall_mod":
		if decision.Target == "" {
			return fmt.Errorf("uninstall_mod requires target module ID")
		}
		r.logger.Printf("[%s] -> UninstallMod('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.UninstallMod(actionCtx, decision.Target)

	case "buy_ship":
		if decision.Target == "" {
			return fmt.Errorf("buy_ship requires target ship class")
		}
		r.logger.Printf("[%s] -> BuyShip('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.BuyShip(actionCtx, decision.Target)

	case "buy_insurance":
		r.logger.Printf("[%s] -> BuyInsurance(100)", r.agent.ID())
		return r.gameClient.BuyInsurance(actionCtx, 100)

	case "claim_insurance":
		r.logger.Printf("[%s] -> ClaimInsurance()", r.agent.ID())
		return r.gameClient.ClaimInsurance(actionCtx)

	// ---- Cargo & Storage ----
	case "deposit_items":
		if decision.Target == "" {
			// No specific item — deposit everything
			r.logger.Printf("[%s] -> DepositAllItems()", r.agent.ID())
			return r.gameClient.DepositAllItems(actionCtx)
		}
		r.logger.Printf("[%s] -> DepositItems('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.DepositItems(actionCtx, decision.Target, 0)

	case "deposit_all_items":
		r.logger.Printf("[%s] -> DepositAllItems()", r.agent.ID())
		return r.gameClient.DepositAllItems(actionCtx)

	case "withdraw_items":
		if decision.Target == "" {
			return fmt.Errorf("withdraw_items requires target item ID")
		}
		r.logger.Printf("[%s] -> WithdrawItems('%s', 1)", r.agent.ID(), decision.Target)
		return r.gameClient.WithdrawItems(actionCtx, decision.Target, 1)

	case "view_storage":
		r.logger.Printf("[%s] -> ViewStorage()", r.agent.ID())
		return r.gameClient.ViewStorage(actionCtx)

	// ---- Wrecks ----
	case "get_wrecks":
		r.logger.Printf("[%s] -> GetWrecks()", r.agent.ID())
		return r.gameClient.GetWrecks(actionCtx)

	case "loot_wreck":
		if decision.Target == "" {
			return fmt.Errorf("loot_wreck requires target wreck ID")
		}
		// Loot all items from the wreck (empty itemID + quantity 0 = loot all)
		r.logger.Printf("[%s] -> LootWreck('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.LootWreck(actionCtx, decision.Target, "", 0)

	case "salvage_wreck":
		if decision.Target == "" {
			return fmt.Errorf("salvage_wreck requires target wreck ID")
		}
		r.logger.Printf("[%s] -> SalvageWreck('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.SalvageWreck(actionCtx, decision.Target)

	// ---- Queries ----
	case "get_status":
		r.logger.Printf("[%s] -> GetStatus()", r.agent.ID())
		return r.gameClient.GetStatus(actionCtx)

	case "get_notifications":
		r.logger.Printf("[%s] -> GetNotifications()", r.agent.ID())
		return r.gameClient.GetNotifications(actionCtx)

	case "get_system":
		r.logger.Printf("[%s] -> GetSystem()", r.agent.ID())
		return r.gameClient.GetSystem(actionCtx)

	case "get_ship":
		r.logger.Printf("[%s] -> GetShip()", r.agent.ID())
		return r.gameClient.GetShip(actionCtx)

	case "get_skills":
		r.logger.Printf("[%s] -> GetSkills()", r.agent.ID())
		return r.gameClient.GetSkills(actionCtx)

	case "get_poi":
		r.logger.Printf("[%s] -> GetPOI()", r.agent.ID())
		return r.gameClient.GetPOI(actionCtx)

	case "get_base":
		r.logger.Printf("[%s] -> GetBase()", r.agent.ID())
		return r.gameClient.GetBase(actionCtx)

	case "get_map":
		r.logger.Printf("[%s] -> GetMap()", r.agent.ID())
		return r.gameClient.GetMap(actionCtx)

	case "get_nearby":
		r.logger.Printf("[%s] -> GetNearby()", r.agent.ID())
		return r.gameClient.GetNearby(actionCtx)

	case "get_version":
		r.logger.Printf("[%s] -> GetVersion()", r.agent.ID())
		return r.gameClient.GetVersion(actionCtx)

	case "get_notes":
		r.logger.Printf("[%s] -> GetNotes()", r.agent.ID())
		return r.gameClient.GetNotes(actionCtx)

	case "help":
		r.logger.Printf("[%s] -> Help()", r.agent.ID())
		return r.gameClient.Help(actionCtx, nil)

	// ---- Route Planning ----
	case "find_route":
		if decision.Target == "" {
			return fmt.Errorf("find_route requires target system")
		}
		r.logger.Printf("[%s] -> FindRoute('%s')", r.agent.ID(), decision.Target)
		route, err := r.gameClient.FindRoute(actionCtx, decision.Target)
		if err != nil {
			return err
		}
		r.agent.SetRouteHome(route, "")
		return nil

	// ---- Faction ----
	case "create_faction":
		r.logger.Printf("[%s] -> CreateFaction()", r.agent.ID())
		return r.gameClient.CreateFaction(actionCtx, map[string]any{"name": decision.Target})

	case "join_faction":
		if decision.Target == "" {
			return fmt.Errorf("join_faction requires target faction ID")
		}
		r.logger.Printf("[%s] -> JoinFaction('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.JoinFaction(actionCtx, decision.Target)

	case "leave_faction":
		r.logger.Printf("[%s] -> LeaveFaction()", r.agent.ID())
		return r.gameClient.LeaveFaction(actionCtx)

	case "faction_invite":
		if decision.Target == "" {
			return fmt.Errorf("faction_invite requires target player ID")
		}
		r.logger.Printf("[%s] -> FactionInvite('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.FactionInvite(actionCtx, decision.Target)

	case "faction_kick":
		if decision.Target == "" {
			return fmt.Errorf("faction_kick requires target player ID")
		}
		r.logger.Printf("[%s] -> FactionKick('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.FactionKick(actionCtx, decision.Target)

	case "faction_promote":
		if decision.Target == "" {
			return fmt.Errorf("faction_promote requires target player ID")
		}
		r.logger.Printf("[%s] -> FactionPromote('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.FactionPromote(actionCtx, decision.Target, "")

	// ---- Communication ----
	case "chat":
		r.logger.Printf("[%s] -> Chat('local', '%s')", r.agent.ID(), decision.Target)
		return r.gameClient.Chat(actionCtx, "local", decision.Target, "")

	case "set_status":
		r.logger.Printf("[%s] -> SetPlayerStatus()", r.agent.ID())
		return r.gameClient.SetPlayerStatus(actionCtx, map[string]any{"status": decision.Target})

	case "set_home_base":
		if decision.Target == "" {
			return fmt.Errorf("set_home_base requires target base ID")
		}
		r.logger.Printf("[%s] -> SetHomeBase('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.SetHomeBase(actionCtx, decision.Target)

	// ---- Forum ----
	case "forum_list":
		r.logger.Printf("[%s] -> ForumList(1)", r.agent.ID())
		return r.gameClient.ForumList(actionCtx, 1)

	case "forum_create_thread":
		r.logger.Printf("[%s] -> ForumCreateThread()", r.agent.ID())
		return r.gameClient.ForumCreateThread(actionCtx, decision.Target, "", "general")

	case "forum_get_thread":
		if decision.Target == "" {
			return fmt.Errorf("forum_get_thread requires target thread ID")
		}
		r.logger.Printf("[%s] -> ForumGetThread('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.ForumGetThread(actionCtx, decision.Target)

	case "forum_reply":
		if decision.Target == "" {
			return fmt.Errorf("forum_reply requires target thread ID")
		}
		r.logger.Printf("[%s] -> ForumReply('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.ForumReply(actionCtx, decision.Target, "")

	case "forum_upvote":
		if decision.Target == "" {
			return fmt.Errorf("forum_upvote requires target thread ID")
		}
		r.logger.Printf("[%s] -> ForumUpvote('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.ForumUpvote(actionCtx, decision.Target, "")

	case "forum_delete_thread":
		if decision.Target == "" {
			return fmt.Errorf("forum_delete_thread requires target thread ID")
		}
		r.logger.Printf("[%s] -> ForumDeleteThread('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.ForumDeleteThread(actionCtx, decision.Target)

	case "forum_delete_reply":
		if decision.Target == "" {
			return fmt.Errorf("forum_delete_reply requires target reply ID")
		}
		r.logger.Printf("[%s] -> ForumDeleteReply('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.ForumDeleteReply(actionCtx, decision.Target)

	// ---- Notes ----
	case "create_note":
		r.logger.Printf("[%s] -> CreateNote('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.CreateNote(actionCtx, decision.Target, "")

	case "write_note":
		if decision.Target == "" {
			return fmt.Errorf("write_note requires target note ID")
		}
		r.logger.Printf("[%s] -> WriteNote('%s')", r.agent.ID(), decision.Target)
		return r.gameClient.WriteNote(actionCtx, decision.Target, "")

	// ---- Meta ----
	case "wait":
		r.logger.Printf("[%s] -> Waiting (deliberate)", r.agent.ID())
		return nil

	default:
		r.logger.Printf("[%s] ERROR: Unknown action: '%s' (does not match any case)", r.agent.ID(), decision.Action)
		return fmt.Errorf("unknown action: %s", decision.Action)
	}
}

// maybeUpdateRouteHome checks if the agent has entered a new system and, if so,
// calls find_route to cache the route back to the safe system. This costs one
// game tick but ensures the agent always has an escape route available.
func (r *Runner) maybeUpdateRouteHome(ctx context.Context, state *game.State) {
	currentSystem := state.System.ID
	if currentSystem == "" || currentSystem == r.lastKnownSystem {
		return
	}
	if state.InCombat || state.Traveling {
		return
	}

	r.lastKnownSystem = currentSystem

	// Resolve safe system
	safeSystem := state.Player.HomeBase
	if safeSystem == "" {
		safeSystem = EmpireCapitalSystem(state.Player.Empire)
	}
	if safeSystem == "" || safeSystem == currentSystem {
		return
	}

	r.logger.Printf("[%s] System changed to %s, finding route home to %s",
		r.agent.ID(), currentSystem, safeSystem)

	route, err := r.gameClient.FindRoute(ctx, safeSystem)
	if err != nil {
		r.logger.Printf("[%s] Failed to find route home: %v", r.agent.ID(), err)
		return
	}

	r.agent.SetRouteHome(route, currentSystem)
	r.logger.Printf("[%s] Cached route home: %d steps to %s", r.agent.ID(), len(route), safeSystem)
}

// isActionCommand returns true for commands that consume the 10-second action tick
// SOURCE: https://www.spacemolt.com/api (check regularly for updates!)
// IMPORTANT: Failed actions do NOT count against rate limit
func isActionCommand(action string) bool {
	// Query/info commands that do NOT consume ticks
	queryCommands := map[string]bool{
		// Player & Ship Info
		"get_status":        true,
		"get_notifications": true,
		"get_ship":          true,
		"get_skills": true,
		// World Info
		"get_system":    true,
		"get_poi":       true,
		"get_base":      true,
		"get_map":       true,
		"get_version":   true,
		"get_base_cost": true,
		// Market & Trading
		"get_listings":    true,
		"get_trades":      true,
		"get_wrecks":      true,
		"get_base_wrecks": true,
		"get_recipes":     true,
		"get_notes":       true,
		// Raid Info
		"raid_status": true,
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
	// refuel, repair, craft, recycle, chat, create_faction, join_faction, leave_faction,
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

// Pause stops the runner from starting new decision/ToT cycles.
// Any in-progress LLM call will complete, but no new cycles will begin.
func (r *Runner) Pause() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paused = true
	r.logger.Printf("[%s] Runner paused", r.agent.ID())
}

// Resume allows the runner to start decision cycles again.
func (r *Runner) Resume() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paused = false
	r.logger.Printf("[%s] Runner resumed", r.agent.ID())
}

// IsPaused returns whether the runner is currently paused.
func (r *Runner) IsPaused() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.paused
}

// SetEventCallback sets the callback function for streaming events
func (r *Runner) SetEventCallback(cb EventCallback) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventCallback = cb
}

// SetEnrichedState configures the enriched state provider for this runner.
// When set, each decision cycle will call Refresh() and pass the enriched
// state to Agent.Decide and ToTEvaluator. When nil, a raw fallback wrapping
// the cloned game state is used instead.
func (r *Runner) SetEnrichedState(es EnrichedState) {
	r.enrichedState = es
}

// GetEnrichedState returns the configured enriched state, or nil.
func (r *Runner) GetEnrichedState() EnrichedState {
	return r.enrichedState
}

// SetToTEvaluator enables Tree-of-Thought decision making for this runner.
func (r *Runner) SetToTEvaluator(eval ToTEvaluator) {
	r.totEvaluator = eval
}

// GetToTEvaluator returns the configured ToT evaluator, or nil.
func (r *Runner) GetToTEvaluator() ToTEvaluator {
	return r.totEvaluator
}

// emitEvent publishes an event if a callback is set
func (r *Runner) emitEvent(eventType string, data any) {
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
