package galaxy

import (
	"context"
	"fmt"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// setupLargeMockKB creates a mock KB with 500 systems in a grid pattern
func setupLargeMockKB() *mockKB {
	systems := make([]knowledge.System, 500)
	connections := []knowledge.Connection{}

	// Create a 25x20 grid of systems
	idx := 0
	for x := range 25 {
		for y := range 20 {
			systems[idx] = knowledge.System{
				ID:               fmt.Sprintf("sys_%d", idx),
				Name:             fmt.Sprintf("System_%d", idx),
				Position:         game.Position{X: float64(x * 10), Y: float64(y * 10)},
				Empire:           "test_empire",
				LastUpdatedTick:  1000,
			}
			idx++
		}
	}

	// Create horizontal connections
	for x := range 25 {
		for y := range 20 {
			idx := x*20 + y
			if x < 24 { // Connect to next column
				rightIdx := (x+1)*20 + y
				connections = append(connections, knowledge.Connection{
					FromSystem:       systems[idx].ID,
					ToSystem:         systems[rightIdx].ID,
					Distance:         1,
					LastUpdatedTick:  1000,
				})
			}
			if y < 19 { // Connect to next row
				downIdx := x*20 + (y + 1)
				connections = append(connections, knowledge.Connection{
					FromSystem:       systems[idx].ID,
					ToSystem:         systems[downIdx].ID,
					Distance:         1,
					LastUpdatedTick:  1000,
				})
			}
		}
	}

	return &mockKB{
		systems:     systems,
		connections: connections,
	}
}

// loadAllStationSystems returns all system IDs for benchmarking
func loadAllStationSystems() []string {
	targets := make([]string, 500)
	for i := 0; i < 500; i++ {
		targets[i] = fmt.Sprintf("sys_%d", i)
	}
	return targets
}


// mockKB implements knowledge.Base for testing
type mockKB struct {
	systems     []knowledge.System
	connections []knowledge.Connection
	connMetrics []knowledge.ConnectionMetric
}

func (m *mockKB) Close() error {
	return nil
}

func (m *mockKB) RecordSightings(_ []knowledge.SeenPlayer) error {
	return nil
}

func (m *mockKB) RecordPassengers(_ []knowledge.SeenPassenger) error {
	return nil
}

func (m *mockKB) GetSystems(ctx context.Context) ([]knowledge.System, error) {
	return m.systems, nil
}

func (m *mockKB) GetConnections(ctx context.Context) ([]knowledge.Connection, error) {
	return m.connections, nil
}

func (m *mockKB) GetConnectionMetrics(ctx context.Context) ([]knowledge.ConnectionMetric, error) {
	return m.connMetrics, nil
}

func (m *mockKB) RememberSystem(ctx context.Context, sys knowledge.System) error {
	return nil
}

func (m *mockKB) GetSystem(ctx context.Context, systemID string) (*knowledge.System, error) {
	return nil, nil
}

func (m *mockKB) GetUnknownConnections(ctx context.Context, systemID string) ([]string, error) {
	return nil, nil
}

func (m *mockKB) RememberConnection(ctx context.Context, fromSystem, toSystem string) error {
	return nil
}

func (m *mockKB) RememberPOI(ctx context.Context, poi knowledge.POI) error {
	return nil
}

func (m *mockKB) GetPOIs(ctx context.Context, systemID string) ([]knowledge.POI, error) {
	return nil, nil
}

func (m *mockKB) RememberBase(ctx context.Context, base knowledge.SpaceBase) error {
	return nil
}

func (m *mockKB) GetBase(ctx context.Context, baseID string) (*knowledge.SpaceBase, error) {
	return nil, nil
}

func (m *mockKB) GetBaseByPOI(ctx context.Context, poiID string) (*knowledge.SpaceBase, error) {
	return nil, nil
}

func (m *mockKB) AddExperience(ctx context.Context, agentID, expType, description, outcome, location string) error {
	return nil
}

func (m *mockKB) GetRecentExperiences(ctx context.Context, agentID string, limit int) ([]knowledge.Experience, error) {
	return nil, nil
}

func (m *mockKB) RegisterAgent(ctx context.Context, agentID, name, role, faction string, personality []byte) error {
	return nil
}

func (m *mockKB) StoreShipListings(ctx context.Context, listings knowledge.ShipListings, agentID string) error {
	return nil
}

func (m *mockKB) GetShipListings(ctx context.Context, systemID, stationID string, limit int) ([]knowledge.ShipListings, error) {
	return nil, nil
}

func (m *mockKB) GetLatestShipListings(ctx context.Context, systemID, stationID string) (*knowledge.ShipListings, error) {
	return nil, nil
}

func (m *mockKB) HasShipListingsToday(ctx context.Context, systemID, stationID string) (bool, error) {
	return false, nil
}

func (m *mockKB) RecordResourceState(ctx context.Context, poiID, resourceID string, richness, remaining float64, gameTick int64, agentID string) error {
	return nil
}

func (m *mockKB) GetResourceHistory(ctx context.Context, poiID, resourceID string, limit int) ([]knowledge.ResourceHistory, error) {
	return nil, nil
}

func (m *mockKB) GetDepletingResources(ctx context.Context, threshold float64) ([]knowledge.DepletingResource, error) {
	return nil, nil
}

func (m *mockKB) RecordJourney(ctx context.Context, fromSystem, toSystem string, fuelCost, travelTime float64, agentID string) error {
	return nil
}

func (m *mockKB) GetOptimalRoute(ctx context.Context, fromSystem, toSystem string) (*knowledge.ConnectionMetrics, error) {
	return nil, nil
}

func (m *mockKB) FindCheapestRoute(ctx context.Context, fromSystem, toSystem string, maxHops int) ([]string, float64, error) {
	return nil, 0, nil
}

func (m *mockKB) RecordAnomaly(ctx context.Context, anomaly knowledge.Anomaly) error {
	return nil
}

func (m *mockKB) GetActiveAnomalies(ctx context.Context, systemID string) ([]knowledge.Anomaly, error) {
	return nil, nil
}

func (m *mockKB) GetAnomaliesByType(ctx context.Context, anomalyType, severity string, limit int) ([]knowledge.Anomaly, error) {
	return nil, nil
}

func (m *mockKB) ResolveAnomaly(ctx context.Context, anomalyID int64, status string) error {
	return nil
}

func (m *mockKB) GetPriceHistory(ctx context.Context, itemID, stationID string, limit int) ([]knowledge.PricePoint, error) {
	return nil, nil
}

func (m *mockKB) RecordHostileEncounter(ctx context.Context, systemID string, encounterType string, details string) error {
	return nil
}

func (m *mockKB) GetDangerZones(ctx context.Context, minDangerLevel int) ([]knowledge.DangerZone, error) {
	return nil, nil
}

func (m *mockKB) GetSystemDanger(ctx context.Context, systemID string) (*knowledge.DangerZone, error) {
	return nil, nil
}

func (m *mockKB) ExportKnowledge(ctx context.Context, description string, agentID string) (*knowledge.KnowledgeExport, error) {
	return nil, nil
}

func (m *mockKB) ImportKnowledge(ctx context.Context, exportData string) error {
	return nil
}

func (m *mockKB) ListExports(ctx context.Context) ([]knowledge.KnowledgeExportMeta, error) {
	return nil, nil
}

func (m *mockKB) GetSkill(id string) (*knowledge.Skill, error) {
	return nil, nil
}

func (m *mockKB) GetSkills() []knowledge.Skill {
	return nil
}

func (m *mockKB) StoreSkills(ctx context.Context, skills []knowledge.Skill) error {
	return nil
}

func (m *mockKB) StoreItems(ctx context.Context, items []knowledge.CatalogItem) error {
	return nil
}

func (m *mockKB) GetItem(ctx context.Context, itemID string) (*knowledge.CatalogItem, error) {
	return nil, nil
}

func (m *mockKB) GetItems(ctx context.Context) ([]knowledge.CatalogItem, error) {
	return nil, nil
}

func (m *mockKB) GetItemsByCategory(ctx context.Context, category string) ([]knowledge.CatalogItem, error) {
	return nil, nil
}

func (m *mockKB) StoreShipClasses(ctx context.Context, classes []knowledge.ShipClassDef) error {
	return nil
}

func (m *mockKB) GetShipClass(ctx context.Context, classID string) (*knowledge.ShipClassDef, error) {
	return nil, nil
}

func (m *mockKB) GetShipClasses(ctx context.Context) ([]knowledge.ShipClassDef, error) {
	return nil, nil
}

func (m *mockKB) GetShipClassesByCategory(ctx context.Context, category string) ([]knowledge.ShipClassDef, error) {
	return nil, nil
}

func (m *mockKB) StoreRecipes(ctx context.Context, recipes []knowledge.RecipeDef) error {
	return nil
}

func (m *mockKB) GetRecipe(ctx context.Context, recipeID string) (*knowledge.RecipeDef, error) {
	return nil, nil
}

func (m *mockKB) GetRecipes(ctx context.Context) ([]knowledge.RecipeDef, error) {
	return nil, nil
}

func (m *mockKB) GetRecipesByCategory(ctx context.Context, category string) ([]knowledge.RecipeDef, error) {
	return nil, nil
}

func (m *mockKB) StorePlayer(ctx context.Context, player knowledge.PlayerRecord) error {
	return nil
}

func (m *mockKB) GetPlayer(ctx context.Context, playerID string) (*knowledge.PlayerRecord, error) {
	return nil, nil
}

func (m *mockKB) StorePlayerSkills(ctx context.Context, playerID string, skills []knowledge.PlayerSkillRecord) error {
	return nil
}

func (m *mockKB) GetPlayerSkills(ctx context.Context, playerID string) ([]knowledge.PlayerSkillRecord, error) {
	return nil, nil
}

func (m *mockKB) StoreShip(ctx context.Context, ship knowledge.ShipRecord) error {
	return nil
}

func (m *mockKB) GetShip(ctx context.Context, shipID string) (*knowledge.ShipRecord, error) {
	return nil, nil
}

func (m *mockKB) GetPlayerShips(ctx context.Context, playerID string) ([]knowledge.ShipRecord, error) {
	return nil, nil
}

func (m *mockKB) UpdateAgentWalletCredits(ctx context.Context, agentID string, credits int) error {
	return nil
}

func (m *mockKB) StoreStorageSnapshot(ctx context.Context, snapshot knowledge.StorageSnapshot) error {
	return nil
}

func (m *mockKB) GetStorageSnapshot(ctx context.Context, agentID, baseID string) (*knowledge.StorageSnapshot, error) {
	return nil, nil
}

func (m *mockKB) GetAllStorageSnapshots(ctx context.Context) ([]knowledge.StorageSnapshot, error) {
	return nil, nil
}

func (m *mockKB) RecordChangeSnapshot(ctx context.Context, snapshot knowledge.ChangeSnapshot) error {
	return nil
}

func (m *mockKB) GetChangeSnapshots(ctx context.Context, systemID string, limit int) ([]knowledge.ChangeSnapshot, error) {
	return nil, nil
}

func (m *mockKB) RecordXPObservation(ctx context.Context, obs knowledge.XPObservation) error {
	return nil
}

func (m *mockKB) GetXPObservations(ctx context.Context, action string, limit int) ([]knowledge.XPObservation, error) {
	return nil, nil
}

func (m *mockKB) GetXPSummary(ctx context.Context) ([]knowledge.XPSummaryRow, error) {
	return nil, nil
}

func (m *mockKB) UpsertMissionTemplate(ctx context.Context, entry serverapi.MissionBoardEntry, baseID, systemID string, tick int64) (*knowledge.MissionUpsertResult, error) {
	return nil, nil
}

func TestGalaxyGraph_BuildFromDB_ValidationAndMetrics(t *testing.T) {
	ctx := context.Background()

	// Test data with connection metrics
	kb := &mockKB{
		systems: []knowledge.System{
			{ID: "sol", Name: "Sol", Position: game.Position{X: 0, Y: 0}, Empire: "earth", LastUpdatedTick: 1000},
			{ID: "rigel", Name: "Rigel", Position: game.Position{X: 100, Y: 50}, Empire: "nebula", LastUpdatedTick: 1000},
		},
		connections: []knowledge.Connection{
			{FromSystem: "sol", ToSystem: "rigel", Distance: 5, LastUpdatedTick: 1000},
			// This connection should be filtered out due to missing system
			{FromSystem: "sol", ToSystem: "nonexistent", Distance: 10, LastUpdatedTick: 1000},
		},
		connMetrics: []knowledge.ConnectionMetric{
			{FromSystem: "sol", ToSystem: "rigel", AvgFuelCost: 2.5, AvgTravelTime: 30.0},
		},
	}

	g := &GalaxyGraph{}
	err := g.BuildFromDB(ctx, kb)

	if err != nil {
		t.Fatalf("BuildFromDB failed: %v", err)
	}

	// Should only have the 2 valid systems
	if len(g.nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(g.nodes))
	}

	// Check that connection to nonexistent system was filtered
	if _, exists := g.adj["nonexistent"]; exists {
		t.Error("should not have adjacency for nonexistent system")
	}

	// Verify sol has 1 outgoing edge (to rigel)
	solEdges := g.adj["sol"]
	if len(solEdges) != 1 {
		t.Errorf("expected sol to have 1 edge, got %d", len(solEdges))
	}

	// Check that forward edge has metrics
	solToRigel := solEdges[0]
	if solToRigel.FuelCost != 2.5 {
		t.Errorf("expected FuelCost 2.5, got %f", solToRigel.FuelCost)
	}
	if solToRigel.TravelTime != 30.0 {
		t.Errorf("expected TravelTime 30.0, got %f", solToRigel.TravelTime)
	}

	// Check that reverse edge also has metrics (Issue 1 fix)
	rigelEdges := g.adj["rigel"]
	if len(rigelEdges) != 1 {
		t.Errorf("expected rigel to have 1 edge, got %d", len(rigelEdges))
	}

	rigelToSol := rigelEdges[0]
	if rigelToSol.To != "sol" {
		t.Errorf("expected rigel edge to go to sol, got %s", rigelToSol.To)
	}
	if rigelToSol.FuelCost != 2.5 {
		t.Errorf("expected reverse edge FuelCost 2.5, got %f", rigelToSol.FuelCost)
	}
	if rigelToSol.TravelTime != 30.0 {
		t.Errorf("expected reverse edge TravelTime 30.0, got %f", rigelToSol.TravelTime)
	}
}

