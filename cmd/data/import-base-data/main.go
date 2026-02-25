package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// BaseResponse represents the server response from get_base
type BaseResponse struct {
	Base struct {
		ID           string            `json:"id"`
		POIID        string            `json:"poi_id"`
		Name         string            `json:"name"`
		Description  string            `json:"description"`
		Empire       string            `json:"empire"`
		DefenseLevel int               `json:"defense_level"`
		HasDrones    bool              `json:"has_drones"`
		PublicAccess bool              `json:"public_access"`
		Services     map[string]bool   `json:"services"`
		Facilities   []string          `json:"facilities"`
		Market       []json.RawMessage `json:"market"`
	} `json:"base"`
	POI struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		SystemID    string `json:"system_id"`
		Description string `json:"description"`
		Position    struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		} `json:"position"`
		Type string `json:"type"`
	} `json:"poi"`
}

// MarketItem represents a market item from the server
type MarketItem struct {
	ID        string  `json:"id"`
	ItemID    string  `json:"item_id"`
	PriceEach float64 `json:"price_each"`
	Quantity  int     `json:"quantity"`
	IsNPC     bool    `json:"is_npc"`
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <base-data.json>", os.Args[0])
	}

	jsonFile := os.Args[1]

	// Read the JSON file
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	var response BaseResponse
	if err := json.Unmarshal(data, &response); err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	// Create knowledge base
	config := knowledge.DefaultConfig()
	if dbPath := os.Getenv("SPACEMOLT_DB"); dbPath != "" {
		config.DBPath = dbPath
	} else {
		config.DBPath = "data/spacemolt-knowledge.db"
	}
	kb, err := knowledge.NewSQLiteKB(config)
	if err != nil {
		log.Fatalf("Failed to open knowledge base: %v", err)
	}
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Parse market items
	var marketItems []knowledge.BaseMarketItem
	for _, rawItem := range response.Base.Market {
		var item MarketItem
		if err := json.Unmarshal(rawItem, &item); err != nil {
			log.Printf("Warning: failed to parse market item: %v", err)
			continue
		}
		marketItems = append(marketItems, knowledge.BaseMarketItem{
			ID:        item.ID,
			ItemID:    item.ItemID,
			PriceEach: item.PriceEach,
			Quantity:  item.Quantity,
			IsNPC:     item.IsNPC,
		})
	}

	// Parse facilities with category information
	var facilities []knowledge.Facility
	for _, facilityID := range response.Base.Facilities {
		// Look up facility in the mapping to get category and level
		if facilityInfo, ok := knowledge.FacilityCategoryMapping[facilityID]; ok {
			facilities = append(facilities, knowledge.Facility{
				ID:          facilityInfo.ID,
				Name:        facilityInfo.Name,
				Category:    facilityInfo.Category,
				Level:       facilityInfo.Level,
				LastUpdated: 0, // Import tool doesn't have tick info
			})
		} else {
			// Unknown facility - add with minimal info
			facilities = append(facilities, knowledge.Facility{
				ID:          facilityID,
				Name:        facilityID,
				Category:    "unknown",
				Level:       0,
				LastUpdated: 0,
			})
		}
	}

	// Create SpaceBase
	base := knowledge.SpaceBase{
		ID:              response.Base.ID,
		POIID:           response.Base.POIID,
		Name:            response.Base.Name,
		Description:     response.Base.Description,
		Empire:          response.Base.Empire,
		DefenseLevel:    response.Base.DefenseLevel,
		HasDrones:       response.Base.HasDrones,
		PublicAccess:    response.Base.PublicAccess,
		Services:        response.Base.Services,
		Facilities:      facilities,
		Market:          marketItems,
		LastUpdatedTick: 0,
	}

	// Save to database
	if err := kb.RememberBase(ctx, base); err != nil {
		log.Fatalf("Failed to save base: %v", err)
	}

	fmt.Printf("✓ Successfully imported base: %s (ID: %s)\n", base.Name, base.ID)
	fmt.Printf("  POI: %s\n", base.POIID)
	fmt.Printf("  Empire: %s\n", base.Empire)
	fmt.Printf("  Facilities: %d\n", len(base.Facilities))
	fmt.Printf("  Market items: %d\n", len(base.Market))
	fmt.Printf("  Services: %d\n", len(base.Services))
}
