package galaxy

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// mockKB implements knowledge.Base for testing
type mockKB struct {
	systems     []knowledge.System
	connections []knowledge.Connection
	connMetrics []knowledge.ConnectionMetric
}

func (m *mockKB) Close() error {
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

func (m *mockKB) StoreMarketSnapshot(ctx context.Context, snapshot knowledge.MarketSnapshot, agentID string) error {
	return nil
}

func (m *mockKB) GetMarketSnapshots(ctx context.Context, systemID, stationID string, limit int) ([]knowledge.MarketSnapshot, error) {
	return nil, nil
}

func (m *mockKB) GetLatestMarketSnapshot(ctx context.Context, systemID, stationID string) (*knowledge.MarketSnapshot, error) {
	return nil, nil
}

func (m *mockKB) GetMarketItems(ctx context.Context, itemType string) ([]string, error) {
	return nil, nil
}

func (m *mockKB) HasMarketSnapshotToday(ctx context.Context, systemID, stationID string) (bool, error) {
	return false, nil
}

func (m *mockKB) StoreMarketAnalysis(ctx context.Context, analysis knowledge.MarketAnalysis, agentID string) error {
	return nil
}

func (m *mockKB) GetLatestMarketAnalysis(ctx context.Context, systemID, stationID string) (*knowledge.MarketAnalysis, error) {
	return nil, nil
}

func (m *mockKB) GetMarketAnalysisHistory(ctx context.Context, systemID, stationID string, limit int) ([]knowledge.MarketAnalysis, error) {
	return nil, nil
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

func (m *mockKB) AnalyzePriceTrends(ctx context.Context, itemID, stationID string, windowHours int) (*knowledge.PriceTrend, error) {
	return nil, nil
}

func (m *mockKB) FindBestPrices(ctx context.Context, itemID string, listingType string, limit int) ([]knowledge.BestPrice, error) {
	return nil, nil
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
