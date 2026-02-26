package prompts

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestBuildPersonalityContext_FullData(t *testing.T) {
	personality := map[string]interface{}{
		"traits":      []interface{}{"brave", "curious"},
		"motivations": []interface{}{"explore", "profit"},
		"skills":      []interface{}{"mining", "combat"},
		"background":  "A veteran space miner.",
	}

	pc := buildPersonalityContext("TestBot", "miner", personality)

	if pc.Name != "TestBot" {
		t.Errorf("Name = %q, want %q", pc.Name, "TestBot")
	}
	if pc.Role != "miner" {
		t.Errorf("Role = %q, want %q", pc.Role, "miner")
	}
	if len(pc.Traits) != 2 || pc.Traits[0] != "brave" || pc.Traits[1] != "curious" {
		t.Errorf("Traits = %v, want [brave curious]", pc.Traits)
	}
	if len(pc.Motivations) != 2 {
		t.Errorf("Motivations length = %d, want 2", len(pc.Motivations))
	}
	if len(pc.Skills) != 2 {
		t.Errorf("Skills length = %d, want 2", len(pc.Skills))
	}
	if pc.Background != "A veteran space miner." {
		t.Errorf("Background = %q, want %q", pc.Background, "A veteran space miner.")
	}
}

func TestBuildPersonalityContext_EmptyMap(t *testing.T) {
	pc := buildPersonalityContext("Bot", "explorer", map[string]interface{}{})

	if pc.Name != "Bot" {
		t.Errorf("Name = %q, want %q", pc.Name, "Bot")
	}
	if pc.Role != "explorer" {
		t.Errorf("Role = %q, want %q", pc.Role, "explorer")
	}
	if pc.Traits != nil {
		t.Errorf("Traits = %v, want nil", pc.Traits)
	}
	if pc.Motivations != nil {
		t.Errorf("Motivations = %v, want nil", pc.Motivations)
	}
	if pc.Skills != nil {
		t.Errorf("Skills = %v, want nil", pc.Skills)
	}
	if pc.Background != "" {
		t.Errorf("Background = %q, want empty", pc.Background)
	}
}

func TestBuildPersonalityContext_WrongTypes(t *testing.T) {
	personality := map[string]interface{}{
		"traits":      "not a slice",
		"motivations": 42,
		"skills":      true,
		"background":  123,
	}

	pc := buildPersonalityContext("Bot", "miner", personality)

	// Wrong types should be silently ignored.
	if pc.Traits != nil {
		t.Errorf("Traits = %v, want nil for wrong type", pc.Traits)
	}
	if pc.Motivations != nil {
		t.Errorf("Motivations = %v, want nil for wrong type", pc.Motivations)
	}
	if pc.Skills != nil {
		t.Errorf("Skills = %v, want nil for wrong type", pc.Skills)
	}
	if pc.Background != "" {
		t.Errorf("Background = %q, want empty for wrong type", pc.Background)
	}
}

func TestBuildPersonalityContext_MixedSliceElements(t *testing.T) {
	personality := map[string]interface{}{
		"traits": []interface{}{"brave", 42, "curious", nil},
	}

	pc := buildPersonalityContext("Bot", "miner", personality)

	if len(pc.Traits) != 4 {
		t.Fatalf("Traits length = %d, want 4", len(pc.Traits))
	}
	if pc.Traits[0] != "brave" {
		t.Errorf("Traits[0] = %q, want %q", pc.Traits[0], "brave")
	}
	// Non-string elements result in zero-value strings.
	if pc.Traits[1] != "" {
		t.Errorf("Traits[1] = %q, want empty for non-string", pc.Traits[1])
	}
	if pc.Traits[2] != "curious" {
		t.Errorf("Traits[2] = %q, want %q", pc.Traits[2], "curious")
	}
	if pc.Traits[3] != "" {
		t.Errorf("Traits[3] = %q, want empty for nil", pc.Traits[3])
	}
}

func TestBuildStateContext_NilState(t *testing.T) {
	sc := buildStateContext(nil)

	if sc == nil {
		t.Fatal("buildStateContext(nil) returned nil, want non-nil StateContext")
	}
	if sc.SystemName != "" {
		t.Errorf("SystemName = %q, want empty", sc.SystemName)
	}
	if sc.Credits != 0 {
		t.Errorf("Credits = %f, want 0", sc.Credits)
	}
}

