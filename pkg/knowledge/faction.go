package knowledge

import "time"

// FactionRecord is the current-state header row for a faction.
type FactionRecord struct {
	FactionID      string
	Name           string
	Tag            string
	LeaderID       string
	LeaderUsername string
	Treasury       int
	MemberCount    int
	OwnedBases     int
	Description    string
	Charter        string
	Emblem         string
	PrimaryColor   string
	SecondaryColor string
	FoundedUTC     string
	IntelSystems   int
	IntelTrade     int
	CapturedAt     time.Time
}

// FactionListEntry holds the lightweight header fields a faction_list response
// carries — a strict subset of FactionRecord. It omits the columns only a full
// faction_info capture provides (treasury, leader_id, description, charter,
// emblem, founded_utc, intel_*), so seeding from it must not clobber those.
type FactionListEntry struct {
	FactionID      string
	Name           string
	Tag            string
	LeaderUsername string
	MemberCount    int
	OwnedBases     int
	PrimaryColor   string
	SecondaryColor string
}

// FactionMember is one member of a faction.
type FactionMember struct {
	FactionID   string
	PlayerID    string
	Username    string
	Role        string
	JoinedUTC   string
	LastSeenUTC string
	IsOnline    bool
	CapturedAt  time.Time
}

// FactionRelation is an ally/enemy/war/peace_proposal edge.
type FactionRelation struct {
	FactionID       string
	TargetFactionID string
	TargetName      string
	TargetTag       string
	Kind            string // "ally" | "enemy" | "war" | "peace_proposal"
	Reason          string
	Terms           string
	OurKills        int
	TheirKills      int
	StartedUTC      string
	CapturedAt      time.Time
}

// FactionBaseRow is an owned base / location.
type FactionBaseRow struct {
	FactionID    string
	BaseID       string
	BaseName     string
	SystemID     string
	SystemName   string
	POIID        string
	ServicesJSON string
	CapturedAt   time.Time
}

// FactionFacilityRow is a faction facility at a base.
type FactionFacilityRow struct {
	FactionID    string
	BaseID       string
	FacilityID   string
	FacilityType string
	Category     string
	Level        int
	Status       string
	RecipeID     string
	DetailsJSON  string
	CapturedAt   time.Time
}

// FactionStorageItem is one item in faction storage at a base.
type FactionStorageItem struct {
	ItemID   string
	Name     string
	Quantity float64
	Size     int
}

// FactionStorageRow is faction storage at a single base (header + items).
type FactionStorageRow struct {
	FactionID  string
	BaseID     string
	Credits    int
	ItemCount  int
	Items      []FactionStorageItem
	CapturedAt time.Time
}

// FactionOrderRow is a faction market buy/sell order.
type FactionOrderRow struct {
	FactionID  string
	BaseID     string
	OrderID    string
	Side       string // "buy" | "sell"
	ItemID     string
	ItemName   string
	PriceEach  float64
	Quantity   float64
	CapturedAt time.Time
}

// FactionMissionRow is a posted faction mission.
type FactionMissionRow struct {
	FactionID        string
	BaseID           string
	MissionID        string
	Title            string
	Type             string
	Description      string
	GiverName        string
	RewardsJSON      string
	ObjectivesJSON   string
	AssignedPlayerID string
	ExpirationUTC    string
	CapturedAt       time.Time
}

// FactionRoomRow is a faction common-space room at a base.
type FactionRoomRow struct {
	FactionID   string
	BaseID      string
	RoomID      string
	Name        string
	Access      string
	Description string
	CapturedAt  time.Time
}

// FactionFuelBunkerRow is one base's fuel-bunker status for a faction
// (faction_info galaxy-wide fuel summary, gameserver v0.346.0+).
type FactionFuelBunkerRow struct {
	FactionID    string
	BaseID       string
	BaseName     string
	FuelReserve  int
	FuelCapacity int
	CapturedAt   time.Time
}

// FactionView is the full assembled current state for one faction, used by the
// dashboard renderer.
type FactionView struct {
	Faction     FactionRecord
	Members     []FactionMember
	Relations   []FactionRelation
	Bases       []FactionBaseRow
	Facilities  []FactionFacilityRow
	Storage     []FactionStorageRow
	Orders      []FactionOrderRow
	Missions    []FactionMissionRow
	Rooms       []FactionRoomRow
	FuelBunkers []FactionFuelBunkerRow
}
