package assets

import "time"

// Identity is the three-way agent identity map. None of the three is derivable
// from the others:
//   - PlayerID is the server's stable hex id (Player.ID). Never changes.
//   - Username is the in-game display name. CAN change.
//   - AgentID is our local label (engineer-3) from data/agents and the fleet
//     YAMLs. Not a game concept at all.
//
// PlayerID is the primary key everywhere in this package.
type Identity struct {
	PlayerID string
	AgentID  string
	Username string
}

// Profile is the scalar half of get_status: one row per agent.
type Profile struct {
	PlayerID      string
	Username      string
	Empire        string
	Credits       float64
	HomeBase      string
	DockedAtBase  string
	CurrentSystem string
	CurrentPOI    string
	ActiveShipID  string
	FactionID     string
	FactionRank   string
	Experience    int64
	CapturedAt    time.Time
}

// SkillRow is one entry of Player.Skills.
type SkillRow struct {
	Skill string
	Level int
	XP    float64
}

// StandingRow is one entry of Player.Standings. Baseline is the decay target
// and therefore the durable signal; Reputation floats above it.
type StandingRow struct {
	Faction           string
	Reputation        int
	Baseline          int
	OutstandingBounty int
	JailedUntil       string
}

// Hull is one owned ship from list_ships. ListShips reports EVERY owned ship
// and its location, and serverapi.OwnedShip is a strict superset of
// StorageShip, so one free call replaces a per-base ship sweep.
//
// HullRaw/FuelRaw retain the server's "current/max" strings so a format change
// shows up as a mismatch rather than as silent zeros.
type Hull struct {
	ShipID         string
	ClassID        string
	ClassName      string
	IsActive       bool
	HullCurrent    int
	HullMax        int
	HullRaw        string
	FuelCurrent    int
	FuelMax        int
	FuelRaw        string
	CargoUsed      int
	Location       string
	LocationBaseID string
	Modules        int
	ListingID      string
	ListingPrice   int64
	ListingBaseID  string
}