func TestGalaxyGraph_BuildFromDB_LoadsSystemsAndConnections(t *testing.T) {
	ctx := context.Background()

	kb := &mockKB{
		systems: []knowledge.System{
			{ID: "sol", Name: "Sol", Position: game.Position{X: 0, Y: 0}, Empire: "earth", LastUpdatedTick: 1000},
			{ID: "rigel", Name: "Rigel", Position: game.Position{X: 100, Y: 50}, Empire: "nebula", LastUpdatedTick: 1000},
			{ID: "haven", Name: "Haven", Position: game.Position{X: -50, Y: 75}, Empire: "earth", LastUpdatedTick: 1000},
		},
		connections: []knowledge.Connection{
			{FromSystem: "sol", ToSystem: "rigel", Distance: 5, LastUpdatedTick: 1000},
			{FromSystem: "sol", ToSystem: "haven", Distance: 3, LastUpdatedTick: 1000},
		},
	}

	g := &GalaxyGraph{}
	err := g.BuildFromDB(ctx, kb)

	if err != nil {
		t.Fatalf("BuildFromDB failed: %v", err)
	}

	if len(g.nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(g.nodes))
	}

	if len(g.adj) != 3 {
		t.Errorf("expected 3 adjacency entries, got %d", len(g.adj))
	}

	// Check sol has 2 outgoing edges
	solEdges := g.adj["sol"]
	if len(solEdges) != 2 {
		t.Errorf("expected sol to have 2 edges, got %d", len(solEdges))
	}
}

