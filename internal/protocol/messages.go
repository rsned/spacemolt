package protocol

// Message types sent to/from the Spacemolt server
const (
	// Connection and Authentication
	TypeWelcome    = "welcome"
	TypeRegistered = "registered"
	TypeLoggedIn   = "logged_in"

	// Action responses
	TypeOK           = "ok"
	TypeError        = "error"
	TypeActionError  = "action_error"
	TypeActionResult = "action_result"
	TypeDocked       = "docked"
	TypeUndocked     = "undocked"
	TypeStateUpdate  = "state_update"
	TypeTick         = "tick"
	TypeListings     = "listings"

	// Game events
	TypeChatMessage   = "chat_message"
	TypeCombatUpdate  = "combat_update"
	TypeBattleAlert   = "battle_alert"
	TypeBattleStarted = "battle_started"
	// TypeBattleJoined is pushed when another player enters a battle already in
	// progress. It names only the arrival (player_id, side_id, username) and
	// carries NO battle_id — see serverapi.BattleJoined.
	TypeBattleJoined = "battle_joined"

	// TypeBattleLeft is pushed when a combatant leaves a battle -- fled,
	// destroyed, or otherwise gone. It names only who left and why, carrying NO
	// battle_id, and the departing combatant may be a CREATURE (crt_ prefix)
	// with an empty username. One participant leaving is NOT the battle ending;
	// that is TypeBattleEnded.
	TypeBattleLeft         = "battle_left"
	TypeBattleUpdate       = "battle_update"
	TypeBattleDamage       = "battle_damage"
	TypeBattleEnded        = "battle_ended"
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

	// Ship commissioning events
	TypeShipCommissionComplete = "ship_commission_complete"

	// Police system events
	TypePoliceWarning  = "police_warning"
	TypePoliceSpawn    = "police_spawn"
	TypePoliceCombat   = "police_combat"
	TypePoliceResponse = "police_response"

	// Pirate system events
	TypePirateWarning   = "pirate_warning"
	TypePirateCombat    = "pirate_combat"
	TypePirateDestroyed = "pirate_destroyed"
	TypePirateSpawn     = "pirate_spawn"

	// Drone system events
	TypeDroneUpdate    = "drone_update"
	TypeDroneDestroyed = "drone_destroyed"

	// Base building and raiding events
	TypeBaseRaidUpdate = "base_raid_update"
	TypeBaseDestroyed  = "base_destroyed"

	// Facility events
	TypeFacilityRentWarning = "facility_rent_warning"

	// Crafting events
	TypeCraftingUpdate = "crafting_update"

	// Skill progression events
	TypeSkillLevelUp = "skill_level_up"

	// Mission events
	TypeCompleteMission = "complete_mission"

	// Gameplay tips and notifications
	TypeGameplayTip = "gameplay_tip"

	// Faction events
	TypeFactionPromote          = "faction_promote"
	TypeFactionInvite           = "faction_invite"
	TypeFactionAllianceProposal = "faction_alliance_proposal"
	TypeFactionAllianceFormed   = "faction_alliance_formed"

	// Achievement events
	TypeAchievementUnlocked = "achievement_unlocked"

	// Server lifecycle events
	TypeServerRestartWarning = "server_restart_warning"
)

// Message represents a message sent to the server
type Message struct {
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload,omitempty"`
	Timestamp int64          `json:"timestamp,omitempty"`
	// RequestID is set by Client.Submit to correlate responses (server
	// v0.296.1+). Echoed by the server on the pending:true ack, terminal
	// action_result, and any error/action_error tied to this request.
	RequestID string `json:"request_id,omitempty"`
}

// Response represents a message received from the server
type Response struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload,omitempty"`
	// RequestID, when non-empty, identifies the client request this
	// response correlates to. Empty on server-initiated pushes
	// (welcome, tick, chat_message, pirate_warning, ...).
	RequestID string `json:"request_id,omitempty"`
}
