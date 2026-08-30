package serverapi

// Battle replay wire types for get_battle_log / get_battle_summary.
//
// These are the spectator endpoints: they answer for ANY battle, active or
// completed, belonging to anyone. Field names and shapes are transcribed from
// server_docs/openapi.json and verified against a live capture of battle
// a2619bbe328676445828b4e1007fe9aa (Node Beta, 11v30 + station, 30 ticks, 42
// participants) on 2026-08-16.
//
// Nothing here is wired into the GameClient interface yet — the battle-export
// tool decodes into these from a RawCommand reply. Adding typed client methods
// is deliberately deferred, because touching the interface breaks the pkg/agent
// and pkg/skills mocks in a way `go build` does not catch.

// GetBattleLogResponse wraps the response from the get_battle_log command.
type GetBattleLogResponse struct {
	BattleID string `json:"battle_id"`
	// Status is "active" or "completed". A live battle can be polled by
	// re-requesting from the last tick seen.
	Status string `json:"status"`
	// TotalTicks is the number of logged ticks for the whole battle, which may
	// exceed the number of entries returned in this page.
	TotalTicks int `json:"total_ticks"`
	// HasMore reports that entries exist past this page's tick window.
	HasMore bool             `json:"has_more"`
	Entries []BattleLogEntry `json:"entries"`
}

// BattleLogEntry is one tick of a battle: the state of every participant
// present, plus every event that resolved during it.
type BattleLogEntry struct {
	BattleID string `json:"battle_id"`
	SystemID string `json:"system_id"`
	Tick     int64  `json:"tick"`

	// Snapshots carries one row per participant PRESENT this tick. It is
	// sparse: combatants appear as they engage (15 of 42 on the first tick of
	// the reference battle) and stop appearing once destroyed.
	Snapshots []ParticipantSnapshot `json:"snapshots"`

	Attacks   []AttackLogEntry    `json:"attacks"`
	AutoPilot []AutoPilotLogEntry `json:"autopilot"`
	ZoneMoves []ZoneMoveLogEntry  `json:"zone_moves"`
	Regen     []RegenLogEntry     `json:"regen"`
	Kills     []KillLogEntry      `json:"kills"`
	Joins     []JoinLogEntry      `json:"joins"`
	Flee      []FleeLogEntry      `json:"flee"`
	Fuel      []FuelLogEntry      `json:"fuel"`
	Burns     []BurnLogEntry      `json:"burns"`
	Commands  []CommandLogEntry   `json:"commands"`

	BattleEnded *BattleEndLogEntry `json:"battle_ended,omitempty"`
}

// ParticipantSnapshot is one combatant's full state at one tick.
type ParticipantSnapshot struct {
	PlayerID string `json:"player_id"`
	Username string `json:"username"`
	// Kind is player, pirate, police, drone, creature, or station.
	Kind      string `json:"kind"`
	SideID    int    `json:"side_id"`
	FactionID string `json:"faction_id,omitempty"`
	// ShipClass is the join key to ship art (lowercase, e.g. "vigil").
	ShipClass string `json:"ship_class"`

	// X is position along the engagement axis and Y is lateral spread. The
	// sides face each other along X: in the reference battle side 1 held
	// 0.50..1.61 and side 2 held 1.29..2.66. Treat these as presentation
	// layout — Zone and AttackLogEntry.ZoneDistance are what combat resolves
	// against.
	X float64 `json:"x"`
	Y float64 `json:"y"`
	// Zone is outer, mid, inner, or engaged — a band of the X axis.
	Zone string `json:"zone"`

	Hull      int `json:"hull"`
	MaxHull   int `json:"max_hull"`
	Shield    int `json:"shield"`
	MaxShield int `json:"max_shield"`
	Fuel      int `json:"fuel"`
	MaxFuel   int `json:"max_fuel"`

	Stance    string `json:"stance"`
	TargetID  string `json:"target_id,omitempty"`
	AutoPilot bool   `json:"auto_pilot"`

	DamageDealt int `json:"damage_dealt"`
	DamageTaken int `json:"damage_taken"`
	KillCount   int `json:"kill_count"`
	FleeCounter int `json:"flee_counter"`

	DamagePenaltyPct  int `json:"damage_penalty_pct,omitempty"`
	SpeedPenaltyPct   int `json:"speed_penalty_pct,omitempty"`
	DisruptionTicks   int `json:"disruption_ticks,omitempty"`
	BurnTicks         int `json:"burn_ticks,omitempty"`
	BurnDamagePerTick int `json:"burn_damage_per_tick,omitempty"`
	ArmorMeltPct      int `json:"armor_melt_pct,omitempty"`
	ArmorMeltTicks    int `json:"armor_melt_ticks,omitempty"`

	Modules []FittedModuleSnapshot `json:"modules,omitempty"`
}

// FittedModuleSnapshot is one module in a participant's loadout.
type FittedModuleSnapshot struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