func TestGalaxyGraph_FindPath_SimpleRoute(t *testing.T) {
	ctx := context.Background()

	kb := &mockKB{
		systems: []knowledge.System{
			{ID: "sol", Name: "Sol", Position: game.Position{X: 0, Y: 0}, Empire: "earth", LastUpdatedTick: 1000},
			{ID: "rigel", Name: "Rigel", Position: game.Position{X: 100, Y: 50}, Empire: "nebula", LastUpdatedTick: 1000},
		},
		connections: []knowledge.Connection{
			{FromSystem: "sol", ToSystem: "rigel", Distance: 5, LastUpdatedTick: 1000},
		},
		connMetrics: []knowledge.ConnectionMetric{
			{FromSystem: "sol", ToSystem: "rigel", AvgFuelCost: 2.5, AvgTravelTime: 30.0},
		},
	}

	g := &GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatalf("BuildFromDB failed: %v", err)
	}

	// Test unweighted (hop count)
	route, err := g.FindPath("sol", "rigel", false)
	if err != nil {
		t.Fatalf("FindPath failed: %v", err)
	}

	if len(route.Path) != 2 {
		t.Errorf("expected path length 2, got %d", len(route.Path))
	}
	if route.Path[0] != "sol" {
		t.Errorf("expected path start 'sol', got %s", route.Path[0])
	}
	if route.Path[1] != "rigel" {
		t.Errorf("expected path end 'rigel', got %s", route.Path[1])
	}
	if route.Hops != 1 {
		t.Errorf("expected 1 hop, got %d", route.Hops)
	}

	// Test weighted (fuel cost)
	routeWeighted, err := g.FindPath("sol", "rigel", true)
	if err != nil {
		t.Fatalf("FindPath weighted failed: %v", err)
	}

	if routeWeighted.TotalFuel != 2.5 {
		t.Errorf("expected TotalFuel 2.5, got %f", routeWeighted.TotalFuel)
	}
	if routeWeighted.TotalTicks != 30 {
		t.Errorf("expected TotalTicks 30, got %d", routeWeighted.TotalTicks)
	}
}

