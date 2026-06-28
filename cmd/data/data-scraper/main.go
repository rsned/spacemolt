package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

const (
	baseOutputDir = "data/game-api"
)

type Scraper struct {
	client    game.GameClient
	wsClient  *game.Client // non-nil only for WS transport
	logger    *log.Logger
	agentID   string
	outputDir string
}

// clearLastError delegates to the WS client's ClearLastError.
func (s *Scraper) clearLastError() {
	if s.wsClient != nil {
		s.wsClient.ClearLastError()
	}
}

// getLastError delegates to the WS client's GetLastError.
func (s *Scraper) getLastError() map[string]any {
	if s.wsClient != nil {
		return s.wsClient.GetLastError()
	}
	return nil
}

// ensureConnected delegates to the WS client's EnsureConnected.
func (s *Scraper) ensureConnected(ctx context.Context) error {
	if s.wsClient != nil {
		return s.wsClient.EnsureConnected(ctx)
	}
	return nil
}

// formatErrorMessage provides a helpful error message based on error codes
func formatErrorMessage(callType string, errResp map[string]any) string {
	if errResp == nil {
		return fmt.Sprintf("no data available for %s", callType)
	}

	code, hasCode := errResp["code"].(string)
	message, hasMessage := errResp["message"].(string)

	if !hasCode {
		if hasMessage {
			return fmt.Sprintf("%s failed: %s", callType, message)
		}
		return fmt.Sprintf("%s failed with unknown error", callType)
	}

	// Provide helpful context based on error codes
	switch code {
	case "not_docked":
		return fmt.Sprintf("%s failed: Player is not docked at a station", callType)
	case "docked":
		return fmt.Sprintf("%s failed: Player is docked - must undock first", callType)
	case "no_fuel":
		return fmt.Sprintf("%s failed: Insufficient fuel - dock at station to refuel", callType)
	case "no_credits":
		return fmt.Sprintf("%s failed: Insufficient credits", callType)
	case "no_cargo_space":
		return fmt.Sprintf("%s failed: Cargo hold full", callType)
	case "missing_materials":
		return fmt.Sprintf("%s failed: Missing required materials", callType)
	case "cannot_craft":
		return fmt.Sprintf("%s failed: Insufficient skill level", callType)
	case "rate_limited":
		return fmt.Sprintf("%s failed: Rate limited - wait before retrying", callType)
	case "no_faction":
		return fmt.Sprintf("%s failed: Player has not joined a faction", callType)
	case "not_authenticated":
		return fmt.Sprintf("%s failed: Not authenticated", callType)
	default:
		if hasMessage {
			return fmt.Sprintf("%s failed: %s", callType, message)
		}
		return fmt.Sprintf("%s failed: %s", callType, code)
	}
}

// ensureConnectionReady waits for the client to be connected and ready
func (s *Scraper) ensureConnectionReady() error {
	ctx := context.Background()

	// Wait briefly for any in-flight reconnection to complete
	maxWait := game.SleepReconnect // Use the reconnection wait constant
	checkInterval := game.SleepShort

	startTime := time.Now()
	for time.Since(startTime) < maxWait {
		if s.client.IsConnected() {
			// Connection is ready
			return nil
		}
		s.logger.Printf("  ⏳ Waiting for connection to be ready...")
		time.Sleep(checkInterval)
	}

	// If we're here, connection is not ready - try to reconnect
	s.logger.Printf("  🔌 Attempting to reconnect...")
	if err := s.ensureConnected(ctx); err != nil {
		return fmt.Errorf("failed to reconnect: %w", err)
	}

	return nil
}

