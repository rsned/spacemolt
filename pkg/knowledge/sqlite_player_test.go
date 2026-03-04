package knowledge

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestSQLiteKB_StorePlayer(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	player := PlayerRecord{
		ID:            "player-1",
		Username:      "TestPlayer",
		Empire:        "solarian",
		Credits:       5000.0,
		CurrentSystem: "sys-1",
		CurrentPOI:    "poi-1",
		CurrentShipID: "ship-1",
		HomeBase:      "base-1",
		DockedAtBase:  "base-1",
		FactionID:     "faction-1",
		FactionRank:   "member",
		Experience:    1500,
		Stats: PlayerStatsRecord{
			ShipsDestroyed:    3,
			TimesDestroyed:    1,
			OreMined:          500.0,
			CreditsEarned:     10000.0,
			CreditsSpent:      5000.0,
			TradesCompleted:   20,
			SystemsDiscovered: 15,
			ItemsCrafted:      10,
			MissionsCompleted: 5,
			DistanceTraveled:  1000,
		},
		LastUpdatedTick: 42,
	}

	if err := kb.StorePlayer(ctx, player); err != nil {
		t.Fatalf("StorePlayer failed: %v", err)
	}
}

func TestSQLiteKB_GetPlayer(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	player := PlayerRecord{
		ID:            "player-1",
		Username:      "TestPlayer",
		Empire:        "solarian",
		Credits:       5000.0,
		CurrentSystem: "sys-1",
		Experience:    1500,
		Stats: PlayerStatsRecord{
			ShipsDestroyed:    3,
			OreMined:          500.0,
			TradesCompleted:   20,
			SystemsDiscovered: 15,
		},
	}
	if err := kb.StorePlayer(ctx, player); err != nil {
		t.Fatalf("StorePlayer failed: %v", err)
	}

	retrieved, err := kb.GetPlayer(ctx, "player-1")
	if err != nil {
		t.Fatalf("GetPlayer failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Expected non-nil player")
	}
	if retrieved.Username != "TestPlayer" {
		t.Errorf("Expected username 'TestPlayer', got '%s'", retrieved.Username)
	}
	if retrieved.Credits != 5000.0 {
		t.Errorf("Expected credits 5000.0, got %f", retrieved.Credits)
	}
	if retrieved.Stats.ShipsDestroyed != 3 {
		t.Errorf("Expected 3 ships destroyed, got %d", retrieved.Stats.ShipsDestroyed)
	}
	if retrieved.Stats.OreMined != 500.0 {
		t.Errorf("Expected 500.0 ore mined, got %f", retrieved.Stats.OreMined)
	}
}

func TestSQLiteKB_GetPlayer_NotFound(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	player, err := kb.GetPlayer(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetPlayer for nonexistent failed: %v", err)
	}
	if player != nil {
		t.Error("Expected nil for nonexistent player")
	}
}

func TestSQLiteKB_StorePlayer_Upsert(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Store initial player
	player := PlayerRecord{
		ID:       "player-1",
		Username: "TestPlayer",
		Credits:  1000.0,
	}
	if err := kb.StorePlayer(ctx, player); err != nil {
		t.Fatalf("StorePlayer failed: %v", err)
	}

	// Update player
	player.Credits = 5000.0
	player.Username = "UpdatedPlayer"
	if err := kb.StorePlayer(ctx, player); err != nil {
		t.Fatalf("StorePlayer upsert failed: %v", err)
	}

	retrieved, err := kb.GetPlayer(ctx, "player-1")
	if err != nil {
		t.Fatalf("GetPlayer failed: %v", err)
	}
	if retrieved.Credits != 5000.0 {
		t.Errorf("Expected updated credits 5000.0, got %f", retrieved.Credits)
	}
	if retrieved.Username != "UpdatedPlayer" {
		t.Errorf("Expected updated username 'UpdatedPlayer', got '%s'", retrieved.Username)
	}
}

