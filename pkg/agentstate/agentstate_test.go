package agentstate

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/agent"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Verify *AgentState satisfies agent.EnrichedState at compile time.
var _ agent.EnrichedState = (*AgentState)(nil)

// testState returns a minimal game.State for testing.
func testState() *game.State {
	return &game.State{
		Doc:           true,
		CurrentSystem: "alpha-1",
		CurrentPOI:    "station-1",
		Credits:       5000,
		Ship: game.Ship{
			Fuel:          80,
			MaxFuel:       100,
			Hull:          90,
			MaxHull:       100,
			CargoUsed:     20,
			CargoCapacity: 50,
			ClassID:       "scout",
			Name:          "Wanderer",
			Cargo: []game.CargoItem{
				{ItemID: "iron_ore", Quantity: 10},
				{ItemID: "copper_ore", Quantity: 10},
			},
		},
		Player: game.Player{
			Username: "testplayer",
			Empire:   "federation",
			Skills: map[string]game.Skill{
				"mining": {Level: 3, XP: 150},
			},
		},
		System: game.SystemData{
			ID:          "alpha-1",
			Name:        "Alpha Centauri",
			PoliceLevel: 2,
			POIs: []game.POI{
				{ID: "station-1", Name: "Hub Station", Type: "station"},
				{ID: "belt-1", Name: "Main Belt", Type: "asteroid_belt"},
			},
			Connections: []game.ConnectionInfo{
				{SystemID: "beta-1", Name: "Beta Crucis", Distance: 3},
				{SystemID: "gamma-1", Name: "Gamma Draconis", Distance: 5},
			},
		},
		InCombat:    false,
		InBattle:    false,
		CurrentTick: 1000,
	}
}

func TestNew(t *testing.T) {
	state := testState()
	kb := knowledge.NewMemoryKB()

	as := New(state, kb, nil)
	if as == nil {
		t.Fatal("New returned nil")
	}
	if as.agent != nil {
		t.Error("New should not set agent context")
	}
}

func TestNewWithAgent(t *testing.T) {
	state := testState()
	kb := knowledge.NewMemoryKB()
	agentCtx := &AgentContext{
		Personality: &agent.Personality{Name: "TestBot", Role: "miner"},
		Goal:        &agent.Goal{Type: "wealth", Target: "10000_credits", Progress: 0.5},
	}

	as := NewWithAgent(state, kb, nil, agentCtx)
	if as.agent == nil {
		t.Fatal("NewWithAgent should set agent context")
	}
	if as.Goal().Target != "10000_credits" {
		t.Errorf("Goal target: got %q, want %q", as.Goal().Target, "10000_credits")
	}
}

func TestRefresh(t *testing.T) {
	state := testState()
	kb := knowledge.NewMemoryKB()

	// Seed KB with a neighbor system.
	_ = kb.RememberSystem(context.Background(), knowledge.System{
		ID:           "beta-1",
		Name:         "Beta Crucis",
		PoliceLevel:  3,
		Empire:       "empire",
		IsStronghold: false,
	})
	_ = kb.RememberPOI(context.Background(), knowledge.POI{
		ID:       "beta-station",
		SystemID: "beta-1",
		Name:     "Beta Hub",
		Type:     "station",
	})
	_ = kb.RememberPOI(context.Background(), knowledge.POI{
		ID:       "beta-belt",
		SystemID: "beta-1",
		Name:     "Beta Belt",
		Type:     "asteroid_belt",
	})

	as := New(state, kb, nil)
	as.Refresh(context.Background())

	// Check system security.
	if got := as.SystemSecurity(); got != "Medium Security" {
		t.Errorf("SystemSecurity: got %q, want %q", got, "Medium Security")
	}

	// Check current POI type resolved.
	if got := as.CurrentPOIType(); got != "station" {
		t.Errorf("CurrentPOIType: got %q, want %q", got, "station")
	}

	// Check neighbors enriched.
	neighbors := as.Neighbors()
	if len(neighbors) != 2 {
		t.Fatalf("Neighbors: got %d, want 2", len(neighbors))
	}

	beta := as.NeighborInfo("beta-1")
	if beta == nil {
		t.Fatal("NeighborInfo(beta-1) returned nil")
	}
	if beta.Security != "High Security" {
		t.Errorf("beta-1 security: got %q, want %q", beta.Security, "High Security")
	}
	if beta.StationCount != 1 {
		t.Errorf("beta-1 stations: got %d, want 1", beta.StationCount)
	}
	if beta.ResourcePOIs != 1 {
		t.Errorf("beta-1 resource POIs: got %d, want 1", beta.ResourcePOIs)
	}

	// Gamma has no KB data — should still appear with zero enrichment.
	gamma := as.NeighborInfo("gamma-1")
	if gamma == nil {
		t.Fatal("NeighborInfo(gamma-1) returned nil")
	}
	if gamma.Security != "" {
		t.Errorf("gamma-1 security: got %q, want empty", gamma.Security)
	}

	// Check valid actions (docked at station).
	if !as.CanDo("undock") {
		t.Error("should be able to undock when docked")
	}
	if as.CanDo("mine") {
		t.Error("should not be able to mine when docked")
	}
}

