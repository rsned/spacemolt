// Package battlereplay turns raw get_battle_log pages into a normalized replay
// model a renderer can draw without knowing anything about the game API.
//
// The split matters because the visualizer has two hosts with different data
// paths: the KB page is fed a file written offline (battle reads require a
// logged-in session, so a static page cannot self-fetch), while the in-client
// view will fetch live. Only this package knows the wire shape; everything
// downstream sees ReplayModel. See
// kb/docs/superpowers/specs/2026-08-16-battle-holotable-visualizer-design.md.
package battlereplay

// SchemaVersion is stamped into exported models. Bump it when a field changes
// meaning, so a renderer can refuse a file it does not understand rather than
// drawing it wrongly.
const SchemaVersion = 1

// ReplayModel is a whole battle, ready to draw.
type ReplayModel struct {
	Schema   int    `json:"schema"`
	BattleID string `json:"battle_id"`
	SystemID string `json:"system_id"`
	// SystemName and the summary-derived fields below are empty when the model
	// was built from log pages alone (summary is optional).
	SystemName string `json:"system_name,omitempty"`
	Status     string `json:"status"`

	StartTick   int64  `json:"start_tick"`
	EndTick     int64  `json:"end_tick"`
	TickCount   int    `json:"tick_count"`
	TotalTicks  int    `json:"total_ticks"`
	HasStation  bool   `json:"has_station"`
	Outcome     string `json:"outcome,omitempty"`
	WinningSide int    `json:"winning_side,omitempty"`
	TotalDamage int    `json:"total_damage,omitempty"`

	// Bounds is the extent of every position in the battle, so a renderer can
	// fit the table without a pre-pass.
	Bounds Bounds `json:"bounds"`
	// Centre is the middle of the table: the point zones are measured from, and
	// the point every side's advance/retreat axis runs toward. Taken as the
	// midpoint of Bounds, which reproduces the ordering of the range bands
	// (mean radius engaged < inner < mid < outer in the reference battle).
	Centre Point `json:"centre"`
	// Zones lists the distinct range bands seen, nearest-to-contact last. They
	// are RADIAL bands around Centre, not slices of an axis.
	Zones []string `json:"zones"`
	// Sides is every faction in the fight, ordered by side id. There is NO
	// fixed limit of two: three- and four-sided battles occur, so a renderer
	// must lay out whatever this contains rather than assuming a duel.
	Sides []Side `json:"sides"`

	Participants []Participant `json:"participants"`
	Frames       []Frame       `json:"frames"`
}

// Point is a position on the table.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Bounds is the axis-aligned extent of all observed positions.
type Bounds struct {
	XMin float64 `json:"x_min"`
	XMax float64 `json:"x_max"`
	YMin float64 `json:"y_min"`
	YMax float64 `json:"y_max"`
}

// Side is one faction in the battle, with the layout facts a renderer needs to
// place and label it. Battles are not always two-sided — three and four sides
// happen, and the official viewer arranges them as sectors around the rings —
// so nothing here assumes an opponent count.
type Side struct {
	SideID     int    `json:"side_id"`
	FactionID  string `json:"faction_id,omitempty"`
	FactionTag string `json:"faction_tag,omitempty"`
	// Count is how many participants belong to this side.
	Count int `json:"count"`
	// Won marks the winning side, when the battle reported one.
	Won bool `json:"won,omitempty"`

	// BearingMean is this side's mean angle around the table in degrees,
	// measured from Centre with 0° along +x and increasing toward +y. Sides
	// occupy roughly distinct arcs, so this is where to anchor a side's roster
	// panel or label.
	BearingMean float64 `json:"bearing_mean"`
	// RadiusMean is this side's mean distance from Centre — a rough measure of
	// how committed it is, since closing the range means moving inward.
	RadiusMean float64 `json:"radius_mean"`
}

// Participant is one combatant's constant identity plus its lifespan. Anything
// that changes tick to tick lives in ShipState instead.
type Participant struct {
	PlayerID  string `json:"player_id"`
	Username  string `json:"username"`
	Kind      string `json:"kind"`
	SideID    int    `json:"side_id"`
	FactionID string `json:"faction_id,omitempty"`
	// ShipClass is the art join key. Empty for anything the server did not
	// classify; the renderer falls back to a procedural glyph.
	ShipClass string `json:"ship_class"`

	MaxHull   int `json:"max_hull"`
	MaxShield int `json:"max_shield"`
	MaxFuel   int `json:"max_fuel"`

	Modules []Module `json:"modules,omitempty"`

	// FirstTick is when this combatant first appeared. Do not draw it before.
	FirstTick int64 `json:"first_tick"`
	// LastTick is the last tick it was seen alive.
	LastTick int64 `json:"last_tick"`
	// DestroyedAtTick is the tick a kill event named it as victim; 0 means it
	// survived. Snapshots simply stop for a dead ship, so without this a
	// renderer cannot tell "destroyed" from "not yet engaged".
	DestroyedAtTick int64 `json:"destroyed_at_tick,omitempty"`
	// KilledBy is the player_id credited with the kill, when known.
	KilledBy string `json:"killed_by,omitempty"`
}

