package skills

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// TestClientDispatcher_RouteField verifies the Route field exists and can be set
func TestClientDispatcher_RouteField(t *testing.T) {
	dispatcher := NewClientDispatcher(nil, "", "", log.New(os.Stderr, "[test] ", 0))

	// Route should be nil initially
	if dispatcher.Route != nil {
		t.Error("Expected Route to be nil initially")
	}

	// Can set Route
	dispatcher.Route = &RouteProgress{
		DestinationSystem: "test",
		Route: []RouteStep{
			{SystemID: "haven", Name: "Haven", Jumps: 1},
			{SystemID: "crimson", Name: "Crimson", Jumps: 2},
		},
		CurrentStep: 0,
		Timestamp:   time.Now(),
	}

	if dispatcher.Route == nil {
		t.Error("Expected Route to be non-nil after setting")
	}

	// Verify route structure
	if dispatcher.Route.DestinationSystem != "test" {
		t.Errorf("DestinationSystem = %s, want test", dispatcher.Route.DestinationSystem)
	}
	if len(dispatcher.Route.Route) != 2 {
		t.Errorf("Route length = %d, want 2", len(dispatcher.Route.Route))
	}
	if dispatcher.Route.Route[0].SystemID != "haven" {
		t.Errorf("First step system_id = %s, want haven", dispatcher.Route.Route[0].SystemID)
	}
	if dispatcher.Route.Route[1].SystemID != "crimson" {
		t.Errorf("Second step system_id = %s, want crimson", dispatcher.Route.Route[1].SystemID)
	}
	if dispatcher.Route.CurrentStep != 0 {
		t.Errorf("CurrentStep = %d, want 0", dispatcher.Route.CurrentStep)
	}
}

// TestDispatch_findRouteStruct verifies route structure can be created and stored
func TestDispatch_findRouteStruct(t *testing.T) {
	dispatcher := &ClientDispatcher{
		Client:  nil,
		Logger:  log.New(os.Stderr, "[test] ", 0),
		baseDir: "",
		agentID: "",
	}

	// Test that Route field can be populated with proper structure
	dispatcher.Route = &RouteProgress{
		DestinationSystem: "haven",
		Route: []RouteStep{
			{SystemID: "haven", Name: "Haven", Jumps: 1},
			{SystemID: "crimson", Name: "Crimson", Jumps: 2},
		},
		CurrentStep: 0,
		Timestamp:   time.Now(),
	}

	if dispatcher.Route == nil {
		t.Error("Expected Route to be set")
	}
	if dispatcher.Route.DestinationSystem != "haven" {
		t.Errorf("DestinationSystem = %s, want haven", dispatcher.Route.DestinationSystem)
	}
	if len(dispatcher.Route.Route) != 2 {
		t.Errorf("Route length = %d, want 2", len(dispatcher.Route.Route))
	}
}

// TestDispatch_storeRouteProgress verifies store_route_progress action
func TestDispatch_storeRouteProgress(t *testing.T) {
	tmpDir := t.TempDir()
	agentID := "test-agent"

	dispatcher := &ClientDispatcher{
		Client:  nil,
		Logger:  log.New(os.Stderr, "[test] ", 0),
		baseDir: tmpDir,
		agentID: agentID,
		Route: &RouteProgress{
			DestinationSystem: "crimson",
			Route: []RouteStep{
				{SystemID: "haven", Name: "Haven", Jumps: 1},
				{SystemID: "crimson", Name: "Crimson", Jumps: 0},
			},
			CurrentStep: 1,
			Timestamp:   time.Now(),
		},
	}

	ctx := context.Background()
	err := dispatcher.Dispatch(ctx, "store_route_progress", "")
	if err != nil {
		t.Fatalf("store_route_progress failed: %v", err)
	}

	// Verify file was created
	routeFile := filepath.Join(tmpDir, "agents", agentID, "route.json")
	if _, err := os.Stat(routeFile); os.IsNotExist(err) {
		t.Errorf("Route file not created at %s", routeFile)
	}
}

