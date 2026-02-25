package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
)

const (
	baseOutputDir = "data/game-api"
)

type Scraper struct {
	client    *game.Client
	logger    *log.Logger
	agentID   string
	outputDir string
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
	if err := s.client.EnsureConnected(ctx); err != nil {
		return fmt.Errorf("failed to reconnect: %w", err)
	}

	return nil
}

// Helper function to get POI name by ID
func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	agentID := os.Args[1]
	var endpoint string
	if len(os.Args) >= 3 {
		endpoint = os.Args[2]
	}

	logger := log.New(os.Stdout, "[SCRAPER] ", log.LstdFlags)

	logger.Println("🚀 SpaceMolt Game API Scraper (WebSocket)")
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
	client, creds, err := game.InitializeAgent(agentID, logger, ctx)
	if err != nil {
		logger.Fatalf("Initialization failed: %v", err)
	}
	scraper.client = client
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
	fmt.Println("Usage: data-scraper <agent-id> [endpoint]")
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
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  data-scraper craftsman-1           # Scrape all endpoints")
	fmt.Println("  data-scraper craftsman-1 status    # Scrape only status")
	fmt.Println("  data-scraper craftsman-1 map       # Scrape only map data")
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
		"status":     {"Status & Player Info", s.scrapeStatus},
		"ship":       {"Ship Info", s.scrapeShip},
		"poi":        {"Current POI", s.scrapePOI},
		"system":     {"System Data", s.scrapeSystem},
		"map":        {"Map Data", s.scrapeMap},
		"listings":     {"Market Listings", s.scrapeListings},
		"ships":        {"Shipyard Showroom", s.scrapeShips},
		"ship_catalog": {"Ship Catalog", s.scrapeShipCatalog},
		"nearby":       {"Nearby Players", s.scrapeNearby},
		"skill_defs":   {"Skill Definitions", s.scrapeSkillDefinitions},
		"recipes":    {"Recipe Definitions", s.scrapeRecipeDefinitions},
		"items":      {"Item Definitions", s.scrapeItemDefinitions},
		"wrecks":     {"Wrecks", s.scrapeWrecks},
		"drones":     {"Drones", s.scrapeDrones},
		"base":       {"Base Info", s.scrapeBase},
		"faction":          {"Faction Info", s.scrapeFactionInfo},
		"log":              {"Captain's Log", s.scrapeCaptainsLog},
		"cargo":            {"Cargo", s.scrapeCargo},
		"missions":         {"Available Missions", s.scrapeMissions},
		"active_missions":  {"Active Missions", s.scrapeActiveMissions},
		"orders":           {"Exchange Orders", s.scrapeOrders},
		"notes":            {"Player Notes", s.scrapeNotes},
		"insurance":        {"Insurance Quote", s.scrapeInsuranceQuote},
		"version":          {"Game Version", s.scrapeVersion},
		"commands":         {"Available Commands", s.scrapeCommands},
		"storage":          {"Station Storage", s.scrapeStorage},
		"market":           {"Market Exchange", s.scrapeMarket},
	}

	// Look up the endpoint
	ep, ok := endpointMap[endpoint]
	if !ok {
		return fmt.Errorf("unknown endpoint: %s\n\nAvailable endpoints:\n  status, ship, poi, system, map, listings, ships, ship_catalog, nearby, skill_defs, recipes, items, wrecks, drones, base, faction, log, cargo, missions, active_missions, orders, notes, insurance, version, commands, storage, market", endpoint)
	}

	// Scrape the single endpoint
	s.logger.Printf("\n📊 Scraping: %s...", ep.name)
	if err := ep.fn(); err != nil {
		return fmt.Errorf("scraping failed: %w", err)
	}

	return nil
}