func TestRefreshMiningPOI(t *testing.T) {
	state := testState()
	state.Doc = false
	state.CurrentPOI = "belt-1"
	kb := knowledge.NewMemoryKB()

	as := New(state, kb, nil)
	as.Refresh(context.Background())

	if got := as.CurrentPOIType(); got != "asteroid_belt" {
		t.Errorf("CurrentPOIType: got %q, want %q", got, "asteroid_belt")
	}
}

func TestRefreshMarketOnlyWhenDocked(t *testing.T) {
	state := testState()
	state.Doc = false
	kb := knowledge.NewMemoryKB()

	as := New(state, kb, nil)
	as.Refresh(context.Background())

	if as.MarketSnapshot() != nil {
		t.Error("MarketSnapshot should be nil when undocked")
	}
}

func TestLiveAccessors(t *testing.T) {
	state := testState()
	kb := knowledge.NewMemoryKB()
	as := New(state, kb, nil)

	fuel, maxFuel := as.Fuel()
	if fuel != 80 || maxFuel != 100 {
		t.Errorf("Fuel: got (%.0f, %.0f), want (80, 100)", fuel, maxFuel)
	}

	hull, maxHull := as.Hull()
	if hull != 90 || maxHull != 100 {
		t.Errorf("Hull: got (%.0f, %.0f), want (90, 100)", hull, maxHull)
	}

	if as.Credits() != 5000 {
		t.Errorf("Credits: got %.0f, want 5000", as.Credits())
	}

	if !as.Docked() {
		t.Error("Docked: got false, want true")
	}

	if as.CurrentSystem() != "alpha-1" {
		t.Errorf("CurrentSystem: got %q, want %q", as.CurrentSystem(), "alpha-1")
	}

	if as.Tick() != 1000 {
		t.Errorf("Tick: got %d, want 1000", as.Tick())
	}

	cargo := as.Cargo()
	if len(cargo) != 2 {
		t.Errorf("Cargo: got %d items, want 2", len(cargo))
	}

	used, cap := as.CargoSpace()
	if used != 20 || cap != 50 {
		t.Errorf("CargoSpace: got (%.0f, %.0f), want (20, 50)", used, cap)
	}

	skills := as.Skills()
	if skills["mining"].Level != 3 {
		t.Errorf("mining skill level: got %d, want 3", skills["mining"].Level)
	}
}

func TestAgentContextAccessorsNil(t *testing.T) {
	state := testState()
	kb := knowledge.NewMemoryKB()
	as := New(state, kb, nil)

	if as.Goal() != nil {
		t.Error("Goal should be nil without agent context")
	}
	if as.Focus() != "" {
		t.Error("Focus should be empty without agent context")
	}
	if as.Constraints() != nil {
		t.Error("Constraints should be nil without agent context")
	}
	if as.HasQueuedPlan() {
		t.Error("HasQueuedPlan should be false without agent context")
	}
	if as.QueuedPlan() != nil {
		t.Error("QueuedPlan should be nil without agent context")
	}
}

func TestAgentContextAccessors(t *testing.T) {
	state := testState()
	kb := knowledge.NewMemoryKB()
	agentCtx := &AgentContext{
		Priority: &agent.Priority{
			Focus:       "mining",
			Constraints: []string{"low_fuel"},
			Urgency:     5,
		},
		QueuedPlan: []agent.PlannedAction{
			{Sequence: 1, Action: "mine", Target: "belt-1"},
		},
	}
	as := NewWithAgent(state, kb, nil, agentCtx)

	if as.Focus() != "mining" {
		t.Errorf("Focus: got %q, want %q", as.Focus(), "mining")
	}
	if len(as.Constraints()) != 1 || as.Constraints()[0] != "low_fuel" {
		t.Errorf("Constraints: got %v, want [low_fuel]", as.Constraints())
	}
	if !as.HasQueuedPlan() {
		t.Error("HasQueuedPlan: got false, want true")
	}
}

