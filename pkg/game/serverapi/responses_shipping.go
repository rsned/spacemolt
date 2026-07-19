package serverapi

// Response structs for the /shipping freight-contract endpoint (one
// action-dispatched endpoint; responses are a discriminated union keyed on
// "action"). Field names are taken verbatim from server_docs/openapi.json
// (live server >= v0.532.0). Money / value / liability / escrow / tick fields
// are int64; counts are int; ids, enums and date-time timestamps are string.
// Optional nested actors are *ShipmentActor (absent -> nil).

// ShipmentActor identifies a party to a shipment (kind: player|faction|station).
type ShipmentActor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// ShipmentContract is a freight contract. Deadlines are ticks (DeadlineTick,
// TargetTick), not wall-clock. Status: posted|in_transit|delivered|returned|
// breached|defaulted|canceled. RiskBand / carrier tier:
// probationary|licensed|trusted|prime (+ unpriced for risk_band).
type ShipmentContract struct {
	ID                      string         `json:"id"`
	PackageID               string         `json:"package_id"`
	Shipper                 *ShipmentActor `json:"shipper"`
	Recipient               *ShipmentActor `json:"recipient"`
	Contractor              *ShipmentActor `json:"contractor,omitempty"`
	OriginBaseID            string         `json:"origin_base_id"`
	DestinationBaseID       string         `json:"destination_base_id"`
	ShippingHouseID         string         `json:"shipping_house_id"`
	Visibility              string         `json:"visibility"`
	ServiceLevel            string         `json:"service_level"`
	Status                  string         `json:"status"`
	PostedAt                string         `json:"posted_at"`
	ListingExpiresAt        string         `json:"listing_expires_at"`
	AcceptedAt              string         `json:"accepted_at,omitempty"`
	AcceptedTick            int64          `json:"accepted_tick,omitempty"`
	DeliveredAt             string         `json:"delivered_at,omitempty"`
	BreachedAt              string         `json:"breached_at,omitempty"`
	SettledAt               string         `json:"settled_at,omitempty"`
	TargetTick              int64          `json:"target_tick,omitempty"`
	DeadlineTick            int64          `json:"deadline_tick,omitempty"`
	BaseReward              int64          `json:"base_reward"`
	MaxSpeedBonus           int64          `json:"max_speed_bonus"`
	ServiceFee              int64          `json:"service_fee"`
	RewardEscrow            int64          `json:"reward_escrow"`
	SpeedBonusEscrow        int64          `json:"speed_bonus_escrow"`
	AppraisedValue          int64          `json:"appraised_value,omitempty"`
	CoveredValue            int64          `json:"covered_value,omitempty"`
	Premium                 int64          `json:"premium,omitempty"`
	ReservedExposure        int64          `json:"reserved_exposure"`
	FailureDebt             int64          `json:"failure_debt"`
	CarrierPayout           int64          `json:"carrier_payout,omitempty"`
	ClaimPaid               int64          `json:"claim_paid,omitempty"`
	PolicyStatus            string         `json:"policy_status"`
	Insurable               bool           `json:"insurable"`
	UninsurableReason       string         `json:"uninsurable_reason,omitempty"`
	RiskBand                string         `json:"risk_band"`
	Insurer                 *ShipmentActor `json:"insurer,omitempty"`
	InvitedCarrier          *ShipmentActor `json:"invited_carrier,omitempty"`
	SalvageOwner            *ShipmentActor `json:"salvage_owner,omitempty"`
	ReputationEligible      bool           `json:"reputation_eligible,omitempty"`
	RouteHops               int            `json:"route_hops,omitempty"`
	TerminalReason          string         `json:"terminal_reason,omitempty"`
	LatestBeaconAt          string         `json:"latest_beacon_at,omitempty"`
	LatestBeaconFingerprint string         `json:"latest_beacon_fingerprint,omitempty"`
}

// ShippingListing wraps a contract for the board view; Eligible reports whether
// this carrier can actually accept it (Reason explains when not).
type ShippingListing struct {
	Contract ShipmentContract `json:"contract"`
	Eligible bool             `json:"eligible"`
	Reason   string           `json:"reason,omitempty"`
}

// CarrierProfile is global carrier standing for an actor.
type CarrierProfile struct {
	Actor                *ShipmentActor `json:"actor"`
	Tier                 string         `json:"tier"`
	SuccessfulDeliveries int            `json:"successful_deliveries"`
	DeliveredValue       int64          `json:"delivered_value"`
	PriorityDeliveries   int            `json:"priority_deliveries"`
	Returns              int            `json:"returns"`
	Breaches             int            `json:"breaches"`
	Defaults             int            `json:"defaults"`
	ActiveContracts      int            `json:"active_contracts"`
	ActiveLiability      int64          `json:"active_liability"`
	OutstandingDebt      int64          `json:"outstanding_debt"`
	UpdatedAt            string         `json:"updated_at"`
	LastConsequenceAt    string         `json:"last_consequence_at,omitempty"`
	LastRecoveryAt       string         `json:"last_recovery_at,omitempty"`
}