func (s *Scraper) scrapeStatus() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Get status
	if err := s.client.GetStatus(ctx); err != nil {
		return fmt.Errorf("get_status failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	// Get raw JSON response
	rawJSON := s.client.GetRawJSON("status")
	if rawJSON == nil {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_status", errResp))
	}

	return s.saveJSON("get_status.json", rawJSON)
}

func (s *Scraper) scrapeShip() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Request ship info
	msg := protocol.Message{
		Type: "get_ship",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("get_ship failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	// Get raw JSON response
	rawJSON := s.client.GetRawJSON("ship")
	if rawJSON == nil {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_ship", errResp))
	}

	return s.saveJSON("get_ship.json", rawJSON)
}

func (s *Scraper) scrapePOI() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Get POI info
	if err := s.client.GetPOI(ctx); err != nil {
		return fmt.Errorf("get_poi failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	// Get raw JSON response
	rawJSON := s.client.GetRawJSON("poi")
	if rawJSON == nil {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_poi", errResp))
	}

	return s.saveJSON("get_poi.json", rawJSON)
}

func (s *Scraper) scrapeSystem() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Get system info
	if err := s.client.GetSystem(ctx); err != nil {
		return fmt.Errorf("get_system failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON response
	rawJSON := s.client.GetRawJSON("system")
	if rawJSON == nil {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_system", errResp))
	}

	return s.saveJSON("get_system.json", rawJSON)
}

func (s *Scraper) scrapeMap() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Request first page of map data with pagination parameters
	msg := protocol.Message{
		Type: "get_map",
		Payload: map[string]any{
			"offset": 0,
			"limit":  100, // Server default is 100 systems per page
		},
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("get_map page 1 failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get first page response
	rawJSON := s.client.GetRawJSON("systems")
	if rawJSON == nil {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_map", errResp))
	}

	// Parse the response to get pagination info
	var firstPage struct {
		Systems    []json.RawMessage `json:"systems"`
		TotalCount int               `json:"total_count"`
		Offset     int               `json:"offset,omitempty"`
		Limit      int               `json:"limit,omitempty"`
	}

	if err := json.Unmarshal(rawJSON, &firstPage); err != nil {
		// If response doesn't have pagination fields, it might be old format
		// Just save it as-is
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

	// Fetch remaining pages
	limit := firstPage.Limit
	if limit == 0 {
		limit = 100 // Default if not specified
	}

	allSystems := make([]json.RawMessage, 0, totalCount)
	allSystems = append(allSystems, firstPage.Systems...)

	offset := limit
	for offset < totalCount {
		// Retry logic for each page
		maxRetries := 3
		var pageErr error
		var pageResp struct {
			Systems []json.RawMessage `json:"systems"`
			Offset  int               `json:"offset,omitempty"`
		}

		for retry := 0; retry < maxRetries; retry++ {
			if retry > 0 {
				pageNum := (offset / limit) + 1
				s.logger.Printf("  🔄 Retry %d/%d for page %d", retry, maxRetries-1, pageNum)
				// Ensure connection is ready before retrying
				if err := s.ensureConnectionReady(); err != nil {
					s.logger.Printf("  ⚠️  Failed to restore connection: %v", err)
					time.Sleep(game.SleepShort)
					continue
				}
			}

			s.client.ClearLastError()

			msg := protocol.Message{
				Type: "get_map",
				Payload: map[string]any{
					"offset": offset,
					"limit":  limit,
				},
			}
			if err := s.client.Send(ctx, msg); err != nil {
				pageErr = fmt.Errorf("get_map offset %d failed: %w", offset, err)
				time.Sleep(game.SleepRetry)
				continue
			}
			time.Sleep(2 * time.Second)

			rawJSON := s.client.GetRawJSON("systems")
			if rawJSON == nil {
				errResp := s.client.GetLastError()
				pageErr = fmt.Errorf("%s", formatErrorMessage(fmt.Sprintf("get_map offset %d", offset), errResp))
				time.Sleep(game.SleepRetry)
				continue
			}

			if err := json.Unmarshal(rawJSON, &pageResp); err != nil {
				pageErr = fmt.Errorf("failed to parse map page at offset %d: %w", offset, err)
				time.Sleep(game.SleepRetry)
				continue
			}

			// Success - break retry loop
			pageErr = nil
			break
		}

		// If all retries failed, log error but continue with remaining pages
		if pageErr != nil {
			pageNum := (offset / limit) + 1
			s.logger.Printf("  ⚠️  Error fetching page %d after %d retries: %v", pageNum, maxRetries, pageErr)
			// Continue with next page instead of failing completely
			offset += limit
			continue
		}

		pageNum := (offset / limit) + 1
		s.logger.Printf("  📖 Page %d (offset %d): %d systems", pageNum, offset, len(pageResp.Systems))
		allSystems = append(allSystems, pageResp.Systems...)

		// If we got fewer systems than the limit, we've reached the end
		if len(pageResp.Systems) < limit {
			break
		}

		offset += limit
	}

	// Build combined response
	combinedResponse := map[string]any{
		"systems": allSystems,
		"total":   len(allSystems),
		"offset":  0,
		"limit":   len(allSystems),
	}

	// Marshal combined response
	combinedJSON, err := json.MarshalIndent(combinedResponse, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal combined map data: %w", err)
	}

	s.logger.Printf("  📚 Total systems: %d", len(allSystems))
	return s.saveJSON("get_map.json", combinedJSON)
}

func (s *Scraper) scrapeWrecks() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Request wrecks
	msg := protocol.Message{
		Type: "get_wrecks",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("get_wrecks failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON
	rawJSON := s.client.GetRawJSON("wrecks")
	if rawJSON == nil {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_wrecks", errResp))
	}

	return s.saveJSON("get_wrecks.json", rawJSON)
}

func (s *Scraper) scrapeDrones() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Request drones
	msg := protocol.Message{
		Type: "get_drones",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("get_drones failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON
	rawJSON := s.client.GetRawJSON("drones")
	if rawJSON == nil {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_drones", errResp))
	}

	return s.saveJSON("get_drones.json", rawJSON)
}

func (s *Scraper) scrapeBase() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Request base info
	msg := protocol.Message{
		Type: "get_base",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("get_base failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON
	rawJSON := s.client.GetRawJSON("base")
	if rawJSON == nil {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_base", errResp))
	}

	return s.saveJSON("get_base.json", rawJSON)
}

func (s *Scraper) scrapeFactionInfo() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Request faction info
	msg := protocol.Message{
		Type: "faction_info",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("faction_info failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON
	rawJSON := s.client.GetRawJSON("faction_info")
	if rawJSON == nil {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		// If errResp is empty or only has empty values, it means no error occurred
		// but the data wasn't in the expected format
		if len(errResp) == 0 || (len(errResp) > 0 && errResp["code"] == nil && errResp["message"] == nil) {
			s.logger.Printf("  ⚠️  No faction data in response - skipping (player may not be in a faction)")
			return nil // Not a fatal error
		}
		return fmt.Errorf("%s", formatErrorMessage("faction_info", errResp))
	}

	return s.saveJSON("faction_info.json", rawJSON)
}

func (s *Scraper) scrapeCaptainsLog() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Request captain's log
	msg := protocol.Message{
		Type: "captains_log_list",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("captains_log_list failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON
	rawJSON := s.client.GetRawJSON("captains_log_list")
	if rawJSON == nil {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("captains_log_list", errResp))
	}

	return s.saveJSON("captains_log_list.json", rawJSON)
}

func (s *Scraper) scrapeListings() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Get market listings
	if err := s.client.GetListings(ctx); err != nil {
		return fmt.Errorf("get_listings failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON response (stored as "market" not "listings")
	rawJSON := s.client.GetRawJSON("market")
	if rawJSON == nil {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_listings", errResp))
	}

	return s.saveJSON("get_listings.json", rawJSON)
}

func (s *Scraper) scrapeShips() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Request shipyard showroom
	msg := protocol.Message{
		Type: "shipyard_showroom",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("shipyard_showroom failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON response (stored as "shipyard" or "ships")
	rawJSON := s.client.GetRawJSON("shipyard")
	if rawJSON == nil {
		// Try "ships" as fallback
		rawJSON = s.client.GetRawJSON("ships")
	}
	if rawJSON == nil {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("shipyard_showroom", errResp))
	}

	return s.saveJSON("get_ships.json", rawJSON)
}

func (s *Scraper) scrapeNearby() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Request nearby players
	msg := protocol.Message{
		Type: "get_nearby",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("get_nearby failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON response
	rawJSON := s.client.GetRawJSON("nearby")
	if rawJSON == nil {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_nearby", errResp))
	}

	return s.saveJSON("get_nearby.json", rawJSON)
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

	// Clear previous error
	s.client.ClearLastError()

	// Request first page from catalog
	msg := protocol.Message{
		Type: "catalog",
		Payload: map[string]any{
			"type":      catalogType,
			"page":      1,
			"page_size": 50, // Server max is 50
		},
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("catalog %s page 1 failed: %w", catalogType, err)
	}
	time.Sleep(2 * time.Second)

	// Get first page response
	rawJSON := s.client.GetRawJSON("catalog")
	if rawJSON == nil {
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage(fmt.Sprintf("catalog %s", catalogType), errResp))
	}

	// Parse the response to get pagination info
	var firstPage struct {
		Items       []json.RawMessage `json:"items"`
		Page        int               `json:"page"`
		PageSize    int               `json:"page_size"`
		Total       int               `json:"total"`
		TotalPages  int               `json:"total_pages"`
		Type        string            `json:"type"`
		Message     string            `json:"message"`
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
		// Retry logic for each page
		maxRetries := 3
		var pageErr error

		for retry := 0; retry < maxRetries; retry++ {
			if retry > 0 {
				s.logger.Printf("  🔄 Retry %d/%d for page %d", retry, maxRetries-1, page)
				// Ensure connection is ready before retrying
				if err := s.ensureConnectionReady(); err != nil {
					s.logger.Printf("  ⚠️  Failed to restore connection: %v", err)
					time.Sleep(game.SleepShort)
					continue
				}
			}

			s.client.ClearLastError()

			msg := protocol.Message{
				Type: "catalog",
				Payload: map[string]any{
					"type":      catalogType,
					"page":      page,
					"page_size": 50,
				},
			}
			if err := s.client.Send(ctx, msg); err != nil {
				pageErr = fmt.Errorf("catalog %s page %d failed: %w", catalogType, page, err)
				time.Sleep(game.SleepRetry)
				continue
			}
			time.Sleep(2 * time.Second)

			rawJSON := s.client.GetRawJSON("catalog")
			if rawJSON == nil {
				errResp := s.client.GetLastError()
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

			// Success - add items and break retry loop
			s.logger.Printf("  📖 Page %d/%d: %d items", page, firstPage.TotalPages, len(pageResp.Items))
			allItems = append(allItems, pageResp.Items...)
			pageErr = nil
			break
		}

		// If all retries failed, log error but continue with remaining pages
		if pageErr != nil {
			s.logger.Printf("  ⚠️  Error fetching page %d after %d retries: %v", page, maxRetries, pageErr)
			// Continue with next page instead of failing completely
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

	// Marshal combined response
	combinedJSON, err := json.MarshalIndent(combinedResponse, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal combined catalog: %w", err)
	}

	s.logger.Printf("  📚 Total %s: %d items", catalogType, len(allItems))
	return s.saveJSON(filename, combinedJSON)
}

func (s *Scraper) scrapeCargo() error {
	ctx := context.Background()
	s.client.ClearLastError()

	if err := s.client.GetCargo(ctx); err != nil {
		return fmt.Errorf("get_cargo failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	rawJSON := s.client.GetRawJSON("cargo")
	if rawJSON == nil {
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_cargo", errResp))
	}
	return s.saveJSON("get_cargo.json", rawJSON)
}

func (s *Scraper) scrapeMissions() error {
	ctx := context.Background()
	s.client.ClearLastError()

	if err := s.client.GetMissions(ctx); err != nil {
		return fmt.Errorf("get_missions failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	rawJSON := s.client.GetRawJSON("missions")
	if rawJSON == nil {
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_missions", errResp))
	}
	return s.saveJSON("get_missions.json", rawJSON)
}

func (s *Scraper) scrapeActiveMissions() error {
	ctx := context.Background()
	s.client.ClearLastError()

	if err := s.client.GetActiveMissions(ctx); err != nil {
		return fmt.Errorf("get_active_missions failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	rawJSON := s.client.GetRawJSON("active_missions")
	if rawJSON == nil {
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_active_missions", errResp))
	}
	return s.saveJSON("get_active_missions.json", rawJSON)
}

func (s *Scraper) scrapeOrders() error {
	ctx := context.Background()
	s.client.ClearLastError()

	msg := protocol.Message{
		Type: "view_orders",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("view_orders failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	rawJSON := s.client.GetRawJSON("orders")
	if rawJSON == nil {
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("view_orders", errResp))
	}
	return s.saveJSON("view_orders.json", rawJSON)
}

func (s *Scraper) scrapeNotes() error {
	ctx := context.Background()
	s.client.ClearLastError()

	if err := s.client.GetNotes(ctx); err != nil {
		return fmt.Errorf("get_notes failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	rawJSON := s.client.GetRawJSON("notes")
	if rawJSON == nil {
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_notes", errResp))
	}
	return s.saveJSON("get_notes.json", rawJSON)
}

func (s *Scraper) scrapeInsuranceQuote() error {
	ctx := context.Background()
	s.client.ClearLastError()

	if err := s.client.GetInsuranceQuote(ctx); err != nil {
		return fmt.Errorf("get_insurance_quote failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	rawJSON := s.client.GetRawJSON("insurance_quote")
	if rawJSON == nil {
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_insurance_quote", errResp))
	}
	return s.saveJSON("get_insurance_quote.json", rawJSON)
}

func (s *Scraper) scrapeVersion() error {
	ctx := context.Background()
	s.client.ClearLastError()

	if err := s.client.GetVersion(ctx); err != nil {
		return fmt.Errorf("get_version failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	rawJSON := s.client.GetRawJSON("version")
	if rawJSON == nil {
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_version", errResp))
	}
	return s.saveJSON("get_version.json", rawJSON)
}

func (s *Scraper) scrapeCommands() error {
	ctx := context.Background()
	s.client.ClearLastError()

	if err := s.client.GetCommands(ctx); err != nil {
		return fmt.Errorf("get_commands failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	rawJSON := s.client.GetRawJSON("commands")
	if rawJSON == nil {
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_commands", errResp))
	}
	return s.saveJSON("get_commands.json", rawJSON)
}

func (s *Scraper) scrapeStorage() error {
	ctx := context.Background()
	s.client.ClearLastError()

	if err := s.client.ViewStorage(ctx); err != nil {
		return fmt.Errorf("view_storage failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	rawJSON := s.client.GetRawJSON("storage")
	if rawJSON == nil {
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("view_storage", errResp))
	}
	return s.saveJSON("view_storage.json", rawJSON)
}

func (s *Scraper) scrapeMarket() error {
	ctx := context.Background()
	s.client.ClearLastError()

	msg := protocol.Message{
		Type: "view_market",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("view_market failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	rawJSON := s.client.GetRawJSON("view_market")
	if rawJSON == nil {
		// Try "market" as fallback (stored with action name)
		rawJSON = s.client.GetRawJSON("market")
	}
	if rawJSON == nil {
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("view_market", errResp))
	}
	return s.saveJSON("view_market.json", rawJSON)
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