// TestDispatch_storeRouteProgress_noRoute verifies error when no route is set
func TestDispatch_storeRouteProgress_noRoute(t *testing.T) {
	tmpDir := t.TempDir()
	agentID := "test-agent"

	dispatcher := &ClientDispatcher{
		Client:  nil,
		Logger:  log.New(os.Stderr, "[test] ", 0),
		baseDir: tmpDir,
		agentID: agentID,
		Route:   nil,
	}

	ctx := context.Background()
	err := dispatcher.Dispatch(ctx, "store_route_progress", "")
	if err == nil {
		t.Fatal("Expected error when storing nil route, got nil")
	}
	if err.Error() != "no route to store" {
		t.Errorf("Error = %q, want %q", err.Error(), "no route to store")
	}
}

// TestDispatch_loadRouteProgress verifies load_route_progress action
func TestDispatch_loadRouteProgress(t *testing.T) {
	tmpDir := t.TempDir()
	agentID := "test-agent"

	// Create a mock client with test state at crimson (step 1)
	mockClient := &mockGameClient{
		state: &game.State{
			System: game.SystemData{
				ID: "crimson",
			},
		},
	}

	// First, save a route with CurrentStep at 1 (crimson)
	route := &RouteProgress{
		DestinationSystem: "crimson",
		Route: []RouteStep{
			{SystemID: "haven", Name: "Haven", Jumps: 1},
			{SystemID: "crimson", Name: "Crimson", Jumps: 0},
		},
		CurrentStep: 1,
		Timestamp:   time.Now(),
	}
	if err := SaveRouteProgress(tmpDir, agentID, route); err != nil {
		t.Fatalf("Failed to save test route: %v", err)
	}

	dispatcher := &ClientDispatcher{
		Client:  mockClient,
		Logger:  log.New(os.Stderr, "[test] ", 0),
		baseDir: tmpDir,
		agentID: agentID,
		Route:   nil,
	}

	ctx := context.Background()
	err := dispatcher.Dispatch(ctx, "load_route_progress", "")
	if err != nil {
		t.Fatalf("load_route_progress failed: %v", err)
	}

	// Verify route was loaded
	if dispatcher.Route == nil {
		t.Fatal("Route not loaded")
	}
	if dispatcher.Route.DestinationSystem != "crimson" {
		t.Errorf("DestinationSystem = %s, want crimson", dispatcher.Route.DestinationSystem)
	}
	if dispatcher.Route.CurrentStep != 1 {
		t.Errorf("CurrentStep = %d, want 1", dispatcher.Route.CurrentStep)
	}
}

// TestDispatch_loadRouteProgress_positionAdjustment verifies step adjustment when position doesn't match
func TestDispatch_loadRouteProgress_positionAdjustment(t *testing.T) {
	tmpDir := t.TempDir()
	agentID := "test-agent"

	// Create a mock client with test state at different position
	mockClient := &mockGameClient{
		state: &game.State{
			System: game.SystemData{
				ID: "crimson", // Agent is at step 1, but route says step 0
			},
		},
	}

	// Save a route with CurrentStep at 0 (haven)
	route := &RouteProgress{
		DestinationSystem: "crimson",
		Route: []RouteStep{
			{SystemID: "haven", Name: "Haven", Jumps: 1},
			{SystemID: "crimson", Name: "Crimson", Jumps: 0},
		},
		CurrentStep: 0,
		Timestamp:   time.Now(),
	}
	if err := SaveRouteProgress(tmpDir, agentID, route); err != nil {
		t.Fatalf("Failed to save test route: %v", err)
	}

	dispatcher := &ClientDispatcher{
		Client:  mockClient,
		Logger:  log.New(os.Stderr, "[test] ", 0),
		baseDir: tmpDir,
		agentID: agentID,
		Route:   nil,
	}

	ctx := context.Background()
	err := dispatcher.Dispatch(ctx, "load_route_progress", "")
	if err != nil {
		t.Fatalf("load_route_progress failed: %v", err)
	}

	// Verify step was adjusted to 1 (crimson)
	if dispatcher.Route.CurrentStep != 1 {
		t.Errorf("CurrentStep = %d, want 1 (adjusted)", dispatcher.Route.CurrentStep)
	}
}

