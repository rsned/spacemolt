package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// ImportTracker tracks what data exists in files vs database
type ImportTracker struct {
	MissingInDB  []MissingItem
	ElementsNoID []NoIDElement
}

// MissingItem represents data found in files but missing from database
type MissingItem struct {
	DataType   string // e.g., "system", "poi", "base", "item", "skill", "recipe", "ship"
	ID         string
	SourceFile string
}

// NoIDElement represents an element without an ID field
type NoIDElement struct {
	DataType   string
	Index      int    // Position in array
	SourceFile string
	Context    string // JSON snippet for identification
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <game-api-data-dir>", os.Args[0])
	}

	dataDir := os.Args[1]
	tracker := &ImportTracker{}

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

	// Walk through all JSON files
	err = filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}

		// Skip session files and non-data files
		if strings.Contains(filepath.Base(path), "session") ||
			strings.Contains(filepath.Base(path), "spacemolt") {
			return nil
		}

		relPath, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}

		// Process the file based on its name
		if err := processFile(ctx, kb, path, relPath, tracker); err != nil {
			log.Printf("Warning: error processing %s: %v", relPath, err)
		}
		return nil
	})

	if err != nil {
		log.Fatalf("Error walking directory: %v", err)
	}

	// Print report
	printReport(tracker)
}

func processFile(ctx context.Context, kb *knowledge.SQLiteKB, path, relPath string, tracker *ImportTracker) error {
	filename := filepath.Base(path)

	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Warning: failed to read %s: %v", relPath, err)
		return nil
	}

	// Use tagged switch on filename
	switch filename {
	case "get_system.json":
		return checkSystemData(ctx, kb, data, path, tracker)
	case "get_map.json":
		return checkMapData(ctx, kb, data, path, tracker)
	case "get_poi.json":
		return checkPOIData(ctx, kb, data, path, tracker)
	case "get_base.json":
		return checkBaseData(ctx, kb, data, path, tracker)
	case "catalog_items.json":
		return checkCatalogItems(ctx, kb, data, path, tracker)
	case "catalog_skills.json":
		return checkCatalogSkills(ctx, kb, data, path, tracker)
	case "catalog_recipes.json":
		return checkCatalogRecipes(ctx, kb, data, path, tracker)
	case "catalog_ships.json":
		return checkCatalogShips(ctx, kb, data, path, tracker)
	case "get_nearby.json":
		// Nearby players are ephemeral
		return nil
	case "get_wrecks.json":
		return checkWrecksData(ctx, kb, data, path, tracker)
	case "get_ship.json", "get_status.json", "get_skills.json", "get_ships.json", "get_listings.json":
		// These are either player-specific or ephemeral data
		return nil
	default:
		// Unknown file type - skip
		return nil
	}
}

func checkSystemData(ctx context.Context, kb *knowledge.SQLiteKB, data []byte, path string, tracker *ImportTracker) error {
	var response struct {
		System struct {
			ID string `json:"id"`
		} `json:"system"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return err
	}

	if response.System.ID == "" {
		tracker.ElementsNoID = append(tracker.ElementsNoID, NoIDElement{
			DataType:   "system",
			SourceFile: path,
			Context:    "root system object",
		})
		return nil
	}

	// Check if system exists in DB
	sys, err := kb.GetSystem(ctx, response.System.ID)
	if err != nil || sys == nil {
		tracker.MissingInDB = append(tracker.MissingInDB, MissingItem{
			DataType:   "system",
			ID:         response.System.ID,
			SourceFile: path,
		})
	}
	return nil
}

func checkMapData(ctx context.Context, kb *knowledge.SQLiteKB, data []byte, path string, tracker *ImportTracker) error {
	var response struct {
		Systems []struct {
			SystemID     string   `json:"system_id"`
			Name         string   `json:"name"`
			PositionX    float64  `json:"position_x"`
			PositionY    float64  `json:"position_y"`
			Empire       string   `json:"empire"`
			IsStronghold bool     `json:"is_stronghold"`
			Connections  []string `json:"connections"`
			Position     struct {
				X float64 `json:"x"`
				Y float64 `json:"y"`
			} `json:"position"`
		} `json:"systems"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return err
	}

	for i, sysData := range response.Systems {
		// Map data uses system_id instead of id
		systemID := sysData.SystemID
		if systemID == "" {
			tracker.ElementsNoID = append(tracker.ElementsNoID, NoIDElement{
				DataType:   "map_system",
				Index:      i,
				SourceFile: path,
				Context:    fmt.Sprintf("name=%s", sysData.Name),
			})
			continue
		}

		// Check if system exists in DB
		sys, err := kb.GetSystem(ctx, systemID)
		if err != nil || sys == nil {
			tracker.MissingInDB = append(tracker.MissingInDB, MissingItem{
				DataType:   "system",
				ID:         systemID,
				SourceFile: path,
			})
		}
	}
	return nil
}

