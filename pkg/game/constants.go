package game

import "time"

// Sleep durations for various game operations
const (
	SleepQuick     = 2 * time.Second  // Quick state updates, small delays
	SleepShort     = 3 * time.Second  // Short operations (refuel, repair, install)
	SleepMedium    = 5 * time.Second  // Medium wait operations
	SleepDock      = 12 * time.Second // Undock timing
	SleepLong      = 10 * time.Second // Longer wait operations
	SleepTick      = SleepLong
	SleepDocked    = 15 * time.Second // Dock timing
	SleepTravel    = 20 * time.Second // POI travel within system
	SleepJump      = 25 * time.Second // System jump travel time
	SleepReconnect = 30 * time.Second // Reconnection recovery wait
)

// Hull percentage thresholds
const (
	HullCriticalThreshold   = 40.0 // Hull percent: critical damage, flee immediately
	HullDamagedThreshold    = 50.0 // Hull percent: damaged, seek repairs
	HullCombatFleeThreshold = 70.0 // Hull percent in combat: consider fleeing
)

// Shield percentage thresholds
const (
	ShieldDepletedThreshold = 10.0 // Shield percent: depleted, combine with hull for flee decision
)

// Fuel thresholds
const (
	FuelCriticalThreshold = 0.3 // 30% fuel: critically low, refuel soon
	FuelLowThreshold      = 0.8 // 80% fuel: refuel when at station
)

// Cargo capacity thresholds
const (
	CargoFullThreshold       = 0.9 // 90% capacity: consider full, return to sell
	CargoHalfFullThreshold   = 0.5 // 50% capacity: half full, limit purchases
	CargoReserveForPurchases = 0.5 // Reserve 50% space when buying equipment
)

// Combat thresholds
const (
	CombatHullCriticalThreshold = 30.0 // Hull percent: critical in combat, flee immediately
	CombatHullFleeThreshold     = 70.0 // Hull percent: flee if below this AND shields low
	CombatShieldLowThreshold    = 10.0 // Shield percent: low enough to combine with hull for flee decision
)

// Timing constants
const (
	MiningCycleTime    = 11 * time.Second // Time between mining operations
	GameTickRate       = 1 * time.Second  // Basic game tick rate
	ModuleInstallDelay = 10 * time.Second // Delay between module installations
)

// Credit thresholds
const (
	MinimumPurchaseThreshold = 100.0 // Minimum credits to attempt any purchase
)

// Data freshness thresholds (in game ticks, where 1 tick = 1 second)
const (
	// FreshnessResourcePOI is the threshold for resource POIs (asteroid_belt, asteroid_field, gas_cloud, ice_field): 6 hours
	FreshnessResourcePOI = 6 * 60 * 60 // 21,600 ticks

	// FreshnessDefaultPOI is the threshold for non-resource POIs (planet, sun, jump_gate, wreck): 1 week
	FreshnessDefaultPOI = 7 * 24 * 60 * 60 // 604,800 ticks

	// FreshnessStationPOI is the threshold for stations and bases: 1 day
	FreshnessStationPOI = 24 * 60 * 60 // 86,400 ticks

	// FreshnessSystem is the threshold for system data: 1 day
	FreshnessSystem = 24 * 60 * 60 // 86,400 ticks
)

// POIFreshnessThreshold returns the appropriate freshness threshold for a given POI type.
func POIFreshnessThreshold(poiType string) int64 {
	switch poiType {
	case "asteroid_belt", "asteroid", "asteroid_field", "gas_cloud", "ice_field", "nebula":
		return FreshnessResourcePOI
	case "station", "base":
		return FreshnessStationPOI
	default:
		// Covers: planet, moon, sun, relic, jump_gate, wreck, and unknown types
		return FreshnessDefaultPOI
	}
}