// TestDispatch_loadRouteProgress_positionNotInRoute verifies error when current position not in route
func TestDispatch_loadRouteProgress_positionNotInRoute(t *testing.T) {
	tmpDir := t.TempDir()
	agentID := "test-agent"

	// Create a mock client with test state at system not in route
	mockClient := &mockGameClient{
		state: &game.State{
			System: game.SystemData{
				ID: "void", // Not in the route
			},
		},
	}

	// Save a route
	route := &RouteProgress{
		DestinationSystem: "crimson",
		Route: []RouteStep{
			{SystemID: "haven", Name: "Haven", Jumps: 1},
			{SystemID: "crimson", Name: "Crimson", Jumps: 0},
		},
		CurrentStep: 0,
		Timestamp:   time.Now(),
	}
	if err := SaveRouteProgress(tmpDir, agentID, route); err != nil {
		t.Fatalf("Failed to save test route: %v", err)
	}

	dispatcher := &ClientDispatcher{
		Client:  mockClient,
		Logger:  log.New(os.Stderr, "[test] ", 0),
		baseDir: tmpDir,
		agentID: agentID,
		Route:   nil,
	}

	ctx := context.Background()
	err := dispatcher.Dispatch(ctx, "load_route_progress", "")
	if err == nil {
		t.Fatal("Expected error when current system not in route, got nil")
	}
}

// TestDispatch_clearRouteProgress verifies clear_route_progress action
func TestDispatch_clearRouteProgress(t *testing.T) {
	tmpDir := t.TempDir()
	agentID := "test-agent"

	// Create a route file first
	route := &RouteProgress{
		DestinationSystem: "crimson",
		Route: []RouteStep{
			{SystemID: "haven", Name: "Haven", Jumps: 1},
		},
		CurrentStep: 0,
		Timestamp:   time.Now(),
	}
	if err := SaveRouteProgress(tmpDir, agentID, route); err != nil {
		t.Fatalf("Failed to save test route: %v", err)
	}

	dispatcher := &ClientDispatcher{
		Client:  nil,
		Logger:  log.New(os.Stderr, "[test] ", 0),
		baseDir: tmpDir,
		agentID: agentID,
		Route:   route,
	}

	ctx := context.Background()
	err := dispatcher.Dispatch(ctx, "clear_route_progress", "")
	if err != nil {
		t.Fatalf("clear_route_progress failed: %v", err)
	}

	// Verify route was cleared from memory
	if dispatcher.Route != nil {
		t.Error("Route not cleared from memory")
	}

	// Verify file was deleted
	routeFile := filepath.Join(tmpDir, "agents", agentID, "route.json")
	if _, err := os.Stat(routeFile); !os.IsNotExist(err) {
		t.Errorf("Route file still exists at %s", routeFile)
	}
}

// TestDispatch_findPOIInSystem verifies find_poi_in_system action
func TestDispatch_findPOIInSystem(t *testing.T) {
	// Create a mock client with test system containing POIs
	mockClient := &mockGameClient{
		state: &game.State{
			System: game.SystemData{
				ID:   "haven",
				Name: "Haven",
				POIs: []game.POI{
					{ID: "poi-1", Name: "Haven Station", Type: "station"},
					{ID: "poi-2", Name: "Asteroid Belt", Type: "asteroid"},
				},
			},
		},
	}

	dispatcher := &ClientDispatcher{
		Client:   mockClient,
		Logger:   log.New(os.Stderr, "[test] ", 0),
		baseDir:  "",
		agentID:  "",
		FoundPOI: "",
	}

	ctx := context.Background()

	// Test finding exact match
	err := dispatcher.Dispatch(ctx, "find_poi_in_system", "Haven Station")
	if err != nil {
		t.Fatalf("find_poi_in_system failed: %v", err)
	}
	if dispatcher.FoundPOI != "poi-1" {
		t.Errorf("FoundPOI = %s, want poi-1", dispatcher.FoundPOI)
	}

	// Test case-insensitive match
	dispatcher.FoundPOI = ""
	err = dispatcher.Dispatch(ctx, "find_poi_in_system", "haven station")
	if err != nil {
		t.Fatalf("find_poi_in_system (case-insensitive) failed: %v", err)
	}
	if dispatcher.FoundPOI != "poi-1" {
		t.Errorf("FoundPOI = %s, want poi-1", dispatcher.FoundPOI)
	}

	// Test finding another POI
	dispatcher.FoundPOI = ""
	err = dispatcher.Dispatch(ctx, "find_poi_in_system", "Asteroid Belt")
	if err != nil {
		t.Fatalf("find_poi_in_system (asteroid) failed: %v", err)
	}
	if dispatcher.FoundPOI != "poi-2" {
		t.Errorf("FoundPOI = %s, want poi-2", dispatcher.FoundPOI)
	}
}

