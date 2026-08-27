package serverapi

// ============================================================================
// Server Event Messages (notifications, not responses to commands)
// These types mirror the game server's JSON wire format exactly.
// ============================================================================

// CombatUpdate represents a combat event (player vs player or player vs pirate).
// Server event type: combat_update
type CombatUpdate struct {
	Tick       int64   `json:"tick"`
	Attacker   string  `json:"attacker"`
	Target     string  `json:"target"`
	Damage     float64 `json:"damage"`
	DamageType string  `json:"damage_type"`
	ShieldHit  float64 `json:"shield_hit"`
	HullHit    float64 `json:"hull_hit"`
	Destroyed  bool    `json:"destroyed"`
}

// PlayerDied represents ship destruction and respawn in an Escape Pod.
// Server event type: player_died
type PlayerDied struct {
	KillerID        string           `json:"killer_id"`
	KillerName      string           `json:"killer_name"`
	RespawnBase     string           `json:"respawn_base"`
	Cause           string           `json:"cause,omitempty"`
	CombatLog       []CombatLogEntry `json:"combat_log,omitempty"`
	CloneCost       int              `json:"clone_cost"`
	InsurancePayout int              `json:"insurance_payout"`
	ShipLost        string           `json:"ship_lost"`
	NewShipClass    string           `json:"new_ship_class"`
	WreckID         string           `json:"wreck_id"`
}

// BattleStarted is broadcast when a tactical battle the player is part of
// begins. Its participant/side shapes match the battle_alert push: integer
// side_id and identity-only participants (no live hull/shield).
// Server event type: battle_started
type BattleStarted struct {
	BattleID     string                   `json:"battle_id"`
	SystemID     string                   `json:"system_id,omitempty"`
	Message      string                   `json:"message,omitempty"`
	Participants []BattleAlertParticipant `json:"participants,omitempty"`
	Sides        []BattleAlertSide        `json:"sides,omitempty"`
}

// BattleUpdateParticipant is a participant entry in a battle_update push.
// Unlike BattleParticipant it carries an integer side_id and reports live
// hull/shield as percentages rather than absolute values.
type BattleUpdateParticipant struct {
	PlayerID  string `json:"player_id"`
	Username  string `json:"username"`
	ShipClass string `json:"ship_class"`
	SideID    int    `json:"side_id"`
	Stance    string `json:"stance,omitempty"`
	Zone      string `json:"zone,omitempty"`
	HullPct   int    `json:"hull_pct"`
	ShieldPct int    `json:"shield_pct"`
}

// BattleUpdate is a periodic authoritative snapshot of a battle the player is
// participating in, including the player's own side, stance, target and zone.
// Server event type: battle_update
type BattleUpdate struct {
	BattleID     string                    `json:"battle_id"`
	Tick         int64                     `json:"tick,omitempty"`
	AutoPilot    bool                      `json:"auto_pilot,omitempty"`
	Participants []BattleUpdateParticipant `json:"participants,omitempty"`
	Sides        []BattleAlertSide         `json:"sides,omitempty"`
	YourSideID   int                       `json:"your_side_id,omitempty"`
	YourStance   string                    `json:"your_stance,omitempty"`
	YourTargetID string                    `json:"your_target_id,omitempty"`
	YourZone     string                    `json:"your_zone,omitempty"`
}

// BattleDamage is a single weapon-hit telemetry event during a battle.
// Server event type: battle_damage
type BattleDamage struct {
	Tick         int64    `json:"tick,omitempty"`
	AttackerID   string   `json:"attacker_id"`
	AttackerName string   `json:"attacker_name,omitempty"`
	TargetID     string   `json:"target_id"`
	TargetName   string   `json:"target_name,omitempty"`
	DamageType   string   `json:"damage_type,omitempty"`
	HitSuccess   bool     `json:"hit_success"`
	HullHit      float64  `json:"hull_hit"`
	ShieldHit    float64  `json:"shield_hit"`
	TotalDamage  float64  `json:"total_damage"`
	WeaponsFired []string `json:"weapons_fired,omitempty"`
	// XPGained maps skill_id -> xp earned from this hit (e.g. gunnery).
	// Consistent with the map[string]int xp_gained shape used elsewhere.
	XPGained map[string]int `json:"xp_gained,omitempty"`
}

// BattleEndedParticipant is a participant entry in a battle_ended push,
// carrying that combatant's per-battle tally. Identity-only otherwise (no
// ship_class / live hull-shield).
type BattleEndedParticipant struct {
	PlayerID    string  `json:"player_id"`
	Username    string  `json:"username"`
	SideID      int     `json:"side_id"`
	DamageDealt float64 `json:"damage_dealt"`
	DamageTaken float64 `json:"damage_taken"`
	KillCount   int     `json:"kill_count"`
	Survived    bool    `json:"survived"`
}

