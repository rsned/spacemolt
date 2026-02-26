package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// mockMemory implements the Memory interface for testing.
type mockMemory struct {
	mu          sync.Mutex
	systems     []game.SystemData
	pois        map[string][]game.POI
	connections map[string][]string
	experiences []Experience
}

func newMockMemory() *mockMemory {
	return &mockMemory{
		pois:        make(map[string][]game.POI),
		connections: make(map[string][]string),
	}
}

func (m *mockMemory) KnownSystems() []game.SystemData {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]game.SystemData, len(m.systems))
	copy(result, m.systems)
	return result
}

func (m *mockMemory) KnownPOIs(systemID string) []game.POI {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pois[systemID]
}

func (m *mockMemory) GetUnknownConnections(systemID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connections[systemID], nil
}

func (m *mockMemory) RememberSystem(_ context.Context, sys game.SystemData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.systems = append(m.systems, sys)
	return nil
}

func (m *mockMemory) RememberPOI(_ context.Context, poi game.POI) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pois[poi.SystemID] = append(m.pois[poi.SystemID], poi)
	return nil
}

func (m *mockMemory) RememberConnection(_ context.Context, fromSystem, toSystem string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connections[fromSystem] = append(m.connections[fromSystem], toSystem)
	return nil
}

func (m *mockMemory) AddExperience(_ context.Context, exp Experience) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.experiences = append(m.experiences, exp)
	return nil
}

func (m *mockMemory) GetRecentExperiences(count int) ([]Experience, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if count > len(m.experiences) {
		count = len(m.experiences)
	}
	result := make([]Experience, count)
	// Return the most recent ones
	start := len(m.experiences) - count
	copy(result, m.experiences[start:])
	return result, nil
}

func newTestBaseAgent(role string) *BaseAgent {
	mem := newMockMemory()
	personality := Personality{
		Name: "TestAgent",
		ID:   "test-1",
		Role: role,
		Traits: map[string]float64{
			"boldness": 0.5,
		},
		Motivations: Motivations{
			Primary:   "wealth",
			Secondary: "exploration",
		},
	}
	return NewBaseAgent("test-1", personality, mem, nil)
}

func TestNewBaseAgent(t *testing.T) {
	mem := newMockMemory()
	personality := Personality{
		Name: "TestBot",
		ID:   "bot-1",
		Role: "miner",
	}

	agent := NewBaseAgent("bot-1", personality, mem, nil)

	if agent.ID() != "bot-1" {
		t.Errorf("ID() = %q, want %q", agent.ID(), "bot-1")
	}
	if agent.Name() != "TestBot" {
		t.Errorf("Name() = %q, want %q", agent.Name(), "TestBot")
	}
	if agent.Memory() != mem {
		t.Error("Memory() returned unexpected value")
	}

	status := agent.Status()
	if status.State != AgentStateIdle {
		t.Errorf("initial state = %v, want AgentStateIdle", status.State)
	}
}

func TestBaseAgent_Personality(t *testing.T) {
	agent := newTestBaseAgent("miner")

	p := agent.Personality()
	if p.Name != "TestAgent" {
		t.Errorf("Personality().Name = %q, want %q", p.Name, "TestAgent")
	}
	if p.Role != "miner" {
		t.Errorf("Personality().Role = %q, want %q", p.Role, "miner")
	}
}

func TestBaseAgent_StartAndStop(t *testing.T) {
	agent := newTestBaseAgent("miner")

	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	status := agent.Status()
	if status.State != AgentStateIdle {
		t.Errorf("after Start(), state = %v, want AgentStateIdle", status.State)
	}
	if status.CurrentAction != "Ready" {
		t.Errorf("after Start(), action = %q, want %q", status.CurrentAction, "Ready")
	}

	if err := agent.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	status = agent.Status()
	if status.State != AgentStateStopped {
		t.Errorf("after Stop(), state = %v, want AgentStateStopped", status.State)
	}
}

func TestBaseAgent_StopIdempotent(t *testing.T) {
	agent := newTestBaseAgent("miner")

	// Stop should be safe to call multiple times.
	for range 5 {
		if err := agent.Stop(); err != nil {
			t.Fatalf("Stop() error on repeated call: %v", err)
		}
	}
}