// TestDispatch_findPOIInSystem_notFound verifies error when POI not found
func TestDispatch_findPOIInSystem_notFound(t *testing.T) {
	mockClient := &mockGameClient{
		state: &game.State{
			System: game.SystemData{
				ID:   "haven",
				Name: "Haven",
				POIs: []game.POI{
					{ID: "poi-1", Name: "Haven Station", Type: "station"},
				},
			},
		},
	}

	dispatcher := &ClientDispatcher{
		Client:   mockClient,
		Logger:   log.New(os.Stderr, "[test] ", 0),
		baseDir:  "",
		agentID:  "",
		FoundPOI: "",
	}

	ctx := context.Background()
	err := dispatcher.Dispatch(ctx, "find_poi_in_system", "Nonexistent POI")
	if err == nil {
		t.Fatal("Expected error when POI not found, got nil")
	}
}

// TestDispatch_findPOIInSystem_requiresTarget verifies error when no target specified
func TestDispatch_findPOIInSystem_requiresTarget(t *testing.T) {
	dispatcher := &ClientDispatcher{
		Client:   &mockGameClient{},
		Logger:   log.New(os.Stderr, "[test] ", 0),
		baseDir:  "",
		agentID:  "",
		FoundPOI: "",
	}

	ctx := context.Background()
	err := dispatcher.Dispatch(ctx, "find_poi_in_system", "")
	if err == nil {
		t.Fatal("Expected error when no target specified, got nil")
	}
}

// mockGameClient is a minimal implementation of game.GameClient for testing
// Most methods return nil (no-op) except GetState which returns the configured state
type mockGameClient struct {
	state *game.State
}

func (m *mockGameClient) GetState() *game.State {
	if m.state == nil {
		return &game.State{}
	}
	return m.state
}