// BattleEnded is broadcast when a tactical battle the player was part of
// concludes. WinningSide is -1 for a stalemate.
// Server event type: battle_ended
type BattleEnded struct {
	BattleID       string                   `json:"battle_id"`
	Duration       int                      `json:"duration,omitempty"`
	Reason         string                   `json:"reason,omitempty"`
	WinningSide    int                      `json:"winning_side"`
	ShipsDestroyed int                      `json:"ships_destroyed,omitempty"`
	TotalDamage    float64                  `json:"total_damage,omitempty"`
	Participants   []BattleEndedParticipant `json:"participants,omitempty"`
}

// MiningYield represents successful mining action with resource extraction.
// Server event type: mining_yield
type MiningYield struct {
	// Kind is the v0.531.4 response discriminator: yield.
	Kind             string         `json:"kind,omitempty"`
	ResourceID       string         `json:"resource_id"`
	ResourceName     string         `json:"resource_name"`
	Quantity         float64        `json:"quantity"`
	Remaining        float64        `json:"remaining"`
	MaxRemaining     float64        `json:"max_remaining"`
	DepletionPercent float64        `json:"depletion_percent"`
	RemainingDisplay string         `json:"remaining_display"`
	XPGained         map[string]int `json:"xp_gained,omitempty"`
}

// ScanResult represents the results of scanning a player.
// Server event type: scan_result
type ScanResult struct {
	TargetID     string   `json:"target_id"`
	Success      bool     `json:"success"`
	RevealedInfo []string `json:"revealed_info"`
	Username     string   `json:"username,omitempty"`
	ShipClass    string   `json:"ship_class,omitempty"`
	Hull         float64  `json:"hull,omitempty"`
	Shield       float64  `json:"shield,omitempty"`
	Cloaked      bool     `json:"cloaked,omitempty"`
}

// ScanDetected is sent when you are scanned by another player.
// Server event type: scan_detected
type ScanDetected struct {
	ScannerID        string   `json:"scanner_id"`
	ScannerUsername  string   `json:"scanner_username"`
	ScannerShipClass string   `json:"scanner_ship_class"`
	RevealedInfo     []string `json:"revealed_info"`
	Message          string   `json:"message"`
}

// TradeOffer represents an incoming trade offer from another player.
// Server event type: trade_offer_received
type TradeOffer struct {
	TradeID        string      `json:"trade_id"`
	FromPlayer     string      `json:"from_player"`
	FromName       string      `json:"from_name"`
	OfferItems     []CargoItem `json:"offer_items"`
	OfferCredits   int         `json:"offer_credits"`
	RequestItems   []CargoItem `json:"request_items"`
	RequestCredits int         `json:"request_credits"`
}

// PilotlessShip represents a player who disconnected during combat.
// Server event type: pilotless_ship
type PilotlessShip struct {
	PlayerID       string `json:"player_id"`
	PlayerUsername string `json:"player_username"`
	ShipID         string `json:"ship_id"`
	ShipClass      string `json:"ship_class"`
	SystemID       string `json:"system_id"`
	POIID          string `json:"poi_id"`
	ExpireTick     int64  `json:"expire_tick"`
	TicksRemaining int    `json:"ticks_remaining"`
}

// ReconnectedMessage is sent when you reconnect after disconnecting during combat.
// Server event type: reconnected
type ReconnectedMessage struct {
	Message        string `json:"message"`
	WasPilotless   bool   `json:"was_pilotless"`
	TicksRemaining int    `json:"ticks_remaining"`
}

// POIArrival is broadcast when a player arrives at your POI.
// Server event type: poi_arrival
type POIArrival struct {
	Username string `json:"username"`
	ClanTag  string `json:"clan_tag"`
	POIName  string `json:"poi_name"`
	POIID    string `json:"poi_id"`
}

// POIDeparture is broadcast when a player leaves your POI.
// Server event type: poi_departure
type POIDeparture struct {
	Username string `json:"username"`
	ClanTag  string `json:"clan_tag"`
	POIName  string `json:"poi_name"`
	POIID    string `json:"poi_id"`
}

// PoliceWarning is sent when you commit a crime in policed space.
// Server event type: police_warning
type PoliceWarning struct {
	Message       string `json:"message"`
	PoliceLevel   int    `json:"police_level"`
	ResponseTicks int    `json:"response_ticks"`
	System        string `json:"system"`
}

// PoliceSpawn is sent when police drones arrive to engage hostile.
// Server event type: police_spawn
type PoliceSpawn struct {
	Message   string `json:"message"`
	NumDrones int    `json:"num_drones"`
	Target    string `json:"target"`
}

