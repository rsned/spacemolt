package game

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