func TestBaseAgent_EnqueueDequeueActions(t *testing.T) {
	agent := newTestBaseAgent("miner")

	actions := []PlannedAction{
		{Sequence: 1, Action: "travel", Target: "poi-1"},
		{Sequence: 2, Action: "mine"},
		{Sequence: 3, Action: "sell", Target: "iron"},
	}

	agent.EnqueueActions(actions)

	queue := agent.GetActionQueue()
	if len(queue) != 3 {
		t.Fatalf("GetActionQueue() len = %d, want 3", len(queue))
	}

	// Dequeue first
	a, ok := agent.DequeueAction()
	if !ok {
		t.Fatal("DequeueAction() returned false")
	}
	if a.Action != "travel" {
		t.Errorf("first dequeued action = %q, want %q", a.Action, "travel")
	}

	// Dequeue second
	a, ok = agent.DequeueAction()
	if !ok {
		t.Fatal("DequeueAction() returned false")
	}
	if a.Action != "mine" {
		t.Errorf("second dequeued action = %q, want %q", a.Action, "mine")
	}

	// Queue should have 1 left
	queue = agent.GetActionQueue()
	if len(queue) != 1 {
		t.Fatalf("after two dequeues, len = %d, want 1", len(queue))
	}
}

func TestBaseAgent_DequeueEmpty(t *testing.T) {
	agent := newTestBaseAgent("miner")

	a, ok := agent.DequeueAction()
	if ok {
		t.Error("DequeueAction() on empty queue returned true")
	}
	if a != nil {
		t.Error("DequeueAction() on empty queue returned non-nil")
	}
}

func TestBaseAgent_ClearActionQueue(t *testing.T) {
	agent := newTestBaseAgent("miner")

	agent.EnqueueActions([]PlannedAction{
		{Sequence: 1, Action: "mine"},
		{Sequence: 2, Action: "sell"},
	})
	agent.SetUsingQueuedAction(true)

	agent.ClearActionQueue("test reset")

	queue := agent.GetActionQueue()
	if len(queue) != 0 {
		t.Errorf("after ClearActionQueue, len = %d, want 0", len(queue))
	}
	if agent.IsUsingQueuedAction() {
		t.Error("IsUsingQueuedAction() should be false after clear")
	}
}

func TestBaseAgent_UsingQueuedAction(t *testing.T) {
	agent := newTestBaseAgent("miner")

	if agent.IsUsingQueuedAction() {
		t.Error("initially IsUsingQueuedAction() should be false")
	}

	agent.SetUsingQueuedAction(true)
	if !agent.IsUsingQueuedAction() {
		t.Error("after SetUsingQueuedAction(true), should be true")
	}

	agent.SetUsingQueuedAction(false)
	if agent.IsUsingQueuedAction() {
		t.Error("after SetUsingQueuedAction(false), should be false")
	}
}

func TestBaseAgent_RouteHome(t *testing.T) {
	agent := newTestBaseAgent("explorer")

	route, system := agent.GetRouteHome()
	if route != nil {
		t.Error("initial route should be nil")
	}
	if system != "" {
		t.Errorf("initial system = %q, want empty", system)
	}

	steps := []game.RouteStep{
		{SystemID: "sys-1", Name: "Alpha", Jumps: 1},
		{SystemID: "sys-2", Name: "Beta", Jumps: 2},
	}
	agent.SetRouteHome(steps, "sys-current")

	route, system = agent.GetRouteHome()
	if len(route) != 2 {
		t.Fatalf("route len = %d, want 2", len(route))
	}
	if system != "sys-current" {
		t.Errorf("system = %q, want %q", system, "sys-current")
	}
	if route[0].Name != "Alpha" {
		t.Errorf("route[0].Name = %q, want %q", route[0].Name, "Alpha")
	}
}

func TestBaseAgent_ConcurrentQueueAccess(t *testing.T) {
	agent := newTestBaseAgent("miner")

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			agent.EnqueueActions([]PlannedAction{{Action: "mine"}})
		}()
	}
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			agent.DequeueAction()
		}()
	}
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			agent.GetActionQueue()
		}()
	}
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			agent.ClearActionQueue("concurrent test")
		}()
	}
	wg.Wait()
}

func TestBaseAgent_ConcurrentRouteHome(t *testing.T) {
	agent := newTestBaseAgent("explorer")

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			agent.SetRouteHome([]game.RouteStep{{SystemID: "s1"}}, "from-s")
		}()
		go func() {
			defer wg.Done()
			agent.GetRouteHome()
		}()
	}
	wg.Wait()
}

