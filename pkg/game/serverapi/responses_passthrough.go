package serverapi

// Response structs for commands the client has no dedicated method for. They are
// still reachable — play_as's raw passthrough will send any command the server
// accepts — so a response can and does arrive for every one of them.
//
// These were previously listed in pkg/game's ignoredCommands as "not
// implemented", which was misleading: the commands ARE implemented server-side
// and return fully-specified payloads. Without a struct here, the API monitor
// logs `Unhandled action "..."` on first use and, worse, we get no field-drift
// detection for them at all. That is exactly how get_faction_tax_estimate went
// unnoticed until 2026-07-14.
//
// Shapes are taken from openapi.json (v0.495.1). Only get_faction_tax_estimate
// has been confirmed against a live payload; the rest are spec-derived and
// unverified — see BuildBaseResponse for what that caveat can cost.

// FactionGaragesResponse is returned by faction_garages: ships the faction has
// stored, grouped by the station holding them.
//   - faction_garages
type FactionGaragesResponse struct {
	StationCount int             `json:"station_count"`
	TotalShips   int             `json:"total_ships"`
	Stations     []FactionGarage `json:"stations"`
}

// FactionGarage is one station's garage in a faction_garages response.
type FactionGarage struct {
	BaseID     string   `json:"base_id"`
	BaseName   string   `json:"base_name"`
	SystemName string   `json:"system_name"`
	Capacity   int      `json:"capacity"`
	Used       int      `json:"used"`
	Ships      []string `json:"ships,omitempty"`
}

// HuntResponse is returned by hunt. Hunting resolves on a later tick, so the
// immediate reply only acknowledges the command (Pending).
//   - hunt
type HuntResponse struct {
	Command string `json:"command"`
	Message string `json:"message"`
	Pending bool   `json:"pending"`
}

// ShipLicenseResponse is returned by buy_ship_license: the right to build an
// empire's hull, paid for out of faction credits, against a per-sale royalty.
//   - buy_ship_license
type ShipLicenseResponse struct {
	Empire             string `json:"empire"`
	ShipClass          string `json:"ship_class"`
	ShipName           string `json:"ship_name"`
	CostPaid           int    `json:"cost_paid"`
	RoyaltyPercent     int    `json:"royalty_percent"`
	FactionCreditsLeft int    `json:"faction_credits_left"`
	Hint               string `json:"hint,omitempty"`
}

// PlaceShipBuyOrderResponse is returned by place_ship_buy_order: a standing
// offer to buy a hull of ClassID at Price, with the sales tax held in escrow.
//   - place_ship_buy_order
type PlaceShipBuyOrderResponse struct {
	OrderID     string `json:"order_id"`
	ClassID     string `json:"class_id"`
	Price       int    `json:"price"`
	TaxEscrow   int    `json:"tax_escrow,omitempty"`
	CreditsLeft int    `json:"credits_left"`
	Message     string `json:"message"`
}

// CancelShipBuyOrderResponse is returned by cancel_ship_buy_order.
//   - cancel_ship_buy_order
type CancelShipBuyOrderResponse struct {
	OrderID     string `json:"order_id"`
	Refund      int    `json:"refund"`
	CreditsLeft int    `json:"credits_left"`
	Message     string `json:"message"`
}

// ViewShipBuyOrdersResponse is returned by view_ship_buy_orders.
//   - view_ship_buy_orders
type ViewShipBuyOrdersResponse struct {
	Count  int            `json:"count"`
	Orders []ShipBuyOrder `json:"orders"`
}

// ShipBuyOrder is one standing ship buy order. BeingBuilt is true once a seller
// has committed to filling it.
type ShipBuyOrder struct {
	OrderID    string `json:"order_id"`
	ClassID    string `json:"class_id"`
	ClassName  string `json:"class_name"`
	Buyer      string `json:"buyer"`
	BaseID     string `json:"base_id"`
	BaseName   string `json:"base_name"`
	Price      int    `json:"price"`
	TaxEscrow  int    `json:"tax_escrow,omitempty"`
	BeingBuilt bool   `json:"being_built,omitempty"`
	CreatedAt  string `json:"created_at,omitempty"`
}

// SellShipToOrderResponse is returned by sell_ship_to_order.
//   - sell_ship_to_order
type SellShipToOrderResponse struct {
	ShipID      string `json:"ship_id"`
	ClassID     string `json:"class_id"`
	Price       int    `json:"price"`
	CreditsLeft int    `json:"credits_left"`
	Message     string `json:"message"`
}

// PrepayTaxResponse is returned by prepay_tax: credits paid ahead of the next
// personal assessment, offsetting what will be owed.
//   - prepay_tax
type PrepayTaxResponse struct {
	Action            string `json:"action"`
	AmountPrepaid     int64  `json:"amount_prepaid"`
	TaxPrepaidBalance int64  `json:"tax_prepaid_balance"`
	Credits           int64  `json:"credits"`
	Message           string `json:"message"`
}

