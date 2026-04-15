package knowledge

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestSQLiteKB_RecordResourceState(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	if err := kb.RecordResourceState(ctx, "poi-1", "iron", 0.8, 100.0, 1000, "agent-1"); err != nil {
		t.Fatalf("RecordResourceState failed: %v", err)
	}

	// Record another state at a later tick
	if err := kb.RecordResourceState(ctx, "poi-1", "iron", 0.8, 80.0, 1100, "agent-1"); err != nil {
		t.Fatalf("Second RecordResourceState failed: %v", err)
	}
}

func TestSQLiteKB_GetResourceHistory(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Record multiple states
	for i := range 5 {
		remaining := 100.0 - float64(i)*10.0
		tick := int64(1000 + i*100)
		if err := kb.RecordResourceState(ctx, "poi-1", "iron", 0.8, remaining, tick, "agent-1"); err != nil {
			t.Fatalf("RecordResourceState %d failed: %v", i, err)
		}
	}

	history, err := kb.GetResourceHistory(ctx, "poi-1", "iron", 3)
	if err != nil {
		t.Fatalf("GetResourceHistory failed: %v", err)
	}

	if len(history) != 3 {
		t.Fatalf("Expected 3 history entries, got %d", len(history))
	}

	// Should be ordered by game_tick DESC
	if history[0].GameTick != 1400 {
		t.Errorf("Expected most recent tick 1400, got %d", history[0].GameTick)
	}
	if history[0].Remaining != 60.0 {
		t.Errorf("Expected remaining 60.0, got %f", history[0].Remaining)
	}
}

func TestSQLiteKB_GetResourceHistory_Empty(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	history, err := kb.GetResourceHistory(ctx, "nonexistent", "iron", 10)
	if err != nil {
		t.Fatalf("GetResourceHistory failed: %v", err)
	}

	if len(history) != 0 {
		t.Errorf("Expected 0 history entries, got %d", len(history))
	}
}

func TestSQLiteKB_RecordJourney(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	if err := kb.RecordJourney(ctx, "sys-a", "sys-b", 10.5, 5.0, "agent-1"); err != nil {
		t.Fatalf("RecordJourney failed: %v", err)
	}

	// Record same journey again to test upsert
	if err := kb.RecordJourney(ctx, "sys-a", "sys-b", 12.0, 6.0, "agent-2"); err != nil {
		t.Fatalf("Second RecordJourney failed: %v", err)
	}
}

func TestSQLiteKB_GetOptimalRoute(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// No route data yet
	route, err := kb.GetOptimalRoute(ctx, "sys-a", "sys-b")
	if err != nil {
		t.Fatalf("GetOptimalRoute failed: %v", err)
	}
	if route != nil {
		t.Error("Expected nil route for unknown systems")
	}

	// Record a journey and check
	if err := kb.RecordJourney(ctx, "sys-a", "sys-b", 10.0, 5.0, "agent-1"); err != nil {
		t.Fatalf("RecordJourney failed: %v", err)
	}

	route, err = kb.GetOptimalRoute(ctx, "sys-a", "sys-b")
	if err != nil {
		t.Fatalf("GetOptimalRoute after journey failed: %v", err)
	}
	if route == nil {
		t.Fatal("Expected non-nil route after journey")
	}
	if route.FromSystem != "sys-a" {
		t.Errorf("Expected FromSystem sys-a, got %s", route.FromSystem)
	}
	if route.ToSystem != "sys-b" {
		t.Errorf("Expected ToSystem sys-b, got %s", route.ToSystem)
	}
	if route.TravelCount != 1 {
		t.Errorf("Expected TravelCount 1, got %d", route.TravelCount)
	}
	if route.AvgFuelCost != 10.0 {
		t.Errorf("Expected AvgFuelCost 10.0, got %f", route.AvgFuelCost)
	}
}

