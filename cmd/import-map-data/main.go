package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// MapResponse represents the server response from get_map
type MapResponse struct {
	Systems []MapSystem `json:"systems"`
}

// MapSystem represents a system in the map data
type MapSystem struct {
	SystemID     string   `json:"system_id"`
	Name         string   `json:"name"`
	Empire       string   `json:"empire,omitempty"`
	Position     Position `json:"position"`
	Connections  []string `json:"connections"`
	POICount     int      `json:"poi_count"`
	Online       int      `json:"online"`
	Visited      bool     `json:"visited"`
	VisitedAt    string   `json:"visited_at"`
	IsStronghold bool     `json:"is_stronghold,omitempty"`
}

// Position represents 2D coordinates
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <map-data.json>", os.Args[0])
	}

	jsonFile := os.Args[1]

	// Read the JSON file
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	var response MapResponse
	if err := json.Unmarshal(data, &response); err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	if len(response.Systems) == 0 {
		log.Fatalf("No systems found in JSON file")
	}

	// Create knowledge base
	config := knowledge.DefaultConfig()
	config.DBPath = "data/spacemolt-knowledge.db"
	kb, err := knowledge.NewSQLiteKB(config)
	if err != nil {
		log.Fatalf("Failed to open knowledge base: %v", err)
	}

	ctx := context.Background()

	// Import systems using partial upsert that preserves richer explorer data.
	// Map data provides: name, position, connections, empire (some), is_stronghold (some).
	// It does NOT provide: police_level, description — so we must not overwrite those.
	systemCount := 0
	connectionCount := 0
	for _, sysData := range response.Systems {
		if err := kb.UpsertSystemFromMap(ctx, knowledge.MapSystemData{
			ID:           sysData.SystemID,
			Name:         sysData.Name,
			Empire:       sysData.Empire,
			PositionX:    sysData.Position.X,
			PositionY:    sysData.Position.Y,
			IsStronghold: sysData.IsStronghold,
			Connections:  sysData.Connections,
		}); err != nil {
			log.Printf("Warning: failed to save system %s: %v", sysData.SystemID, err)
			continue
		}

		systemCount++
		connectionCount += len(sysData.Connections)
	}

	fmt.Printf("✓ Successfully imported map data:\n")
	fmt.Printf("  Systems: %d\n", systemCount)
	fmt.Printf("  Connections: %d\n", connectionCount)

	// Close the knowledge base
	if err := kb.Close(); err != nil {
		log.Printf("Warning: failed to close knowledge base: %v", err)
	}
}
