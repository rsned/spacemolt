package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/llm"
	"github.com/rsned/spacemolt/pkg/mbox"
	"github.com/rsned/spacemolt/pkg/prompts"
)

// EmpireCapitalSystem returns the capital system ID for a given empire.
func EmpireCapitalSystem(empire string) string {
	switch strings.ToLower(empire) {
	case "solarian":
		return "sol"
	case "outerrim":
		return "frontier"
	case "voidborn":
		return "nexus_prime"
	case "nebula":
		return "haven"
	case "crimson":
		return "krynn"
	default:
		return ""
	}
}

// empireCapitalName returns the display name for a capital system ID.
func empireCapitalName(systemID string) string {
	switch systemID {
	case "sol":
		return "Sol"
	case "frontier":
		return "Frontier"
	case "nexus_prime":
		return "Nexus Prime"
	case "haven":
		return "Haven"
	case "krynn":
		return "Krynn"
	default:
		return systemID
	}
}

// LLMClient defines the interface for LLM operations
// This allows injecting mock clients for testing
type LLMClient interface {
	Decide(ctx context.Context, prompt string) (*llm.DecisionResponse, error)
	TestConnection(ctx context.Context) error
}

// BaseAgent provides default agent behavior
type BaseAgent struct {
	id          string
	name        string
	personality Personality
	memory      Memory
	llm         LLMClient

	status             Status
	lastActionFeedback string // Feedback from the most recent action
	lastActionResult   *ActionResult
	stopCh             chan struct{}
	stopOnce           sync.Once
	mu                 sync.RWMutex

	// Strategic planning
	currentGoal *Goal
	priority    Priority

	// Tactical action queue
	actionQueue       []PlannedAction
	usingQueuedAction bool

	// Cached route home
	routeHome       []game.RouteStep // Cached route to safe system
	routeHomeSystem string           // System this route was calculated from

	mboxStore *mbox.Store
}

// NewBaseAgent creates a new agent
func NewBaseAgent(
	id string,
	personality Personality,
	memory Memory,
	llmClient LLMClient,
) *BaseAgent {
	agent := &BaseAgent{
		id:          id,
		name:        personality.Name,
		personality: personality,
		memory:      memory,
		llm:         llmClient,
		status:      Status{State: AgentStateIdle},
		stopCh:      make(chan struct{}),
	}

	// Initialize default goal based on role
	agent.currentGoal = initializeGoalForRole(personality.Role)
	agent.priority = Priority{
		Focus:       getDefaultFocusForRole(personality.Role),
		Constraints: []string{},
		Urgency:     5, // Medium urgency by default
	}

	return agent
}

// Name returns the agent's name
func (a *BaseAgent) Name() string {
	return a.name
}

// ID returns the agent's ID
func (a *BaseAgent) ID() string {
	return a.id
}

// Personality returns the agent's personality
func (a *BaseAgent) Personality() Personality {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.personality
}

// Decide uses the LLM to choose the next action.
// The EnrichedState provides both the live game state (via GameState()) and
// optional KB enrichment. Currently the prompt is built from the raw game
// state; enriched accessors will be incorporated incrementally.
func (a *BaseAgent) Decide(ctx context.Context, es EnrichedState) (Decision, error) {
	a.mu.Lock()
	a.status.State = AgentStateDeciding
	a.status.CurrentAction = "thinking..."
	a.mu.Unlock()

	// Build prompt from personality, memory, and current state.
	// Use the underlying game state for backward-compatible prompt building.
	state := es.GameState()
	prompt := a.buildDecisionPrompt(state)

	// Get LLM decision
	response, err := a.llm.Decide(ctx, prompt)
	if err != nil {
		a.mu.Lock()
		a.status.State = AgentStateError
		a.status.Error = fmt.Errorf("LLM decision failed: %w", err)
		a.mu.Unlock()
		return Decision{}, err
	}

	decision := Decision{
		Action:     response.Action,
		Target:     response.Target,
		Reasoning:  response.Reasoning,
		Confidence: response.Confidence,
	}

	// Log the decision (protected by lock)
	a.mu.Lock()
	a.status.CurrentAction = fmt.Sprintf("Decided: %s (%.1f%% confidence)", response.Action, response.Confidence*100)
	a.mu.Unlock()

	return decision, nil
}

// buildDecisionPrompt creates a prompt for the LLM
func (a *BaseAgent) buildDecisionPrompt(state *game.State) string {
	// Try to use template system if LLM client supports it
	if client, ok := a.llm.(*llm.Client); ok && client.HasPromptManager() {
		// Build template context
		ctx := a.buildTemplateContext(state)

		// Render prompt using template
		prompt, err := client.RenderPrompt("decision", a.personality.Role, ctx)
		if err != nil {
			fmt.Printf("[Agent %s] Warning: Failed to render template, using fallback: %v\n", a.id, err)
			return a.buildFallbackPrompt(state)
		}

		return prompt
	}

	// Fallback to hardcoded prompt
	return a.buildFallbackPrompt(state)
}

