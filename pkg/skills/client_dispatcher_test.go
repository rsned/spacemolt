package skills

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
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
func (m *mockGameClient) Connect(ctx context.Context) error                                    { return nil }
func (m *mockGameClient) Close() error                                                         { return nil }
func (m *mockGameClient) IsConnected() bool                                                    { return true }
func (m *mockGameClient) Ready() <-chan struct{}                                               { return nil }
func (m *mockGameClient) Login(ctx context.Context) error                                      { return nil }
func (m *mockGameClient) Register(ctx context.Context, empire, registrationCode string) error  { return nil }
func (m *mockGameClient) Undock(ctx context.Context) error                                     { return nil }
func (m *mockGameClient) Dock(ctx context.Context) error                                       { return nil }
func (m *mockGameClient) Travel(ctx context.Context, targetPOI string) (*game.TravelResult, error) { return nil, nil }
func (m *mockGameClient) Jump(ctx context.Context, targetSystem string) (*game.JumpResult, error) { return nil, nil }
func (m *mockGameClient) Mine(ctx context.Context) error                                       { return nil }
func (m *mockGameClient) Scan(ctx context.Context) error                                       { return nil }
func (m *mockGameClient) Attack(ctx context.Context, targetID string) error                    { return nil }
func (m *mockGameClient) Cloak(ctx context.Context, enable bool) error                         { return nil }
func (m *mockGameClient) Sell(ctx context.Context, itemID string, quantity float64) error      { return nil }
func (m *mockGameClient) SellAllBulk(ctx context.Context, reservedItems []string) error       { return nil }
func (m *mockGameClient) Buy(ctx context.Context, itemID string, quantity float64) error       { return nil }
func (m *mockGameClient) GetListings(ctx context.Context) error                                { return nil }
func (m *mockGameClient) GetTrades(ctx context.Context) error                                  { return nil }
func (m *mockGameClient) CraftWithQuantity(ctx context.Context, recipeID string, quantity int) error { return nil }
func (m *mockGameClient) GetRecipes(ctx context.Context) error                                 { return nil }
func (m *mockGameClient) Refuel(ctx context.Context) error                                     { return nil }
func (m *mockGameClient) Repair(ctx context.Context) error                                     { return nil }
func (m *mockGameClient) Install(ctx context.Context, itemID string) error                     { return nil }
func (m *mockGameClient) UninstallMod(ctx context.Context, moduleID string) error              { return nil }
func (m *mockGameClient) BuyShip(ctx context.Context, shipClass string) error                 { return nil }
func (m *mockGameClient) BuyInsurance(ctx context.Context, ticks int) error                   { return nil }
func (m *mockGameClient) ClaimInsurance(ctx context.Context) error                             { return nil }
func (m *mockGameClient) DepositItems(ctx context.Context, itemID string, quantity float64) error { return nil }
func (m *mockGameClient) DepositAllItems(ctx context.Context) error                            { return nil }
func (m *mockGameClient) WithdrawItems(ctx context.Context, itemID string, quantity float64) error { return nil }
func (m *mockGameClient) ViewStorage(ctx context.Context) error                                { return nil }
func (m *mockGameClient) GetWrecks(ctx context.Context) error                                  { return nil }
func (m *mockGameClient) LootWreck(ctx context.Context, wreckID, itemID string, quantity float64) error { return nil }
func (m *mockGameClient) SalvageWreck(ctx context.Context, wreckID string) error               { return nil }
func (m *mockGameClient) GetSystem(ctx context.Context) error                                  { return nil }
func (m *mockGameClient) GetStatus(ctx context.Context) error                                  { return nil }
func (m *mockGameClient) GetShip(ctx context.Context) error                                    { return nil }
func (m *mockGameClient) GetSkills(ctx context.Context) error                                  { return nil }
func (m *mockGameClient) GetPOI(ctx context.Context) error                                     { return nil }
func (m *mockGameClient) GetBase(ctx context.Context) error                                    { return nil }
func (m *mockGameClient) GetMap(ctx context.Context, force ...bool) error                      { return nil }
func (m *mockGameClient) GetNearby(ctx context.Context) error                                  { return nil }
func (m *mockGameClient) GetVersion(ctx context.Context) error                                 { return nil }
func (m *mockGameClient) Help(ctx context.Context, payload map[string]any) error               { return nil }
func (m *mockGameClient) FindRoute(ctx context.Context, targetSystem string) ([]game.RouteStep, error) { return nil, nil }
func (m *mockGameClient) CreateFaction(ctx context.Context, payload map[string]any) error      { return nil }
func (m *mockGameClient) JoinFaction(ctx context.Context, factionID string) error              { return nil }
func (m *mockGameClient) LeaveFaction(ctx context.Context) error                               { return nil }
func (m *mockGameClient) FactionInvite(ctx context.Context, playerID string) error             { return nil }
func (m *mockGameClient) FactionKick(ctx context.Context, playerID string) error               { return nil }
func (m *mockGameClient) FactionPromote(ctx context.Context, playerID, roleID string) error    { return nil }
func (m *mockGameClient) Chat(ctx context.Context, channel, content string, targetID string) error { return nil }
func (m *mockGameClient) GetChatHistory(ctx context.Context, channel string, payload map[string]any) error { return nil }
func (m *mockGameClient) Jettison(ctx context.Context, itemID string, quantity float64) error  { return nil }
func (m *mockGameClient) SetPlayerStatus(ctx context.Context, payload map[string]any) error    { return nil }
func (m *mockGameClient) SetHomeBase(ctx context.Context, baseID string) error                 { return nil }
func (m *mockGameClient) ForumList(ctx context.Context, page int) error                        { return nil }
func (m *mockGameClient) ForumCreateThread(ctx context.Context, title, content string, category string) error { return nil }
func (m *mockGameClient) ForumGetThread(ctx context.Context, threadID string) error            { return nil }
func (m *mockGameClient) ForumReply(ctx context.Context, threadID, content string) error        { return nil }
func (m *mockGameClient) ForumUpvote(ctx context.Context, threadID string, replyID string) error { return nil }
func (m *mockGameClient) ForumDeleteThread(ctx context.Context, threadID string) error         { return nil }
func (m *mockGameClient) ForumDeleteReply(ctx context.Context, replyID string) error           { return nil }
func (m *mockGameClient) CreateNote(ctx context.Context, title, content string) error          { return nil }
func (m *mockGameClient) WriteNote(ctx context.Context, noteID, content string) error          { return nil }
func (m *mockGameClient) GetNotes(ctx context.Context) error                                   { return nil }