// Implement all other required methods from game.GameClient interface (no-op stubs)
func (m *mockGameClient) Connect(ctx context.Context) error { return nil }
func (m *mockGameClient) Close() error                      { return nil }
func (m *mockGameClient) IsConnected() bool                 { return true }
func (m *mockGameClient) Ready() <-chan struct{}            { return nil }
func (m *mockGameClient) Login(ctx context.Context) error   { return nil }
func (m *mockGameClient) Register(ctx context.Context, empire, registrationCode string) error {
	return nil
}
func (m *mockGameClient) Undock(ctx context.Context) error { return nil }
func (m *mockGameClient) Dock(ctx context.Context) error   { return nil }
func (m *mockGameClient) Travel(ctx context.Context, targetPOI string) (*game.TravelResult, error) {
	return nil, nil
}
func (m *mockGameClient) Jump(ctx context.Context, targetSystem string) (*game.JumpResult, error) {
	return nil, nil
}
func (m *mockGameClient) Mine(ctx context.Context) error                                  { return nil }
func (m *mockGameClient) Scan(ctx context.Context) error                                  { return nil }
func (m *mockGameClient) ScanTarget(ctx context.Context, targetID string) error           { return nil }
func (m *mockGameClient) Attack(ctx context.Context, targetID string) error               { return nil }
func (m *mockGameClient) Cloak(ctx context.Context, enable bool) error                    { return nil }
func (m *mockGameClient) Sell(ctx context.Context, itemID string, quantity float64) error { return nil }
func (m *mockGameClient) SellAllBulk(ctx context.Context, reservedItems []string) error   { return nil }
func (m *mockGameClient) Buy(ctx context.Context, itemID string, quantity float64) error  { return nil }
func (m *mockGameClient) GetListings(ctx context.Context) error                           { return nil }
func (m *mockGameClient) GetTrades(ctx context.Context) error                             { return nil }
func (m *mockGameClient) CraftWithQuantity(ctx context.Context, recipeID string, quantity int) error {
	return nil
}
func (m *mockGameClient) CraftWithOptions(ctx context.Context, recipeID string, quantity int, deliverTo string) error {
	return nil
}
func (m *mockGameClient) CraftBulk(ctx context.Context, jobs []map[string]any) error { return nil }
func (m *mockGameClient) GetRecipes(ctx context.Context) error                            { return nil }
func (m *mockGameClient) Recycle(ctx context.Context, recipeID string, quantity int) error { return nil }
func (m *mockGameClient) RecycleWithOptions(ctx context.Context, recipeID string, quantity int, deliverTo string) error {
	return nil
}
func (m *mockGameClient) Refuel(ctx context.Context) error                                { return nil }
func (m *mockGameClient) Repair(ctx context.Context) error                                { return nil }
func (m *mockGameClient) RepairWith(ctx context.Context, payload map[string]any) error    { return nil }
func (m *mockGameClient) Fleet(ctx context.Context, action string, playerID string) error { return nil }
func (m *mockGameClient) DistressSignal(ctx context.Context, distressType string) error   { return nil }
func (m *mockGameClient) Install(ctx context.Context, itemID string) error                { return nil }
func (m *mockGameClient) RefitShip(ctx context.Context) error                             { return nil }
func (m *mockGameClient) InstallMod(ctx context.Context, moduleID string) error           { return nil }
func (m *mockGameClient) UninstallMod(ctx context.Context, moduleID string) error         { return nil }
func (m *mockGameClient) BuyShip(ctx context.Context, shipClass string) error             { return nil }
func (m *mockGameClient) BrowseShips(ctx context.Context, payload map[string]any) error   { return nil }
func (m *mockGameClient) BuyInsurance(ctx context.Context, ticks int) error               { return nil }
func (m *mockGameClient) ClaimInsurance(ctx context.Context) error                        { return nil }
func (m *mockGameClient) DepositItems(ctx context.Context, itemID string, quantity float64) error {
	return nil
}
func (m *mockGameClient) DepositItemsPayload(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) DepositAllItems(ctx context.Context) error { return nil }
func (m *mockGameClient) WithdrawItems(ctx context.Context, itemID string, quantity float64) error {
	return nil
}
func (m *mockGameClient) WithdrawItemsPayload(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) ViewStorage(ctx context.Context) error                     { return nil }
func (m *mockGameClient) ViewStorageAt(ctx context.Context, stationID string) error { return nil }
func (m *mockGameClient) GetWrecks(ctx context.Context) error                       { return nil }
func (m *mockGameClient) LootWreck(ctx context.Context, wreckID, itemID string, quantity float64) error {
	return nil
}
func (m *mockGameClient) SalvageWreck(ctx context.Context, wreckID string) error { return nil }
func (m *mockGameClient) GetSystem(ctx context.Context) error                    { return nil }
func (m *mockGameClient) GetStatus(ctx context.Context) error                    { return nil }
func (m *mockGameClient) GetNotifications(ctx context.Context) error             { return nil }
func (m *mockGameClient) GetShip(ctx context.Context) error                      { return nil }
func (m *mockGameClient) GetCargo(ctx context.Context) error                     { return nil }
func (m *mockGameClient) GetSkills(ctx context.Context) error                    { return nil }
func (m *mockGameClient) GetPOI(ctx context.Context) error                       { return nil }
func (m *mockGameClient) GetBase(ctx context.Context) error                      { return nil }
func (m *mockGameClient) GetMap(ctx context.Context, force ...bool) error        { return nil }
func (m *mockGameClient) GetNearby(ctx context.Context) error                    { return nil }
func (m *mockGameClient) GetSystemAgents(ctx context.Context) error              { return nil }
func (m *mockGameClient) GetVersion(ctx context.Context) error                   { return nil }
func (m *mockGameClient) GetCommands(ctx context.Context) error                  { return nil }
func (m *mockGameClient) GetActiveMissions(ctx context.Context) error            { return nil }
func (m *mockGameClient) GetInsuranceQuote(ctx context.Context) error            { return nil }
func (m *mockGameClient) Help(ctx context.Context, payload map[string]any) error { return nil }
func (m *mockGameClient) FactionInfo(ctx context.Context) error                  { return nil }
func (m *mockGameClient) CaptainsLogList(ctx context.Context) error              { return nil }
func (m *mockGameClient) Catalog(ctx context.Context, catalogType string, page, pageSize int) error {
	return nil
}
func (m *mockGameClient) FindRoute(ctx context.Context, targetSystem string) ([]game.RouteStep, error) {
	return nil, nil
}
func (m *mockGameClient) CreateFaction(ctx context.Context, payload map[string]any) error { return nil }
func (m *mockGameClient) JoinFaction(ctx context.Context, factionID string) error         { return nil }
func (m *mockGameClient) LeaveFaction(ctx context.Context) error                          { return nil }
func (m *mockGameClient) FactionInvite(ctx context.Context, playerID string) error        { return nil }
func (m *mockGameClient) FactionKick(ctx context.Context, playerID string) error          { return nil }
func (m *mockGameClient) FactionPromote(ctx context.Context, playerID, roleID string) error {
	return nil
}
func (m *mockGameClient) Chat(ctx context.Context, channel, content string, targetID string) error {
	return nil
}
func (m *mockGameClient) GetChatHistory(ctx context.Context, channel string, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) Jettison(ctx context.Context, itemID string, quantity float64) error {
	return nil
}
func (m *mockGameClient) SetPlayerStatus(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) SetHomeBase(ctx context.Context, baseID string) error { return nil }
func (m *mockGameClient) ForumList(ctx context.Context, page int) error        { return nil }
func (m *mockGameClient) ForumCreateThread(ctx context.Context, title, content string, category string) error {
	return nil
}
func (m *mockGameClient) ForumGetThread(ctx context.Context, threadID string) error      { return nil }
func (m *mockGameClient) ForumReply(ctx context.Context, threadID, content string) error { return nil }
func (m *mockGameClient) ForumUpvote(ctx context.Context, threadID string, replyID string) error {
	return nil
}
func (m *mockGameClient) ForumDeleteThread(ctx context.Context, threadID string) error { return nil }
func (m *mockGameClient) ForumDeleteReply(ctx context.Context, replyID string) error   { return nil }
func (m *mockGameClient) CreateNote(ctx context.Context, title, content string) error  { return nil }
func (m *mockGameClient) WriteNote(ctx context.Context, noteID, content string) error  { return nil }
func (m *mockGameClient) GetNotes(ctx context.Context) error                           { return nil }
func (m *mockGameClient) GetMarketListings() []game.MarketListing                      { return nil }
func (m *mockGameClient) GetRawJSON(key string) []byte                                 { return nil }
func (m *mockGameClient) ListShips(ctx context.Context) error                          { return nil }
func (m *mockGameClient) SwitchShip(ctx context.Context, shipID string) error          { return nil }
func (m *mockGameClient) SellShip(ctx context.Context, shipID string) error            { return nil }
func (m *mockGameClient) CreateSellOrder(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) CreateBuyOrder(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) ViewMarket(ctx context.Context, payload map[string]any) error   { return nil }
func (m *mockGameClient) ViewOrders(ctx context.Context) error                           { return nil }
func (m *mockGameClient) GetActionLog(ctx context.Context, payload map[string]any) error { return nil }
func (m *mockGameClient) GetMissions(ctx context.Context) error                          { return nil }
func (m *mockGameClient) AcceptMission(ctx context.Context, missionID string) error      { return nil }
func (m *mockGameClient) CompleteMission(ctx context.Context, missionID string) error    { return nil }
func (m *mockGameClient) AbandonMission(ctx context.Context, missionID string) error     { return nil }
func (m *mockGameClient) DeclineMission(ctx context.Context, templateID string) error    { return nil }
func (m *mockGameClient) SurveySystem(ctx context.Context) error                         { return nil }
func (m *mockGameClient) CaptainsLogAdd(ctx context.Context, entry string) error         { return nil }
func (m *mockGameClient) CaptainsLogGet(ctx context.Context, index int) error            { return nil }
func (m *mockGameClient) RawCommand(ctx context.Context, cmd string, args map[string]any) error {
	return nil
}

