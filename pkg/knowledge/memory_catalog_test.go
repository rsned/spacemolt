package knowledge

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestMemoryKB_StoreAndGetItems(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	items := []CatalogItem{
		{ID: "iron_ore", Name: "Iron Ore", Category: "ore"},
		{ID: "laser", Name: "Laser", Category: "weapon"},
	}
	if err := kb.StoreItems(ctx, items); err != nil {
		t.Fatalf("StoreItems failed: %v", err)
	}

	// Get single item
	item, err := kb.GetItem(ctx, "iron_ore")
	if err != nil {
		t.Fatalf("GetItem failed: %v", err)
	}
	if item == nil || item.Name != "Iron Ore" {
		t.Error("Expected Iron Ore item")
	}

	// Get nonexistent item
	item, err = kb.GetItem(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetItem nonexistent failed: %v", err)
	}
	if item != nil {
		t.Error("Expected nil for nonexistent item")
	}

	// Get all items
	all, err := kb.GetItems(ctx)
	if err != nil {
		t.Fatalf("GetItems failed: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("Expected 2 items, got %d", len(all))
	}

	// Get by category
	ores, err := kb.GetItemsByCategory(ctx, "ore")
	if err != nil {
		t.Fatalf("GetItemsByCategory failed: %v", err)
	}
	if len(ores) != 1 {
		t.Errorf("Expected 1 ore, got %d", len(ores))
	}
}

func TestMemoryKB_StoreAndGetShipClasses(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	classes := []ShipClassDef{
		{ID: "shuttle", Name: "Shuttle", Class: "Civilian"},
		{ID: "fighter", Name: "Fighter", Class: "Combat"},
	}
	if err := kb.StoreShipClasses(ctx, classes); err != nil {
		t.Fatalf("StoreShipClasses failed: %v", err)
	}

	sc, err := kb.GetShipClass(ctx, "shuttle")
	if err != nil {
		t.Fatalf("GetShipClass failed: %v", err)
	}
	if sc == nil || sc.Name != "Shuttle" {
		t.Error("Expected Shuttle class")
	}

	sc, err = kb.GetShipClass(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetShipClass nonexistent failed: %v", err)
	}
	if sc != nil {
		t.Error("Expected nil for nonexistent class")
	}

	all, err := kb.GetShipClasses(ctx)
	if err != nil {
		t.Fatalf("GetShipClasses failed: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("Expected 2 classes, got %d", len(all))
	}

	civilian, err := kb.GetShipClassesByCategory(ctx, "Civilian")
	if err != nil {
		t.Fatalf("GetShipClassesByCategory failed: %v", err)
	}
	if len(civilian) != 1 {
		t.Errorf("Expected 1 civilian, got %d", len(civilian))
	}
}

func TestMemoryKB_StoreAndGetRecipes(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	recipes := []RecipeDef{
		{ID: "iron_plate", Name: "Iron Plate", Category: "smelting"},
		{ID: "wire", Name: "Wire", Category: "fabrication"},
	}
	if err := kb.StoreRecipes(ctx, recipes); err != nil {
		t.Fatalf("StoreRecipes failed: %v", err)
	}

	r, err := kb.GetRecipe(ctx, "iron_plate")
	if err != nil {
		t.Fatalf("GetRecipe failed: %v", err)
	}
	if r == nil || r.Name != "Iron Plate" {
		t.Error("Expected Iron Plate recipe")
	}

	r, err = kb.GetRecipe(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetRecipe nonexistent failed: %v", err)
	}
	if r != nil {
		t.Error("Expected nil for nonexistent recipe")
	}

	all, err := kb.GetRecipes(ctx)
	if err != nil {
		t.Fatalf("GetRecipes failed: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("Expected 2 recipes, got %d", len(all))
	}

	smelting, err := kb.GetRecipesByCategory(ctx, "smelting")
	if err != nil {
		t.Fatalf("GetRecipesByCategory failed: %v", err)
	}
	if len(smelting) != 1 {
		t.Errorf("Expected 1 smelting recipe, got %d", len(smelting))
	}
}

func TestMemoryKB_StoreAndGetPlayer(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	player := PlayerRecord{
		ID:       "player-1",
		Username: "TestPlayer",
		Credits:  5000.0,
	}
	if err := kb.StorePlayer(ctx, player); err != nil {
		t.Fatalf("StorePlayer failed: %v", err)
	}

	p, err := kb.GetPlayer(ctx, "player-1")
	if err != nil {
		t.Fatalf("GetPlayer failed: %v", err)
	}
	if p == nil || p.Username != "TestPlayer" {
		t.Error("Expected TestPlayer")
	}

	p, err = kb.GetPlayer(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetPlayer nonexistent failed: %v", err)
	}
	if p != nil {
		t.Error("Expected nil for nonexistent player")
	}
}

func TestMemoryKB_StoreAndGetPlayerSkills(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	skills := []PlayerSkillRecord{
		{SkillID: "mining_basic", Level: 5, CurrentXP: 1200},
	}
	if err := kb.StorePlayerSkills(ctx, "player-1", skills); err != nil {
		t.Fatalf("StorePlayerSkills failed: %v", err)
	}

	retrieved, err := kb.GetPlayerSkills(ctx, "player-1")
	if err != nil {
		t.Fatalf("GetPlayerSkills failed: %v", err)
	}
	if len(retrieved) != 1 {
		t.Errorf("Expected 1 skill, got %d", len(retrieved))
	}

	// Nonexistent player
	empty, err := kb.GetPlayerSkills(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetPlayerSkills nonexistent failed: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("Expected 0 skills, got %d", len(empty))
	}
}

func TestMemoryKB_StoreAndGetShip(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	ship := ShipRecord{
		ID:      "ship-1",
		OwnerID: "player-1",
		ClassID: "shuttle",
		Name:    "My Ship",
	}
	if err := kb.StoreShip(ctx, ship); err != nil {
		t.Fatalf("StoreShip failed: %v", err)
	}

	s, err := kb.GetShip(ctx, "ship-1")
	if err != nil {
		t.Fatalf("GetShip failed: %v", err)
	}
	if s == nil || s.Name != "My Ship" {
		t.Error("Expected My Ship")
	}

	s, err = kb.GetShip(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetShip nonexistent failed: %v", err)
	}
	if s != nil {
		t.Error("Expected nil for nonexistent ship")
	}
}

func TestMemoryKB_GetPlayerShips(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	ships := []ShipRecord{
		{ID: "ship-1", OwnerID: "player-1", ClassID: "shuttle", Name: "Ship A"},
		{ID: "ship-2", OwnerID: "player-1", ClassID: "fighter", Name: "Ship B"},
		{ID: "ship-3", OwnerID: "player-2", ClassID: "shuttle", Name: "Ship C"},
	}
	for _, s := range ships {
		if err := kb.StoreShip(ctx, s); err != nil {
			t.Fatalf("StoreShip %s failed: %v", s.ID, err)
		}
	}

	p1Ships, err := kb.GetPlayerShips(ctx, "player-1")
	if err != nil {
		t.Fatalf("GetPlayerShips failed: %v", err)
	}
	if len(p1Ships) != 2 {
		t.Errorf("Expected 2 ships, got %d", len(p1Ships))
	}

	noShips, err := kb.GetPlayerShips(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetPlayerShips nonexistent failed: %v", err)
	}
	if len(noShips) != 0 {
		t.Errorf("Expected 0 ships, got %d", len(noShips))
	}
}

func TestMemoryKB_StoreMissionTemplates(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	missions := []MissionTemplate{
		{ID: "m1", Title: "Mission 1", BaseID: "base-1"},
		{ID: "m2", Title: "Mission 2", BaseID: "base-1"},
	}
	if err := kb.StoreMissionTemplates(ctx, "base-1", missions); err != nil {
		t.Fatalf("StoreMissionTemplates failed: %v", err)
	}

	retrieved, err := kb.GetMissionTemplates(ctx, "base-1")
	if err != nil {
		t.Fatalf("GetMissionTemplates failed: %v", err)
	}
	if len(retrieved) != 2 {
		t.Errorf("Expected 2 missions, got %d", len(retrieved))
	}

	// Nonexistent base
	empty, err := kb.GetMissionTemplates(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetMissionTemplates nonexistent failed: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("Expected 0 missions, got %d", len(empty))
	}
}

func TestMemoryKB_MarketSnapshots(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	snapshot := MarketSnapshot{
		SystemID:    "sys-1",
		SystemName:  "System One",
		StationID:   "station-1",
		StationName: "Station One",
		GameTick:    100,
		Listings: []MarketListing{
			{ItemID: "iron_ore", ItemType: "ore", Quantity: 50, PricePerUnit: 10, Type: "sell"},
		},
	}

	if err := kb.StoreMarketSnapshot(ctx, snapshot, "agent-1"); err != nil {
		t.Fatalf("StoreMarketSnapshot failed: %v", err)
	}

	// Get latest
	latest, err := kb.GetLatestMarketSnapshot(ctx, "sys-1", "station-1")
	if err != nil {
		t.Fatalf("GetLatestMarketSnapshot failed: %v", err)
	}
	if latest == nil {
		t.Fatal("Expected non-nil snapshot")
	}
	if latest.GameTick != 100 {
		t.Errorf("Expected game tick 100, got %d", latest.GameTick)
	}

	// Get nonexistent
	latest, err = kb.GetLatestMarketSnapshot(ctx, "nonexistent", "nonexistent")
	if err != nil {
		t.Fatalf("GetLatestMarketSnapshot nonexistent failed: %v", err)
	}
	if latest != nil {
		t.Error("Expected nil for nonexistent snapshot")
	}
}

func TestMemoryKB_MarketSnapshots_Limit(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	for i := range 5 {
		snapshot := MarketSnapshot{
			SystemID:    "sys-1",
			StationID:   "station-1",
			StationName: "Station One",
			GameTick:    int64(100 + i),
		}
		if err := kb.StoreMarketSnapshot(ctx, snapshot, "agent-1"); err != nil {
			t.Fatalf("StoreMarketSnapshot %d failed: %v", i, err)
		}
	}

	snapshots, err := kb.GetMarketSnapshots(ctx, "sys-1", "station-1", 3)
	if err != nil {
		t.Fatalf("GetMarketSnapshots failed: %v", err)
	}
	if len(snapshots) != 3 {
		t.Errorf("Expected 3 snapshots with limit, got %d", len(snapshots))
	}
}

func TestMemoryKB_HasMarketSnapshotToday(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	has, err := kb.HasMarketSnapshotToday(ctx, "sys-1", "station-1")
	if err != nil {
		t.Fatalf("HasMarketSnapshotToday failed: %v", err)
	}
	if has {
		t.Error("Expected false for empty KB")
	}

	snapshot := MarketSnapshot{
		SystemID:    "sys-1",
		StationID:   "station-1",
		CapturedAt:  time.Now(),
	}
	if err := kb.StoreMarketSnapshot(ctx, snapshot, "agent-1"); err != nil {
		t.Fatalf("StoreMarketSnapshot failed: %v", err)
	}

	has, err = kb.HasMarketSnapshotToday(ctx, "sys-1", "station-1")
	if err != nil {
		t.Fatalf("HasMarketSnapshotToday after store failed: %v", err)
	}
	if !has {
		t.Error("Expected true after storing today's snapshot")
	}
}

func TestMemoryKB_GetMarketItems(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	snapshot := MarketSnapshot{
		SystemID:  "sys-1",
		StationID: "station-1",
		Listings: []MarketListing{
			{ItemID: "iron_ore", ItemType: "ore"},
			{ItemID: "copper_ore", ItemType: "ore"},
			{ItemID: "laser", ItemType: "weapon"},
		},
	}
	if err := kb.StoreMarketSnapshot(ctx, snapshot, "agent-1"); err != nil {
		t.Fatalf("StoreMarketSnapshot failed: %v", err)
	}

	// All items
	items, err := kb.GetMarketItems(ctx, "")
	if err != nil {
		t.Fatalf("GetMarketItems failed: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("Expected 3 items, got %d", len(items))
	}

	// Filter by type
	ores, err := kb.GetMarketItems(ctx, "ore")
	if err != nil {
		t.Fatalf("GetMarketItems ore failed: %v", err)
	}
	if len(ores) != 2 {
		t.Errorf("Expected 2 ore items, got %d", len(ores))
	}
}

func TestMemoryKB_ShipListings(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	listings := ShipListings{
		SystemID:    "sys-1",
		StationID:   "station-1",
		StationName: "Station One",
		GameTick:    100,
		Listings: []ShipListing{
			{ShipClass: "shuttle", ShipName: "Shuttle", BasePrice: 1000},
		},
	}
	if err := kb.StoreShipListings(ctx, listings, "agent-1"); err != nil {
		t.Fatalf("StoreShipListings failed: %v", err)
	}

	latest, err := kb.GetLatestShipListings(ctx, "sys-1", "station-1")
	if err != nil {
		t.Fatalf("GetLatestShipListings failed: %v", err)
	}
	if latest == nil {
		t.Fatal("Expected non-nil listings")
	}

	// Nonexistent
	latest, err = kb.GetLatestShipListings(ctx, "nonexistent", "nonexistent")
	if err != nil {
		t.Fatalf("GetLatestShipListings nonexistent failed: %v", err)
	}
	if latest != nil {
		t.Error("Expected nil for nonexistent listings")
	}
}

func TestMemoryKB_HasShipListingsToday(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	has, err := kb.HasShipListingsToday(ctx, "sys-1", "station-1")
	if err != nil {
		t.Fatalf("HasShipListingsToday failed: %v", err)
	}
	if has {
		t.Error("Expected false for empty KB")
	}

	listings := ShipListings{
		SystemID:   "sys-1",
		StationID:  "station-1",
		CapturedAt: time.Now(),
	}
	if err := kb.StoreShipListings(ctx, listings, "agent-1"); err != nil {
		t.Fatalf("StoreShipListings failed: %v", err)
	}

	has, err = kb.HasShipListingsToday(ctx, "sys-1", "station-1")
	if err != nil {
		t.Fatalf("HasShipListingsToday after store failed: %v", err)
	}
	if !has {
		t.Error("Expected true after storing today's listings")
	}
}

func TestMemoryKB_RememberBase(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	base := SpaceBase{
		ID:    "base-1",
		POIID: "poi-1",
		Name:  "Test Base",
	}
	if err := kb.RememberBase(ctx, base); err != nil {
		t.Fatalf("RememberBase failed: %v", err)
	}

	// Get by ID
	retrieved, err := kb.GetBase(ctx, "base-1")
	if err != nil {
		t.Fatalf("GetBase failed: %v", err)
	}
	if retrieved.Name != "Test Base" {
		t.Errorf("Expected name 'Test Base', got '%s'", retrieved.Name)
	}

	// Get by POI
	retrieved, err = kb.GetBaseByPOI(ctx, "poi-1")
	if err != nil {
		t.Fatalf("GetBaseByPOI failed: %v", err)
	}
	if retrieved.ID != "base-1" {
		t.Errorf("Expected ID 'base-1', got '%s'", retrieved.ID)
	}
}

func TestMemoryKB_GetBase_NotFound(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	_, err := kb.GetBase(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent base")
	}
}

func TestMemoryKB_GetBaseByPOI_NotFound(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	_, err := kb.GetBaseByPOI(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent POI")
	}
}

func TestMemoryKB_GetPOIs(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()

	pois := []POI{
		{ID: "poi-1", SystemID: "sys-1", Name: "Station A", Type: "station", Position: game.Position{X: 1, Y: 2}},
		{ID: "poi-2", SystemID: "sys-1", Name: "Asteroid B", Type: "asteroid", Position: game.Position{X: 3, Y: 4}},
		{ID: "poi-3", SystemID: "sys-2", Name: "Station C", Type: "station", Position: game.Position{X: 5, Y: 6}},
	}
	for _, p := range pois {
		if err := kb.RememberPOI(ctx, p); err != nil {
			t.Fatalf("RememberPOI %s failed: %v", p.ID, err)
		}
	}

	sys1POIs, err := kb.GetPOIs(ctx, "sys-1")
	if err != nil {
		t.Fatalf("GetPOIs failed: %v", err)
	}
	if len(sys1POIs) != 2 {
		t.Errorf("Expected 2 POIs in sys-1, got %d", len(sys1POIs))
	}

	// Empty system
	empty, err := kb.GetPOIs(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetPOIs nonexistent failed: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("Expected 0 POIs, got %d", len(empty))
	}
}

func TestMemoryKB_ConcurrentCatalogAccess(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent writes and reads
	for i := range 20 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			items := []CatalogItem{{ID: fmt.Sprintf("item-%d", idx), Name: fmt.Sprintf("Item %d", idx)}}
			_ = kb.StoreItems(ctx, items)
			_, _ = kb.GetItem(ctx, fmt.Sprintf("item-%d", idx))
			_, _ = kb.GetItems(ctx)
		}(i)
	}
	wg.Wait()
}