// CarrierCapacity is the actor's concurrent-contract and liability headroom.
type CarrierCapacity struct {
	ActiveContracts             int   `json:"active_contracts"`
	ActiveContractsUnlimited    bool  `json:"active_contracts_unlimited"`
	ActiveLiability             int64 `json:"active_liability"`
	LiabilityUnlimited          bool  `json:"liability_unlimited"`
	ActiveContractLimit         int   `json:"active_contract_limit,omitempty"`
	AggregateLiabilityLimit     int64 `json:"aggregate_liability_limit,omitempty"`
	RemainingAggregateLiability int64 `json:"remaining_aggregate_liability,omitempty"`
	SinglePackageLiabilityLimit int64 `json:"single_package_liability_limit,omitempty"`
}

// CarrierTierProgress is progress toward the next carrier tier.
type CarrierTierProgress struct {
	CurrentTier                   string `json:"current_tier"`
	NextTier                      string `json:"next_tier,omitempty"`
	AtMaximumTier                 bool   `json:"at_maximum_tier"`
	SuccessfulDeliveries          int    `json:"successful_deliveries"`
	RequiredSuccessfulDeliveries  int    `json:"required_successful_deliveries"`
	RemainingSuccessfulDeliveries int    `json:"remaining_successful_deliveries"`
	DeliveredValue                int64  `json:"delivered_value"`
	RequiredDeliveredValue        int64  `json:"required_delivered_value"`
	RemainingDeliveredValue       int64  `json:"remaining_delivered_value"`
}

// FreightDebt is an outstanding failure-debt owed by a carrier.
type FreightDebt struct {
	ID          string         `json:"id"`
	ShipmentID  string         `json:"shipment_id"`
	Debtor      *ShipmentActor `json:"debtor"`
	Creditor    *ShipmentActor `json:"creditor"`
	Original    int64          `json:"original"`
	Outstanding int64          `json:"outstanding"`
	CreatedAt   string         `json:"created_at"`
	PaidAt      string         `json:"paid_at,omitempty"`
}

// ShipmentTrackingEvent is one beacon custody event (class: shipping_house_escrow
// |ship|player_storage|faction_storage|faction_bucket|wreck|unpack_job_escrow|
// destroyed|unknown).
type ShipmentTrackingEvent struct {
	ID           string         `json:"id"`
	ShipmentID   string         `json:"shipment_id"`
	PackageID    string         `json:"package_id"`
	Class        string         `json:"class"`
	Custodian    *ShipmentActor `json:"custodian,omitempty"`
	Fingerprint  string         `json:"fingerprint"`
	ObservedAt   string         `json:"observed_at"`
	ObservedTick int64          `json:"observed_tick"`
	BaseID       string         `json:"base_id,omitempty"`
	POIID        string         `json:"poi_id,omitempty"`
	ReferenceID  string         `json:"reference_id,omitempty"`
	SystemID     string         `json:"system_id,omitempty"`
}

// ShippingListResponse is action=list (the docked freight board).
type ShippingListResponse struct {
	Action          string            `json:"action"`
	Shipments       []ShippingListing `json:"shipments"`
	Page            int               `json:"page"`
	PerPage         int               `json:"per_page"`
	Total           int               `json:"total"`
	EmptyReason     string            `json:"empty_reason,omitempty"`
	EmptyReasonCode string            `json:"empty_reason_code,omitempty"`
}

// ShippingContractResponse is action=post|get|accept.
type ShippingContractResponse struct {
	Action   string           `json:"action"`
	Contract ShipmentContract `json:"contract"`
}

// ShippingSettlementResponse is action=deliver|return|cancel.
type ShippingSettlementResponse struct {
	Action        string           `json:"action"`
	Contract      ShipmentContract `json:"contract"`
	CarrierPayout int64            `json:"carrier_payout,omitempty"`
	ClaimPaid     int64            `json:"claim_paid,omitempty"`
	DebtCreated   int64            `json:"debt_created,omitempty"`
	ShipperRefund int64            `json:"shipper_refund,omitempty"`
}

// ShippingProfileResponse is action=profile.
type ShippingProfileResponse struct {
	Action               string              `json:"action"`
	Profile              CarrierProfile      `json:"profile"`
	Capacity             CarrierCapacity     `json:"capacity"`
	Progression          CarrierTierProgress `json:"progression"`
	Debts                []FreightDebt       `json:"debts"`
	DebtBlocksAcceptance bool                `json:"debt_blocks_acceptance"`
	DebtBlockReason      string              `json:"debt_block_reason,omitempty"`
}

// ShippingTrackResponse is action=track.
type ShippingTrackResponse struct {
	Action   string                  `json:"action"`
	Contract ShipmentContract        `json:"contract"`
	Events   []ShipmentTrackingEvent `json:"events"`
}

// ShippingDebtPaymentResponse is action=pay_debt.
type ShippingDebtPaymentResponse struct {
	Action               string              `json:"action"`
	AmountPaid           int64               `json:"amount_paid"`
	Profile              CarrierProfile      `json:"profile"`
	Capacity             CarrierCapacity     `json:"capacity"`
	Progression          CarrierTierProgress `json:"progression"`
	DebtBlocksAcceptance bool                `json:"debt_blocks_acceptance"`
	DebtBlockReason      string              `json:"debt_block_reason,omitempty"`
	UpdatedDebts         []FreightDebt       `json:"updated_debts"`
	OutstandingDebts     []FreightDebt       `json:"outstanding_debts"`
}