// PoliceCombat represents police drone combat (damage dealt each tick).
// Server event type: police_combat
type PoliceCombat struct {
	Tick       int64   `json:"tick"`
	DroneID    string  `json:"drone_id"`
	TargetID   string  `json:"target_id"`
	Damage     float64 `json:"damage"`
	DamageType string  `json:"damage_type"`
	Destroyed  bool    `json:"destroyed"`
}

// PirateWarning is sent when a pirate NPC detects you at their POI.
// Server event type: pirate_warning
type PirateWarning struct {
	PirateID      string `json:"pirate_id"`
	PirateName    string `json:"pirate_name"`
	Tier          string `json:"tier"`
	IsBoss        bool   `json:"is_boss"`
	AttackInTicks int    `json:"attack_in_ticks"`
	Message       string `json:"message"`
}

// PirateCombat represents pirate NPC combat (damage dealt each tick).
// Server event type: pirate_combat
type PirateCombat struct {
	Tick         int64   `json:"tick"`
	PirateID     string  `json:"pirate_id"`
	PirateName   string  `json:"pirate_name"`
	Damage       float64 `json:"damage"`
	DamageType   string  `json:"damage_type"`
	PlayerHull   float64 `json:"player_hull"`
	PlayerShield float64 `json:"player_shield"`
	Destroyed    bool    `json:"destroyed"`
}

// PirateDestroyed is sent when you destroy a pirate NPC.
// Server event type: pirate_destroyed
type PirateDestroyed struct {
	PirateID      string  `json:"pirate_id"`
	PirateName    string  `json:"pirate_name"`
	Tier          string  `json:"tier"`
	IsBoss        bool    `json:"is_boss"`
	CreditsReward int     `json:"credits_reward"`
	XPGained      float64 `json:"xp_gained"`
	WreckID       string  `json:"wreck_id"`
	Message       string  `json:"message"`
}

// PirateSpawn is sent when a pirate NPC respawns at your current POI.
// Server event type: pirate_spawn
type PirateSpawn struct {
	PirateID   string `json:"pirate_id"`
	PirateName string `json:"pirate_name"`
	Tier       string `json:"tier"`
	IsBoss     bool   `json:"is_boss"`
	Message    string `json:"message"`
}

// DroneUpdate represents drone combat activity.
// Server event type: drone_update
type DroneUpdate struct {
	Tick      int64   `json:"tick"`
	DroneID   string  `json:"drone_id"`
	TargetID  string  `json:"target_id"`
	Damage    float64 `json:"damage"`
	Destroyed bool    `json:"destroyed"`
}

// DroneDestroyed is sent when one of your drones is destroyed in combat.
// Server event type: drone_destroyed
type DroneDestroyed struct {
	DroneID   string `json:"drone_id"`
	DroneType string `json:"drone_type"`
	Message   string `json:"message"`
}

// BaseRaidUpdate is sent during a base raid.
// Server event type: base_raid_update
type BaseRaidUpdate struct {
	BaseID        string `json:"base_id"`
	BaseName      string `json:"base_name"`
	CurrentHealth int    `json:"current_health"`
	MaxHealth     int    `json:"max_health"`
	DamagePerTick int    `json:"damage_per_tick"`
	AttackerCount int    `json:"attacker_count"`
	Message       string `json:"message"`
}

// BaseDestroyed is sent when a base is destroyed.
// Server event type: base_destroyed
type BaseDestroyed struct {
	BaseID    string `json:"base_id"`
	BaseName  string `json:"base_name"`
	OwnerID   string `json:"owner_id"`
	OwnerName string `json:"owner_name"`
	WreckID   string `json:"wreck_id"`
	Message   string `json:"message"`
}

// SkillLevelUp is sent when you level up a skill through passive training.
// Server event type: skill_level_up
type SkillLevelUp struct {
	SkillID  string  `json:"skill_id"`
	NewLevel int     `json:"new_level"`
	XPGained float64 `json:"xp_gained"`
}

// BattleJoined is the battle_joined push: another player has entered a battle
// already in progress.
//
// It is deliberately thin — the server sends only who arrived and which side
// they took, with NO battle_id and no participant roster. Observed live
// 2026-08-26 in the Dheneb station battle:
//
//	{"player_id":"32309e...","side_id":2,"username":"Munawar"}
//
// Because it carries no battle_id, a handler must not pass one to
// rememberBattleIDLocked: an empty id would be ignored, but the absence is the
// point — the id has to come from the attack reply or battle_started.
type BattleJoined struct {
	PlayerID string `json:"player_id"`
	SideID   int    `json:"side_id"`
	Username string `json:"username,omitempty"`
}
