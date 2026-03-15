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

	// Create output directory for this agent
	outputDir := filepath.Join(baseOutputDir, agentID)

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

	logger.Println("\n✅ Scraping complete!")
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
	fmt.Println("  ships        - Shipyard Showroom (station)")
	fmt.Println("  ship_catalog - Ship Catalog (all ship types)")
	fmt.Println("  nearby       - Nearby Players")
	fmt.Println("  skill_defs   - Skill Definitions (catalog)")
	fmt.Println("  recipes      - Recipe Definitions (catalog)")
	fmt.Println("  items        - Item Definitions (catalog)")
	fmt.Println("  wrecks       - Wrecks")
	fmt.Println("  drones       - Drones")
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
	fmt.Println("  facilities   - Facility Types (all categories, paginated)")
	fmt.Println("  facility_details - Facility Details (one file per type, skips existing)")
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
		{"Shipyard Showroom", s.scrapeShips},
		{"Ship Catalog", s.scrapeShipCatalog},
		{"Nearby Players", s.scrapeNearby},
		{"Skill Definitions", s.scrapeSkillDefinitions},
		{"Recipe Definitions", s.scrapeRecipeDefinitions},
		{"Item Definitions", s.scrapeItemDefinitions},
		{"Wrecks", s.scrapeWrecks},
		{"Drones", s.scrapeDrones},
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
		{"Facility Types", s.scrapeFacilities},
		{"Facility Details", s.scrapeFacilityDetails},
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
		"ships":           {"Shipyard Showroom", s.scrapeShips},
		"ship_catalog":    {"Ship Catalog", s.scrapeShipCatalog},
		"nearby":          {"Nearby Players", s.scrapeNearby},
		"skill_defs":      {"Skill Definitions", s.scrapeSkillDefinitions},
		"recipes":         {"Recipe Definitions", s.scrapeRecipeDefinitions},
		"items":           {"Item Definitions", s.scrapeItemDefinitions},
		"wrecks":          {"Wrecks", s.scrapeWrecks},
		"drones":          {"Drones", s.scrapeDrones},
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
		"facilities":         {"Facility Types", s.scrapeFacilities},
		"facility_details":   {"Facility Details", s.scrapeFacilityDetails},
	}

	// Look up the endpoint
	ep, ok := endpointMap[endpoint]
	if !ok {
		return fmt.Errorf("unknown endpoint: %s\n\nAvailable endpoints:\n  status, ship, poi, system, map, listings, ships, ship_catalog, nearby, skill_defs, recipes, items, wrecks, drones, base, faction, log, cargo, missions, active_missions, orders, notes, insurance, version, commands, storage, market, facilities, facility_details", endpoint)
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
	return s.scrapeSimple(s.client.GetPOI, "poi", "get_poi.json", "get_poi", 1*time.Second)
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

func (s *Scraper) scrapeDrones() error {
	return s.scrapeSimple(s.client.GetDrones, "drones", "get_drones.json", "get_drones", 2*time.Second)
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
	return s.scrapeSimple(s.client.ShipyardShowroom, "shipyard", "get_ships.json", "shipyard_showroom", 2*time.Second)
}

func (s *Scraper) scrapeMarket() error {
	ctx := context.Background()
	s.clearLastError()

	if err := s.client.ViewMarket(ctx, ""); err != nil {
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

func (s *Scraper) scrapeSkillDefinitions() error {
	return s.scrapeCatalog("skills", "catalog_skills.json")
}

func (s *Scraper) scrapeRecipeDefinitions() error {
	return s.scrapeCatalog("recipes", "catalog_recipes.json")
}

func (s *Scraper) scrapeShipCatalog() error {
	return s.scrapeCatalog("ships", "catalog_ships.json")
}

func (s *Scraper) scrapeItemDefinitions() error {
	return s.scrapeCatalog("items", "catalog_items.json")
}

// scrapeCatalog fetches all pages of a catalog type and merges them
func (s *Scraper) scrapeCatalog(catalogType, filename string) error {
	ctx := context.Background()
	s.clearLastError()

	// Request first page from catalog
	if err := s.client.Catalog(ctx, catalogType, 1, 50); err != nil {
		return fmt.Errorf("catalog %s page 1 failed: %w", catalogType, err)
	}
	time.Sleep(2 * time.Second)

	rawJSON := s.getRawJSON("catalog")
	if rawJSON == nil {
		errResp := s.getLastError()
		return fmt.Errorf("%s", formatErrorMessage(fmt.Sprintf("catalog %s", catalogType), errResp))
	}

	// Parse the response to get pagination info
	var firstPage struct {
		Items      []json.RawMessage `json:"items"`
		Page       int               `json:"page"`
		PageSize   int               `json:"page_size"`
		Total      int               `json:"total"`
		TotalPages int               `json:"total_pages"`
		Type       string            `json:"type"`
		Message    string            `json:"message"`
	}

	if err := json.Unmarshal(rawJSON, &firstPage); err != nil {
		return fmt.Errorf("failed to parse catalog response: %w", err)
	}

	s.logger.Printf("  📖 Page %d/%d: %d items", firstPage.Page, firstPage.TotalPages, len(firstPage.Items))

	// If there's only one page, save and return
	if firstPage.TotalPages <= 1 {
		return s.saveJSON(filename, rawJSON)
	}

	// Fetch remaining pages
	allItems := make([]json.RawMessage, 0, firstPage.Total)
	allItems = append(allItems, firstPage.Items...)

	for page := 2; page <= firstPage.TotalPages; page++ {
		maxRetries := 3
		var pageErr error

		for retry := range maxRetries {
			if retry > 0 {
				s.logger.Printf("  🔄 Retry %d/%d for page %d", retry, maxRetries-1, page)
				if err := s.ensureConnectionReady(); err != nil {
					s.logger.Printf("  ⚠️  Failed to restore connection: %v", err)
					time.Sleep(game.SleepShort)
					continue
				}
			}

			s.clearLastError()

			if err := s.client.Catalog(ctx, catalogType, page, 50); err != nil {
				pageErr = fmt.Errorf("catalog %s page %d failed: %w", catalogType, page, err)
				time.Sleep(game.SleepRetry)
				continue
			}
			time.Sleep(2 * time.Second)

			rawJSON := s.getRawJSON("catalog")
			if rawJSON == nil {
				errResp := s.getLastError()
				pageErr = fmt.Errorf("%s", formatErrorMessage(fmt.Sprintf("catalog %s page %d", catalogType, page), errResp))
				time.Sleep(game.SleepRetry)
				continue
			}

			var pageResp struct {
				Items      []json.RawMessage `json:"items"`
				Page       int               `json:"page"`
				TotalPages int               `json:"total_pages"`
			}

			if err := json.Unmarshal(rawJSON, &pageResp); err != nil {
				pageErr = fmt.Errorf("failed to parse catalog page %d: %w", page, err)
				time.Sleep(game.SleepRetry)
				continue
			}

			// Success
			s.logger.Printf("  📖 Page %d/%d: %d items", page, firstPage.TotalPages, len(pageResp.Items))
			allItems = append(allItems, pageResp.Items...)
			pageErr = nil
			break
		}

		if pageErr != nil {
			s.logger.Printf("  ⚠️  Error fetching page %d after %d retries: %v", page, maxRetries, pageErr)
		}
	}

	// Build combined response
	combinedResponse := map[string]any{
		"items":       allItems,
		"page":        1,
		"page_size":   len(allItems),
		"total":       len(allItems),
		"total_pages": 1,
		"type":        catalogType,
		"message":     fmt.Sprintf("%s: showing all %d items", catalogType, len(allItems)),
	}

	combinedJSON, err := json.MarshalIndent(combinedResponse, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal combined catalog: %w", err)
	}

	s.logger.Printf("  📚 Total %s: %d items", catalogType, len(allItems))
	return s.saveJSON(filename, combinedJSON)
}

// scrapeFacilities fetches facility type definitions for all categories with pagination.
func (s *Scraper) scrapeFacilities() error {
	categories := []string{"infrastructure", "service", "production", "faction", "personal"}
	ctx := context.Background()

	for _, category := range categories {
		s.logger.Printf("  📦 Fetching facility types: %s...", category)

		// Fetch first page
		s.clearLastError()
		if err := s.client.RawCommand(ctx, "facility", map[string]any{
			"action":   "types",
			"category": category,
			"page":     1,
			"per_page": 50,
		}); err != nil {
			s.logger.Printf("  ⚠️  facility types %s failed: %v", category, err)
			continue
		}
		time.Sleep(1 * time.Second)

		rawJSON := s.getRawJSON("_last")
		if rawJSON == nil {
			s.logger.Printf("  ⚠️  No data for facility types %s", category)
			continue
		}

		var firstPage struct {
			Types      []json.RawMessage `json:"types"`
			Page       int               `json:"page"`
			TotalPages int               `json:"total_pages"`
			Total      int               `json:"total"`
			Category   string            `json:"category"`
		}
		if err := json.Unmarshal(rawJSON, &firstPage); err != nil {
			s.logger.Printf("  ⚠️  Failed to parse facility types %s: %v", category, err)
			// Save raw response as-is
			if saveErr := s.saveJSON(fmt.Sprintf("facility_%s.json", category), rawJSON); saveErr != nil {
				s.logger.Printf("  ⚠️  Failed to save: %v", saveErr)
			}
			continue
		}

		s.logger.Printf("  📖 Page %d/%d: %d types", firstPage.Page, firstPage.TotalPages, len(firstPage.Types))

		// Single page — save directly
		if firstPage.TotalPages <= 1 {
			if err := s.saveJSON(fmt.Sprintf("facility_%s.json", category), rawJSON); err != nil {
				s.logger.Printf("  ⚠️  Failed to save: %v", err)
			}
			continue
		}

		// Fetch remaining pages
		allTypes := make([]json.RawMessage, 0, firstPage.Total)
		allTypes = append(allTypes, firstPage.Types...)

		for page := 2; page <= firstPage.TotalPages; page++ {
			s.clearLastError()
			if err := s.client.RawCommand(ctx, "facility", map[string]any{
				"action":   "types",
				"category": category,
				"page":     page,
				"per_page": 50,
			}); err != nil {
				s.logger.Printf("  ⚠️  facility types %s page %d failed: %v", category, page, err)
				continue
			}
			time.Sleep(1 * time.Second)

			pageJSON := s.getRawJSON("_last")
			if pageJSON == nil {
				s.logger.Printf("  ⚠️  No data for facility types %s page %d", category, page)
				continue
			}

			var pageResp struct {
				Types []json.RawMessage `json:"types"`
				Page  int               `json:"page"`
			}
			if err := json.Unmarshal(pageJSON, &pageResp); err != nil {
				s.logger.Printf("  ⚠️  Failed to parse page %d: %v", page, err)
				continue
			}

			s.logger.Printf("  📖 Page %d/%d: %d types", page, firstPage.TotalPages, len(pageResp.Types))
			allTypes = append(allTypes, pageResp.Types...)
		}

		// Build combined response
		combined := map[string]any{
			"types":       allTypes,
			"page":        1,
			"total_pages": 1,
			"total":       len(allTypes),
			"category":    category,
		}
		combinedJSON, err := json.MarshalIndent(combined, "", "  ")
		if err != nil {
			s.logger.Printf("  ⚠️  Failed to marshal combined: %v", err)
			continue
		}

		s.logger.Printf("  📚 Total %s facility types: %d", category, len(allTypes))
		if err := s.saveJSON(fmt.Sprintf("facility_%s.json", category), combinedJSON); err != nil {
			s.logger.Printf("  ⚠️  Failed to save: %v", err)
		}
	}

	return nil
}

// scrapeFacilityDetails fetches detailed info for every facility type and saves each as a separate file.
func (s *Scraper) scrapeFacilityDetails() error {
	ctx := context.Background()

	// First, collect all type IDs from the category listing files (or fetch them).
	categories := []string{"infrastructure", "service", "production", "faction", "personal"}
	var allIDs []string

	for _, category := range categories {
		s.clearLastError()
		if err := s.client.RawCommand(ctx, "facility", map[string]any{
			"action":   "types",
			"category": category,
			"page":     1,
			"per_page": 50,
		}); err != nil {
			s.logger.Printf("  ⚠️  facility types %s failed: %v", category, err)
			continue
		}
		time.Sleep(500 * time.Millisecond)

		rawJSON := s.getRawJSON("_last")
		if rawJSON == nil {
			continue
		}

		var firstPage struct {
			Types []struct {
				ID string `json:"id"`
			} `json:"types"`
			TotalPages int `json:"total_pages"`
		}
		if err := json.Unmarshal(rawJSON, &firstPage); err != nil {
			continue
		}
		for _, t := range firstPage.Types {
			allIDs = append(allIDs, t.ID)
		}

		for page := 2; page <= firstPage.TotalPages; page++ {
			s.clearLastError()
			if err := s.client.RawCommand(ctx, "facility", map[string]any{
				"action":   "types",
				"category": category,
				"page":     page,
				"per_page": 50,
			}); err != nil {
				continue
			}
			time.Sleep(500 * time.Millisecond)

			pageJSON := s.getRawJSON("_last")
			if pageJSON == nil {
				continue
			}
			var pageResp struct {
				Types []struct {
					ID string `json:"id"`
				} `json:"types"`
			}
			if err := json.Unmarshal(pageJSON, &pageResp); err != nil {
				continue
			}
			for _, t := range pageResp.Types {
				allIDs = append(allIDs, t.ID)
			}
		}
	}

	s.logger.Printf("  📋 Found %d facility type IDs to fetch details for", len(allIDs))

	// Create subdirectory for detail files
	detailDir := filepath.Join(s.outputDir, "facility_details")
	if err := os.MkdirAll(detailDir, 0755); err != nil {
		return fmt.Errorf("failed to create facility_details directory: %w", err)
	}

	// Fetch details for each type, skipping ones that already exist on disk
	fetched, skipped := 0, 0
	for _, typeID := range allIDs {
		detailPath := filepath.Join(detailDir, typeID+".json")
		if _, err := os.Stat(detailPath); err == nil {
			skipped++
			continue
		}

		s.clearLastError()
		if err := s.client.RawCommand(ctx, "facility", map[string]any{
			"action":        "types",
			"facility_type": typeID,
		}); err != nil {
			s.logger.Printf("  ⚠️  facility detail %s failed: %v", typeID, err)
			continue
		}
		time.Sleep(500 * time.Millisecond)

		rawJSON := s.getRawJSON("_last")
		if rawJSON == nil {
			s.logger.Printf("  ⚠️  No data for facility detail %s", typeID)
			continue
		}

		if err := s.saveJSONTo(detailPath, rawJSON); err != nil {
			s.logger.Printf("  ⚠️  Failed to save %s: %v", typeID, err)
			continue
		}
		fetched++
		if fetched%50 == 0 {
			s.logger.Printf("  📖 Fetched %d/%d details...", fetched, len(allIDs)-skipped)
		}
	}

	s.logger.Printf("  📚 Facility details: %d fetched, %d skipped (already on disk)", fetched, skipped)
	return nil
}

// saveJSONTo writes pretty-printed JSON to an absolute path.
func (s *Scraper) saveJSONTo(path string, data []byte) error {
	formatted, err := json.MarshalIndent(json.RawMessage(data), "", "  ")
	if err != nil {
		return fmt.Errorf("failed to format JSON: %w", err)
	}
	if err := os.WriteFile(path, formatted, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
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