// buildTemplateContext creates a template context from agent state
func (a *BaseAgent) buildTemplateContext(state *game.State) *prompts.TemplateContext {
	// Build knowledge context
	knowledge := a.buildKnowledgeContext(state)

	// Build history context
	history := a.buildHistoryContext()

	// Build last feedback context
	lastFeedback := a.buildFeedbackContext()

	// Build goal context
	goalCtx := a.buildGoalContext(state)

	// Build personality map
	personality := map[string]interface{}{
		"traits":      a.personality.Traits,
		"motivations": a.personality.Motivations,
		"skills":      a.personality.Skills,
	}

	ctx := prompts.NewTemplateContext(
		a.id,
		a.name,
		a.personality.Role,
		personality,
		state,
		knowledge,
		history,
		lastFeedback,
		goalCtx,
	)

	// Resolve safe system: prefer player's home base, fall back to empire capital
	if state != nil {
		safeSystemID := state.Player.HomeBase
		if safeSystemID == "" {
			safeSystemID = EmpireCapitalSystem(state.Player.Empire)
		}
		if safeSystemID != "" {
			ctx.State.SafeSystem = safeSystemID
			// Try to find the name from known systems first
			ctx.State.SafeSystemName = safeSystemID
			for _, sys := range knowledge.KnownSystems {
				if sys.ID == safeSystemID {
					ctx.State.SafeSystemName = sys.Name
					break
				}
			}
			// Fall back to capital name lookup if not found in knowledge
			if ctx.State.SafeSystemName == safeSystemID {
				if name := empireCapitalName(safeSystemID); name != safeSystemID {
					ctx.State.SafeSystemName = name
				}
			}
		}
	}

	// Populate cached route home
	route, _ := a.GetRouteHome()
	if len(route) > 0 {
		ctx.State.RouteHome = make([]prompts.RouteStepInfo, len(route))
		totalJumps := 0
		for i, step := range route {
			ctx.State.RouteHome[i] = prompts.RouteStepInfo{
				SystemID: step.SystemID,
				Name:     step.Name,
			}
			totalJumps += step.Jumps
		}
		ctx.State.RouteHomeJumps = totalJumps
	}

	return ctx
}

// buildKnowledgeContext builds knowledge context for templates
func (a *BaseAgent) buildKnowledgeContext(state *game.State) *prompts.KnowledgeContext {
	// Get a limited set of systems for the LLM context
	// Include: current system, connected systems, and recent visited systems
	// This prevents sending all 505+ systems which wastes tokens and context
	systems := a.memory.KnownSystems()

	// Build a set of relevant system IDs
	relevantSystemIDs := make(map[string]bool)
	maxRecentSystems := 10

	// Always include current system
	relevantSystemIDs[state.System.ID] = true

	// Include connected systems (neighbors)
	for _, conn := range state.System.Connections {
		relevantSystemIDs[conn.SystemID] = true
	}

	// Include recently visited systems (up to maxRecentSystems)
	experiences, _ := a.memory.GetRecentExperiences(maxRecentSystems)
	for _, exp := range experiences {
		// Extract system ID from location if available
		// Location format is typically "System-POI" or just system name
		if exp.Location != "" {
			// Try to match location to a known system ID
			for _, sys := range systems {
				if sys.Name == exp.Location || sys.ID == exp.Location {
					relevantSystemIDs[sys.ID] = true
					break
				}
			}
			if len(relevantSystemIDs) >= maxRecentSystems+10 { // +10 for current and connections
				break
			}
		}
	}

	// Filter to only relevant systems
	filteredSystems := make([]game.SystemData, 0, len(relevantSystemIDs))
	for _, sys := range systems {
		if relevantSystemIDs[sys.ID] {
			filteredSystems = append(filteredSystems, sys)
		}
	}

	systemInfos := make([]prompts.SystemInfo, len(filteredSystems))
	for i, sys := range filteredSystems {
		systemInfos[i] = prompts.SystemInfo{
			ID:          sys.ID,
			Name:        sys.Name,
			PoliceLevel: sys.PoliceLevel,
			Empire:      sys.Empire,
			VisitCount:  0, // VisitCount is knowledge base metadata, not in game.SystemData
		}
	}

	// Get POIs in current system
	poiInfos := make([]prompts.POIInfo, len(state.System.POIs))
	for i, poi := range state.System.POIs {
		poiInfos[i] = prompts.POIInfo{
			ID:       poi.ID,
			Name:     poi.Name,
			Type:     poi.Type,
			Position: fmt.Sprintf("(%.1f, %.1f)", poi.Position.X, poi.Position.Y),
		}
	}

	// Convert ConnectionInfo to just system IDs for prompts
	connectionIDs := make([]string, len(state.System.Connections))
	for i, conn := range state.System.Connections {
		connectionIDs[i] = conn.SystemID
	}

	return &prompts.KnowledgeContext{
		KnownSystems: systemInfos,
		POIsInSystem: poiInfos,
		Connections:  connectionIDs,
	}
}