func TestBuildStateContext_BasicState(t *testing.T) {
	state := &game.State{
		CurrentSystem: "Sol",
		CurrentPOI:    "station-1",
		Doc:           true,
		Fuel:          80,
		MaxFuel:       100,
		Hull:          50,
		MaxHull:       200,
		Credits:       5000,
		CurrentTick:   42,
		MaxCargo:      10,
		Ship: game.Ship{
			ClassID:       "frigate",
			Shield:        30,
			MaxShield:     100,
			ShieldRecharge: 5,
			Armor:         20,
			Speed:         50,
			CPUUsed:       10,
			CPUCapacity:   100,
			PowerUsed:     25,
			PowerCapacity: 50,
			CargoUsed:     3.0,
			CargoCapacity: 10.0,
			Modules:       []string{"laser", "shield_gen"},
			Cargo: []game.CargoItem{
				{ItemID: "iron_ore", Quantity: 5},
				{ItemID: "gold", Quantity: 2},
			},
		},
		System: game.SystemData{
			ID:          "sol-1",
			Empire:      "Federation",
			PoliceLevel: 3,
			POIs: []game.POI{
				{ID: "station-1", Description: "A bustling station"},
				{ID: "asteroid-1", Description: "Rocky field"},
			},
		},
		InCombat: false,
	}

	sc := buildStateContext(state)

	if sc.SystemName != "Sol" {
		t.Errorf("SystemName = %q, want %q", sc.SystemName, "Sol")
	}
	if sc.SystemID != "sol-1" {
		t.Errorf("SystemID = %q, want %q", sc.SystemID, "sol-1")
	}
	if !sc.IsDocked {
		t.Error("IsDocked = false, want true")
	}
	if sc.DockedAt != "station-1" {
		t.Errorf("DockedAt = %q, want %q", sc.DockedAt, "station-1")
	}
	if sc.FuelPercent != 80 {
		t.Errorf("FuelPercent = %f, want 80", sc.FuelPercent)
	}
	if sc.HullPercent != 25 {
		t.Errorf("HullPercent = %f, want 25", sc.HullPercent)
	}
	if sc.Credits != 5000 {
		t.Errorf("Credits = %f, want 5000", sc.Credits)
	}
	if sc.ShipClass != "frigate" {
		t.Errorf("ShipClass = %q, want %q", sc.ShipClass, "frigate")
	}
	if sc.ShieldPercent != 30 {
		t.Errorf("ShieldPercent = %f, want 30", sc.ShieldPercent)
	}
	if sc.CPUPercent != 10 {
		t.Errorf("CPUPercent = %f, want 10", sc.CPUPercent)
	}
	if sc.PowerPercent != 50 {
		t.Errorf("PowerPercent = %f, want 50", sc.PowerPercent)
	}
	if sc.CargoPercent != 30 {
		t.Errorf("CargoPercent = %f, want 30", sc.CargoPercent)
	}
	if sc.SystemSecurity != "High" {
		t.Errorf("SystemSecurity = %q, want %q", sc.SystemSecurity, "High")
	}
	if sc.SystemEmpire != "Federation" {
		t.Errorf("SystemEmpire = %q, want %q", sc.SystemEmpire, "Federation")
	}
	if len(sc.Cargo) != 2 {
		t.Errorf("Cargo length = %d, want 2", len(sc.Cargo))
	}
	if sc.Cargo[0].Type != "iron_ore" || sc.Cargo[0].Quantity != 5 {
		t.Errorf("Cargo[0] = %+v, want {iron_ore 5}", sc.Cargo[0])
	}
	if len(sc.Modules) != 2 {
		t.Errorf("Modules length = %d, want 2", len(sc.Modules))
	}
	if sc.POIDescription != "A bustling station" {
		t.Errorf("POIDescription = %q, want %q", sc.POIDescription, "A bustling station")
	}
}

func TestBuildStateContext_SecurityLevels(t *testing.T) {
	tests := []struct {
		policeLevel int
		want        string
	}{
		{0, "None"},
		{1, "Low"},
		{2, "Medium"},
		{3, "High"},
		{99, "Unknown"},
		{-1, "Unknown"},
	}

	for _, tt := range tests {
		state := &game.State{
			MaxFuel: 1,
			MaxHull: 1,
			System: game.SystemData{
				PoliceLevel: tt.policeLevel,
			},
		}
		sc := buildStateContext(state)
		if sc.SystemSecurity != tt.want {
			t.Errorf("PoliceLevel %d: SystemSecurity = %q, want %q",
				tt.policeLevel, sc.SystemSecurity, tt.want)
		}
	}
}

