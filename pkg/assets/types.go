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

// Carrier is the freight carrier standing: CarrierProfile, CarrierCapacity and
// CarrierTierProgress flattened. Remaining* is progress toward the next tier.
type Carrier struct {
	Tier                          string
	SuccessfulDeliveries          int
	DeliveredValue                int64
	PriorityDeliveries            int
	Returns                       int
	Breaches                      int
	Defaults                      int
	ActiveContracts               int
	ActiveLiability               int64
	OutstandingDebt               int64
	DebtBlocksAcceptance          bool
	NextTier                      string
	AtMaximumTier                 bool
	RequiredSuccessfulDeliveries  int
	RemainingSuccessfulDeliveries int
	RequiredDeliveredValue        int64
	RemainingDeliveredValue       int64
	ActiveContractLimit           int
	ActiveContractsUnlimited      bool
	AggregateLiabilityLimit       int64
	RemainingAggregateLiability   int64
	SinglePackageLiabilityLimit   int64
	LiabilityUnlimited            bool
}

// StorageItem is one line of a base's storage manifest. Quantity is float64
// because CargoItem.Quantity is float64 on the wire.
type StorageItem struct {
	ItemID   string
	Name     string
	Quantity float64
	Size     int
}

// StorageBase is one agent's holdings at one base. Credits is often absent from
// the payload (omitted when zero), which decodes to 0 -- correct either way.
type StorageBase struct {
	BaseID  string
	Credits int
	Items   []StorageItem
}

// FactionProfile is the scalar half of faction_info: one row per faction.
type FactionProfile struct {
	FactionID   string
	Name        string
	Tag         string
	LeaderID    string
	Treasury    int
	MemberCount int
	OwnedBases  int
}

// FactionStorageBase is one faction's shared holdings at one base, including
// its fuel bunker there.
type FactionStorageBase struct {
	BaseID       string
	Credits      int
	FuelReserve  int
	FuelCapacity int
	Items        []StorageItem
}
