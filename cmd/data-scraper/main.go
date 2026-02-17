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

// Helper function to get POI name by ID
func getPOIName(poiID string, pois []game.POI) string {
	for _, poi := range pois {
		if poi.ID == poiID {
			return poi.Name
		}
	}
	return poiID
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: data-scraper <agent-id>")
		fmt.Println("This tool scrapes game API data for the specified agent")
		fmt.Println()
		os.Exit(1)
	}

	agentID := os.Args[1]
	logger := log.New(os.Stdout, "[SCRAPER] ", log.LstdFlags)

	logger.Println("🚀 SpaceMolt Game API Scraper (WebSocket)")
	logger.Println("==========================================")
	logger.Printf("Agent: %s\n", agentID)
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

	// Scrape all data
	if err := scraper.scrapeAll(); err != nil {
		logger.Fatalf("Scraping failed: %v", err)
	}

	logger.Println("\n✅ Scraping complete!")
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
		{"Ship Listings", s.scrapeShips},
		{"Nearby Players", s.scrapeNearby},
		{"Skills", s.scrapeSkills},
		{"Recipes", s.scrapeRecipes},
		{"Wrecks", s.scrapeWrecks},
		{"Drones", s.scrapeDrones},
		{"Base Info", s.scrapeBase},
		{"Faction Info", s.scrapeFactionInfo},
		{"Captain's Log", s.scrapeCaptainsLog},
	}

	for _, cat := range categories {
		s.logger.Printf("\n📊 Scraping: %s...", cat.name)
		if err := cat.fn(); err != nil {
			s.logger.Printf("⚠️  Error: %v", err)
			// Continue with other categories
		}
		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

func (s *Scraper) scrapeStatus() error {
	ctx := context.Background()

	// Get status
	if err := s.client.GetStatus(ctx); err != nil {
		return fmt.Errorf("get_status failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	// Save state
	state := s.client.GetState()
	return s.saveJSON("get_status.json", state)
}

func (s *Scraper) scrapeShip() error {
	// Ship data is already populated after login
	// Just save the ship portion of the state
	state := s.client.GetState()
	return s.saveJSON("get_ship.json", map[string]any{
		"id":             state.Ship.ID,
		"owner_id":       state.Ship.OwnerID,
		"class_id":       state.Ship.ClassID,
		"name":           state.Ship.Name,
		"hull":           state.Ship.Hull,
		"max_hull":       state.Ship.MaxHull,
		"shield":         state.Ship.Shield,
		"max_shield":     state.Ship.MaxShield,
		"shield_recharge": state.Ship.ShieldRecharge,
		"armor":          state.Ship.Armor,
		"speed":          state.Ship.Speed,
		"fuel":           state.Ship.Fuel,
		"max_fuel":       state.Ship.MaxFuel,
		"cargo_used":     state.Ship.CargoUsed,
		"cargo_capacity": state.Ship.CargoCapacity,
		"cpu_used":       state.Ship.CPUUsed,
		"cpu_capacity":   state.Ship.CPUCapacity,
		"power_used":     state.Ship.PowerUsed,
		"power_capacity": state.Ship.PowerCapacity,
		"weapon_slots":   state.Ship.WeaponSlots,
		"utility_slots":  state.Ship.UtilitySlots,
		"cargo":          state.Ship.Cargo,
		"modules":        state.Ship.Modules,
	})
}

func (s *Scraper) scrapePOI() error {
	ctx := context.Background()

	// Get POI info
	if err := s.client.GetPOI(ctx); err != nil {
		return fmt.Errorf("get_poi failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	// Save state
	state := s.client.GetState()
	return s.saveJSON("get_poi.json", map[string]any{
		"id":          state.CurrentPOI,
		"name":        getPOIName(state.CurrentPOI, state.System.POIs),
		"system_id":   state.System.ID,
		"system_name": state.System.Name,
		"pois":        state.System.POIs,
	})
}

func (s *Scraper) scrapeSystem() error {
	ctx := context.Background()

	// Get system info
	if err := s.client.GetSystem(ctx); err != nil {
		return fmt.Errorf("get_system failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	// Save state
	state := s.client.GetState()
	return s.saveJSON("get_system.json", state.System)
}

func (s *Scraper) scrapeMap() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Request map data
	msg := protocol.Message{
		Type: "get_map",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("get_map failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON (stored with key "systems" since payload has {"systems": [...]})
	rawJSON := s.client.GetRawJSON("systems")
	if rawJSON == nil {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_map", errResp))
	}

	return s.saveJSON("get_map.json", rawJSON)
}

func (s *Scraper) scrapeListings() error {
	ctx := context.Background()

	// Get listings
	if err := s.client.GetListings(ctx); err != nil {
		return fmt.Errorf("get_listings failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	// Get market listings
	listings := s.client.GetMarketListings()
	return s.saveJSON("get_listings.json", listings)
}

func (s *Scraper) scrapeShips() error {
	ctx := context.Background()

	// Get ships
	if err := s.client.GetShips(ctx); err != nil {
		return fmt.Errorf("get_ships failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	// Get ship listings
	ships := s.client.GetShipListings()
	return s.saveJSON("get_ships.json", ships)
}

func (s *Scraper) scrapeNearby() error {
	ctx := context.Background()

	// Status update includes nearby players
	if err := s.client.GetStatus(ctx); err != nil {
		return fmt.Errorf("get_nearby failed: %w", err)
	}
	time.Sleep(1 * time.Second)

	// Save nearby
	state := s.client.GetState()
	return s.saveJSON("get_nearby.json", state.Nearby)
}

func (s *Scraper) scrapeSkills() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Request skills info
	msg := protocol.Message{
		Type: "get_skills",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("get_skills failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get skills from state
	state := s.client.GetState()

	// Check if we actually got skills data
	if len(state.SkillDefinitions) == 0 && len(state.SkillXP) == 0 {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		if len(errResp) > 0 {
			return fmt.Errorf("%s", formatErrorMessage("get_skills", errResp))
		}
		return fmt.Errorf("no skills data available")
	}

	return s.saveJSON("get_skills.json", map[string]any{
		"skills":          state.SkillDefinitions,
		"skill_xp":        state.SkillXP,
		"skill_next_xp":   state.SkillNextLevelXP,
	})
}

func (s *Scraper) scrapeRecipes() error {
	ctx := context.Background()

	// Clear previous error
	s.client.ClearLastError()

	// Request recipes info
	msg := protocol.Message{
		Type: "get_recipes",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("get_recipes failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON response
	rawJSON := s.client.GetRawJSON("recipes")
	if rawJSON == nil {
		// Check if there was an error response
		errResp := s.client.GetLastError()
		return fmt.Errorf("%s", formatErrorMessage("get_recipes", errResp))
	}

	// Save raw JSON
	return s.saveJSON("get_recipes.json", rawJSON)
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