func TestSnapshot(t *testing.T) {
	state := testState()
	kb := knowledge.NewMemoryKB()
	agentCtx := &AgentContext{
		Goal: &agent.Goal{Type: "wealth", Target: "10000_credits", Progress: 0.5},
		Priority: &agent.Priority{
			Focus:       "trading",
			Constraints: []string{"cargo_full"},
		},
	}

	as := NewWithAgent(state, kb, nil, agentCtx)
	as.Refresh(context.Background())

	snap := as.Snapshot()

	if snap.Username != "testplayer" {
		t.Errorf("Username: got %q, want %q", snap.Username, "testplayer")
	}
	if snap.Credits != 5000 {
		t.Errorf("Credits: got %.0f, want 5000", snap.Credits)
	}
	if snap.CurrentSystem != "alpha-1" {
		t.Errorf("CurrentSystem: got %q, want %q", snap.CurrentSystem, "alpha-1")
	}
	if !snap.Docked {
		t.Error("Docked: got false, want true")
	}
	if snap.SystemSecurity != "Medium Security" {
		t.Errorf("SystemSecurity: got %q, want %q", snap.SystemSecurity, "Medium Security")
	}
	if snap.ShipClass != "scout" {
		t.Errorf("ShipClass: got %q, want %q", snap.ShipClass, "scout")
	}
	if snap.Goal == nil || snap.Goal.Target != "10000_credits" {
		t.Error("Goal not included in snapshot")
	}
	if snap.Focus != "trading" {
		t.Errorf("Focus: got %q, want %q", snap.Focus, "trading")
	}
	if snap.ValidActionCount == 0 {
		t.Error("ValidActionCount should be > 0 when docked")
	}
	if len(snap.ValidActionNames) != snap.ValidActionCount {
		t.Errorf("ValidActionNames length %d != ValidActionCount %d", len(snap.ValidActionNames), snap.ValidActionCount)
	}
}

func TestSetAgentContext(t *testing.T) {
	state := testState()
	kb := knowledge.NewMemoryKB()
	as := New(state, kb, nil)

	if as.Goal() != nil {
		t.Error("Goal should be nil initially")
	}

	as.SetAgentContext(&AgentContext{
		Goal: &agent.Goal{Type: "exploration", Target: "sol"},
	})

	if as.Goal() == nil || as.Goal().Target != "sol" {
		t.Error("Goal should be set after SetAgentContext")
	}
}

func TestSecurityLabel(t *testing.T) {
	tests := []struct {
		level int
		want  string
	}{
		{0, "Lawless"},
		{1, "Low Security"},
		{2, "Medium Security"},
		{3, "High Security"},
		{99, "Unknown"},
	}
	for _, tt := range tests {
		if got := securityLabel(tt.level); got != tt.want {
			t.Errorf("securityLabel(%d): got %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestResolvePOIType(t *testing.T) {
	state := testState()
	if got := resolvePOIType(state); got != "station" {
		t.Errorf("resolvePOIType: got %q, want %q", got, "station")
	}

	state.CurrentPOI = "belt-1"
	if got := resolvePOIType(state); got != "asteroid_belt" {
		t.Errorf("resolvePOIType: got %q, want %q", got, "asteroid_belt")
	}

	state.CurrentPOI = "nonexistent"
	if got := resolvePOIType(state); got != "" {
		t.Errorf("resolvePOIType: got %q, want empty", got)
	}
}

func TestFuelCostTo(t *testing.T) {
	state := testState()
	kb := knowledge.NewMemoryKB()
	as := New(state, kb, nil)
	as.Refresh(context.Background())

	// No route data in MemoryKB — should return false.
	_, ok := as.FuelCostTo("beta-1")
	if ok {
		t.Error("FuelCostTo should return false with no KB route data")
	}
}

func TestCanDo(t *testing.T) {
	state := testState()
	kb := knowledge.NewMemoryKB()
	as := New(state, kb, nil)
	as.Refresh(context.Background())

	// When docked, undock should be available.
	if !as.CanDo("undock") {
		t.Error("CanDo(undock): got false, want true (docked)")
	}

	// travel requires undocked.
	if as.CanDo("travel") {
		t.Error("CanDo(travel): got true, want false (docked)")
	}
}
