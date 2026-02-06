package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/llm"
)

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

	status   Status
	stopCh   chan struct{}
	stopOnce sync.Once
	mu       sync.RWMutex
}

// NewBaseAgent creates a new agent
func NewBaseAgent(
	id string,
	personality Personality,
	memory Memory,
	llmClient LLMClient,
) *BaseAgent {
	return &BaseAgent{
		id:          id,
		name:        personality.Name,
		personality: personality,
		memory:      memory,
		llm:         llmClient,
		status:      Status{State: AgentStateIdle},
		stopCh:      make(chan struct{}),
	}
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

// Decide uses the LLM to choose the next action
func (a *BaseAgent) Decide(ctx context.Context, state *game.State) (Decision, error) {
	a.mu.Lock()
	a.status.State = AgentStateDeciding
	a.status.CurrentAction = "thinking..."
	a.mu.Unlock()

	// Build prompt from personality, memory, and current state
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
		Action:      response.Action,
		Target:      response.Target,
		Reasoning:   response.Reasoning,
		Confidence:  response.Confidence,
	}

	// Log the decision (protected by lock)
	a.mu.Lock()
	a.status.CurrentAction = fmt.Sprintf("Decided: %s (%.1f%% confidence)", response.Action, response.Confidence*100)
	a.mu.Unlock()

	return decision, nil
}

// buildDecisionPrompt creates a prompt for the LLM
func (a *BaseAgent) buildDecisionPrompt(state *game.State) string {
	// Get current knowledge
	systems := a.memory.KnownSystems()
	knowledgeText := fmt.Sprintf("Known systems: %d\n", len(systems))
	for _, sys := range systems {
		knowledgeText += fmt.Sprintf("  - %s (%s)\n", sys.Name, sys.ID)
	}

	// Get recent experiences
	experiences, _ := a.memory.GetRecentExperiences(5)
	expText := "Recent experiences:\n"
	for _, exp := range experiences {
		expText += fmt.Sprintf("  - [%s] %s: %s\n", exp.Type, exp.Description, exp.Outcome)
	}

	// Build state info
	stateInfo := map[string]interface{}{
		"location": state.GetCurrentSystem(),
		"fuel":     fmt.Sprintf("%.0f/%.0f", state.Fuel, state.MaxFuel),
		"hull":     fmt.Sprintf("%.0f/%.0f", state.Hull, state.MaxHull),
		"cargo":    fmt.Sprintf("%d/%d", len(state.Cargo), state.MaxCargo),
		"credits":  state.Credits,
		"docked":   state.IsDocked(),
	}

	return llm.BuildDecisionPrompt(
		a.name,
		a.personality.Role,
		map[string]interface{}{
			"traits":     a.personality.Traits,
			"motivations": a.personality.Motivations,
			"skills":     a.personality.Skills,
		},
		stateInfo,
	) + "\n\n" + knowledgeText + "\n\n" + expText
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

	// Update status
	a.mu.Lock()
	defer a.mu.Unlock()

	if result.Success {
		a.status.State = AgentStateIdle
		a.status.CurrentAction = "Ready"
		a.status.Error = nil
	} else {
		a.status.State = AgentStateError
		a.status.Error = fmt.Errorf("action failed: %w", result.Error)
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

// KBMemory implements the Memory interface using the knowledge base
type KBMemory struct {
	kb      knowledge.Base
	agentID string
}

// NewKBMemory creates a new memory backed by the knowledge base
func NewKBMemory(kb knowledge.Base, agentID string) *KBMemory {
	return &KBMemory{
		kb:      kb,
		agentID: agentID,
	}
}

// KnownSystems returns all known systems
func (m *KBMemory) KnownSystems() []SystemKnowledge {
	systems := m.kb.GetSystems()

	result := make([]SystemKnowledge, len(systems))
	for i, sys := range systems {
		result[i] = SystemKnowledge{
			ID:            sys.ID,
			Name:          sys.Name,
			Position:      Position{X: sys.Position.X, Y: sys.Position.Y, Z: sys.Position.Z},
			SecurityLevel: sys.SecurityLevel,
			Faction:       sys.Faction,
			Connections:   sys.Connections,
			VisitCount:     sys.VisitCount,
			DiscoveredBy:  sys.DiscoveredBy,
		}
	}

	return result
}

// KnownPOIs returns POIs in a system
func (m *KBMemory) KnownPOIs(systemID string) []POIKnowledge {
	// Query the knowledge base
	return []POIKnowledge{}
}

// GetUnknownConnections finds unexplored connections
func (m *KBMemory) GetUnknownConnections(systemID string) ([]string, error) {
	return m.kb.GetUnknownConnections(context.Background(), systemID)
}

// RememberSystem stores a system in memory
func (m *KBMemory) RememberSystem(ctx context.Context, sys System) error {
	kbSys := knowledge.System{
		ID:            sys.ID,
		Name:          sys.Name,
		Position:      knowledge.Position{X: sys.Position.X, Y: sys.Position.Y, Z: sys.Position.Z},
		SecurityLevel: sys.SecurityLevel,
		Faction:       sys.Faction,
		Connections:   sys.Connections,
		DiscoveredBy:  sys.DiscoveredBy,
	}

	return m.kb.RememberSystem(ctx, kbSys)
}

// RememberPOI stores a POI in memory
func (m *KBMemory) RememberPOI(ctx context.Context, poi POI) error {
	kbPOI := knowledge.POI{
		ID:       poi.ID,
		SystemID: poi.SystemID,
		Name:     poi.Name,
		Type:     poi.Type,
		Position: knowledge.Position{X: poi.Position.X, Y: poi.Position.Y},
		DiscoveredBy: poi.DiscoveredBy,
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