// buildHistoryContext builds history context for templates
func (a *BaseAgent) buildHistoryContext() *prompts.HistoryContext {
	experiences, _ := a.memory.GetRecentExperiences(5)
	expRecords := make([]prompts.ExperienceRecord, len(experiences))
	for i, exp := range experiences {
		expRecords[i] = prompts.ExperienceRecord{
			Time:        exp.Time,
			Type:        exp.Type,
			Description: exp.Description,
			Outcome:     exp.Outcome,
			Location:    exp.Location,
		}
	}

	return &prompts.HistoryContext{
		RecentExperiences: expRecords,
	}
}

// buildFeedbackContext builds feedback context for templates
func (a *BaseAgent) buildFeedbackContext() *prompts.FeedbackContext {
	a.mu.RLock()
	result := a.lastActionResult
	a.mu.RUnlock()

	if result == nil {
		return nil
	}

	feedback := &prompts.FeedbackContext{
		Success: result.Success,
		Action:  result.Action,
		Target:  result.Target,
		Message: result.Message,
	}

	if result.Error != nil {
		feedback.Error = result.Error.Error()
		// Try to categorize error type
		errStr := result.Error.Error()
		if strings.Contains(errStr, "timeout") {
			feedback.ErrorType = "timeout"
		} else if strings.Contains(errStr, "not found") || strings.Contains(errStr, "invalid") {
			feedback.ErrorType = "validation"
		} else {
			feedback.ErrorType = "execution"
		}
	}

	return feedback
}

// buildGoalContext builds goal context for templates
func (a *BaseAgent) buildGoalContext(state *game.State) *prompts.GoalContext {
	a.mu.RLock()
	goal := a.currentGoal
	priority := a.priority
	a.mu.RUnlock()

	if goal == nil {
		// Return nil if no goal is set
		return nil
	}

	// Update constraints based on current state
	constraints := a.detectConstraints(state)

	return &prompts.GoalContext{
		Type:        goal.Type,
		Target:      goal.Target,
		Progress:    goal.Progress,
		Priority:    goal.Priority,
		Reasoning:   goal.Reasoning,
		Focus:       priority.Focus,
		Constraints: constraints,
		Urgency:     priority.Urgency,
	}
}

// detectConstraints analyzes game state to identify active constraints
func (a *BaseAgent) detectConstraints(state *game.State) []string {
	var constraints []string

	// Fuel constraints
	if state.Fuel < 10 {
		constraints = append(constraints, "critical_fuel")
	} else if state.Fuel < state.MaxFuel*0.2 {
		constraints = append(constraints, "low_fuel")
	}

	// Cargo constraints
	cargoPercent := float64(len(state.Ship.Cargo)) / float64(state.MaxCargo)
	if cargoPercent >= 1.0 {
		constraints = append(constraints, "cargo_full")
	} else if cargoPercent >= 0.9 {
		constraints = append(constraints, "cargo_nearly_full")
	}

	// Credits constraints
	if state.Credits < 100 {
		constraints = append(constraints, "no_credits")
	} else if state.Credits < 500 {
		constraints = append(constraints, "low_credits")
	}

	// Hull constraints
	hullPercent := state.Hull / state.MaxHull
	if hullPercent < 0.3 {
		constraints = append(constraints, "critical_hull")
	} else if hullPercent < 0.5 {
		constraints = append(constraints, "damaged_hull")
	}

	// Combat constraints
	if state.InCombat {
		constraints = append(constraints, "in_combat")
	}

	// Travel constraints
	if state.Traveling {
		constraints = append(constraints, "traveling")
	}

	return constraints
}