func checkPOIData(_ context.Context, _ *knowledge.SQLiteKB, data []byte, path string, tracker *ImportTracker) error {
	var response struct {
		POI struct {
			ID string `json:"id"`
		} `json:"poi"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return err
	}

	if response.POI.ID == "" {
		tracker.ElementsNoID = append(tracker.ElementsNoID, NoIDElement{
			DataType:   "poi",
			SourceFile: path,
			Context:    "root poi object",
		})
	}
	return nil
}

func checkBaseData(ctx context.Context, kb *knowledge.SQLiteKB, data []byte, path string, tracker *ImportTracker) error {
	var response struct {
		Base *struct {
			ID string `json:"id"`
		} `json:"base"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return err
	}

	// If base is null (agent not at a base), skip
	if response.Base == nil {
		return nil
	}

	if response.Base.ID == "" {
		tracker.ElementsNoID = append(tracker.ElementsNoID, NoIDElement{
			DataType:   "base",
			SourceFile: path,
			Context:    "root base object",
		})
		return nil
	}

	// Check if base exists in DB
	base, err := kb.GetBase(ctx, response.Base.ID)
	if err != nil || base == nil {
		tracker.MissingInDB = append(tracker.MissingInDB, MissingItem{
			DataType:   "base",
			ID:         response.Base.ID,
			SourceFile: path,
		})
	}
	return nil
}

func checkCatalogItems(ctx context.Context, kb *knowledge.SQLiteKB, data []byte, path string, tracker *ImportTracker) error {
	var response struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return err
	}

	for i, item := range response.Items {
		// Empty ID is treated as missing ID
		if item.ID == "" {
			tracker.ElementsNoID = append(tracker.ElementsNoID, NoIDElement{
				DataType:   "catalog_item",
				Index:      i,
				SourceFile: path,
				Context:    fmt.Sprintf("item at index %d", i),
			})
			continue
		}

		// Check if item exists in DB
		dbItem, err := kb.GetItem(ctx, item.ID)
		if err != nil || dbItem == nil {
			tracker.MissingInDB = append(tracker.MissingInDB, MissingItem{
				DataType:   "catalog_item",
				ID:         item.ID,
				SourceFile: path,
			})
		}
	}
	return nil
}

