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

	// Initial response timeouts for actions that may take multiple ticks to start
	SleepActionStartTimeout = 3 * SleepTick // 30s timeout for travel/jump to acknowledge

	// Hard cap on the wait window a travel/jump can spend blocked waiting
	// for an arrival event. Within-system travel never exceeds ~9 ticks of
	// game time, so 18 ticks (~3min) is an overgenerous safety bound that
	// also defeats the failure mode where a stale local CurrentTick inflates
	// `arrival_tick - currentTick`.
	SleepTravelMaxWait = 18 * SleepTick // 3min hard cap for in-system travel
	SleepJumpMaxWait   = 30 * SleepTick // 5min hard cap for cross-system jump

	SleepReconnect = 30 * time.Second // Reconnection recovery wait
	SleepRetry             = 1 * time.Second  // Retry delay for failed operations
	SleepScanInterval      = 5 * time.Minute  // Delay between scan_for_distress cycles
	SleepChatPoll          = 30 * time.Minute // Chat channel polling interval
	SleepIPRateLimitJitter = 60 * time.Second // Max random jitter added after IP rate limit expires

	SleepPartnerJoinWindow = 5 * time.Minute // Max wait for a human to attack a bot in spar partner mode

	// WebSocket keepalive timing
	// We actively send protocol-level pings to keep NAT/proxy flows warm and to
	// detect dead connections promptly. The passive no-message timeout remains as
	// a safety net for the case where the ping goroutine itself wedges.
	SleepWSPingInterval = 20 * time.Second // Send a WebSocket ping every 20s
	SleepWSPingTimeout  = 10 * time.Second // Per-ping deadline waiting for pong
	SleepWSHealthCheck  = 30 * time.Second // Check connection health every 30 seconds
	SleepWSPongTimeout  = 5 * time.Minute  // Safety net: only trigger if pings also stopped firing

	// PingMaxConsecutiveFailures is how many consecutive ping failures we
	// tolerate before force-closing the socket to trigger a reconnect. Two
	// failures means ~30s of unresponsiveness (20s interval + 10s timeout).
	PingMaxConsecutiveFailures = 2

	// SessionContentionMinUptime is the threshold under which a connection
	// that just died is considered "short-lived" for session-contention
	// detection. Healthy reconnects normally stay up for minutes; if a
	// fresh connection drops in <30s we're almost certainly being kicked.
	SessionContentionMinUptime = 30 * time.Second

	// SessionContentionMaxShortLived is how many consecutive short-lived
	// connects we accept before concluding another client is logged in
	// with the same credentials. The server does not send a structured
	// kick notification — it just closes the socket — so we detect the
	// pattern heuristically and stop the reconnect loop rather than
	// ping-pong kicking the other session indefinitely.
	SessionContentionMaxShortLived = 2
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

	// PassiveSkillCheckInterval is how often the agent runner injects a
	// get_skills query to capture passive XP gains (e.g. Engineering when
	// the ship is fully power-loaded). Wall-clock based, not tick-based,
	// so reports can normalise to per-real-second or per-tick rates
	// regardless of server tick-rate variance.
	PassiveSkillCheckInterval = 20 * time.Minute
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
