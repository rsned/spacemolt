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
	outputDir    = "data/game-api"
	agentID      = "random-2"
	serverURL    = "wss://game.spacemolt.com/ws"
)

type Scraper struct {
	client *game.Client
	logger *log.Logger
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
	logger := log.New(os.Stdout, "[SCRAPER] ", log.LstdFlags)

	logger.Println("🚀 SpaceMolt Game API Scraper (WebSocket)")
	logger.Println("==========================================")
	logger.Printf("Agent: %s\n", agentID)
	logger.Println()

	// Create scraper
	scraper := &Scraper{
		logger: logger,
	}

	// Initialize client
	if err := scraper.init(); err != nil {
		logger.Fatalf("Initialization failed: %v", err)
	}
	defer scraper.client.Close()

	// Scrape all data
	if err := scraper.scrapeAll(); err != nil {
		logger.Fatalf("Scraping failed: %v", err)
	}

	logger.Println("\n✅ Scraping complete!")
}

func (s *Scraper) init() error {
	// Load credentials
	credsPath := filepath.Join("data/agents", agentID, "credentials.json")
	credsData, err := os.ReadFile(credsPath)
	if err != nil {
		return fmt.Errorf("failed to read credentials: %w", err)
	}

	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Empire   string `json:"empire"`
	}
	if err := json.Unmarshal(credsData, &creds); err != nil {
		return fmt.Errorf("failed to parse credentials: %w", err)
	}

	// Create client with credentials
	s.client = game.NewClient(serverURL, creds.Username, creds.Password, s.logger)

	// Connect
	s.logger.Println("🔌 Connecting to game server...")
	ctx := context.Background()
	if err := s.client.Connect(ctx); err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	// Wait for ready
	s.logger.Println("⏳ Waiting for connection ready...")
	<-s.client.Ready()
	time.Sleep(1 * time.Second)
	s.logger.Println("✓ Connected")

	// Login
	s.logger.Printf("🔑 Logging in as %s...", creds.Username)
	if err := s.client.Login(context.Background()); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}
	time.Sleep(2 * time.Second)
	s.logger.Println("✓ Logged in")

	return nil
}

func (s *Scraper) scrapeAll() error {
	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
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
		{"Market Listings", s.scrapeListings},
		{"Ship Listings", s.scrapeShips},
		{"Nearby Players", s.scrapeNearby},
		{"Skills", s.scrapeSkills},
		{"Recipes", s.scrapeRecipes},
		{"Notifications", s.scrapeNotifications},
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
	return s.saveJSON("get_skills.json", map[string]any{
		"skills":          state.SkillDefinitions,
		"skill_xp":        state.SkillXP,
		"skill_next_xp":   state.SkillNextLevelXP,
	})
}

func (s *Scraper) scrapeRecipes() error {
	ctx := context.Background()

	// Request recipes info
	msg := protocol.Message{
		Type: "get_recipes",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("get_recipes failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON response
	rawJSON := s.client.GetRawJSON("get_recipes")
	if rawJSON == nil {
		return fmt.Errorf("no recipes data available")
	}

	// Save raw JSON
	return s.saveJSON("get_recipes.json", rawJSON)
}

func (s *Scraper) scrapeNotifications() error {
	ctx := context.Background()

	// Request notifications
	msg := protocol.Message{
		Type: "get_notifications",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("get_notifications failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON
	rawJSON := s.client.GetRawJSON("get_notifications")
	if rawJSON == nil {
		return fmt.Errorf("no notifications data available")
	}

	return s.saveJSON("get_notifications.json", rawJSON)
}

func (s *Scraper) scrapeWrecks() error {
	ctx := context.Background()

	// Request wrecks
	msg := protocol.Message{
		Type: "get_wrecks",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("get_wrecks failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON
	rawJSON := s.client.GetRawJSON("get_wrecks")
	if rawJSON == nil {
		return fmt.Errorf("no wrecks data available")
	}

	return s.saveJSON("get_wrecks.json", rawJSON)
}

func (s *Scraper) scrapeDrones() error {
	ctx := context.Background()

	// Request drones
	msg := protocol.Message{
		Type: "get_drones",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("get_drones failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON
	rawJSON := s.client.GetRawJSON("get_drones")
	if rawJSON == nil {
		return fmt.Errorf("no drones data available")
	}

	return s.saveJSON("get_drones.json", rawJSON)
}

func (s *Scraper) scrapeBase() error {
	ctx := context.Background()

	// Request base info
	msg := protocol.Message{
		Type: "get_base",
	}
	if err := s.client.Send(ctx, msg); err != nil {
		return fmt.Errorf("get_base failed: %w", err)
	}
	time.Sleep(2 * time.Second)

	// Get raw JSON
	rawJSON := s.client.GetRawJSON("get_base")
	if rawJSON == nil {
		return fmt.Errorf("no base data available")
	}

	return s.saveJSON("get_base.json", rawJSON)
}

func (s *Scraper) scrapeFactionInfo() error {
	ctx := context.Background()

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
		return fmt.Errorf("no faction info available")
	}

	return s.saveJSON("faction_info.json", rawJSON)
}

func (s *Scraper) scrapeCaptainsLog() error {
	ctx := context.Background()

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
		return fmt.Errorf("no captain's log data available")
	}

	return s.saveJSON("captains_log_list.json", rawJSON)
}

func (s *Scraper) saveJSON(filename string, data any) error {
	// Marshal JSON
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Save to file
	outputPath := filepath.Join(outputDir, filename)
	if err := os.WriteFile(outputPath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	s.logger.Printf("  ✓ Saved %s", filename)
	return nil
}