// buildFallbackPrompt creates the fallback hardcoded prompt
func (a *BaseAgent) buildFallbackPrompt(state *game.State) string {
	// Get a limited set of relevant systems for the prompt
	// Include: current system, connected systems, and recent visited systems
	systems := a.memory.KnownSystems()

	// Build a set of relevant system IDs
	relevantSystemIDs := make(map[string]bool)
	maxRecentSystems := 10

	// Always include current system
	relevantSystemIDs[state.System.ID] = true

	// Include connected systems (neighbors)
	for _, conn := range state.System.Connections {
		relevantSystemIDs[conn.SystemID] = true
	}

	// Include recently visited systems
	experiences, _ := a.memory.GetRecentExperiences(maxRecentSystems)
	for _, exp := range experiences {
		if exp.Location != "" {
			// Try to match location to a known system ID
			for _, sys := range systems {
				if sys.Name == exp.Location || sys.ID == exp.Location {
					relevantSystemIDs[sys.ID] = true
					break
				}
			}
			if len(relevantSystemIDs) >= maxRecentSystems+10 {
				break
			}
		}
	}

	// Filter to only relevant systems and build knowledge text
	filteredSystems := make([]game.SystemData, 0, len(relevantSystemIDs))
	for _, sys := range systems {
		if relevantSystemIDs[sys.ID] {
			filteredSystems = append(filteredSystems, sys)
		}
	}

	knowledgeText := fmt.Sprintf("Known systems: %d (showing relevant systems)\n", len(filteredSystems))
	for _, sys := range filteredSystems {
		knowledgeText += fmt.Sprintf("  - %s (%s)\n", sys.Name, sys.ID)
	}

	// Get recent experiences
	expText := "Recent experiences:\n"
	for _, exp := range experiences {
		expText += fmt.Sprintf("  - [%s] %s: %s\n", exp.Type, exp.Description, exp.Outcome)
	}

	// Build available POIs list from current system
	poisText := "\nAVAILABLE POIs IN CURRENT SYSTEM:\n"
	if len(state.System.POIs) > 0 {
		for _, poi := range state.System.POIs {
			poisText += fmt.Sprintf("  - %s (Type: %s, ID: %s)\n", poi.Name, poi.Type, poi.ID)
		}
	} else {
		poisText += "  (No POIs discovered yet - use get_system to scan)\n"
	}

	// Build available connections
	connectionsText := "\nAVAILABLE JUMP DESTINATIONS:\n"
	if len(state.System.Connections) > 0 {
		for _, conn := range state.System.Connections {
			connectionsText += fmt.Sprintf("  - %s\n", conn.Name)
		}
	} else {
		connectionsText += "  (No connections known yet)\n"
	}

	// Resolve safe system
	safeSystemID := state.Player.HomeBase
	if safeSystemID == "" {
		safeSystemID = EmpireCapitalSystem(state.Player.Empire)
	}
	safeSystemName := empireCapitalName(safeSystemID)
	if safeSystemName == safeSystemID {
		safeSystemName = safeSystemID // no pretty name available
	}

	// Build route home text
	routeHomeText := ""
	route, _ := a.GetRouteHome()
	if len(route) > 0 {
		routeHomeText = "\nROUTE HOME: "
		for i, step := range route {
			if i > 0 {
				routeHomeText += " → "
			}
			routeHomeText += step.Name
		}
		totalJumps := 0
		for _, step := range route {
			totalJumps += step.Jumps
		}
		routeHomeText += fmt.Sprintf(" (%d jumps)\n", totalJumps)
	}

	// Build state info
	stateInfo := map[string]interface{}{
		"location":    state.GetCurrentSystem(),
		"fuel":        fmt.Sprintf("%.0f/%.0f", state.Fuel, state.MaxFuel),
		"hull":        fmt.Sprintf("%.0f/%.0f", state.Hull, state.MaxHull),
		"cargo":       fmt.Sprintf("%d/%d", len(state.Cargo), state.MaxCargo),
		"credits":     state.Credits,
		"docked":      state.IsDocked(),
		"safe_system": fmt.Sprintf("%s (%s)", safeSystemName, safeSystemID),
	}

	// Get last action result (protected by lock)
	a.mu.RLock()
	lastResult := a.lastActionResult
	lastFeedback := a.lastActionFeedback
	a.mu.RUnlock()

	// Build feedback section
	feedbackText := ""
	if lastResult != nil {
		if lastResult.Success {
			feedbackText = fmt.Sprintf("\nLAST ACTION FEEDBACK:\n✓ %s → %s: %s\n", lastResult.Action, lastResult.Target, lastResult.Message)
		} else {
			feedbackText = fmt.Sprintf("\nLAST ACTION FEEDBACK:\n✗ %s → %s FAILED: %s\n", lastResult.Action, lastResult.Target, lastResult.Message)
			if lastResult.Error != nil {
				feedbackText += fmt.Sprintf("ERROR: %s\n", lastResult.Error.Error())
			}
			feedbackText += "IMPORTANT: Learn from this feedback! If the last action failed, you must address the error.\n"
		}
	}
	if lastFeedback != "" {
		feedbackText = "\nLAST ACTION FEEDBACK:\n" + lastFeedback + "\n"
		feedbackText += "IMPORTANT: Learn from this feedback! If the last action failed, you must address the error.\n"
	}

	return llm.BuildDecisionPrompt(
		a.name,
		a.personality.Role,
		map[string]interface{}{
			"traits":      a.personality.Traits,
			"motivations": a.personality.Motivations,
			"skills":      a.personality.Skills,
		},
		stateInfo,
	) + feedbackText + routeHomeText + "\n" + knowledgeText + "\n\n" + poisText + "\n" + connectionsText + "\n\n" + expText
}