func TestGalaxyGraph_FindPath_MultiHop(t *testing.T) {
	ctx := context.Background()

	kb := &mockKB{
		systems: []knowledge.System{
			{ID: "sol", Name: "Sol", Position: game.Position{X: 0, Y: 0}, Empire: "earth", LastUpdatedTick: 1000},
			{ID: "rigel", Name: "Rigel", Position: game.Position{X: 100, Y: 50}, Empire: "nebula", LastUpdatedTick: 1000},
			{ID: "haven", Name: "Haven", Position: game.Position{X: -50, Y: 75}, Empire: "earth", LastUpdatedTick: 1000},
		},
		connections: []knowledge.Connection{
			{FromSystem: "sol", ToSystem: "rigel", Distance: 5, LastUpdatedTick: 1000},
			{FromSystem: "rigel", ToSystem: "haven", Distance: 3, LastUpdatedTick: 1000},
		},
		connMetrics: []knowledge.ConnectionMetric{
			{FromSystem: "sol", ToSystem: "rigel", AvgFuelCost: 2.5, AvgTravelTime: 30.0},
			{FromSystem: "rigel", ToSystem: "haven", AvgFuelCost: 1.5, AvgTravelTime: 20.0},
		},
	}

	g := &GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatalf("BuildFromDB failed: %v", err)
	}

	route, err := g.FindPath("sol", "haven", false)
	if err != nil {
		t.Fatalf("FindPath failed: %v", err)
	}

	if len(route.Path) != 3 {
		t.Errorf("expected path length 3, got %d", len(route.Path))
	}
	if route.Path[0] != "sol" {
		t.Errorf("expected path start 'sol', got %s", route.Path[0])
	}
	if route.Path[1] != "rigel" {
		t.Errorf("expected path middle 'rigel', got %s", route.Path[1])
	}
	if route.Path[2] != "haven" {
		t.Errorf("expected path end 'haven', got %s", route.Path[2])
	}
	if route.Hops != 2 {
		t.Errorf("expected 2 hops, got %d", route.Hops)
	}

	// Test weighted
	routeWeighted, err := g.FindPath("sol", "haven", true)
	if err != nil {
		t.Fatalf("FindPath weighted failed: %v", err)
	}

	if routeWeighted.TotalFuel != 4.0 {
		t.Errorf("expected TotalFuel 4.0, got %f", routeWeighted.TotalFuel)
	}
	if routeWeighted.TotalTicks != 50 {
		t.Errorf("expected TotalTicks 50, got %d", routeWeighted.TotalTicks)
	}
}