func TestSQLiteKB_StorePlayerSkills(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Need a player first (foreign key)
	player := PlayerRecord{ID: "player-1", Username: "TestPlayer"}
	if err := kb.StorePlayer(ctx, player); err != nil {
		t.Fatalf("StorePlayer failed: %v", err)
	}

	skills := []PlayerSkillRecord{
		{SkillID: "mining", Level: 5, CurrentXP: 1200.0},
		{SkillID: "trading", Level: 3, CurrentXP: 500.0},
		{SkillID: "navigation", Level: 2, CurrentXP: 200.0},
	}

	if err := kb.StorePlayerSkills(ctx, "player-1", skills); err != nil {
		t.Fatalf("StorePlayerSkills failed: %v", err)
	}

	retrieved, err := kb.GetPlayerSkills(ctx, "player-1")
	if err != nil {
		t.Fatalf("GetPlayerSkills failed: %v", err)
	}
	if len(retrieved) != 3 {
		t.Fatalf("Expected 3 skills, got %d", len(retrieved))
	}
}

func TestSQLiteKB_GetPlayerSkills_Empty(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	skills, err := kb.GetPlayerSkills(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetPlayerSkills for nonexistent player failed: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("Expected 0 skills, got %d", len(skills))
	}
}

func TestSQLiteKB_StorePlayerSkills_Replaces(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	player := PlayerRecord{ID: "player-1", Username: "TestPlayer"}
	if err := kb.StorePlayer(ctx, player); err != nil {
		t.Fatalf("StorePlayer failed: %v", err)
	}

	// Store initial skills
	skills1 := []PlayerSkillRecord{
		{SkillID: "mining", Level: 5, CurrentXP: 1200.0},
	}
	if err := kb.StorePlayerSkills(ctx, "player-1", skills1); err != nil {
		t.Fatalf("StorePlayerSkills 1 failed: %v", err)
	}

	// Replace skills
	skills2 := []PlayerSkillRecord{
		{SkillID: "mining", Level: 7, CurrentXP: 2500.0},
		{SkillID: "trading", Level: 1, CurrentXP: 50.0},
	}
	if err := kb.StorePlayerSkills(ctx, "player-1", skills2); err != nil {
		t.Fatalf("StorePlayerSkills 2 failed: %v", err)
	}

	retrieved, err := kb.GetPlayerSkills(ctx, "player-1")
	if err != nil {
		t.Fatalf("GetPlayerSkills failed: %v", err)
	}
	if len(retrieved) != 2 {
		t.Errorf("Expected 2 skills after replacement, got %d", len(retrieved))
	}
}

func TestSQLiteKB_StoreShip(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	ship := ShipRecord{
		ID:            "ship-1",
		OwnerID:       "player-1",
		ClassID:       "shuttle",
		Name:          "My Ship",
		Hull:          100,
		MaxHull:       100,
		Shield:        50,
		MaxShield:     50,
		Fuel:          80,
		MaxFuel:       100,
		CargoCapacity: 20,
		WeaponSlots:   1,
		Cargo: []CargoEntry{
			{ItemID: "iron_ore", Quantity: 10},
			{ItemID: "copper_ore", Quantity: 5},
		},
		Modules: []ShipModuleRecord{
			{ID: "mod-1", TypeID: "laser_mk1", Name: "Laser Mk1", Type: "weapon", CPUUsage: 5, PowerUsage: 10, Quality: 0.9, QualityGrade: "B"},
		},
	}

	if err := kb.StoreShip(ctx, ship); err != nil {
		t.Fatalf("StoreShip failed: %v", err)
	}
}

func TestSQLiteKB_GetShip(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	ship := ShipRecord{
		ID:        "ship-1",
		OwnerID:   "player-1",
		ClassID:   "shuttle",
		Name:      "My Ship",
		Hull:      100,
		MaxHull:   100,
		MaxFuel:   100,
		Fuel:      80,
		Cargo:     []CargoEntry{{ItemID: "iron_ore", Quantity: 10}},
		Modules:   []ShipModuleRecord{{ID: "mod-1", TypeID: "laser_mk1", Name: "Laser"}},
	}
	if err := kb.StoreShip(ctx, ship); err != nil {
		t.Fatalf("StoreShip failed: %v", err)
	}

	retrieved, err := kb.GetShip(ctx, "ship-1")
	if err != nil {
		t.Fatalf("GetShip failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Expected non-nil ship")
	}
	if retrieved.Name != "My Ship" {
		t.Errorf("Expected name 'My Ship', got '%s'", retrieved.Name)
	}
	if retrieved.Hull != 100 {
		t.Errorf("Expected hull 100, got %f", retrieved.Hull)
	}
	if len(retrieved.Cargo) != 1 {
		t.Errorf("Expected 1 cargo item, got %d", len(retrieved.Cargo))
	}
	if len(retrieved.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(retrieved.Modules))
	}
}

