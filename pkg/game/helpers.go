package game

import "context"

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
