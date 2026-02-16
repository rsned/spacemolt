package game

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FindStation returns the first station POI in the current system, or nil if none found
func FindStation(state *State) *POI {
	for i := range state.System.POIs {
		if state.System.POIs[i].Type == "station" {
			return &state.System.POIs[i]
		}
	}
	return nil
}

// FindStationID returns the ID of the first station in the current system, or empty string if none found
func FindStationID(state *State) string {
	for i := range state.System.POIs {
		if state.System.POIs[i].Type == "station" {
			return state.System.POIs[i].ID
		}
	}
	return ""
}

// HasStation checks if the current system has a station
func HasStation(state *State) bool {
	return FindStation(state) != nil
}

// FindJumpGate returns the first jump gate POI in the current system, or nil if none found
func FindJumpGate(state *State) *POI {
	for i := range state.System.POIs {
		if state.System.POIs[i].Type == "jump_gate" {
			return &state.System.POIs[i]
		}
	}
	return nil
}

// FindMiningLocation returns the first mining POI (asteroid_belt or asteroid_field) in the current system, or nil if none found
func FindMiningLocation(state *State) *POI {
	for i := range state.System.POIs {
		poi := state.System.POIs[i]
		if poi.Type == "asteroid_belt" || poi.Type == "asteroid_field" {
			return &poi
		}
	}
	return nil
}

// FindPOIByType returns the first POI of the given type in the current system, or nil if none found
func FindPOIByType(state *State, poiType string) *POI {
	for i := range state.System.POIs {
		if state.System.POIs[i].Type == poiType {
			return &state.System.POIs[i]
		}
	}
	return nil
}

// ============================================================================
// STATION INTERACTION HELPERS
// ============================================================================

// DockAtStation docks the client at a station POI
// Returns error if docking fails (excluding "already docked" which is treated as success)
func DockAtStation(client *Client, ctx context.Context, stationPOI string) error {
	if err := client.Dock(ctx); err != nil && err.Error() != "Already docked (success)" {
		return err
	}
	return nil
}

// UndockFromStation undocks the client from the current station
func UndockFromStation(client *Client, ctx context.Context) error {
	return client.Undock(ctx)
}

// WithDocked executes a function while docked at a station
// Docks, runs the action, then undocks. Useful for station operations like
// refueling, repairing, buying/selling, etc.
//
// Example:
//
//	err := game.WithDocked(client, ctx, logger, stationID, func() error {
//	    if err := client.Refuel(ctx); err != nil {
//	        return err
//	    }
//	    time.Sleep(game.SleepShort)
//	    return nil
//	})
func WithDocked(client *Client, ctx context.Context, stationID string, action func() error) error {
	// Dock
	if err := client.Dock(ctx); err != nil && err.Error() != "Already docked (success)" {
		return err
	}

	// Run the action
	if err := action(); err != nil {
		return err
	}

	// Undock
	return client.Undock(ctx)
}

// ============================================================================
// FILE SAVE HELPERS
// ============================================================================

// TimestampFormat constants for consistent filename generation
const (
	TimestampDaily     = "20060102"           // YYYYMMDD - daily snapshots
	TimestampPrecise   = "20060102150405"     // YYYYMMDDHHMMSS - precise timestamps
	TimestampISO8601   = time.RFC3339         // ISO8601 format for data fields
)

// SaveJSONToFile saves JSON data to a file in the specified directory
// Creates the directory if it doesn't exist
//
// Parameters:
//   - data: The JSON data to save (already marshaled to []byte)
//   - dir: The directory path (e.g., "data/server/systems")
//   - filename: The filename (e.g., "sol.20250101.json")
//
// Returns error if directory creation or file write fails
func SaveJSONToFile(data []byte, dir, filename string) error {
	// Ensure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write to file
	filePath := filepath.Join(dir, filename)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// SaveStructToFile saves any Go struct as JSON to a file
// Marshals the struct with pretty-printing (indent 2 spaces)
//
// Example:
//
//	data := map[string]any{
//	    "system_id": "sol",
//	    "timestamp": time.Now(),
//	}
//	if err := game.SaveStructToFile(data, "data/server/systems", "sol.json"); err != nil {
//	    log.Printf("Failed to save: %v", err)
//	}
func SaveStructToFile(data any, dir, filename string) error {
	// Marshal to JSON with indentation
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Save to file
	return SaveJSONToFile(jsonData, dir, filename)
}

// GenerateDataFilename creates a standardized filename for data files
// Format: {sanitized_name}.{timestamp}.{type}.json
//
// Examples:
//   - GenerateDataFilename("Solar System", "20250101", "system") -> "solar_system.20250101.system.json"
//   - GenerateDataFilename("Alpha Station", "20250102150405", "market") -> "alpha_station.20250102150405.market.json"
func GenerateDataFilename(name, timestamp, dataType string) string {
	// Sanitize the name
	sanitized := sanitizeFilename(name)
	return fmt.Sprintf("%s.%s.%s.json", sanitized, timestamp, dataType)
}

// sanitizeFilename replaces spaces and special characters with underscores
// Used by GenerateDataFilename to create safe filenames
func sanitizeFilename(name string) string {
	// Replace spaces and special chars with underscores
	result := ""
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			result += string(c)
		} else if c == ' ' {
			result += "_"
		}
		// Skip other special characters
	}
	return result
}