// Learn updates the agent's memory based on action results
func (a *BaseAgent) Learn(result ActionResult) error {
	// DEBUG: Log what we're learning from
	fmt.Printf("[Agent %s] Learning from action result: %s → %s (success=%v)\n",
		a.id, result.Action, result.Target, result.Success)
	fmt.Printf("[Agent %s]   Message: %s\n", a.id, result.Message)
	if result.Error != nil {
		fmt.Printf("[Agent %s]   Error: %v\n", a.id, result.Error)
	}

	// Log the experience
	exp := Experience{
		Time:        fmt.Sprintf("%d", result.NewState.GetTick()),
		Type:        "action",
		Description: result.Message,
		Outcome:     fmt.Sprintf("Success: %v", result.Success),
		Location:    result.NewState.GetCurrentSystem(),
	}

	if err := a.memory.AddExperience(context.Background(), exp); err != nil {
		return err
	}

	// Persist any discoveries to the knowledge base
	if result.Success && result.NewState != nil {
		if err := a.persistDiscoveries(result); err != nil {
			fmt.Printf("[Agent %s] Warning: failed to persist discoveries: %v\n", a.id, err)
			// Don't fail the learn operation just because persistence failed
		}
	}

	// Update status and store feedback for next decision
	a.mu.Lock()
	defer a.mu.Unlock()

	// Store full action result for next LLM prompt
	a.lastActionResult = &result

	// Update agent status
	// Store action feedback for next LLM prompt
	if result.Success {
		a.lastActionFeedback = fmt.Sprintf("✓ Last action succeeded: %s", result.Message)
		a.status.State = AgentStateIdle
		a.status.CurrentAction = "Ready"
		a.status.Error = nil
	} else {
		// Include error details in feedback so LLM can learn from mistakes
		errorMsg := "unknown error"
		if result.Error != nil {
			errorMsg = result.Error.Error()
		}
		a.lastActionFeedback = fmt.Sprintf("✗ Last action FAILED: %s\nError: %s", result.Message, errorMsg)
		a.status.State = AgentStateError
		a.status.Error = fmt.Errorf("action failed: %w", result.Error)
	}

	return nil
}

// persistDiscoveries extracts and saves discoveries from game state to knowledge base
func (a *BaseAgent) persistDiscoveries(result ActionResult) error {
	ctx := context.Background()
	state := result.NewState

	if state == nil {
		return nil // Nothing to persist
	}

	// Set the current game tick on KBMemory so writes get stamped
	if kbMem, ok := a.memory.(*KBMemory); ok {
		kbMem.SetCurrentTick(state.GetTick())
	}

	// Always persist the current system if we have data about it
	if state.System.ID != "" && state.System.Name != "" {
		// The Memory interface now takes game.SystemData directly
		// Add DiscoveredBy field for knowledge base tracking
		sys := state.System
		sys.DiscoveredBy = a.id

		if err := a.memory.RememberSystem(ctx, sys); err != nil {
			return fmt.Errorf("failed to remember system %s: %w", sys.ID, err)
		}

		fmt.Printf("[Agent %s] Persisted system: %s (%s)\n", a.id, sys.Name, sys.ID)
	}

	// Persist all connections from the current system
	for _, connID := range state.System.Connections {
		if err := a.memory.RememberConnection(ctx, state.System.ID, connID.SystemID); err != nil {
			fmt.Printf("[Agent %s] Warning: failed to remember connection %s -> %s: %v\n",
				a.id, state.System.ID, connID.SystemID, err)
		}
	}

	// Persist all POIs discovered in the current system
	for _, poi := range state.System.POIs {
		if err := a.memory.RememberPOI(ctx, poi); err != nil {
			fmt.Printf("[Agent %s] Warning: failed to remember POI %s: %v\n",
				a.id, poi.Name, err)
		} else {
			// Only log if there's something notable about the POI
			if len(poi.Resources) > 0 || poi.Type == "station" || poi.Type == "base" {
				fmt.Printf("[Agent %s] Persisted POI: %s (%s) type=%s\n",
					a.id, poi.Name, poi.ID, poi.Type)
			}

			// Track resource history and detect anomalies if we have a KB memory
			if kbMem, ok := a.memory.(*KBMemory); ok {
				for _, res := range poi.Resources {
					// Record resource state for depletion tracking
					_ = kbMem.kb.RecordResourceState(ctx, poi.ID, res.ResourceID, res.Richness, res.Remaining, state.GetTick(), a.id)

					// Detect anomalies - exceptionally rich deposits
					if res.Richness >= 0.90 {
						anomaly := knowledge.Anomaly{
							Type:            "rich_deposit",
							Severity:        "opportunity",
							SystemID:        poi.SystemID,
							POIID:           poi.ID,
							Description:     fmt.Sprintf("Exceptionally rich %s deposit (richness: %.2f)", res.ResourceID, res.Richness),
							Details:         fmt.Sprintf(`{"resource":"%s","richness":%.2f,"remaining":%.0f}`, res.ResourceID, res.Richness, res.Remaining),
							DetectedBy:      a.id,
							LastUpdatedTick: state.GetTick(),
						}
						_ = kbMem.kb.RecordAnomaly(ctx, anomaly)
						fmt.Printf("[Agent %s] 💎 ANOMALY: Rich %s deposit at %s (richness: %.2f)\n",
							a.id, res.ResourceID, poi.Name, res.Richness)
					}

					// Detect anomalies - depleting resources
					if res.Remaining < 10000 && res.Remaining > 0 {
						anomaly := knowledge.Anomaly{
							Type:            "depleting_resource",
							Severity:        "warning",
							SystemID:        poi.SystemID,
							POIID:           poi.ID,
							Description:     fmt.Sprintf("Resource %s running low (remaining: %.0f)", res.ResourceID, res.Remaining),
							Details:         fmt.Sprintf(`{"resource":"%s","remaining":%.0f}`, res.ResourceID, res.Remaining),
							DetectedBy:      a.id,
							LastUpdatedTick: state.GetTick(),
						}
						_ = kbMem.kb.RecordAnomaly(ctx, anomaly)
					}
				}
			}
		}
	}

	return nil
}