func TestSQLiteKB_FindCheapestRoute(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// No route known
	_, _, err := kb.FindCheapestRoute(ctx, "sys-a", "sys-b", 3)
	if err == nil {
		t.Error("Expected error for unknown route")
	}

	// Record a journey
	if err := kb.RecordJourney(ctx, "sys-a", "sys-b", 10.0, 5.0, "agent-1"); err != nil {
		t.Fatalf("RecordJourney failed: %v", err)
	}

	route, cost, err := kb.FindCheapestRoute(ctx, "sys-a", "sys-b", 3)
	if err != nil {
		t.Fatalf("FindCheapestRoute failed: %v", err)
	}
	if len(route) != 2 {
		t.Fatalf("Expected route with 2 hops, got %d", len(route))
	}
	if route[0] != "sys-a" || route[1] != "sys-b" {
		t.Errorf("Expected route [sys-a, sys-b], got %v", route)
	}
	if cost != 10.0 {
		t.Errorf("Expected cost 10.0, got %f", cost)
	}
}

func TestSQLiteKB_RecordAnomaly(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	anomaly := Anomaly{
		Type:        "rich_deposit",
		Severity:    "opportunity",
		SystemID:    "sys-1",
		POIID:       "poi-1",
		Description: "Very rich iron deposit",
		Details:     `{"richness": 0.95}`,
		DetectedBy:  "agent-1",
	}

	if err := kb.RecordAnomaly(ctx, anomaly); err != nil {
		t.Fatalf("RecordAnomaly failed: %v", err)
	}
}

func TestSQLiteKB_GetActiveAnomalies(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Record anomalies in different systems
	for i := range 3 {
		anomaly := Anomaly{
			Type:        "rich_deposit",
			Severity:    "opportunity",
			SystemID:    fmt.Sprintf("sys-%d", i),
			Description: fmt.Sprintf("Anomaly %d", i),
			DetectedBy:  "agent-1",
		}
		if err := kb.RecordAnomaly(ctx, anomaly); err != nil {
			t.Fatalf("RecordAnomaly %d failed: %v", i, err)
		}
	}

	// Get all active anomalies
	anomalies, err := kb.GetActiveAnomalies(ctx, "")
	if err != nil {
		t.Fatalf("GetActiveAnomalies failed: %v", err)
	}
	if len(anomalies) != 3 {
		t.Errorf("Expected 3 anomalies, got %d", len(anomalies))
	}

	// Filter by system
	anomalies, err = kb.GetActiveAnomalies(ctx, "sys-1")
	if err != nil {
		t.Fatalf("GetActiveAnomalies with systemID failed: %v", err)
	}
	if len(anomalies) != 1 {
		t.Errorf("Expected 1 anomaly for sys-1, got %d", len(anomalies))
	}
}

func TestSQLiteKB_GetActiveAnomalies_Empty(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	anomalies, err := kb.GetActiveAnomalies(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetActiveAnomalies failed: %v", err)
	}
	if len(anomalies) != 0 {
		t.Errorf("Expected 0 anomalies, got %d", len(anomalies))
	}
}

func TestSQLiteKB_GetAnomaliesByType(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Record different anomaly types
	types := []struct {
		typ      string
		severity string
	}{
		{"rich_deposit", "opportunity"},
		{"rich_deposit", "info"},
		{"hostile_zone", "warning"},
		{"hostile_zone", "critical"},
		{"rare_resource", "opportunity"},
	}

	for i, tt := range types {
		anomaly := Anomaly{
			Type:        tt.typ,
			Severity:    tt.severity,
			SystemID:    "sys-1",
			Description: fmt.Sprintf("Anomaly %d", i),
			DetectedBy:  "agent-1",
		}
		if err := kb.RecordAnomaly(ctx, anomaly); err != nil {
			t.Fatalf("RecordAnomaly %d failed: %v", i, err)
		}
	}

	// Filter by type only
	anomalies, err := kb.GetAnomaliesByType(ctx, "rich_deposit", "", 10)
	if err != nil {
		t.Fatalf("GetAnomaliesByType failed: %v", err)
	}
	if len(anomalies) != 2 {
		t.Errorf("Expected 2 rich_deposit anomalies, got %d", len(anomalies))
	}

	// Filter by type and severity
	anomalies, err = kb.GetAnomaliesByType(ctx, "hostile_zone", "critical", 10)
	if err != nil {
		t.Fatalf("GetAnomaliesByType with severity failed: %v", err)
	}
	if len(anomalies) != 1 {
		t.Errorf("Expected 1 critical hostile_zone, got %d", len(anomalies))
	}

	// No filters (get all)
	anomalies, err = kb.GetAnomaliesByType(ctx, "", "", 10)
	if err != nil {
		t.Fatalf("GetAnomaliesByType no filters failed: %v", err)
	}
	if len(anomalies) != 5 {
		t.Errorf("Expected 5 anomalies, got %d", len(anomalies))
	}
}