// getRawJSON gets raw JSON for a key, falling back to "_last" for MCP transport.
func (s *Scraper) getRawJSON(key string) []byte {
	rawJSON := s.client.GetRawJSON(key)
	if rawJSON == nil {
		rawJSON = s.client.GetRawJSON("_last")
	}
	return rawJSON
}

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging")
	transport := flag.String("transport", "ws", "Transport: ws (WebSocket) or mcp (MCP HTTP)")
	flag.Parse()

	if len(flag.Args()) < 1 {
		printUsage()
		os.Exit(1)
	}

	agentID := flag.Args()[0]
	var endpoint string
	if len(flag.Args()) >= 2 {
		endpoint = flag.Args()[1]
	}

	logger := log.New(os.Stdout, "[SCRAPER] ", log.LstdFlags)

	logger.Println("🚀 SpaceMolt Game API Scraper")
	logger.Println("==========================================")
	logger.Printf("Agent: %s\n", agentID)
	if endpoint != "" {
		logger.Printf("Endpoint: %s\n", endpoint)
	}
	logger.Println()

	// Create output directory named by today's date (YYYYMMDD)
	outputDir := filepath.Join(baseOutputDir, time.Now().Format("20060102"))

	// Create scraper
	scraper := &Scraper{
		logger:    logger,
		agentID:   agentID,
		outputDir: outputDir,
	}

	// Initialize client using standard agent initialization
	ctx := context.Background()
	var creds *game.Credentials
	switch *transport {
	case "mcp":
		logger.Printf("Using MCP transport")
		mcpClient, mcpCreds, mcpErr := game.InitializeMCPAgent(agentID, logger, ctx, *debug, true) // disablePolling=true for sequential scraping
		if mcpErr != nil {
			logger.Fatalf("MCP initialization failed: %v", mcpErr)
		}
		creds = mcpCreds
		scraper.client = mcpClient
	case "ws":
		logger.Printf("Using WebSocket transport")
		wsClient, wsCreds, wsErr := game.InitializeAgent(agentID, logger, ctx, *debug)
		if wsErr != nil {
			logger.Fatalf("Initialization failed: %v", wsErr)
		}
		creds = wsCreds
		scraper.client = wsClient
		scraper.wsClient = wsClient
	default:
		logger.Fatalf("Unknown transport: %s (must be: ws, mcp)", *transport)
	}
	defer func() {
		if err := scraper.client.Close(); err != nil {
			logger.Printf("Warning: error closing client: %v", err)
		}
	}()

	logger.Printf("Credentials: %s | Empire: %s\n", creds.Username, creds.Empire)

	// Scrape data
	if endpoint != "" {
		// Scrape single endpoint
		if err := scraper.scrapeOne(endpoint); err != nil {
			logger.Fatalf("Scraping failed: %v", err)
		}
	} else {
		// Scrape all data
		if err := scraper.scrapeAll(); err != nil {
			logger.Fatalf("Scraping failed: %v", err)
		}
	}

	// Update "latest" symlink to point at this run's output directory
	updateLatestSymlink(logger, scraper.outputDir)

	logger.Println("\n✅ Scraping complete!")
}

// updateLatestSymlink creates or updates a "latest" symlink in the base output
// directory that points to the given day directory.
func updateLatestSymlink(logger *log.Logger, dayDir string) {
	latestLink := filepath.Join(filepath.Dir(dayDir), "latest")
	// Use the directory basename so the symlink is relative (e.g., "20260411")
	target := filepath.Base(dayDir)

	// Remove existing symlink (or stale file) before creating a new one
	if err := os.Remove(latestLink); err != nil && !os.IsNotExist(err) {
		logger.Printf("⚠️  Failed to remove old latest symlink: %v", err)
		return
	}
	if err := os.Symlink(target, latestLink); err != nil {
		logger.Printf("⚠️  Failed to create latest symlink: %v", err)
	} else {
		logger.Printf("🔗 Updated latest → %s", target)
	}
}

func printUsage() {
	fmt.Println("Usage: data-scraper [flags] <agent-id> [endpoint]")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --transport  Transport: ws (WebSocket) or mcp (MCP HTTP)")
	fmt.Println("  --debug      Enable debug logging")
	fmt.Println()
	fmt.Println("Arguments:")
	fmt.Println("  agent-id   Agent identifier (e.g., craftsman-1, explorer-1)")
	fmt.Println("  endpoint   Optional: Specific endpoint to scrape")
	fmt.Println()
	fmt.Println("Available endpoints:")
	fmt.Println("  status       - Status & Player Info")
	fmt.Println("  ship         - Ship Info")
	fmt.Println("  poi          - Current POI")
	fmt.Println("  system       - System Data")
	fmt.Println("  map          - Map Data")
	fmt.Println("  listings     - Market Listings")
	fmt.Println("  ships        - Ships for sale (browse_ships)")
	fmt.Println("  catalog      - Full catalog bundle (ships, skills, recipes, items, modules, facilities via catalog.json)")
	fmt.Println("  nearby       - Nearby Players")
	fmt.Println("  wrecks       - Wrecks")
	fmt.Println("  base         - Base Info")
	fmt.Println("  faction      - Faction Info")
	fmt.Println("  log          - Captain's Log")
	fmt.Println("  cargo        - Cargo Contents")
	fmt.Println("  missions     - Available Missions")
	fmt.Println("  active_missions - Active Missions")
	fmt.Println("  orders       - Exchange Orders")
	fmt.Println("  notes        - Player Notes")
	fmt.Println("  insurance    - Insurance Quote")
	fmt.Println("  version      - Game Version")
	fmt.Println("  commands     - Available Commands")
	fmt.Println("  storage      - Station Storage")
	fmt.Println("  market       - Market Exchange")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  data-scraper craftsman-1           # Scrape all endpoints")
	fmt.Println("  data-scraper craftsman-1 status    # Scrape only status")
	fmt.Println("  data-scraper --transport mcp craftsman-1")
}