func TestBaseAgent_ConcurrentStatusAccess(t *testing.T) {
	agent := newTestBaseAgent("miner")

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			agent.Status()
		}()
		go func() {
			defer wg.Done()
			agent.Personality()
		}()
	}
	wg.Wait()
}

func TestBaseAgent_DetectConstraints(t *testing.T) {
	agent := newTestBaseAgent("miner")

	tests := []struct {
		name     string
		state    *game.State
		contains []string
		absent   []string
	}{
		{
			name: "critical_fuel",
			state: &game.State{
				Fuel:    5,
				MaxFuel: 100,
				Hull:    100,
				MaxHull: 100,
				Ship:    game.Ship{Cargo: []game.CargoItem{}},
			},
			contains: []string{"critical_fuel"},
			absent:   []string{"low_fuel"},
		},
		{
			name: "low_fuel",
			state: &game.State{
				Fuel:    15,
				MaxFuel: 100,
				Hull:    100,
				MaxHull: 100,
				Ship:    game.Ship{Cargo: []game.CargoItem{}},
			},
			contains: []string{"low_fuel"},
			absent:   []string{"critical_fuel"},
		},
		{
			name: "cargo_full",
			state: &game.State{
				Fuel:     100,
				MaxFuel:  100,
				Hull:     100,
				MaxHull:  100,
				MaxCargo: 2,
				Ship: game.Ship{
					Cargo: []game.CargoItem{{ItemID: "a"}, {ItemID: "b"}},
				},
			},
			contains: []string{"cargo_full"},
		},
		{
			name: "no_credits",
			state: &game.State{
				Fuel:    100,
				MaxFuel: 100,
				Hull:    100,
				MaxHull: 100,
				Credits: 50,
				Ship:    game.Ship{Cargo: []game.CargoItem{}},
			},
			contains: []string{"no_credits"},
		},
		{
			name: "critical_hull",
			state: &game.State{
				Fuel:    100,
				MaxFuel: 100,
				Hull:    20,
				MaxHull: 100,
				Credits: 10000,
				Ship:    game.Ship{Cargo: []game.CargoItem{}},
			},
			contains: []string{"critical_hull"},
		},
		{
			name: "in_combat",
			state: &game.State{
				Fuel:     100,
				MaxFuel:  100,
				Hull:     100,
				MaxHull:  100,
				Credits:  10000,
				InCombat: true,
				Ship:     game.Ship{Cargo: []game.CargoItem{}},
			},
			contains: []string{"in_combat"},
		},
		{
			name: "traveling",
			state: &game.State{
				Fuel:      100,
				MaxFuel:   100,
				Hull:      100,
				MaxHull:   100,
				Credits:   10000,
				Traveling: true,
				Ship:      game.Ship{Cargo: []game.CargoItem{}},
			},
			contains: []string{"traveling"},
		},
		{
			name: "healthy_state_no_constraints",
			state: &game.State{
				Fuel:     100,
				MaxFuel:  100,
				Hull:     100,
				MaxHull:  100,
				Credits:  10000,
				MaxCargo: 100,
				Ship:     game.Ship{Cargo: []game.CargoItem{}},
			},
			contains: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constraints := agent.detectConstraints(tt.state)
			cSet := make(map[string]bool)
			for _, c := range constraints {
				cSet[c] = true
			}

			for _, want := range tt.contains {
				if !cSet[want] {
					t.Errorf("expected constraint %q not found in %v", want, constraints)
				}
			}
			for _, absent := range tt.absent {
				if cSet[absent] {
					t.Errorf("unexpected constraint %q found in %v", absent, constraints)
				}
			}
		})
	}
}

func TestBaseAgent_Learn_Success(t *testing.T) {
	mem := newMockMemory()
	personality := Personality{Name: "Learner", ID: "learn-1", Role: "miner"}
	agent := NewBaseAgent("learn-1", personality, mem, nil)

	result := ActionResult{
		Action:  "mine",
		Target:  "iron",
		Success: true,
		Message: "Mined 10 iron",
		NewState: &game.State{
			CurrentTick:   200,
			CurrentSystem: "sys-1",
			System:        game.SystemData{ID: "sys-1", Name: "Alpha"},
		},
	}

	if err := agent.Learn(result); err != nil {
		t.Fatalf("Learn() error: %v", err)
	}

	status := agent.Status()
	if status.State != AgentStateIdle {
		t.Errorf("after successful learn, state = %v, want AgentStateIdle", status.State)
	}

	exps, err := mem.GetRecentExperiences(10)
	if err != nil {
		t.Fatalf("GetRecentExperiences error: %v", err)
	}
	if len(exps) != 1 {
		t.Fatalf("expected 1 experience, got %d", len(exps))
	}
}