func TestSQLiteKB_GetShip_NotFound(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	ship, err := kb.GetShip(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetShip for nonexistent failed: %v", err)
	}
	if ship != nil {
		t.Error("Expected nil for nonexistent ship")
	}
}

func TestSQLiteKB_StoreShip_Upsert(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	ship := ShipRecord{
		ID:      "ship-1",
		OwnerID: "player-1",
		ClassID: "shuttle",
		Name:    "My Ship",
		Hull:    100,
		MaxHull: 100,
		Cargo:   []CargoEntry{{ItemID: "iron_ore", Quantity: 10}},
	}
	if err := kb.StoreShip(ctx, ship); err != nil {
		t.Fatalf("StoreShip failed: %v", err)
	}

	// Update
	ship.Hull = 50
	ship.Cargo = []CargoEntry{{ItemID: "copper_ore", Quantity: 5}}
	if err := kb.StoreShip(ctx, ship); err != nil {
		t.Fatalf("StoreShip upsert failed: %v", err)
	}

	retrieved, err := kb.GetShip(ctx, "ship-1")
	if err != nil {
		t.Fatalf("GetShip failed: %v", err)
	}
	if retrieved.Hull != 50 {
		t.Errorf("Expected updated hull 50, got %f", retrieved.Hull)
	}
	if len(retrieved.Cargo) != 1 {
		t.Fatalf("Expected 1 cargo item, got %d", len(retrieved.Cargo))
	}
	if retrieved.Cargo[0].ItemID != "copper_ore" {
		t.Errorf("Expected cargo copper_ore, got %s", retrieved.Cargo[0].ItemID)
	}
}

func TestSQLiteKB_GetPlayerShips(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Store multiple ships for different players
	ships := []ShipRecord{
		{ID: "ship-1", OwnerID: "player-1", ClassID: "shuttle", Name: "Ship A"},
		{ID: "ship-2", OwnerID: "player-1", ClassID: "fighter", Name: "Ship B"},
		{ID: "ship-3", OwnerID: "player-2", ClassID: "shuttle", Name: "Ship C"},
	}

	for _, ship := range ships {
		if err := kb.StoreShip(ctx, ship); err != nil {
			t.Fatalf("StoreShip %s failed: %v", ship.ID, err)
		}
	}

	// Get player-1's ships
	p1Ships, err := kb.GetPlayerShips(ctx, "player-1")
	if err != nil {
		t.Fatalf("GetPlayerShips failed: %v", err)
	}
	if len(p1Ships) != 2 {
		t.Errorf("Expected 2 ships for player-1, got %d", len(p1Ships))
	}

	// Get player-2's ships
	p2Ships, err := kb.GetPlayerShips(ctx, "player-2")
	if err != nil {
		t.Fatalf("GetPlayerShips player-2 failed: %v", err)
	}
	if len(p2Ships) != 1 {
		t.Errorf("Expected 1 ship for player-2, got %d", len(p2Ships))
	}

	// Nonexistent player
	noShips, err := kb.GetPlayerShips(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetPlayerShips nonexistent failed: %v", err)
	}
	if len(noShips) != 0 {
		t.Errorf("Expected 0 ships for nonexistent, got %d", len(noShips))
	}
}

func TestSQLiteKB_StoreMissionTemplates(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	missions := []MissionTemplate{
		{
			ID:             "mission-1",
			Title:          "Deliver Cargo",
			Description:    "Deliver 10 iron ore to station",
			Type:           "delivery",
			Difficulty:     2,
			BaseID:         "base-1",
			GiverName:      "Captain Rex",
			GiverTitle:     "Station Commander",
			DialogOffer:    "I need you to deliver some cargo.",
			ExpiresInTicks: 500,
			RewardsCredits: 1000,
			RewardsSkillXP: map[string]int{"trading": 100},
			Objectives: []MissionObjectiveRecord{
				{Type: "deliver", Description: "Deliver 10 iron ore", SortOrder: 0},
				{Type: "return", Description: "Return to station", SortOrder: 1},
			},
		},
	}

	if err := kb.StoreMissionTemplates(ctx, "base-1", missions); err != nil {
		t.Fatalf("StoreMissionTemplates failed: %v", err)
	}
}

