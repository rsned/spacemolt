package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RouteProgress stores navigation state for disconnect recovery
type RouteProgress struct {
	DestinationSystem string      `json:"destination_system"`
	DestinationPOI    string      `json:"destination_poi,omitempty"`
	Route             []RouteStep `json:"route"`
	CurrentStep       int         `json:"current_step"`
	Timestamp         time.Time   `json:"timestamp"`
}

// RouteStep represents one system in a multi-jump route
type RouteStep struct {
	SystemID string `json:"system_id"`
	Name     string `json:"name"`
	Jumps    int    `json:"jumps"`
}

// SaveRouteProgress writes route state to agent's route.json
func SaveRouteProgress(baseDir, agentID string, route *RouteProgress) error {
	if route == nil {
		return fmt.Errorf("route cannot be nil")
	}

	agentDir := filepath.Join(baseDir, "agents", agentID)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("create agent dir: %w", err)
	}

	routeFile := filepath.Join(agentDir, "route.json")
	route.Timestamp = time.Now()

	data, err := json.MarshalIndent(route, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal route: %w", err)
	}

	if err := os.WriteFile(routeFile, data, 0644); err != nil {
		return fmt.Errorf("write route file: %w", err)
	}

	return nil
}

// LoadRouteProgress reads route state from agent's route.json
func LoadRouteProgress(baseDir, agentID string) (*RouteProgress, error) {
	routeFile := filepath.Join(baseDir, "agents", agentID, "route.json")

	data, err := os.ReadFile(routeFile)
	if err != nil {
		return nil, fmt.Errorf("read route file: %w", err)
	}

	var route RouteProgress
	if err := json.Unmarshal(data, &route); err != nil {
		return nil, fmt.Errorf("unmarshal route: %w", err)
	}

	return &route, nil
}

// ClearRouteProgress removes the agent's route.json file
func ClearRouteProgress(baseDir, agentID string) error {
	routeFile := filepath.Join(baseDir, "agents", agentID, "route.json")

	if err := os.Remove(routeFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove route file: %w", err)
	}

	return nil
}

// HasRouteProgress checks if an agent has a saved route
func HasRouteProgress(baseDir, agentID string) bool {
	routeFile := filepath.Join(baseDir, "agents", agentID, "route.json")
	_, err := os.Stat(routeFile)
	return err == nil
}