func TestBaseAgent_Learn_Failure(t *testing.T) {
	mem := newMockMemory()
	personality := Personality{Name: "Learner", ID: "learn-1", Role: "miner"}
	agent := NewBaseAgent("learn-1", personality, mem, nil)

	result := ActionResult{
		Action:  "mine",
		Success: false,
		Message: "No resources",
		Error:   errors.New("resource depleted"),
		NewState: &game.State{
			CurrentTick:   200,
			CurrentSystem: "sys-1",
			System:        game.SystemData{ID: "sys-1", Name: "Alpha"},
		},
	}

	if err := agent.Learn(result); err != nil {
		t.Fatalf("Learn() error: %v", err)
	}

	status := agent.Status()
	if status.State != AgentStateError {
		t.Errorf("after failed learn, state = %v, want AgentStateError", status.State)
	}
	if status.Error == nil {
		t.Error("expected error in status after failed learn")
	}
}

func TestBaseAgent_BuildFeedbackContext_NilResult(t *testing.T) {
	agent := newTestBaseAgent("miner")

	ctx := agent.buildFeedbackContext()
	if ctx != nil {
		t.Error("expected nil feedback context when no last result")
	}
}

func TestBaseAgent_BuildFeedbackContext_WithResult(t *testing.T) {
	agent := newTestBaseAgent("miner")

	agent.mu.Lock()
	agent.lastActionResult = &ActionResult{
		Action:  "mine",
		Target:  "iron",
		Success: true,
		Message: "Mined successfully",
	}
	agent.mu.Unlock()

	ctx := agent.buildFeedbackContext()
	if ctx == nil {
		t.Fatal("expected non-nil feedback context")
	}
	if !ctx.Success {
		t.Error("expected Success = true")
	}
	if ctx.Action != "mine" {
		t.Errorf("Action = %q, want %q", ctx.Action, "mine")
	}
}

func TestBaseAgent_BuildFeedbackContext_WithError(t *testing.T) {
	agent := newTestBaseAgent("miner")

	agent.mu.Lock()
	agent.lastActionResult = &ActionResult{
		Action:  "mine",
		Success: false,
		Error:   errors.New("timeout waiting for response"),
	}
	agent.mu.Unlock()

	ctx := agent.buildFeedbackContext()
	if ctx == nil {
		t.Fatal("expected non-nil feedback context")
	}
	if ctx.ErrorType != "timeout" {
		t.Errorf("ErrorType = %q, want %q", ctx.ErrorType, "timeout")
	}
}

func TestBaseAgent_BuildFeedbackContext_ValidationError(t *testing.T) {
	agent := newTestBaseAgent("miner")

	agent.mu.Lock()
	agent.lastActionResult = &ActionResult{
		Action:  "travel",
		Success: false,
		Error:   errors.New("target not found"),
	}
	agent.mu.Unlock()

	ctx := agent.buildFeedbackContext()
	if ctx == nil {
		t.Fatal("expected non-nil feedback context")
	}
	if ctx.ErrorType != "validation" {
		t.Errorf("ErrorType = %q, want %q", ctx.ErrorType, "validation")
	}
}

func TestBaseAgent_BuildGoalContext_NilGoal(t *testing.T) {
	agent := newTestBaseAgent("miner")

	agent.mu.Lock()
	agent.currentGoal = nil
	agent.mu.Unlock()

	state := &game.State{
		Fuel:    100,
		MaxFuel: 100,
		Hull:    100,
		MaxHull: 100,
		Ship:    game.Ship{Cargo: []game.CargoItem{}},
	}

	ctx := agent.buildGoalContext(state)
	if ctx != nil {
		t.Error("expected nil goal context when no goal set")
	}
}