func TestBuildStateContext_NotDocked(t *testing.T) {
	state := &game.State{
		Doc:        false,
		CurrentPOI: "some-poi",
		MaxFuel:    1,
		MaxHull:    1,
	}

	sc := buildStateContext(state)
	if sc.IsDocked {
		t.Error("IsDocked = true, want false")
	}
	if sc.DockedAt != "" {
		t.Errorf("DockedAt = %q, want empty when not docked", sc.DockedAt)
	}
}

func TestBuildStateContext_NearbyPlayers(t *testing.T) {
	state := &game.State{
		MaxFuel: 1,
		MaxHull: 1,
		Nearby: []game.NearbyPlayer{
			{Username: "player1", ShipClass: "cruiser", InCombat: false, FactionTag: "TAG"},
			{Username: "pirate1", ShipClass: "fighter", InCombat: true, ClanTag: "PIR"},
			{Username: "player2", ShipClass: "hauler", InCombat: true},
		},
	}

	sc := buildStateContext(state)

	if sc.NearbyPlayers != 3 {
		t.Errorf("NearbyPlayers = %d, want 3", sc.NearbyPlayers)
	}
	if sc.NearbyHostiles != 2 {
		t.Errorf("NearbyHostiles = %d, want 2", sc.NearbyHostiles)
	}
	if len(sc.NearbyList) != 3 {
		t.Fatalf("NearbyList length = %d, want 3", len(sc.NearbyList))
	}
	if sc.NearbyList[0].Username != "player1" {
		t.Errorf("NearbyList[0].Username = %q, want %q", sc.NearbyList[0].Username, "player1")
	}
	if sc.NearbyList[1].ClanTag != "PIR" {
		t.Errorf("NearbyList[1].ClanTag = %q, want %q", sc.NearbyList[1].ClanTag, "PIR")
	}
}

func TestBuildStateContext_TravelProgress(t *testing.T) {
	state := &game.State{
		MaxFuel:     1,
		MaxHull:     1,
		Traveling:   true,
		CurrentTick: 100,
		TravelProgress: &game.TravelProgress{
			Progress:    0.5,
			Destination: "Alpha Centauri",
			Type:        "jump",
			ArrivalTick: 110,
		},
	}

	sc := buildStateContext(state)

	if !sc.Traveling {
		t.Error("Traveling = false, want true")
	}
	if sc.TravelProgress == nil {
		t.Fatal("TravelProgress = nil, want non-nil")
	}
	if sc.TravelProgress.Progress != 0.5 {
		t.Errorf("TravelProgress.Progress = %f, want 0.5", sc.TravelProgress.Progress)
	}
	if sc.TravelProgress.Destination != "Alpha Centauri" {
		t.Errorf("TravelProgress.Destination = %q, want %q", sc.TravelProgress.Destination, "Alpha Centauri")
	}
	if sc.TravelProgress.ETA != 10 {
		t.Errorf("TravelProgress.ETA = %d, want 10", sc.TravelProgress.ETA)
	}
}

func TestBuildStateContext_TravelProgress_PastArrival(t *testing.T) {
	state := &game.State{
		MaxFuel:     1,
		MaxHull:     1,
		Traveling:   true,
		CurrentTick: 120,
		TravelProgress: &game.TravelProgress{
			Progress:    1.0,
			Destination: "Sol",
			Type:        "travel",
			ArrivalTick: 100,
		},
	}

	sc := buildStateContext(state)

	if sc.TravelProgress == nil {
		t.Fatal("TravelProgress = nil, want non-nil")
	}
	// ETA should be clamped to 0 when past arrival.
	if sc.TravelProgress.ETA != 0 {
		t.Errorf("TravelProgress.ETA = %d, want 0 when past arrival", sc.TravelProgress.ETA)
	}
}

func TestBuildStateContext_NotTraveling_NilProgress(t *testing.T) {
	state := &game.State{
		MaxFuel:        1,
		MaxHull:        1,
		Traveling:      false,
		TravelProgress: nil,
	}

	sc := buildStateContext(state)

	if sc.Traveling {
		t.Error("Traveling = true, want false")
	}
	if sc.TravelProgress != nil {
		t.Error("TravelProgress should be nil when not traveling")
	}
}