func checkCatalogSkills(_ context.Context, kb *knowledge.SQLiteKB, data []byte, path string, tracker *ImportTracker) error {
	var response struct {
		Skills []struct {
			ID string `json:"id"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return err
	}

	for i, skill := range response.Skills {
		if skill.ID == "" {
			tracker.ElementsNoID = append(tracker.ElementsNoID, NoIDElement{
				DataType:   "catalog_skill",
				Index:      i,
				SourceFile: path,
				Context:    fmt.Sprintf("skill at index %d", i),
			})
			continue
		}

		// Check if skill exists in DB
		skills := kb.GetSkills()
		found := false
		for _, dbSkill := range skills {
			if dbSkill.ID == skill.ID {
				found = true
				break
			}
		}
		if !found {
			tracker.MissingInDB = append(tracker.MissingInDB, MissingItem{
				DataType:   "catalog_skill",
				ID:         skill.ID,
				SourceFile: path,
			})
		}
	}
	return nil
}

func checkCatalogRecipes(ctx context.Context, kb *knowledge.SQLiteKB, data []byte, path string, tracker *ImportTracker) error {
	var response struct {
		Recipes []struct {
			ID string `json:"id"`
		} `json:"recipes"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return err
	}

	for i, recipe := range response.Recipes {
		if recipe.ID == "" {
			tracker.ElementsNoID = append(tracker.ElementsNoID, NoIDElement{
				DataType:   "catalog_recipe",
				Index:      i,
				SourceFile: path,
				Context:    fmt.Sprintf("recipe at index %d", i),
			})
			continue
		}

		// Check if recipe exists in DB
		recipes, err := kb.GetRecipes(ctx)
		if err != nil {
			continue
		}
		found := false
		for _, dbRecipe := range recipes {
			if dbRecipe.ID == recipe.ID {
				found = true
				break
			}
		}
		if !found {
			tracker.MissingInDB = append(tracker.MissingInDB, MissingItem{
				DataType:   "catalog_recipe",
				ID:         recipe.ID,
				SourceFile: path,
			})
		}
	}
	return nil
}

func checkCatalogShips(ctx context.Context, kb *knowledge.SQLiteKB, data []byte, path string, tracker *ImportTracker) error {
	var response struct {
		Ships []struct {
			ID string `json:"id"`
		} `json:"ships"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return err
	}

	for i, ship := range response.Ships {
		if ship.ID == "" {
			tracker.ElementsNoID = append(tracker.ElementsNoID, NoIDElement{
				DataType:   "catalog_ship",
				Index:      i,
				SourceFile: path,
				Context:    fmt.Sprintf("ship at index %d", i),
			})
			continue
		}

		// Check if ship exists in DB
		dbShip, err := kb.GetShipClass(ctx, ship.ID)
		if err != nil || dbShip == nil {
			tracker.MissingInDB = append(tracker.MissingInDB, MissingItem{
				DataType:   "catalog_ship",
				ID:         ship.ID,
				SourceFile: path,
			})
		}
	}
	return nil
}

func checkWrecksData(_ context.Context, _ *knowledge.SQLiteKB, data []byte, path string, tracker *ImportTracker) error {
	var response struct {
		Wrecks []struct {
			ID string `json:"id"`
		} `json:"wrecks"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return err
	}

	for i, wreck := range response.Wrecks {
		if wreck.ID == "" {
			tracker.ElementsNoID = append(tracker.ElementsNoID, NoIDElement{
				DataType:   "wreck",
				Index:      i,
				SourceFile: path,
				Context:    fmt.Sprintf("wreck at index %d", i),
			})
		}
	}
	return nil
}

func printReport(tracker *ImportTracker) {
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("GAME API DATA IMPORT CHECK REPORT")
	fmt.Println(strings.Repeat("=", 80))

	// Group missing items by data type
	byType := make(map[string][]MissingItem)
	for _, item := range tracker.MissingInDB {
		byType[item.DataType] = append(byType[item.DataType], item)
	}

	// Print missing items summary
	fmt.Printf("\n📊 SUMMARY: %d items found in files but missing from database\n", len(tracker.MissingInDB))
	fmt.Println(strings.Repeat("-", 80))

	totalFiles := make(map[string]int)
	for dataType := range byType {
		files := make(map[string]bool)
		for _, item := range byType[dataType] {
			files[item.SourceFile] = true
		}
		totalFiles[dataType] = len(files)
	}

	// Print by data type
	types := []string{"system", "poi", "base", "catalog_item", "catalog_skill", "catalog_recipe", "catalog_ship", "wreck"}
	for _, dataType := range types {
		if items, ok := byType[dataType]; ok {
			fmt.Printf("\n🔍 %s: %d missing items across %d files\n", dataType, len(items), totalFiles[dataType])

			// Show first 5 examples
			for i, item := range items {
				if i >= 5 {
					fmt.Printf("  ... and %d more\n", len(items)-5)
					break
				}
				fmt.Printf("  - %s (from %s)\n", item.ID, filepath.Base(item.SourceFile))
			}
		}
	}

	// Print elements without ID
	fmt.Printf("\n⚠️  ELEMENTS WITHOUT ID: %d items\n", len(tracker.ElementsNoID))
	fmt.Println(strings.Repeat("-", 80))

	byTypeNoID := make(map[string][]NoIDElement)
	for _, elem := range tracker.ElementsNoID {
		byTypeNoID[elem.DataType] = append(byTypeNoID[elem.DataType], elem)
	}

	for dataType, elems := range byTypeNoID {
		fmt.Printf("\n🚫 %s: %d items without ID\n", dataType, len(elems))
		for i, elem := range elems {
			if i >= 3 {
				fmt.Printf("  ... and %d more\n", len(elems)-3)
				break
			}
			if elem.Index >= 0 {
				fmt.Printf("  - Index %d in %s (%s)\n", elem.Index, filepath.Base(elem.SourceFile), elem.Context)
			} else {
				fmt.Printf("  - In %s (%s)\n", filepath.Base(elem.SourceFile), elem.Context)
			}
		}

		// Special note for catalog items with empty IDs
		if dataType == "catalog_item" && len(elems) > 0 {
			fmt.Printf("  ℹ️  Note: These items may have 'type_id' instead of 'id'\n")
		}
	}

	// Print conclusion
	fmt.Println("\n" + strings.Repeat("=", 80))
	if len(tracker.MissingInDB) == 0 && len(tracker.ElementsNoID) == 0 {
		fmt.Println("✅ All data appears to be imported successfully!")
	} else {
		fmt.Printf("❌ Found %d missing items and %d items without ID\n",
			len(tracker.MissingInDB), len(tracker.ElementsNoID))
		fmt.Println("\n📝 Recommendations:")
		if len(tracker.MissingInDB) > 0 {
			fmt.Println("  - Run import tools for missing data types")
			fmt.Println("  - Use: go run cmd/import-catalog-*/main.go <path-to-json>")
			fmt.Println("  - Use: go run cmd/import-base-data/main.go <path-to-json>")
			fmt.Println("  - Use: go run cmd/import-map-data/main.go <path-to-json>")
		}
		if len(tracker.ElementsNoID) > 0 {
			fmt.Println("  - Investigate files with missing ID fields")
			fmt.Println("  - These items were skipped during import")
		}
	}
	fmt.Println(strings.Repeat("=", 80))
}