// AttackLogEntry is one attacker's fire at one target in one tick: the whole
// pipeline from roll to applied damage, not just a total.
type AttackLogEntry struct {
	AttackerID string `json:"attacker_id"`
	TargetID   string `json:"target_id"`

	HitChance  float64 `json:"hit_chance"`
	HitRoll    float64 `json:"hit_roll"`
	HitSuccess bool    `json:"hit_success"`

	// DamageType is kinetic, energy, void, or explosive.
	DamageType string `json:"damage_type"`
	// ZoneDistance is the discrete range band between the two, observed 0..5.
	ZoneDistance int  `json:"zone_distance"`
	Splash       bool `json:"splash,omitempty"`
	Disrupted    bool `json:"disrupted,omitempty"`

	RawDamage    int `json:"raw_damage"`
	PreHitDamage int `json:"pre_hit_damage"`
	ShieldDamage int `json:"shield_damage"`
	HullDamage   int `json:"hull_damage"`
	FinalDamage  int `json:"final_damage"`

	ShieldResistPct  int     `json:"shield_resist_pct,omitempty"`
	TypeResistPct    int     `json:"type_resist_pct,omitempty"`
	FlatReductionPct int     `json:"flat_reduction_pct,omitempty"`
	StanceMult       float64 `json:"stance_mult,omitempty"`
	AfterStance      int     `json:"after_stance,omitempty"`
	OffBuffPct       int     `json:"off_buff_pct,omitempty"`
	DefBuffPct       int     `json:"def_buff_pct,omitempty"`
	AfterDefBuff     int     `json:"after_def_buff,omitempty"`
	CapitalBonusPct  int     `json:"capital_bonus_pct,omitempty"`
	WeaponSkillPct   int     `json:"weapon_skill_pct,omitempty"`

	Weapons []WeaponFireDetail `json:"weapons"`
}

// WeaponFireDetail is a single weapon's contribution to an attack. AmmoUsed is
// empty for beam weapons, which is how energy fire is told from a projectile.
type WeaponFireDetail struct {
	Name       string `json:"name"`
	InstanceID string `json:"instance_id,omitempty"`
	AmmoUsed   string `json:"ammo_used,omitempty"`
	DamageType string `json:"damage_type,omitempty"`
	// AmmoMod is a damage MULTIPLIER delta, observed -0.2 and -0.15 — not the
	// string openapi.json declares. Verified against the live capture; decoding
	// it as a string fails outright. Do not "fix" this back to match the spec.
	AmmoMod float64 `json:"ammo_mod,omitempty"`

	BaseDamage      int     `json:"base_damage"`
	Damage          int     `json:"damage"`
	AfterDisruption int     `json:"after_disruption,omitempty"`
	TypeBonusPct    int     `json:"type_bonus_pct,omitempty"`
	CritChance      float64 `json:"crit_chance"`
	CritRoll        float64 `json:"crit_roll"`
	CritFired       bool    `json:"crit_fired"`
}

// AutoPilotLogEntry is one NPC/auto decision and the reason it was taken
// (npc_hold_range, npc_dry_retreat_retarget, station_fire, ...).
type AutoPilotLogEntry struct {
	PlayerID     string `json:"player_id"`
	ChosenTarget string `json:"chosen_target,omitempty"`
	Reason       string `json:"reason"`
}

// ZoneMoveLogEntry is one participant changing range band. Reason is advance,
// retreat, or pulled_closer.
type ZoneMoveLogEntry struct {
	PlayerID string `json:"player_id"`
	OldZone  string `json:"old_zone"`
	NewZone  string `json:"new_zone"`
	Reason   string `json:"reason"`
}

// RegenLogEntry is shield regen / armor repair applied in a tick.
type RegenLogEntry struct {
	PlayerID     string `json:"player_id"`
	ShieldBefore int    `json:"shield_before"`
	ShieldAfter  int    `json:"shield_after"`
	ShieldRegen  int    `json:"shield_regen"`
	HullBefore   int    `json:"hull_before"`
	HullAfter    int    `json:"hull_after"`
	ArmorRepair  int    `json:"armor_repair"`
	RemoteRepair int    `json:"remote_repair"`
}

// KillLogEntry records a destruction. This is the only signal for when a
// participant leaves the battle: snapshots simply stop appearing.
type KillLogEntry struct {
	KillerID       string `json:"killer_id"`
	KillerUsername string `json:"killer_username"`
	VictimID       string `json:"victim_id"`
	VictimUsername string `json:"victim_username"`
	// Cause distinguishes a shot-down hull from a self-destruct during a
	// boarding (server v0.572.0).
	Cause string `json:"cause,omitempty"`
}