// Module is one fitted module.
type Module struct {
	Name     string `json:"name"`
	Category string `json:"category,omitempty"`
}

// Frame is one tick of the battle.
type Frame struct {
	Tick int64 `json:"tick"`
	// Ships holds every participant that is alive and has appeared, whether or
	// not the server emitted a snapshot for it this tick — state is carried
	// forward so a renderer never sees a ship blink out mid-battle.
	Ships   []ShipState `json:"ships"`
	Shots   []Shot      `json:"shots,omitempty"`
	Moves   []Move      `json:"moves,omitempty"`
	Kills   []Kill      `json:"kills,omitempty"`
	Repairs []Repair    `json:"repairs,omitempty"`
	Chatter []Chatter   `json:"chatter,omitempty"`
}

// ShipState is one combatant's drawable state at one tick.
type ShipState struct {
	PlayerID string  `json:"player_id"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Zone     string  `json:"zone"`

	Hull   int `json:"hull"`
	Shield int `json:"shield"`
	Fuel   int `json:"fuel"`

	Stance    string `json:"stance,omitempty"`
	TargetID  string `json:"target_id,omitempty"`
	AutoPilot bool   `json:"auto_pilot,omitempty"`

	DamageDealt int `json:"damage_dealt,omitempty"`
	DamageTaken int `json:"damage_taken,omitempty"`
	KillCount   int `json:"kill_count,omitempty"`

	// Stale marks a state carried forward because the server emitted no
	// snapshot this tick. The renderer may dim it or ignore the flag.
	Stale bool `json:"stale,omitempty"`
}

// ShotKind is the visual family a weapon fire belongs to. It is derived once
// here so the renderer never re-derives it from ammo strings.
type ShotKind string

const (
	// ShotBeam is a continuous energy beam — no ammo is consumed.
	ShotBeam ShotKind = "beam"
	// ShotMissile travels visibly across the gap over about a tick.
	ShotMissile ShotKind = "missile"
	// ShotProjectile is a stream of discrete rounds.
	ShotProjectile ShotKind = "projectile"
	// ShotVoid is exotic damage with its own treatment.
	ShotVoid ShotKind = "void"
	// ShotExplosive bursts at the target.
	ShotExplosive ShotKind = "explosive"
)

// Shot is one weapon's fire, expanded from an attack. An attack firing three
// weapons becomes three shots, because each can be a different weapon with its
// own visual treatment.
type Shot struct {
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`

	Kind       ShotKind `json:"kind"`
	WeaponName string   `json:"weapon_name,omitempty"`
	Ammo       string   `json:"ammo,omitempty"`
	DamageType string   `json:"damage_type,omitempty"`

	Hit  bool `json:"hit"`
	Crit bool `json:"crit,omitempty"`

	// Damage figures are the ATTACK's applied totals, repeated on each shot of
	// a multi-weapon attack: the server resolves resists per attack, not per
	// weapon, so splitting them per weapon would invent precision. WeaponDamage
	// is this weapon's own pre-resist contribution.
	Damage       int `json:"damage"`
	ShieldDamage int `json:"shield_damage,omitempty"`
	HullDamage   int `json:"hull_damage,omitempty"`
	WeaponDamage int `json:"weapon_damage,omitempty"`

	ZoneDistance int  `json:"zone_distance"`
	Splash       bool `json:"splash,omitempty"`
}

// Move is a range-band change.
type Move struct {
	PlayerID string `json:"player_id"`
	From     string `json:"from"`
	To       string `json:"to"`
	Reason   string `json:"reason,omitempty"`
}

// Kill is a destruction.
type Kill struct {
	KillerID string `json:"killer_id"`
	VictimID string `json:"victim_id"`
}

// Repair is shield regen / hull repair applied this tick.
type Repair struct {
	PlayerID     string `json:"player_id"`
	ShieldRegen  int    `json:"shield_regen,omitempty"`
	ArmorRepair  int    `json:"armor_repair,omitempty"`
	RemoteRepair int    `json:"remote_repair,omitempty"`
}

// Chatter is an autopilot decision, for the bridge-log rail.
type Chatter struct {
	PlayerID     string `json:"player_id"`
	Reason       string `json:"reason"`
	ChosenTarget string `json:"chosen_target,omitempty"`
}