func (m *mockGameClient) ListStationPassengers(_ context.Context, _ string) (*serverapi.ListStationPassengersResponse, error) {
	return &serverapi.ListStationPassengersResponse{}, nil
}

func (m *mockGameClient) LoadPassenger(_ context.Context, _ string) (*serverapi.LoadPassengerResponse, error) {
	return &serverapi.LoadPassengerResponse{}, nil
}

// Combat extras
func (m *mockGameClient) Battle(ctx context.Context, action string, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) GetBattleStatus(_ context.Context) error { return nil }
func (m *mockGameClient) Reload(ctx context.Context, weaponInstanceID, ammoItemID string) error {
	return nil
}

// Commerce extras
func (m *mockGameClient) EstimatePurchase(ctx context.Context, itemID string, quantity int) error {
	return nil
}
func (m *mockGameClient) CancelOrder(ctx context.Context, payload map[string]any) error { return nil }
func (m *mockGameClient) ModifyOrder(ctx context.Context, payload map[string]any) error { return nil }
func (m *mockGameClient) TradeOffer(ctx context.Context, targetID string, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) TradeAccept(ctx context.Context, tradeID string) error  { return nil }
func (m *mockGameClient) TradeCancel(ctx context.Context, tradeID string) error  { return nil }
func (m *mockGameClient) TradeDecline(ctx context.Context, tradeID string) error { return nil }