// Memory returns the agent's memory
func (a *BaseAgent) Memory() Memory {
	return a.memory
}

// Start begins the agent's main loop
func (a *BaseAgent) Start(ctx context.Context) error {
	// Agent is now ready to receive decisions
	a.mu.Lock()
	a.status.State = AgentStateIdle
	a.status.CurrentAction = "Ready"
	a.mu.Unlock()

	// In a full implementation, this would start the decision/action loop
	// For now, the agent will be controlled externally
	return nil
}

// Stop halts the agent (safe to call multiple times)
func (a *BaseAgent) Stop() error {
	a.stopOnce.Do(func() {
		close(a.stopCh)
	})

	a.mu.Lock()
	a.status.State = AgentStateStopped
	a.status.CurrentAction = "Stopped"
	a.mu.Unlock()

	return nil
}

// Status returns the agent's current status
func (a *BaseAgent) Status() Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SetMbox attaches a mailbox store to the agent for inter-agent messaging.
func (a *BaseAgent) SetMbox(store *mbox.Store) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mboxStore = store
}

// RecentFromSender returns the n most recent messages from a specific sender.
func (a *BaseAgent) RecentFromSender(senderID string, n int) []mbox.Message {
	a.mu.RLock()
	s := a.mboxStore
	a.mu.RUnlock()
	if s == nil {
		return nil
	}
	msgs, _ := s.List(mbox.Query{SenderID: senderID, Limit: n})
	return msgs
}

// UnreadMatching returns unread messages matching any of the given keywords.
// Duplicates (a message matching multiple keywords) are deduplicated.
func (a *BaseAgent) UnreadMatching(keywords []string) []mbox.Message {
	a.mu.RLock()
	s := a.mboxStore
	a.mu.RUnlock()
	if s == nil {
		return nil
	}
	var results []mbox.Message
	seen := make(map[string]bool)
	for _, kw := range keywords {
		msgs, _ := s.Search(kw, mbox.Query{UnreadOnly: true, Limit: 50})
		for _, m := range msgs {
			if !seen[m.ID] {
				seen[m.ID] = true
				results = append(results, m)
			}
		}
	}
	return results
}

// MarkProcessed marks the given message IDs as read in the mailbox.
func (a *BaseAgent) MarkProcessed(ids ...string) {
	a.mu.RLock()
	s := a.mboxStore
	a.mu.RUnlock()
	if s == nil {
		return
	}
	_ = s.MarkRead(ids...)
}

// KBMemory implements the Memory interface using the knowledge base
type KBMemory struct {
	kb          knowledge.Base
	agentID     string
	currentTick int64
}