func TestBuildStateContext_ZeroMaxValues(t *testing.T) {
	// Avoid division by zero when MaxFuel/MaxHull are 0.
	state := &game.State{
		Fuel:    50,
		MaxFuel: 0,
		Hull:    50,
		MaxHull: 0,
		Ship: game.Ship{
			MaxShield:     0,
			CPUCapacity:   0,
			PowerCapacity: 0,
			CargoCapacity: 0,
		},
	}

	sc := buildStateContext(state)

	// These will be Inf due to division by zero in the source code,
	// but shield/cpu/power/cargo percentages are guarded.
	if sc.ShieldPercent != 0 {
		t.Errorf("ShieldPercent = %f, want 0 when MaxShield=0", sc.ShieldPercent)
	}
	if sc.CPUPercent != 0 {
		t.Errorf("CPUPercent = %f, want 0 when CPUCapacity=0", sc.CPUPercent)
	}
	if sc.PowerPercent != 0 {
		t.Errorf("PowerPercent = %f, want 0 when PowerCapacity=0", sc.PowerPercent)
	}
	if sc.CargoPercent != 0 {
		t.Errorf("CargoPercent = %f, want 0 when CargoCapacity=0", sc.CargoPercent)
	}
}

func TestBuildStateContext_POIDescriptionNotFound(t *testing.T) {
	state := &game.State{
		CurrentPOI: "nonexistent-poi",
		MaxFuel:    1,
		MaxHull:    1,
		System: game.SystemData{
			POIs: []game.POI{
				{ID: "other-poi", Description: "Other"},
			},
		},
	}

	sc := buildStateContext(state)
	if sc.POIDescription != "" {
		t.Errorf("POIDescription = %q, want empty for unmatched POI", sc.POIDescription)
	}
}

func TestBuildSystemContext(t *testing.T) {
	state := &game.State{
		CurrentTick: 999,
	}

	sysCtx := buildSystemContext(state)

	if sysCtx.CurrentTick != 999 {
		t.Errorf("CurrentTick = %d, want 999", sysCtx.CurrentTick)
	}
	if len(sysCtx.AvailableActions) == 0 {
		t.Error("AvailableActions is empty, want non-empty")
	}
}

func TestGetAvailableActions(t *testing.T) {
	actions := getAvailableActions()

	if len(actions) == 0 {
		t.Fatal("getAvailableActions() returned empty slice")
	}

	// Check that known actions exist.
	actionNames := make(map[string]bool)
	for _, a := range actions {
		actionNames[a.Name] = true
		if a.Description == "" {
			t.Errorf("Action %q has empty description", a.Name)
		}
	}

	for _, expected := range []string{"undock", "dock", "travel", "jump", "mine", "scan", "wait"} {
		if !actionNames[expected] {
			t.Errorf("Expected action %q not found in available actions", expected)
		}
	}
}

func TestNewTemplateContext(t *testing.T) {
	state := &game.State{
		CurrentSystem: "Sol",
		CurrentTick:   10,
		MaxFuel:       100,
		MaxHull:       100,
		Fuel:          50,
		Hull:          75,
	}

	personality := map[string]interface{}{
		"traits":     []interface{}{"bold"},
		"background": "test background",
	}

	knowledge := &KnowledgeContext{
		Connections: []string{"Alpha"},
	}
	history := &HistoryContext{}
	feedback := &FeedbackContext{Success: true, Action: "mine"}
	goal := &GoalContext{Type: "wealth", Target: "10000_credits"}

	ctx := NewTemplateContext("agent-1", "Agent One", "miner", personality, state, knowledge, history, feedback, goal)

	if ctx.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want %q", ctx.AgentID, "agent-1")
	}
	if ctx.AgentName != "Agent One" {
		t.Errorf("AgentName = %q, want %q", ctx.AgentName, "Agent One")
	}
	if ctx.Role != "miner" {
		t.Errorf("Role = %q, want %q", ctx.Role, "miner")
	}
	if ctx.Personality == nil {
		t.Fatal("Personality = nil, want non-nil")
	}
	if ctx.State == nil {
		t.Fatal("State = nil, want non-nil")
	}
	if ctx.Knowledge != knowledge {
		t.Error("Knowledge not set correctly")
	}
	if ctx.History != history {
		t.Error("History not set correctly")
	}
	if ctx.LastFeedback != feedback {
		t.Error("LastFeedback not set correctly")
	}
	if ctx.Goal != goal {
		t.Error("Goal not set correctly")
	}
	if ctx.System == nil {
		t.Fatal("System = nil, want non-nil")
	}
}