// FactionPrepayTaxResponse is returned by faction_prepay_tax. Same as
// PrepayTaxResponse but drawn from (and reporting) faction credits.
//   - faction_prepay_tax
type FactionPrepayTaxResponse struct {
	Action            string `json:"action"`
	AmountPrepaid     int64  `json:"amount_prepaid"`
	TaxPrepaidBalance int64  `json:"tax_prepaid_balance"`
	FactionCredits    int64  `json:"faction_credits"`
	Message           string `json:"message"`
}

// FactionScanPOIResponse is returned by faction_scan_poi: a remote scan of a POI
// run through a faction facility, so it reaches Hops systems away rather than
// only the current one.
//   - faction_scan_poi
type FactionScanPOIResponse struct {
	POIID             string        `json:"poi_id"`
	POIName           string        `json:"poi_name,omitempty"`
	SystemID          string        `json:"system_id,omitempty"`
	Hops              int           `json:"hops"`
	ScanPower         int           `json:"scan_power"`
	FacilityLevel     int           `json:"facility_level"`
	FacilityStation   string        `json:"facility_station,omitempty"`
	SignatureDetected bool          `json:"signature_detected,omitempty"`
	Contacts          []ScanContact `json:"contacts,omitempty"`
	NPCs              []ScanNPC     `json:"npcs,omitempty"`
	Pirates           []ScanNPC     `json:"pirates,omitempty"`
	Message           string        `json:"message"`
}

// ScanContact is a player ship turned up by faction_scan_poi. RevealedInfo
// reports how much the scan actually resolved — a cloaked target yields less.
type ScanContact struct {
	TargetID     string `json:"target_id"`
	Username     string `json:"username"`
	ShipName     string `json:"ship_name,omitempty"`
	ShipClass    string `json:"ship_class,omitempty"`
	FactionID    string `json:"faction_id,omitempty"`
	Hull         int    `json:"hull,omitempty"`
	Shield       int    `json:"shield,omitempty"`
	Cloaked      bool   `json:"cloaked,omitempty"`
	RevealedInfo string `json:"revealed_info,omitempty"`
}

// ScanNPC is an NPC or pirate turned up by faction_scan_poi. Pirates carry no
// empire.
type ScanNPC struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role,omitempty"`
	ShipClass string `json:"ship_class,omitempty"`
	Empire    string `json:"empire,omitempty"`
}

// GetFactionAchievementsResponse is returned by get_faction_achievements. It is
// shape-identical to GetAchievementsResponse — the same per-player progress view
// (earned/progress/share_url), scoped to the faction's achievements.
//   - get_faction_achievements
type GetFactionAchievementsResponse struct {
	Achievements []Achievement      `json:"achievements"`
	Summary      AchievementSummary `json:"summary"`
	Message      string             `json:"message,omitempty"`
}

// NotificationSettingsResponse is returned by get_notification_settings and by
// the mute_notifications / unmute_notifications mutations, which each echo back
// the full resulting settings.
//   - get_notification_settings
//   - mute_notifications
//   - unmute_notifications
type NotificationSettingsResponse struct {
	Action   string                `json:"action"`
	Channels []NotificationChannel `json:"channels"`
	Muted    []string              `json:"muted"`
	Message  string                `json:"message"`
}

// NotificationChannel is one mutable notification channel.
type NotificationChannel struct {
	Channel      string   `json:"channel"`
	Description  string   `json:"description,omitempty"`
	MessageTypes []string `json:"message_types,omitempty"`
	Muted        bool     `json:"muted"`
}

// StationConfigResponse is returned by station: the owner-facing admin view of a
// player/faction station. ServiceAccess maps a service name to its access policy.
//   - station
type StationConfigResponse struct {
	Action                  string            `json:"action"`
	BaseID                  string            `json:"base_id"`
	Name                    string            `json:"name"`
	Description             string            `json:"description,omitempty"`
	PublicAccess            bool              `json:"public_access"`
	AllowOutsiderFacilities bool              `json:"allow_outsider_facilities"`
	AutoBuyFuel             bool              `json:"auto_buy_fuel"`
	AllowedFactions         []string          `json:"allowed_factions,omitempty"`
	AllowedPlayers          []string          `json:"allowed_players,omitempty"`
	BannedPlayers           []string          `json:"banned_players,omitempty"`
	ServiceAccess           map[string]string `json:"service_access,omitempty"`
	MarketFeeBps            int               `json:"market_fee_bps,omitempty"`
	RefuelPriceEach         int               `json:"refuel_price_each,omitempty"`
	RepairPricePerHull      int               `json:"repair_price_per_hull,omitempty"`
	Message                 string            `json:"message,omitempty"`
}