func TestEmpireCapitalSystem(t *testing.T) {
	tests := []struct {
		empire   string
		expected string
	}{
		{"solarian", "sol"},
		{"Solarian", "sol"},
		{"SOLARIAN", "sol"},
		{"outerrim", "frontier"},
		{"voidborn", "nexus"},
		{"nebula", "haven"},
		{"crimson", "krynn"},
		{"unknown", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.empire, func(t *testing.T) {
			got := EmpireCapitalSystem(tt.empire)
			if got != tt.expected {
				t.Errorf("EmpireCapitalSystem(%q) = %q, want %q", tt.empire, got, tt.expected)
			}
		})
	}
}

func TestEmpireCapitalName(t *testing.T) {
	tests := []struct {
		systemID string
		expected string
	}{
		{"sol", "Sol"},
		{"frontier", "Frontier"},
		{"nexus", "Nexus Prime"},
		{"haven", "Haven"},
		{"krynn", "Krynn"},
		{"unknown", "unknown"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.systemID, func(t *testing.T) {
			got := empireCapitalName(tt.systemID)
			if got != tt.expected {
				t.Errorf("empireCapitalName(%q) = %q, want %q", tt.systemID, got, tt.expected)
			}
		})
	}
}

func TestInitializeGoalForRole(t *testing.T) {
	tests := []struct {
		role         string
		expectedType string
	}{
		{"miner", "skill"},
		{"Advanced Miner", "skill"},
		{"trader", "wealth"},
		{"Space Trader", "wealth"},
		{"explorer", "exploration"},
		{"Deep Explorer", "exploration"},
		{"combat pilot", "skill"},
		{"Combat Specialist", "skill"},
		{"craftsman", "wealth"},
		{"Master Crafter", "wealth"},
		{"unknown", "wealth"},
		{"", "wealth"},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			goal := initializeGoalForRole(tt.role)
			if goal == nil {
				t.Fatal("initializeGoalForRole returned nil")
			}
			if goal.Type != tt.expectedType {
				t.Errorf("goal.Type = %q, want %q for role %q", goal.Type, tt.expectedType, tt.role)
			}
		})
	}
}

func TestGetDefaultFocusForRole(t *testing.T) {
	tests := []struct {
		role     string
		expected string
	}{
		{"miner", "mining"},
		{"trader", "trading"},
		{"explorer", "exploring"},
		{"combat", "combat"},
		{"craftsman", "crafting"},
		{"crafter", "crafting"},
		{"random", "general"},
		{"", "general"},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			got := getDefaultFocusForRole(tt.role)
			if got != tt.expected {
				t.Errorf("getDefaultFocusForRole(%q) = %q, want %q", tt.role, got, tt.expected)
			}
		})
	}
}

func TestAgentState_String(t *testing.T) {
	tests := []struct {
		state    AgentState
		expected string
	}{
		{AgentStateIdle, "Idle"},
		{AgentStateDeciding, "Deciding"},
		{AgentStateActing, "Acting"},
		{AgentStateWaiting, "Waiting"},
		{AgentStateError, "Error"},
		{AgentStateStopped, "Stopped"},
		{AgentState(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := tt.state.String()
			if got != tt.expected {
				t.Errorf("AgentState(%d).String() = %q, want %q", tt.state, got, tt.expected)
			}
		})
	}
}

func TestBaseAgent_PersistDiscoveries_NilState(t *testing.T) {
	agent := newTestBaseAgent("miner")

	result := ActionResult{
		Success:  true,
		NewState: nil,
	}

	err := agent.persistDiscoveries(result)
	if err != nil {
		t.Errorf("persistDiscoveries with nil state should return nil, got %v", err)
	}
}

