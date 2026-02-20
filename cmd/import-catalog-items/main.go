package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// ItemsResponse represents the server response from catalog_items
type ItemsResponse struct {
	Items []CatalogItemJSON `json:"items"`
}

// CatalogItemJSON represents an item from the game catalog JSON
type CatalogItemJSON struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Rarity      string  `json:"rarity"`
	Size        int     `json:"size"`
	BaseValue   int     `json:"base_value"`
	Stackable   bool    `json:"stackable"`
	Tradeable   bool    `json:"tradeable"`
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <catalog-items.json>", os.Args[0])
	}

	jsonFile := os.Args[1]

	// Read the JSON file
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	var response ItemsResponse
	if err := json.Unmarshal(data, &response); err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	if len(response.Items) == 0 {
		log.Fatalf("No items found in JSON file")
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

	// Convert JSON items to catalog items
	var items []knowledge.CatalogItem
	skippedCount := 0
	for _, item := range response.Items {
		// Skip items with empty IDs
		if item.ID == "" {
			skippedCount++
			continue
		}
		items = append(items, knowledge.CatalogItem{
			ID:          item.ID,
			Name:        item.Name,
			Description: item.Description,
			Category:    item.Category,
			Rarity:      item.Rarity,
			Size:        item.Size,
			BaseValue:   item.BaseValue,
			Stackable:   item.Stackable,
			Tradeable:   item.Tradeable,
		})
	}

	if skippedCount > 0 {
		log.Printf("Warning: skipped %d items with empty IDs", skippedCount)
	}

	// Store items in database
	if err := kb.StoreItems(ctx, items); err != nil {
		log.Fatalf("Failed to store items: %v", err)
	}

	fmt.Printf("✓ Successfully imported %d items\n", len(items))
}