func (s *Scraper) scrapeAll() error {
	// Create output directory
	if err := os.MkdirAll(s.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Scrape different data categories
	categories := []struct {
		name string
		fn   func() error
	}{
		{"Status & Player Info", s.scrapeStatus},
		{"Ship Info", s.scrapeShip},
		{"Current POI", s.scrapePOI},
		{"System Data", s.scrapeSystem},
		{"Map Data", s.scrapeMap},
		{"Market Listings", s.scrapeListings},
		{"Ships For Sale", s.scrapeShips},
		{"Catalog Bundle", s.scrapeCatalogBundle},
		{"Nearby Players", s.scrapeNearby},
		{"Wrecks", s.scrapeWrecks},
		{"Base Info", s.scrapeBase},
		{"Faction Info", s.scrapeFactionInfo},
		{"Captain's Log", s.scrapeCaptainsLog},
		{"Cargo", s.scrapeCargo},
		{"Available Missions", s.scrapeMissions},
		{"Active Missions", s.scrapeActiveMissions},
		{"Exchange Orders", s.scrapeOrders},
		{"Player Notes", s.scrapeNotes},
		{"Insurance Quote", s.scrapeInsuranceQuote},
		{"Game Version", s.scrapeVersion},
		{"Available Commands", s.scrapeCommands},
		{"Station Storage", s.scrapeStorage},
		{"Market Exchange", s.scrapeMarket},
	}

	for _, cat := range categories {
		s.logger.Printf("\n📊 Scraping: %s...", cat.name)
		if err := cat.fn(); err != nil {
			s.logger.Printf("⚠️  Error: %v", err)
			// Ensure connection is ready before continuing to next category
			if err := s.ensureConnectionReady(); err != nil {
				s.logger.Printf("⚠️  Failed to restore connection: %v", err)
				// Continue anyway - next call might trigger reconnect
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

func (s *Scraper) scrapeOne(endpoint string) error {
	// Create output directory
	if err := os.MkdirAll(s.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Map endpoint names to their scrape functions
	endpointMap := map[string]struct {
		name string
		fn   func() error
	}{
		"status":          {"Status & Player Info", s.scrapeStatus},
		"ship":            {"Ship Info", s.scrapeShip},
		"poi":             {"Current POI", s.scrapePOI},
		"system":          {"System Data", s.scrapeSystem},
		"map":             {"Map Data", s.scrapeMap},
		"listings":        {"Market Listings", s.scrapeListings},
		"ships":           {"Ships For Sale", s.scrapeShips},
		"catalog":         {"Catalog Bundle", s.scrapeCatalogBundle},
		"nearby":          {"Nearby Players", s.scrapeNearby},
		"wrecks":          {"Wrecks", s.scrapeWrecks},
		"base":            {"Base Info", s.scrapeBase},
		"faction":         {"Faction Info", s.scrapeFactionInfo},
		"log":             {"Captain's Log", s.scrapeCaptainsLog},
		"cargo":           {"Cargo", s.scrapeCargo},
		"missions":        {"Available Missions", s.scrapeMissions},
		"active_missions": {"Active Missions", s.scrapeActiveMissions},
		"orders":          {"Exchange Orders", s.scrapeOrders},
		"notes":           {"Player Notes", s.scrapeNotes},
		"insurance":       {"Insurance Quote", s.scrapeInsuranceQuote},
		"version":         {"Game Version", s.scrapeVersion},
		"commands":        {"Available Commands", s.scrapeCommands},
		"storage":         {"Station Storage", s.scrapeStorage},
		"market":          {"Market Exchange", s.scrapeMarket},
	}

	// Look up the endpoint
	ep, ok := endpointMap[endpoint]
	if !ok {
		return fmt.Errorf("unknown endpoint: %s\n\nAvailable endpoints:\n  status, ship, poi, system, map, listings, ships, catalog, nearby, wrecks, base, faction, log, cargo, missions, active_missions, orders, notes, insurance, version, commands, storage, market", endpoint)
	}

	// Scrape the single endpoint
	s.logger.Printf("\n📊 Scraping: %s...", ep.name)
	if err := ep.fn(); err != nil {
		return fmt.Errorf("scraping failed: %w", err)
	}

	return nil
}

// scrapeSimple is a helper that calls an interface method, waits, then saves the raw JSON.
func (s *Scraper) scrapeSimple(callFn func(context.Context) error, key, filename, callType string, wait time.Duration) error {
	ctx := context.Background()
	s.clearLastError()

	if err := callFn(ctx); err != nil {
		return fmt.Errorf("%s failed: %w", callType, err)
	}
	time.Sleep(wait)

	rawJSON := s.getRawJSON(key)
	if rawJSON == nil {
		errResp := s.getLastError()
		return fmt.Errorf("%s", formatErrorMessage(callType, errResp))
	}
	return s.saveJSON(filename, rawJSON)
}

func (s *Scraper) scrapeStatus() error {
	return s.scrapeSimple(s.client.GetStatus, "status", "get_status.json", "get_status", 1*time.Second)
}

func (s *Scraper) scrapeShip() error {
	return s.scrapeSimple(s.client.GetShip, "ship", "get_ship.json", "get_ship", 1*time.Second)
}

func (s *Scraper) scrapePOI() error {
	// get_poi was retired server-side (2026-06-24); get_location is its
	// replacement for current-POI data (including live resources).
	return s.scrapeSimple(func(ctx context.Context) error {
		return s.client.RawCommand(ctx, "get_location", nil)
	}, "location", "get_location.json", "get_location", 1*time.Second)
}

func (s *Scraper) scrapeSystem() error {
	return s.scrapeSimple(s.client.GetSystem, "system", "get_system.json", "get_system", 2*time.Second)
}

func (s *Scraper) scrapeListings() error {
	return s.scrapeSimple(s.client.GetListings, "market", "get_listings.json", "get_listings", 2*time.Second)
}

func (s *Scraper) scrapeNearby() error {
	return s.scrapeSimple(s.client.GetNearby, "nearby", "get_nearby.json", "get_nearby", 2*time.Second)
}

func (s *Scraper) scrapeWrecks() error {
	return s.scrapeSimple(s.client.GetWrecks, "wrecks", "get_wrecks.json", "get_wrecks", 2*time.Second)
}

func (s *Scraper) scrapeBase() error {
	return s.scrapeSimple(s.client.GetBase, "base", "get_base.json", "get_base", 2*time.Second)
}

func (s *Scraper) scrapeFactionInfo() error {
	ctx := context.Background()
	s.clearLastError()

	if err := s.client.FactionInfo(ctx); err != nil {
		return fmt.Errorf("faction_info failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	rawJSON := s.getRawJSON("faction_info")
	if rawJSON == nil {
		errResp := s.getLastError()
		if len(errResp) == 0 || (len(errResp) > 0 && errResp["code"] == nil && errResp["message"] == nil) {
			s.logger.Printf("  ⚠️  No faction data in response - skipping (player may not be in a faction)")
			return nil // Not a fatal error
		}
		return fmt.Errorf("%s", formatErrorMessage("faction_info", errResp))
	}
	return s.saveJSON("faction_info.json", rawJSON)
}

func (s *Scraper) scrapeCaptainsLog() error {
	return s.scrapeSimple(s.client.CaptainsLogList, "captains_log_list", "captains_log_list.json", "captains_log_list", 2*time.Second)
}

func (s *Scraper) scrapeCargo() error {
	return s.scrapeSimple(s.client.GetCargo, "cargo", "get_cargo.json", "get_cargo", 1*time.Second)
}

func (s *Scraper) scrapeMissions() error {
	return s.scrapeSimple(s.client.GetMissions, "missions", "get_missions.json", "get_missions", 1*time.Second)
}

func (s *Scraper) scrapeActiveMissions() error {
	return s.scrapeSimple(s.client.GetActiveMissions, "active_missions", "get_active_missions.json", "get_active_missions", 1*time.Second)
}

func (s *Scraper) scrapeOrders() error {
	return s.scrapeSimple(s.client.ViewOrders, "orders", "view_orders.json", "view_orders", 1*time.Second)
}

func (s *Scraper) scrapeNotes() error {
	return s.scrapeSimple(s.client.GetNotes, "notes", "get_notes.json", "get_notes", 1*time.Second)
}

func (s *Scraper) scrapeInsuranceQuote() error {
	return s.scrapeSimple(s.client.GetInsuranceQuote, "insurance_quote", "get_insurance_quote.json", "get_insurance_quote", 1*time.Second)
}

func (s *Scraper) scrapeVersion() error {
	return s.scrapeSimple(s.client.GetVersion, "version", "get_version.json", "get_version", 1*time.Second)
}

func (s *Scraper) scrapeCommands() error {
	return s.scrapeSimple(s.client.GetCommands, "commands", "get_commands.json", "get_commands", 1*time.Second)
}

func (s *Scraper) scrapeStorage() error {
	return s.scrapeSimple(s.client.ViewStorage, "storage", "view_storage.json", "view_storage", 1*time.Second)
}

func (s *Scraper) scrapeShips() error {
	return s.scrapeSimple(func(ctx context.Context) error {
		return s.client.BrowseShips(ctx, nil)
	}, "_last", "browse_ships.json", "browse_ships", 2*time.Second)
}

func (s *Scraper) scrapeMarket() error {
	ctx := context.Background()
	s.clearLastError()

	if err := s.client.ViewMarket(ctx, nil); err != nil {
		return fmt.Errorf("view_market failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	rawJSON := s.getRawJSON("view_market")
	if rawJSON == nil {
		rawJSON = s.client.GetRawJSON("market")
	}
	if rawJSON == nil {
		errResp := s.getLastError()
		return fmt.Errorf("%s", formatErrorMessage("view_market", errResp))
	}
	return s.saveJSON("view_market.json", rawJSON)
}

func (s *Scraper) scrapeMap() error {
	ctx := context.Background()
	s.clearLastError()

	if err := s.client.GetMap(ctx); err != nil {
		return fmt.Errorf("get_map failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	rawJSON := s.getRawJSON("systems")
	if rawJSON == nil {
		errResp := s.getLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_map", errResp))
	}

	// Parse the response to check for pagination
	var firstPage struct {
		Systems    []json.RawMessage `json:"systems"`
		TotalCount int               `json:"total_count"`
		Offset     int               `json:"offset,omitempty"`
		Limit      int               `json:"limit,omitempty"`
	}

	if err := json.Unmarshal(rawJSON, &firstPage); err != nil {
		s.logger.Printf("  📖 Map response appears to be old format (no pagination)")
		return s.saveJSON("get_map.json", rawJSON)
	}

	systemCount := len(firstPage.Systems)
	totalCount := firstPage.TotalCount

	s.logger.Printf("  📖 Page 1 (offset %d): %d systems (total: %d)",
		firstPage.Offset, systemCount, totalCount)

	// If total_count is not set or we got all systems, save and return
	if totalCount == 0 || totalCount <= systemCount {
		s.logger.Printf("  📚 Total systems: %d", systemCount)
		return s.saveJSON("get_map.json", rawJSON)
	}

	// For MCP, map pagination isn't directly supported via GetMap
	// Save what we have
	s.logger.Printf("  📚 Got %d/%d systems (pagination not fully supported on this transport)", systemCount, totalCount)
	return s.saveJSON("get_map.json", rawJSON)
}

func (s *Scraper) saveJSON(filename string, data any) error {
	var jsonData []byte
	var err error

	// Check if data is already raw JSON bytes
	if rawBytes, ok := data.([]byte); ok {
		// Pretty-print the existing JSON
		jsonData, err = json.MarshalIndent(json.RawMessage(rawBytes), "", "  ")
		if err != nil {
			return fmt.Errorf("failed to format JSON: %w", err)
		}
	} else {
		// Marshal the data to JSON
		jsonData, err = json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
	}

	// Save to file
	outputPath := filepath.Join(s.outputDir, filename)
	if err := os.WriteFile(outputPath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	s.logger.Printf("  ✓ Saved %s", filename)
	return nil
}