// Ship Management extras
func (m *mockGameClient) BuyListedShip(ctx context.Context, listingID string) error { return nil }
func (m *mockGameClient) ListShipForSale(ctx context.Context, shipID string, price float64) error {
	return nil
}
func (m *mockGameClient) CommissionQuote(ctx context.Context, shipClass string) error     { return nil }
func (m *mockGameClient) CommissionStatus(ctx context.Context, baseID string) error       { return nil }
func (m *mockGameClient) CancelCommission(ctx context.Context, commissionID string) error { return nil }
func (m *mockGameClient) CommissionShip(ctx context.Context, shipClass string, provideMaterials bool) error {
	return nil
}

// Wrecks extras
func (m *mockGameClient) TowWreck(ctx context.Context, wreckID string) error             { return nil }
func (m *mockGameClient) UseItem(ctx context.Context, itemID string, quantity int) error { return nil }

// Data collection extras
func (m *mockGameClient) SearchSystems(ctx context.Context, query string) error { return nil }
func (m *mockGameClient) GetGuide(ctx context.Context, guide string) error      { return nil }

// Faction extras
func (m *mockGameClient) FactionList(ctx context.Context, limit, offset int) error      { return nil }
func (m *mockGameClient) FactionEdit(ctx context.Context, payload map[string]any) error { return nil }
func (m *mockGameClient) FactionGetInvites(ctx context.Context) error                   { return nil }
func (m *mockGameClient) FactionDeclineInvite(ctx context.Context, factionID string) error {
	return nil
}
func (m *mockGameClient) FactionDeclareWar(ctx context.Context, targetFactionID, reason string) error {
	return nil
}
func (m *mockGameClient) FactionProposePeace(ctx context.Context, targetFactionID, terms string) error {
	return nil
}
func (m *mockGameClient) FactionAcceptPeace(ctx context.Context, targetFactionID string) error {
	return nil
}
func (m *mockGameClient) FactionProposeAlly(ctx context.Context, targetFactionID string) error {
	return nil
}
func (m *mockGameClient) FactionAcceptAlly(ctx context.Context, targetFactionID string) error {
	return nil
}
func (m *mockGameClient) FactionRemoveAlly(ctx context.Context, targetFactionID string) error {
	return nil
}
func (m *mockGameClient) FactionSetEnemy(ctx context.Context, targetFactionID string) error {
	return nil
}
func (m *mockGameClient) FactionDepositCredits(ctx context.Context, amount float64) error { return nil }
func (m *mockGameClient) FactionWithdrawCredits(ctx context.Context, amount float64) error {
	return nil
}
func (m *mockGameClient) FactionDepositItems(ctx context.Context, itemID string, quantity int) error {
	return nil
}
func (m *mockGameClient) FactionDepositItemsPayload(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) FactionWithdrawItems(ctx context.Context, itemID string, quantity int) error {
	return nil
}
func (m *mockGameClient) FactionWithdrawItemsPayload(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) ViewFactionStorage(ctx context.Context) error { return nil }
func (m *mockGameClient) ViewFactionStorageAt(ctx context.Context, stationID string) error {
	return nil
}
func (m *mockGameClient) FactionCreateBuyOrder(ctx context.Context, itemID string, priceEach float64, quantity int) error {
	return nil
}
func (m *mockGameClient) FactionCreateSellOrder(ctx context.Context, itemID string, priceEach float64, quantity int) error {
	return nil
}
func (m *mockGameClient) FactionCreateRole(ctx context.Context, name string, priority int, permissions map[string]any) error {
	return nil
}
func (m *mockGameClient) FactionEditRole(ctx context.Context, roleID string, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) FactionDeleteRole(ctx context.Context, roleID string) error { return nil }
func (m *mockGameClient) FactionSubmitIntel(ctx context.Context, systems []map[string]any) error {
	return nil
}
func (m *mockGameClient) FactionQueryIntel(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) FactionQueryTradeIntel(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) FactionIntelStatus(ctx context.Context) error              { return nil }
func (m *mockGameClient) FactionTradeIntelStatus(ctx context.Context) error         { return nil }
func (m *mockGameClient) FactionRooms(ctx context.Context) error                    { return nil }
func (m *mockGameClient) FactionVisitRoom(ctx context.Context, roomID string) error { return nil }
func (m *mockGameClient) FactionWriteRoom(ctx context.Context, payload map[string]any) error {
	return nil
}
func (m *mockGameClient) FactionDeleteRoom(ctx context.Context, roomID string) error { return nil }
func (m *mockGameClient) FactionListMissions(ctx context.Context) error              { return nil }
func (m *mockGameClient) FactionCancelMission(ctx context.Context, templateID string) error {
	return nil
}