func TestBaseAgent_PersistDiscoveries_WithSystemAndPOIs(t *testing.T) {
	mem := newMockMemory()
	personality := Personality{Name: "Explorer", ID: "exp-1", Role: "explorer"}
	agent := NewBaseAgent("exp-1", personality, mem, nil)

	result := ActionResult{
		Success: true,
		NewState: &game.State{
			CurrentTick:   100,
			CurrentSystem: "sys-1",
			System: game.SystemData{
				ID:   "sys-1",
				Name: "Alpha System",
				POIs: []game.POI{
					{ID: "poi-1", SystemID: "sys-1", Name: "Station Alpha", Type: "station"},
				},
				Connections: []game.ConnectionInfo{
					{SystemID: "sys-2", Name: "Beta", Distance: 5},
				},
			},
		},
	}

	if err := agent.persistDiscoveries(result); err != nil {
		t.Fatalf("persistDiscoveries error: %v", err)
	}

	// Check system was remembered
	systems := mem.KnownSystems()
	if len(systems) != 1 {
		t.Fatalf("expected 1 system, got %d", len(systems))
	}
	if systems[0].ID != "sys-1" {
		t.Errorf("system ID = %q, want %q", systems[0].ID, "sys-1")
	}
	if systems[0].DiscoveredBy != "exp-1" {
		t.Errorf("DiscoveredBy = %q, want %q", systems[0].DiscoveredBy, "exp-1")
	}

	// Check POI was remembered
	pois := mem.KnownPOIs("sys-1")
	if len(pois) != 1 {
		t.Fatalf("expected 1 POI, got %d", len(pois))
	}

	// Check connection was remembered
	conns := mem.connections["sys-1"]
	if len(conns) != 1 || conns[0] != "sys-2" {
		t.Errorf("connections = %v, want [sys-2]", conns)
	}
}

func TestBaseAgent_ConcurrentLearn(t *testing.T) {
	mem := newMockMemory()
	personality := Personality{Name: "ConcLearn", ID: "cl-1", Role: "miner"}
	agent := NewBaseAgent("cl-1", personality, mem, nil)

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result := ActionResult{
				Action:  "mine",
				Success: idx%2 == 0,
				Message: "test",
				NewState: &game.State{
					CurrentTick:   int64(idx),
					CurrentSystem: "sys-1",
					System: game.SystemData{
						ID:   "sys-1",
						Name: "Alpha",
					},
				},
			}
			_ = agent.Learn(result)
		}(i)
	}
	wg.Wait()

	// Should not panic, and experiences should be recorded
	exps, err := mem.GetRecentExperiences(100)
	if err != nil {
		t.Fatalf("GetRecentExperiences error: %v", err)
	}
	if len(exps) != 10 {
		t.Errorf("expected 10 experiences, got %d", len(exps))
	}
}

func TestBaseAgent_EnqueueAppend(t *testing.T) {
	agent := newTestBaseAgent("trader")

	agent.EnqueueActions([]PlannedAction{
		{Sequence: 1, Action: "dock"},
	})
	agent.EnqueueActions([]PlannedAction{
		{Sequence: 2, Action: "sell"},
	})

	queue := agent.GetActionQueue()
	if len(queue) != 2 {
		t.Fatalf("queue len = %d, want 2", len(queue))
	}
	if queue[0].Action != "dock" || queue[1].Action != "sell" {
		t.Errorf("queue = %v, want dock then sell", queue)
	}
}

func TestBaseAgent_GetActionQueueReturnsACopy(t *testing.T) {
	agent := newTestBaseAgent("trader")

	agent.EnqueueActions([]PlannedAction{
		{Sequence: 1, Action: "mine"},
	})

	// Get a copy and modify it
	queue := agent.GetActionQueue()
	queue[0].Action = "modified"

	// Original should be unchanged
	original := agent.GetActionQueue()
	if original[0].Action != "mine" {
		t.Errorf("GetActionQueue should return a copy; got Action = %q", original[0].Action)
	}
}

func TestBaseAgent_BuildHistoryContext(t *testing.T) {
	mem := newMockMemory()
	_ = mem.AddExperience(context.Background(), Experience{
		Time:        "100",
		Type:        "action",
		Description: "Mined iron",
		Outcome:     "Success: true",
		Location:    "sys-1",
	})

	personality := Personality{Name: "Test", ID: "t-1", Role: "miner"}
	agent := NewBaseAgent("t-1", personality, mem, nil)

	histCtx := agent.buildHistoryContext()
	if len(histCtx.RecentExperiences) != 1 {
		t.Fatalf("expected 1 experience, got %d", len(histCtx.RecentExperiences))
	}
	if histCtx.RecentExperiences[0].Description != "Mined iron" {
		t.Errorf("description = %q, want %q", histCtx.RecentExperiences[0].Description, "Mined iron")
	}
}

// Verify that the agent implements the Agent interface.
func TestBaseAgent_ImplementsInterface(t *testing.T) {
	var _ Agent = (*BaseAgent)(nil)

	// Just verify it compiles - the above line is the real check
	_ = time.Now()
}
