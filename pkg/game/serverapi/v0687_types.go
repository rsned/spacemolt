package serverapi

import "encoding/json"

// Types introduced with server v0.687.0 (openapi 20261110). Read-only views:
// no worker acts on them yet.

// Causes of a player_died event (PlayerDied.Cause).
const (
	DeathCauseCombat       = "combat"        // another player
	DeathCausePolice       = "police"        // police drones
	DeathCausePirate       = "pirate"        // pirates
	DeathCauseWildlife     = "wildlife"      // creatures
	DeathCauseSelfDestruct = "self_destruct" // own self-destruct
	DeathCauseHazard       = "hazard"        // environmental hazard; killer name is the hazard
)

// FactionShipLicense is an empire shipbuilding license owned by a faction
// (FactionInfoResponse.ship_licenses). It lets the faction commission_ship
// that hull.
type FactionShipLicense struct {
	ShipClass  string `json:"ship_class"`
	ShipName   string `json:"ship_name"`
	Empire     string `json:"empire"`
	AcquiredAt string `json:"acquired_at"`
}

// FieldWorkshopView is the state of a ship's Field Workshop (hand-crafting
// from cargo while undocked).
type FieldWorkshopView struct {
	// State is idle, running or paused.
	State string `json:"state"`
	// PauseReason is present only when State is paused.
	PauseReason string `json:"pause_reason,omitempty"`
	QueuedJobs  int    `json:"queued_jobs"`
	// ReservedCargo is cargo space held by escrowed job inputs; not part of
	// cargo_used.
	ReservedCargo int `json:"reserved_cargo"`
}

// GateStatus is a jump-gate end as described by the owning faction's station
// info (StationConfigResponse.gate).
type GateStatus struct {
	FarSystemID   string `json:"far_system_id"`
	FarSystemName string `json:"far_system_name"`
	// PartnerBaseID is omitted until the other end is founded.
	PartnerBaseID string `json:"partner_base_id,omitempty"`
	PartnerBuilt  bool   `json:"partner_built"`
	Built         bool   `json:"built"`
	Online        bool   `json:"online"`
	Toll          int    `json:"toll"`
	FactionOnly   bool   `json:"faction_only"`
	PhasesTotal   int    `json:"phases_total"`
	// Construction progress; omitted once built.
	Phase                int            `json:"phase,omitempty"`
	PhaseName            string         `json:"phase_name,omitempty"`
	PhaseCycles          int            `json:"phase_cycles,omitempty"`
	PhaseCyclesDone      int            `json:"phase_cycles_done,omitempty"`
	PhaseLaborPerCycle   int            `json:"phase_labor_per_cycle,omitempty"`
	PhaseMaterialsLeft   map[string]int `json:"phase_materials_left,omitempty"`
	EntanglerDueInCycles int            `json:"entangler_due_in_cycles,omitempty"`
	// UpkeepCyclesCovered counts upkeep cycles faction storage holds (up to 1000).
	UpkeepCyclesCovered int `json:"upkeep_cycles_covered,omitempty"`
}

// BaseGate is the compact jump-gate summary on Base.gate, present only when
// the outpost is one end of a player-built gate.
type BaseGate struct {
	FactionOnly     bool   `json:"faction_only,omitempty"`
	FarSystemID     string `json:"far_system_id,omitempty"`
	LowSupplyWarned bool   `json:"low_supply_warned,omitempty"`
	Online          bool   `json:"online,omitempty"`
	PartnerBaseID   string `json:"partner_base_id,omitempty"`
	// Phase is construction phases completed (0-4); 4 means built.
	Phase int `json:"phase,omitempty"`
	// PhaseCycles is cycles of the current phase done while building, upkeep
	// cycles covered once built.
	PhaseCycles int `json:"phase_cycles,omitempty"`
	Toll        int `json:"toll,omitempty"`
}

// InteractPOIResponse wraps the response from interact_poi (archive POIs).
type InteractPOIResponse struct {
	POIID      string `json:"poi_id"`
	Message    string `json:"message"`
	DataCopied bool   `json:"data_copied,omitempty"`
	// ItemsRecovered is item ID -> quantity moved into cargo.
	ItemsRecovered    map[string]int           `json:"items_recovered,omitempty"`
	Mission           json.RawMessage          `json:"mission,omitempty"`
	Completion        *CompleteMissionResponse `json:"completion,omitempty"`
	PassengersBoarded json.RawMessage          `json:"passengers_boarded,omitempty"`
}

// POIInteractionInfo is the private archive prompt on get_poi.interaction.
type POIInteractionInfo struct {
	Message string          `json:"message"`
	Command string          `json:"command"`
	Mission json.RawMessage `json:"mission,omitempty"`
}

// RefuelXPLedger records which players this player refueled ship-to-ship for
// piloting XP on the given UTC day.
type RefuelXPLedger struct {
	Day     string          `json:"day"`
	Targets map[string]bool `json:"targets"`
}

// RepeaterInfo is a Frontier Repeater at a POI (get_nearby.repeaters).
type RepeaterInfo struct {
	RepeaterID string `json:"repeater_id"`
	Name       string `json:"name"`
	ShipClass  string `json:"ship_class"`
	Hull       int    `json:"hull"`
	MaxHull    int    `json:"max_hull"`
	Shield     int    `json:"shield"`
	MaxShield  int    `json:"max_shield"`
	InBattle   bool   `json:"in_battle"`
}