// Communication extras
func (m *mockGameClient) SendGift(ctx context.Context, payload map[string]any) error { return nil }
func (m *mockGameClient) SetAnonymous(ctx context.Context, anonymous bool) error     { return nil }
func (m *mockGameClient) SetColors(ctx context.Context, primaryColor, secondaryColor string) error {
	return nil
}

// Notes extras
func (m *mockGameClient) ReadNote(ctx context.Context, noteID string) error { return nil }

// Station Facilities
func (m *mockGameClient) Facility(ctx context.Context, payload map[string]any) error { return nil }

func (m *mockGameClient) SetOnChatMessage(fn func(msg serverapi.ChatMessage)) {}

// Phase 1 API-currentness actions (drones, empire/citizenship, economy, logs)
func (m *mockGameClient) GetDrone(ctx context.Context, droneID string) error    { return nil }
func (m *mockGameClient) GetDrones(ctx context.Context) error                   { return nil }
func (m *mockGameClient) LoadDrone(ctx context.Context, itemID string) error    { return nil }
func (m *mockGameClient) UnloadDrone(ctx context.Context, droneID string) error { return nil }
func (m *mockGameClient) RecallDrone(ctx context.Context, droneID string, all bool) error {
	return nil
}
func (m *mockGameClient) UploadDroneScript(ctx context.Context, droneID, script string) error {
	return nil
}
func (m *mockGameClient) DeployDrone(ctx context.Context, droneID string, all bool) error {
	return nil
}
func (m *mockGameClient) SetDroneName(ctx context.Context, droneID, name string) error {
	return nil
}
func (m *mockGameClient) FactionAcceptInvite(ctx context.Context, factionID string) error {
	return nil
}
func (m *mockGameClient) FactionWithdrawInvite(ctx context.Context, playerID string) error {
	return nil
}
func (m *mockGameClient) FactionRemoveEnemy(ctx context.Context, targetFactionID string) error {
	return nil
}
func (m *mockGameClient) Citizenship(ctx context.Context, action, empireID string) error { return nil }
func (m *mockGameClient) GetEmpireInfo(ctx context.Context, empireID string) error       { return nil }
func (m *mockGameClient) Petition(ctx context.Context, empireID, message string) error   { return nil }
func (m *mockGameClient) GetTaxEstimate(ctx context.Context) error                       { return nil }
func (m *mockGameClient) ViewInsurance(ctx context.Context) error                        { return nil }
func (m *mockGameClient) ScrapShip(ctx context.Context, shipID string) error             { return nil }
func (m *mockGameClient) CompletedMissions(ctx context.Context) error                    { return nil }
func (m *mockGameClient) DeleteNote(ctx context.Context, noteID string) error            { return nil }
func (m *mockGameClient) CaptainsLogDelete(ctx context.Context, index int) error         { return nil }
func (m *mockGameClient) AgentLogs(ctx context.Context, category, severity, message string, data map[string]any) error {
	return nil
}
