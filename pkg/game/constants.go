package game

import "time"

// Sleep durations for various game operations
const (
	SleepTick   = 10 * time.Second // One game server tick is 10 seconds.
	SleepQuick  = SleepTick / 5    // Non-mutation command and queries.
	SleepShort  = SleepTick / 3    // Short operations (refuel, repair, install)
	SleepMedium = SleepTick / 2    // Medium wait operations
	SleepLong   = 2 * SleepTick    // Longer wait operations

	SleepDock   = 3 * SleepTick / 2 // Dock timing (15s)
	SleepUndock = 3 * SleepTick / 2 // Undock timing (15s)

	SleepTravel = 1 * SleepTick // POI travel within system
	SleepJump   = 2 * SleepTick // System jump travel timea

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

// Client identity
const (
	ClientName = "rsned/spacemolt"
	VersionID  = "v0.0.1"
	UserAgent  = ClientName + "/" + VersionID
)

// Cache durations
const (
	MapCacheTTL = 1 * time.Hour // Map data changes infrequently; refresh at most once per hour
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