// NewKBMemory creates a new memory backed by the knowledge base
func NewKBMemory(kb knowledge.Base, agentID string) *KBMemory {
	return &KBMemory{
		kb:      kb,
		agentID: agentID,
	}
}

// SetCurrentTick sets the game tick to stamp on subsequent writes.
func (m *KBMemory) SetCurrentTick(tick int64) {
	m.currentTick = tick
}

// KnownSystems returns all known systems
func (m *KBMemory) KnownSystems() []game.SystemData {
	kbSystems, err := m.kb.GetSystems(context.Background())
	if err != nil {
		return []game.SystemData{}
	}

	result := make([]game.SystemData, len(kbSystems))
	for i, sys := range kbSystems {
		// Convert []SystemConnection to []ConnectionInfo
		conns := make([]game.ConnectionInfo, len(sys.Connections))
		for j, conn := range sys.Connections {
			conns[j] = game.ConnectionInfo{
				SystemID: conn.SystemID,
				Name:     conn.SystemID,
				Distance: conn.Distance,
			}
		}
		result[i] = game.SystemData{
			ID:           sys.ID,
			Name:         sys.Name,
			Empire:       sys.Empire,
			PoliceLevel:  sys.PoliceLevel,
			IsStronghold: sys.IsStronghold,
			POIs:         []game.POI{}, // Not stored in knowledge base
			Connections:  conns,
			Position:     sys.Position,
			Discovered:   true,
			DiscoveredBy: "", // No longer tracked in knowledge.System
		}
	}

	return result
}

// KnownPOIs returns POIs in a system
func (m *KBMemory) KnownPOIs(systemID string) []game.POI {
	// Query the knowledge base
	kbPOIs, err := m.kb.GetPOIs(context.Background(), systemID)
	if err != nil {
		// Log error but return empty slice
		return []game.POI{}
	}

	result := make([]game.POI, len(kbPOIs))
	for i, poi := range kbPOIs {
		// Convert resources from knowledge.POI uses game.POIResource
		resources := make([]game.POIResource, len(poi.Resources))
		for j, res := range poi.Resources {
			resources[j] = game.POIResource{
				ResourceID: res.ResourceID,
				Richness:   res.Richness,
				Remaining:  res.Remaining,
			}
		}

		result[i] = game.POI{
			ID:          poi.ID,
			SystemID:    poi.SystemID,
			Name:        poi.Name,
			Type:        poi.Type,
			Description: poi.Description,
			Position:    poi.Position,
			Resources:   resources,
		}
	}

	return result
}

// GetUnknownConnections finds unexplored connections
func (m *KBMemory) GetUnknownConnections(systemID string) ([]string, error) {
	return m.kb.GetUnknownConnections(context.Background(), systemID)
}

// RememberSystem stores a system in memory
func (m *KBMemory) RememberSystem(ctx context.Context, sys game.SystemData) error {
	// Convert []ConnectionInfo to []SystemConnection
	connections := make([]knowledge.SystemConnection, len(sys.Connections))
	for i, conn := range sys.Connections {
		connections[i] = knowledge.SystemConnection{
			SystemID: conn.SystemID,
			Distance: conn.Distance,
		}
	}
	kbSys := knowledge.System{
		ID:              sys.ID,
		Name:            sys.Name,
		Position:        sys.Position,
		PoliceLevel:     sys.PoliceLevel,
		SecurityStatus:  sys.SecurityStatus,
		Empire:          sys.Empire,
		IsStronghold:    sys.IsStronghold,
		Connections:     connections,
		LastUpdatedTick: m.currentTick,
		LastVisitedTick: m.currentTick,
	}

	return m.kb.RememberSystem(ctx, kbSys)
}

// RememberPOI stores a POI in memory
func (m *KBMemory) RememberPOI(ctx context.Context, poi game.POI) error {
	kbPOI := knowledge.POI{
		ID:               poi.ID,
		SystemID:         poi.SystemID,
		Name:             poi.Name,
		Type:             poi.Type,
		Class:            poi.Class,
		Description:      poi.Description,
		Position:         poi.Position,
		Resources:        poi.Resources, // game.POIResource is compatible
		Services:         []string{},    // Not stored in game POI
		Hidden:           poi.Hidden,
		RevealDifficulty: poi.RevealDifficulty,
		ExpiresAt:        poi.ExpiresAt,
		LastUpdatedTick:  m.currentTick,
	}

	return m.kb.RememberPOI(ctx, kbPOI)
}

// RememberConnection stores a connection in memory
func (m *KBMemory) RememberConnection(ctx context.Context, fromSystem, toSystem string) error {
	return m.kb.RememberConnection(ctx, fromSystem, toSystem)
}

