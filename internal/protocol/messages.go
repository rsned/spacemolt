package protocol

// Message types sent to/from the Spacemolt server
const (
	// Connection and Authentication
	TypeWelcome    = "welcome"
	TypeRegistered = "registered"
	TypeLoggedIn   = "logged_in"

	// Action responses
	TypeOK          = "ok"
	TypeError       = "error"
	TypeDocked      = "docked"
	TypeUndocked    = "undocked"
	TypeStateUpdate = "state_update"
	TypeTick        = "tick"
	TypeListings    = "listings"

	// Game events
	TypeChatMessage        = "chat_message"
	TypeCombatUpdate       = "combat_update"
	TypePlayerDied         = "player_died"
	TypeMining             = "mining"
	TypeMiningYield        = "mining_yield"
	TypeScanResult         = "scan_result"
	TypeScanDetected       = "scan_detected"
	TypeTradeOfferReceived = "trade_offer_received"

	// Travel and location events
	TypePOIArrival   = "poi_arrival"
	TypePOIDeparture = "poi_departure"

	// Combat and ship events
	TypePilotlessShip = "pilotless_ship"
	TypeReconnected   = "reconnected"

	// Police system events
	TypePoliceWarning = "police_warning"
	TypePoliceSpawn   = "police_spawn"
	TypePoliceCombat  = "police_combat"

	// Drone system events
	TypeDroneUpdate    = "drone_update"
	TypeDroneDestroyed = "drone_destroyed"

	// Base building and raiding events
	TypeBaseRaidUpdate = "base_raid_update"
	TypeBaseDestroyed  = "base_destroyed"

	// Skill progression events
	TypeSkillLevelUp = "skill_level_up"

	// Gameplay tips and notifications
	TypeGameplayTip = "gameplay_tip"
)

// Message represents a message sent to the server
type Message struct {
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload,omitempty"`
	Timestamp int64          `json:"timestamp,omitempty"`
}

// Response represents a message received from the server
type Response struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload,omitempty"`
}