// CaptureLogEntry records a hull taken intact by boarding, in battle_ended,
// get_battle_summary, and the battle log (server v0.572.0).
type CaptureLogEntry struct {
	BoardingOperationID string `json:"boarding_operation_id"`
	CaptorID            string `json:"captor_id"`
	CaptorUsername      string `json:"captor_username,omitempty"`
	FormerOwnerID       string `json:"former_owner_id"`
	FormerOwnerUsername string `json:"former_owner_username,omitempty"`
	ShipID              string `json:"ship_id"`
	ShipClass           string `json:"ship_class,omitempty"`
}

// JoinLogEntry records a combatant entering the battle mid-fight.
type JoinLogEntry struct {
	PlayerID string `json:"player_id"`
	Username string `json:"username"`
	SideID   int    `json:"side_id"`
}

// FleeLogEntry tracks a flee attempt's progress toward escape.
type FleeLogEntry struct {
	PlayerID     string `json:"player_id"`
	FleeCounter  int    `json:"flee_counter"`
	FleeRequired int    `json:"flee_required"`
	Escaped      bool   `json:"escaped"`
}

// FuelLogEntry records fuel burned for a tick's actions. ForcedFire marks fire
// that happened without the fuel to choose otherwise.
type FuelLogEntry struct {
	PlayerID   string `json:"player_id"`
	FuelBefore int    `json:"fuel_before"`
	FuelAfter  int    `json:"fuel_after"`
	FuelBurned int    `json:"fuel_burned"`
	ForcedFire bool   `json:"forced_fire,omitempty"`
}

// BurnLogEntry is damage-over-time ticking on a target.
type BurnLogEntry struct {
	TargetID       string `json:"target_id"`
	Damage         int    `json:"damage"`
	TicksRemaining int    `json:"ticks_remaining"`
	Destroyed      bool   `json:"destroyed,omitempty"`
}

// CommandLogEntry is a player-issued battle command.
type CommandLogEntry struct {
	PlayerID string `json:"player_id"`
	Command  string `json:"command"`
	TargetID string `json:"target_id,omitempty"`
	Stance   string `json:"stance,omitempty"`
}

// BattleEndLogEntry closes a battle out on its final tick.
type BattleEndLogEntry struct {
	Outcome          string               `json:"outcome"`
	Category         string               `json:"category"`
	WinningSide      int                  `json:"winning_side"`
	Duration         int                  `json:"duration"`
	TotalDamage      int                  `json:"total_damage"`
	ShipsDestroyed   int                  `json:"ships_destroyed"`
	ParticipantNames []string             `json:"participant_names,omitempty"`
	Participants     []ParticipantSummary `json:"participants,omitempty"`
}

// ParticipantSummary is a per-combatant end-of-battle tally.
type ParticipantSummary struct {
	PlayerID    string `json:"player_id,omitempty"`
	Username    string `json:"username,omitempty"`
	SideID      int    `json:"side_id,omitempty"`
	DamageDealt int    `json:"damage_dealt,omitempty"`
	DamageTaken int    `json:"damage_taken,omitempty"`
	KillCount   int    `json:"kill_count,omitempty"`
	Destroyed   bool   `json:"destroyed,omitempty"`
}

// BattleSummaryResponse wraps the response from the get_battle_summary command.
type BattleSummaryResponse struct {
	BattleID   string `json:"battle_id"`
	Status     string `json:"status"`
	Category   string `json:"category"`
	SystemID   string `json:"system_id"`
	SystemName string `json:"system_name"`
	OriginPOI  string `json:"origin_poi,omitempty"`

	StartTick     int64  `json:"start_tick"`
	DurationTicks int    `json:"duration_ticks"`
	EndedAt       string `json:"ended_at,omitempty"`

	Outcome        string `json:"outcome"`
	WinningSide    int    `json:"winning_side"`
	TotalDamage    int    `json:"total_damage"`
	ShipsDestroyed int    `json:"ships_destroyed"`
	// HasStation reports whether a station fought in the battle.
	HasStation       bool     `json:"has_station"`
	ParticipantCount int      `json:"participant_count"`
	PlayerNames      []string `json:"player_names,omitempty"`
	DestroyedNames   []string `json:"destroyed_names,omitempty"`

	Sides                []BattleSideSummary  `json:"sides,omitempty"`
	TopDamage            *BattleTopDamage     `json:"top_damage,omitempty"`
	ParticipantSummaries []ParticipantSummary `json:"participants,omitempty"`
	// Boarding outcomes (server v0.572.0).
	ShipsCaptured int               `json:"ships_captured,omitempty"`
	Captures      []CaptureLogEntry `json:"captures,omitempty"`
}

// BattleSideSummary lists one side's roster.
type BattleSideSummary struct {
	SideID       int      `json:"side_id"`
	FactionID    string   `json:"faction_id,omitempty"`
	FactionTag   string   `json:"faction_tag,omitempty"`
	Participants []string `json:"participants,omitempty"`
}

// BattleTopDamage names the highest-damage combatant.
type BattleTopDamage struct {
	Username string `json:"username"`
	Damage   int    `json:"damage"`
}