func TestSQLiteKB_GetMissionTemplates(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	missions := []MissionTemplate{
		{
			ID:             "mission-1",
			Title:          "Deliver Cargo",
			Type:           "delivery",
			Difficulty:     2,
			BaseID:         "base-1",
			RewardsCredits: 1000,
			RewardsSkillXP: map[string]int{"trading": 100},
			Objectives: []MissionObjectiveRecord{
				{Type: "deliver", Description: "Deliver items", SortOrder: 0},
			},
		},
		{
			ID:             "mission-2",
			Title:          "Eliminate Pirates",
			Type:           "combat",
			Difficulty:     5,
			BaseID:         "base-1",
			RewardsCredits: 5000,
		},
	}

	if err := kb.StoreMissionTemplates(ctx, "base-1", missions); err != nil {
		t.Fatalf("StoreMissionTemplates failed: %v", err)
	}

	retrieved, err := kb.GetMissionTemplates(ctx, "base-1")
	if err != nil {
		t.Fatalf("GetMissionTemplates failed: %v", err)
	}
	if len(retrieved) != 2 {
		t.Fatalf("Expected 2 missions, got %d", len(retrieved))
	}

	// Check that objectives are loaded
	for _, m := range retrieved {
		if m.ID == "mission-1" && len(m.Objectives) != 1 {
			t.Errorf("Expected 1 objective for mission-1, got %d", len(m.Objectives))
		}
	}
}

func TestSQLiteKB_GetMissionTemplates_Empty(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	missions, err := kb.GetMissionTemplates(ctx, "nonexistent-base")
	if err != nil {
		t.Fatalf("GetMissionTemplates for nonexistent base failed: %v", err)
	}
	if len(missions) != 0 {
		t.Errorf("Expected 0 missions, got %d", len(missions))
	}
}

func TestSQLiteKB_StoreMissionTemplates_Replaces(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	missions1 := []MissionTemplate{
		{ID: "mission-1", Title: "Old Mission", BaseID: "base-1"},
	}
	if err := kb.StoreMissionTemplates(ctx, "base-1", missions1); err != nil {
		t.Fatalf("StoreMissionTemplates 1 failed: %v", err)
	}

	missions2 := []MissionTemplate{
		{ID: "mission-2", Title: "New Mission A", BaseID: "base-1"},
		{ID: "mission-3", Title: "New Mission B", BaseID: "base-1"},
	}
	if err := kb.StoreMissionTemplates(ctx, "base-1", missions2); err != nil {
		t.Fatalf("StoreMissionTemplates 2 failed: %v", err)
	}

	retrieved, err := kb.GetMissionTemplates(ctx, "base-1")
	if err != nil {
		t.Fatalf("GetMissionTemplates failed: %v", err)
	}
	if len(retrieved) != 2 {
		t.Errorf("Expected 2 missions after replacement, got %d", len(retrieved))
	}
}

func TestSQLiteKB_Player_ConcurrentAccess(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent player stores
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			player := PlayerRecord{
				ID:       fmt.Sprintf("player-%d", idx),
				Username: fmt.Sprintf("Player%d", idx),
				Credits:  float64(idx * 1000),
			}
			if err := kb.StorePlayer(ctx, player); err != nil {
				t.Errorf("StorePlayer %d failed: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	// Concurrent reads
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := kb.GetPlayer(ctx, fmt.Sprintf("player-%d", idx))
			if err != nil {
				t.Errorf("GetPlayer %d failed: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestSQLiteKB_Ship_ConcurrentAccess(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent ship stores
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ship := ShipRecord{
				ID:      fmt.Sprintf("ship-%d", idx),
				OwnerID: "player-1",
				ClassID: "shuttle",
				Name:    fmt.Sprintf("Ship %d", idx),
				Cargo:   []CargoEntry{{ItemID: "iron_ore", Quantity: float64(idx)}},
			}
			if err := kb.StoreShip(ctx, ship); err != nil {
				t.Errorf("StoreShip %d failed: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	ships, err := kb.GetPlayerShips(ctx, "player-1")
	if err != nil {
		t.Fatalf("GetPlayerShips failed: %v", err)
	}
	if len(ships) != 10 {
		t.Errorf("Expected 10 ships, got %d", len(ships))
	}
}
