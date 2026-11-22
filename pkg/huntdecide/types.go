// Package huntdecide defines the decision seam used by the hunting worker:
// the state and options presented at a decision point, the choice made, and
// an exploring wrapper around a rule-based decider.
package huntdecide

import "context"

// Kind names the type of decision being made.
type Kind string

const (
	// KindProvision is the decision of what to provision before a sortie.
	KindProvision Kind = "provision"
	// KindReturn is the decision of whether to return from a sortie.
	KindReturn Kind = "return"
)

// Weapon describes one weapon module aboard.
type Weapon struct {
	ModuleID     string `json:"module_id"`
	TypeID       string `json:"type_id"`
	DamageType   string `json:"damage_type"`
	Reach        int    `json:"reach"`
	Cooldown     int    `json:"cooldown"`
	AmmoType     string `json:"ammo_type"` // "" = ammo-free
	MagazineSize int    `json:"magazine_size"`
	Loaded       int    `json:"loaded"`
	LoadedAmmoID string `json:"loaded_ammo_id"`
}

// Ammo is one ammo stack aboard.
type Ammo struct {
	AmmoType string `json:"ammo_type"`
	ItemID   string `json:"item_id"`
	Aboard   int    `json:"aboard"` // items; one item = one reload
}

// Job is the work the ship is currently pursuing.
type Job struct {
	Kind    string `json:"kind"` // "mission", "need", "home"
	ID      string `json:"id"`
	Species string `json:"species"`
	Target  int    `json:"target"`
	Done    int    `json:"done"`
}

// GroundVisit records one hunting ground visited during a sortie.
type GroundVisit struct {
	POI    string `json:"poi"`
	Quarry bool   `json:"quarry"`
}

// SortieSoFar summarises the sortie up to the decision point.
type SortieSoFar struct {
	TicksOut     int64         `json:"ticks_out"`
	Grounds      []GroundVisit `json:"grounds"`
	Fights       int           `json:"fights"`
	Kills        int           `json:"kills"`
	ReloadsSpent int           `json:"reloads_spent"`
}

// State is the snapshot of the world and ship at a decision point.
type State struct {
	AgentID        string      `json:"agent_id"`
	ShipClass      string      `json:"ship_class"`
	Hull           float64     `json:"hull"`
	MaxHull        float64     `json:"max_hull"`
	Shield         float64     `json:"shield"`
	MaxShield      float64     `json:"max_shield"`
	Fuel           float64     `json:"fuel"`
	MaxFuel        float64     `json:"max_fuel"`
	FreeHold       int         `json:"free_hold"`
	Credits        float64     `json:"credits"`
	System         string      `json:"system"`
	POI            string      `json:"poi"`
	Docked         bool        `json:"docked"`
	PoliceLevel    int         `json:"police_level"`     // -1 = unknown
	JumpsToStation int         `json:"jumps_to_station"` // -1 = unknown
	JumpsToHome    int         `json:"jumps_to_home"`    // -1 = unknown
	Weapons        []Weapon    `json:"weapons"`
	Ammo           []Ammo      `json:"ammo"`
	Job            Job         `json:"job"`
	Sortie         SortieSoFar `json:"sortie"`
	Tick           int64       `json:"tick"`
	GameVersion    string      `json:"game_version"`
	WorkerCommit   string      `json:"worker_commit"`
}

// Option is one candidate action at a decision point.
type Option struct {
	ID        string         `json:"id"`
	Features  map[string]any `json:"features"`
	NoExplore bool           `json:"no_explore,omitempty"`
}

// Decision is the question put to a Decider.
type Decision struct {
	Kind     Kind     `json:"kind"`
	SortieID string   `json:"sortie_id"`
	Seq      int      `json:"seq"`
	State    State    `json:"state"`
	Options  []Option `json:"options"`
}

const (
	// SourceRule marks a choice made by the rule-based decider.
	SourceRule = "rule"
	// SourceExplore marks a choice drawn at random by the Explorer.
	SourceExplore = "explore"
)

// Choice is the answer a Decider returns.
type Choice struct {
	OptionID     string  `json:"option_id"`
	RuleOptionID string  `json:"rule_option_id"`
	Source       string  `json:"source"`
	Epsilon      float64 `json:"epsilon"`
	Reason       string  `json:"reason"`
}

// Decider picks one of a Decision's options.
type Decider interface {
	Decide(ctx context.Context, d Decision) Choice
}