// AddExperience adds an experience to memory
func (m *KBMemory) AddExperience(ctx context.Context, exp Experience) error {
	return m.kb.AddExperience(ctx, m.agentID, exp.Type, exp.Description, exp.Outcome, exp.Location)
}

// GetRecentExperiences retrieves recent experiences
func (m *KBMemory) GetRecentExperiences(count int) ([]Experience, error) {
	exps, err := m.kb.GetRecentExperiences(context.Background(), m.agentID, count)
	if err != nil {
		return nil, err
	}

	result := make([]Experience, len(exps))
	for i, exp := range exps {
		result[i] = Experience{
			Time:        exp.Time,
			Type:        exp.Type,
			Description: exp.Description,
			Outcome:     exp.Outcome,
			Location:    exp.Location,
		}
	}

	return result, nil
}

// initializeGoalForRole creates a default goal based on the agent's role
func initializeGoalForRole(role string) *Goal {
	roleLower := strings.ToLower(role)

	switch {
	case strings.Contains(roleLower, "miner"):
		return &Goal{
			Type:      "skill",
			Target:    "Mining_5",
			Progress:  0.0,
			Priority:  8,
			Reasoning: "Miners should develop mining skills to extract resources more efficiently",
		}
	case strings.Contains(roleLower, "trader"):
		return &Goal{
			Type:      "wealth",
			Target:    "10000_credits",
			Progress:  0.0,
			Priority:  9,
			Reasoning: "Traders should accumulate wealth through profitable trading",
		}
	case strings.Contains(roleLower, "explorer"):
		return &Goal{
			Type:      "exploration",
			Target:    "discover_5_systems",
			Progress:  0.0,
			Priority:  8,
			Reasoning: "Explorers should discover new systems and map the galaxy",
		}
	case strings.Contains(roleLower, "combat"):
		return &Goal{
			Type:      "skill",
			Target:    "Combat_5",
			Progress:  0.0,
			Priority:  9,
			Reasoning: "Combat pilots should develop combat skills for survival and victory",
		}
	case strings.Contains(roleLower, "craftsman"), strings.Contains(roleLower, "crafter"):
		return &Goal{
			Type:      "wealth",
			Target:    "10000_credits",
			Progress:  0.0,
			Priority:  8,
			Reasoning: "Craftsmen build wealth by crafting items from raw materials and selling for profit",
		}
	default:
		// Generic goal for undefined roles
		return &Goal{
			Type:      "wealth",
			Target:    "5000_credits",
			Progress:  0.0,
			Priority:  5,
			Reasoning: "Build wealth to upgrade ship and access better opportunities",
		}
	}
}

// getDefaultFocusForRole returns the default strategic focus for a role
func getDefaultFocusForRole(role string) string {
	roleLower := strings.ToLower(role)

	switch {
	case strings.Contains(roleLower, "miner"):
		return "mining"
	case strings.Contains(roleLower, "trader"):
		return "trading"
	case strings.Contains(roleLower, "explorer"):
		return "exploring"
	case strings.Contains(roleLower, "combat"):
		return "combat"
	case strings.Contains(roleLower, "craftsman"), strings.Contains(roleLower, "crafter"):
		return "crafting"
	default:
		return "general"
	}
}

// SetRouteHome caches the route to the agent's safe system.
func (a *BaseAgent) SetRouteHome(route []game.RouteStep, fromSystem string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.routeHome = route
	a.routeHomeSystem = fromSystem
}

// GetRouteHome returns the cached route home and the system it was calculated from.
func (a *BaseAgent) GetRouteHome() ([]game.RouteStep, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.routeHome, a.routeHomeSystem
}

// EnqueueActions adds a sequence of planned actions to the queue
func (a *BaseAgent) EnqueueActions(actions []PlannedAction) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actionQueue = append(a.actionQueue, actions...)
}

// DequeueAction removes and returns the next action from the queue
func (a *BaseAgent) DequeueAction() (*PlannedAction, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.actionQueue) == 0 {
		return nil, false
	}
	action := a.actionQueue[0]
	a.actionQueue = a.actionQueue[1:]
	return &action, true
}

// GetActionQueue returns a copy of the current action queue
func (a *BaseAgent) GetActionQueue() []PlannedAction {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]PlannedAction, len(a.actionQueue))
	copy(result, a.actionQueue)
	return result
}

// ClearActionQueue removes all actions from the queue
func (a *BaseAgent) ClearActionQueue(reason string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actionQueue = nil
	a.usingQueuedAction = false
}

// SetUsingQueuedAction sets whether the agent is currently using a queued action
func (a *BaseAgent) SetUsingQueuedAction(using bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.usingQueuedAction = using
}

// IsUsingQueuedAction returns whether the agent is currently using a queued action
func (a *BaseAgent) IsUsingQueuedAction() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.usingQueuedAction
}
