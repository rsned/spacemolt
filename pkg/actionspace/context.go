package actionspace

import (
	"fmt"

	"github.com/rsned/spacemolt/pkg/game"
)

// GameContext is the flattened game state used for precondition evaluation.
// Built from *game.State for real use, or constructed manually for the visualizer.
type GameContext struct {
	// Primary state (mutually exclusive)
	Docked    bool
	InTransit bool

	// Combat states
	InCombat bool // Pirate/NPC combat
	InBattle bool // Tactical battle system

	// Location
	CurrentPOIType string           // "asteroid_belt", "station", "ice_field", etc.
	CurrentPOIID   string           //nolint:revive // POI is an acronym used as a name
	SystemPOIs     []POIInfo        // All POIs in current system
	Connections    []ConnectionInfo // Adjacent systems

	// Ship status
	Fuel          float64
	MaxFuel       float64
	Hull          float64
	MaxHull       float64
	CargoUsed     float64
	CargoCapacity float64
	Credits       float64
	Modules       []string // Installed module IDs
	TowingWreck   bool

	// Player status
	HasFaction  bool
	FactionRank string
	IsCloaked   bool

	// Nearby entities
	NearbyPlayerCount int
	WreckCount        int
	StoredShipCount   int
	CargoItemCount    int // Number of distinct item types in cargo
}

// POIInfo is a minimal POI representation for target generation.
type POIInfo struct {
	ID      string
	Name    string
	Type    string
	HasBase bool
}

// ConnectionInfo is a minimal system connection for target generation.
type ConnectionInfo struct {
	SystemID string
	Name     string
	Distance int
}

// FromState builds a GameContext from live game state.
func FromState(state *game.State) GameContext {
	gc := GameContext{
		Docked:    state.Doc,
		InTransit: state.Traveling,
		InCombat:  state.InCombat,
		InBattle:  state.InBattle,

		Fuel:          state.Ship.Fuel,
		MaxFuel:       state.Ship.MaxFuel,
		Hull:          state.Ship.Hull,
		MaxHull:       state.Ship.MaxHull,
		CargoUsed:     state.Ship.CargoUsed,
		CargoCapacity: state.Ship.CargoCapacity,
		Credits:       state.Credits,
		Modules:       state.Ship.Modules,
		TowingWreck:   state.Player.TowingWreckID != "",

		HasFaction:  state.Player.FactionID != "",
		FactionRank: state.Player.FactionRank,
		IsCloaked:   state.Player.IsCloaked,

		NearbyPlayerCount: len(state.Nearby),
		CargoItemCount:    len(state.Ship.Cargo),

		CurrentPOIID: state.CurrentPOI,
	}

	// Build POI list and resolve current POI type.
	gc.SystemPOIs = make([]POIInfo, len(state.System.POIs))
	for i, poi := range state.System.POIs {
		gc.SystemPOIs[i] = POIInfo{
			ID:      poi.ID,
			Name:    poi.Name,
			Type:    poi.Type,
			HasBase: poi.HasBase,
		}
		if poi.ID == state.CurrentPOI {
			gc.CurrentPOIType = poi.Type
		}
	}

	// Build connection list.
	gc.Connections = make([]ConnectionInfo, len(state.System.Connections))
	for i, conn := range state.System.Connections {
		gc.Connections[i] = ConnectionInfo{
			SystemID: conn.SystemID,
			Name:     conn.Name,
			Distance: conn.Distance,
		}
	}

	return gc
}

// Validate checks that a GameContext has no impossible state combinations.
// Used by the visualizer to prevent nonsense configurations.
func (gc GameContext) Validate() error {
	if gc.Docked && gc.InTransit {
		return fmt.Errorf("invalid state: cannot be docked and in transit")
	}
	if gc.Docked && gc.InCombat {
		return fmt.Errorf("invalid state: cannot be docked and in combat")
	}
	if gc.Docked && gc.InBattle {
		return fmt.Errorf("invalid state: cannot be docked and in battle")
	}
	if gc.InTransit && gc.InCombat {
		return fmt.Errorf("invalid state: cannot be in transit and in combat")
	}
	if gc.InTransit && gc.InBattle {
		return fmt.Errorf("invalid state: cannot be in transit and in battle")
	}
	return nil
}