func TestSQLiteKB_ResolveAnomaly(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	anomaly := Anomaly{
		Type:        "rich_deposit",
		Severity:    "opportunity",
		SystemID:    "sys-1",
		Description: "Rich deposit",
		DetectedBy:  "agent-1",
	}
	if err := kb.RecordAnomaly(ctx, anomaly); err != nil {
		t.Fatalf("RecordAnomaly failed: %v", err)
	}

	// Get the anomaly to find its ID
	anomalies, err := kb.GetActiveAnomalies(ctx, "sys-1")
	if err != nil {
		t.Fatalf("GetActiveAnomalies failed: %v", err)
	}
	if len(anomalies) != 1 {
		t.Fatalf("Expected 1 anomaly, got %d", len(anomalies))
	}

	// Resolve it
	if err := kb.ResolveAnomaly(ctx, anomalies[0].ID, "resolved"); err != nil {
		t.Fatalf("ResolveAnomaly failed: %v", err)
	}

	// Should no longer be active
	active, err := kb.GetActiveAnomalies(ctx, "sys-1")
	if err != nil {
		t.Fatalf("GetActiveAnomalies after resolve failed: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("Expected 0 active anomalies after resolve, got %d", len(active))
	}
}

func TestSQLiteKB_RecordHostileEncounter(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Need a system first for the JOIN in GetDangerZones/GetSystemDanger
	sys := System{
		ID:       "sys-1",
		Name:     "Test System",
		Position: game.Position{X: 0, Y: 0},
	}
	if err := kb.RememberSystem(ctx, sys); err != nil {
		t.Fatalf("RememberSystem failed: %v", err)
	}

	if err := kb.RecordHostileEncounter(ctx, "sys-1", "player_kill", "killed by pirate"); err != nil {
		t.Fatalf("RecordHostileEncounter failed: %v", err)
	}

	// Record another encounter (tests upsert)
	if err := kb.RecordHostileEncounter(ctx, "sys-1", "npc_kill", "killed NPC"); err != nil {
		t.Fatalf("Second RecordHostileEncounter failed: %v", err)
	}
}

func TestSQLiteKB_GetDangerZones(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Create systems and add encounters
	for i := range 3 {
		sysID := fmt.Sprintf("sys-%d", i)
		sys := System{
			ID:       sysID,
			Name:     fmt.Sprintf("System %d", i),
			Position: game.Position{X: float64(i), Y: 0},
		}
		if err := kb.RememberSystem(ctx, sys); err != nil {
			t.Fatalf("RememberSystem %d failed: %v", i, err)
		}

		// Add encounters to increase danger level
		for range (i + 1) * 5 {
			if err := kb.RecordHostileEncounter(ctx, sysID, "player_kill", ""); err != nil {
				t.Fatalf("RecordHostileEncounter failed: %v", err)
			}
		}
	}

	// Get all danger zones (min level 1)
	zones, err := kb.GetDangerZones(ctx, 1)
	if err != nil {
		t.Fatalf("GetDangerZones failed: %v", err)
	}
	if len(zones) != 3 {
		t.Errorf("Expected 3 danger zones, got %d", len(zones))
	}

	// Empty result with high threshold
	zones, err = kb.GetDangerZones(ctx, 100)
	if err != nil {
		t.Fatalf("GetDangerZones with high threshold failed: %v", err)
	}
	if len(zones) != 0 {
		t.Errorf("Expected 0 danger zones, got %d", len(zones))
	}
}

func TestSQLiteKB_GetSystemDanger(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	sys := System{
		ID:       "sys-1",
		Name:     "Test System",
		Position: game.Position{X: 0, Y: 0},
	}
	if err := kb.RememberSystem(ctx, sys); err != nil {
		t.Fatalf("RememberSystem failed: %v", err)
	}

	// No encounters yet - should return zero danger for known system
	zone, err := kb.GetSystemDanger(ctx, "sys-1")
	if err != nil {
		t.Fatalf("GetSystemDanger failed: %v", err)
	}
	if zone == nil {
		t.Fatal("Expected non-nil zone for known system")
	}
	if zone.DangerLevel != 0 {
		t.Errorf("Expected danger level 0, got %d", zone.DangerLevel)
	}

	// Unknown system
	zone, err = kb.GetSystemDanger(ctx, "unknown-system")
	if err != nil {
		t.Fatalf("GetSystemDanger for unknown system failed: %v", err)
	}
	if zone != nil {
		t.Error("Expected nil zone for unknown system")
	}

	// Add encounters and check again
	if err := kb.RecordHostileEncounter(ctx, "sys-1", "player_kill", ""); err != nil {
		t.Fatalf("RecordHostileEncounter failed: %v", err)
	}
	zone, err = kb.GetSystemDanger(ctx, "sys-1")
	if err != nil {
		t.Fatalf("GetSystemDanger after encounter failed: %v", err)
	}
	if zone == nil {
		t.Fatal("Expected non-nil zone after encounter")
	}
	if zone.HostileEncounters != 1 {
		t.Errorf("Expected 1 encounter, got %d", zone.HostileEncounters)
	}
	if zone.PlayerKills != 1 {
		t.Errorf("Expected 1 player kill, got %d", zone.PlayerKills)
	}
}

func TestSQLiteKB_ExportKnowledge(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Add some data
	sys := System{
		ID:       "sys-1",
		Name:     "Export System",
		Position: game.Position{X: 10, Y: 20},
	}
	if err := kb.RememberSystem(ctx, sys); err != nil {
		t.Fatalf("RememberSystem failed: %v", err)
	}

	poi := POI{
		ID:       "poi-1",
		SystemID: "sys-1",
		Name:     "Test Station",
		Type:     "station",
		Position: game.Position{X: 1, Y: 2},
	}
	if err := kb.RememberPOI(ctx, poi); err != nil {
		t.Fatalf("RememberPOI failed: %v", err)
	}

	export, err := kb.ExportKnowledge(ctx, "Test export", "agent-1")
	if err != nil {
		t.Fatalf("ExportKnowledge failed: %v", err)
	}

	if export == nil {
		t.Fatal("Expected non-nil export")
	}
	if export.SystemsCount != 1 {
		t.Errorf("Expected 1 system in export, got %d", export.SystemsCount)
	}
	if export.POIsCount != 1 {
		t.Errorf("Expected 1 POI in export, got %d", export.POIsCount)
	}
	if export.ExportData == "" {
		t.Error("Expected non-empty export data")
	}
	if export.CreatedBy != "agent-1" {
		t.Errorf("Expected CreatedBy agent-1, got %s", export.CreatedBy)
	}
}

func TestSQLiteKB_ExportKnowledge_Empty(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	export, err := kb.ExportKnowledge(ctx, "Empty export", "agent-1")
	if err != nil {
		t.Fatalf("ExportKnowledge on empty DB failed: %v", err)
	}

	if export == nil {
		t.Fatal("Expected non-nil export")
	}
	if export.SystemsCount != 0 {
		t.Errorf("Expected 0 systems, got %d", export.SystemsCount)
	}
	if export.POIsCount != 0 {
		t.Errorf("Expected 0 POIs, got %d", export.POIsCount)
	}
}

func TestSQLiteKB_ImportKnowledge(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Import valid JSON
	validJSON := `{"systems":[],"pois":[],"experiences":[]}`
	if err := kb.ImportKnowledge(ctx, validJSON); err != nil {
		t.Fatalf("ImportKnowledge failed: %v", err)
	}

	// Import invalid JSON
	if err := kb.ImportKnowledge(ctx, "invalid json"); err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestSQLiteKB_ListExports(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Empty list
	exports, err := kb.ListExports(ctx)
	if err != nil {
		t.Fatalf("ListExports failed: %v", err)
	}
	if len(exports) != 0 {
		t.Errorf("Expected 0 exports, got %d", len(exports))
	}

	// Create an export
	if _, err := kb.ExportKnowledge(ctx, "Test", "agent-1"); err != nil {
		t.Fatalf("ExportKnowledge failed: %v", err)
	}

	exports, err = kb.ListExports(ctx)
	if err != nil {
		t.Fatalf("ListExports after export failed: %v", err)
	}
	if len(exports) != 1 {
		t.Errorf("Expected 1 export, got %d", len(exports))
	}
	if exports[0].Description != "Test" {
		t.Errorf("Expected description 'Test', got '%s'", exports[0].Description)
	}
}

func TestSQLiteKB_Analytics_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent resource state recording
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			err := kb.RecordResourceState(ctx, "poi-1", "iron", 0.8, float64(100-idx), int64(1000+idx*100), fmt.Sprintf("agent-%d", idx))
			if err != nil {
				t.Errorf("RecordResourceState %d failed: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	history, err := kb.GetResourceHistory(ctx, "poi-1", "iron", 20)
	if err != nil {
		t.Fatalf("GetResourceHistory after concurrent writes failed: %v", err)
	}
	if len(history) != 10 {
		t.Errorf("Expected 10 history entries, got %d", len(history))
	}
}

func TestSQLiteKB_Journey_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent journey recording
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			from := fmt.Sprintf("sys-%d", idx)
			to := fmt.Sprintf("sys-%d", idx+1)
			err := kb.RecordJourney(ctx, from, to, float64(idx+1)*2, float64(idx+1), fmt.Sprintf("agent-%d", idx))
			if err != nil {
				t.Errorf("RecordJourney %d failed: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestSQLiteKB_GetDepletingResources(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Need system and POI for the JOIN in the query
	sys := System{
		ID:       "sys-1",
		Name:     "Test System",
		Position: game.Position{X: 0, Y: 0},
	}
	if err := kb.RememberSystem(ctx, sys); err != nil {
		t.Fatalf("RememberSystem failed: %v", err)
	}

	poi := POI{
		ID:       "poi-1",
		SystemID: "sys-1",
		Name:     "Asteroid Belt",
		Type:     "asteroid",
		Position: game.Position{X: 1, Y: 2},
	}
	if err := kb.RememberPOI(ctx, poi); err != nil {
		t.Fatalf("RememberPOI failed: %v", err)
	}

	// Record resource states showing depletion
	if err := kb.RecordResourceState(ctx, "poi-1", "iron", 0.8, 50.0, 1000, "agent-1"); err != nil {
		t.Fatalf("RecordResourceState 1 failed: %v", err)
	}
	if err := kb.RecordResourceState(ctx, "poi-1", "iron", 0.8, 30.0, 1100, "agent-1"); err != nil {
		t.Fatalf("RecordResourceState 2 failed: %v", err)
	}

	// Query depleting resources with threshold above remaining
	resources, err := kb.GetDepletingResources(ctx, 100.0)
	if err != nil {
		t.Fatalf("GetDepletingResources failed: %v", err)
	}

	// Should find the depleting resource
	if len(resources) == 0 {
		t.Error("Expected at least 1 depleting resource")
	}
}

func TestSQLiteKB_GetDepletingResources_Empty(t *testing.T) {
	t.Parallel()
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	resources, err := kb.GetDepletingResources(ctx, 50.0)
	if err != nil {
		t.Fatalf("GetDepletingResources on empty DB failed: %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("Expected 0 depleting resources, got %d", len(resources))
	}
}