func TestGalaxyGraph_FindPath_Unreachable(t *testing.T) {
	ctx := context.Background()

	kb := &mockKB{
		systems: []knowledge.System{
			{ID: "sol", Name: "Sol", Position: game.Position{X: 0, Y: 0}, Empire: "earth", LastUpdatedTick: 1000},
			{ID: "rigel", Name: "Rigel", Position: game.Position{X: 100, Y: 50}, Empire: "nebula", LastUpdatedTick: 1000},
			{ID: "haven", Name: "Haven", Position: game.Position{X: -50, Y: 75}, Empire: "earth", LastUpdatedTick: 1000},
		},
		connections: []knowledge.Connection{
			{FromSystem: "sol", ToSystem: "rigel", Distance: 5, LastUpdatedTick: 1000},
			// Note: no connection to haven
		},
	}

	g := &GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatalf("BuildFromDB failed: %v", err)
	}

	_, err := g.FindPath("sol", "haven", false)
	if err == nil {
		t.Fatal("expected error for unreachable path, got nil")
	}

	// Verify error message
	if err.Error() != "no path found from sol to haven" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGalaxyGraph_FindNearest_ReturnsTop3ByHops(t *testing.T) {
	ctx := context.Background()

	kb := &mockKB{
		systems: []knowledge.System{
			{ID: "sol", Name: "Sol", Position: game.Position{X: 0, Y: 0}, Empire: "earth", LastUpdatedTick: 1000},
			{ID: "alpha", Name: "Alpha", Position: game.Position{X: 10, Y: 0}, Empire: "earth", LastUpdatedTick: 1000},
			{ID: "beta", Name: "Beta", Position: game.Position{X: 20, Y: 0}, Empire: "earth", LastUpdatedTick: 1000},
			{ID: "gamma", Name: "Gamma", Position: game.Position{X: 30, Y: 0}, Empire: "earth", LastUpdatedTick: 1000},
			{ID: "delta", Name: "Delta", Position: game.Position{X: 40, Y: 0}, Empire: "earth", LastUpdatedTick: 1000},
		},
		connections: []knowledge.Connection{
			{FromSystem: "sol", ToSystem: "alpha", Distance: 1, LastUpdatedTick: 1000},
			{FromSystem: "alpha", ToSystem: "beta", Distance: 1, LastUpdatedTick: 1000},
			{FromSystem: "beta", ToSystem: "gamma", Distance: 1, LastUpdatedTick: 1000},
			{FromSystem: "gamma", ToSystem: "delta", Distance: 1, LastUpdatedTick: 1000},
		},
	}

	g := &GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatalf("BuildFromDB failed: %v", err)
	}

	targets := []string{"alpha", "beta", "gamma", "delta"}
	results, err := g.FindNearest("sol", targets, 3)

	if err != nil {
		t.Fatalf("FindNearest failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// Check sorted by hops
	if results[0].SystemID != "alpha" || results[0].Hops != 1 {
		t.Errorf("expected first result to be alpha with 1 hop, got %s with %d hops", results[0].SystemID, results[0].Hops)
	}
	if results[1].SystemID != "beta" || results[1].Hops != 2 {
		t.Errorf("expected second result to be beta with 2 hops, got %s with %d hops", results[1].SystemID, results[1].Hops)
	}
	if results[2].SystemID != "gamma" || results[2].Hops != 3 {
		t.Errorf("expected third result to be gamma with 3 hops, got %s with %d hops", results[2].SystemID, results[2].Hops)
	}
}

func TestGalaxyGraph_FindNearest_EmptyTargets(t *testing.T) {
	ctx := context.Background()

	kb := &mockKB{
		systems: []knowledge.System{
			{ID: "sol", Name: "Sol", Position: game.Position{X: 0, Y: 0}, Empire: "earth", LastUpdatedTick: 1000},
		},
	}

	g := &GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		t.Fatalf("BuildFromDB failed: %v", err)
	}

	results, err := g.FindNearest("sol", []string{}, 5)
	if err != nil {
		t.Fatalf("FindNearest failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results for empty targets, got %d", len(results))
	}
}

func BenchmarkGalaxyGraphBuild(b *testing.B) {
	ctx := context.Background()
	kb := setupLargeMockKB() // 500 systems, ~950 connections
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g := &GalaxyGraph{}
		if err := g.BuildFromDB(ctx, kb); err != nil {
			b.Fatalf("BuildFromDB failed: %v", err)
		}
	}
}

func BenchmarkFindNearest(b *testing.B) {
	ctx := context.Background()
	kb := setupLargeMockKB()
	g := &GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		b.Fatalf("BuildFromDB failed: %v", err)
	}
	targets := loadAllStationSystems()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := g.FindNearest("sys_0", targets, 3)
		if err != nil {
			b.Fatalf("FindNearest failed: %v", err)
		}
	}
}

func BenchmarkFindPath(b *testing.B) {
	ctx := context.Background()
	kb := setupLargeMockKB()
	g := &GalaxyGraph{}
	if err := g.BuildFromDB(ctx, kb); err != nil {
		b.Fatalf("BuildFromDB failed: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := g.FindPath("sys_0", "sys_250", false)
		if err != nil {
			b.Fatalf("FindPath failed: %v", err)
		}
	}
}

