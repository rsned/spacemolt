# Freight Carrier — Sub-project A (pkg/game shipping client) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the `pkg/game` client surface + typed response structs for the server's `/shipping` freight-contract endpoint (none exists today), so a worker can list / get / accept / deliver / track / profile / pay_debt / return / cancel freight contracts, and gather the live facts Sub-project B needs.

**Architecture:** `/shipping` is one action-dispatched endpoint: `{"type":"shipping","payload":{"action":"<verb>", ...}}`, with a discriminated response union keyed on `action`. Command methods follow the codebase pattern (build `protocol.Message`, `Submit(...WithAckOnly()...)` + `await`, return `error`); the reply is cached by `storeRawJSON()` under an action-derived key and read back via `Client.GetRawJSON("<key>")` + `json.Unmarshal` (exactly how `missionFetchActiveMissions` works). This sub-project is the foundation reused later by haulers; it deliberately excludes the carrier *behavior* (Sub-project B) and the shipper actions `quote`/`post`.

**Tech Stack:** Go 1.24+, `pkg/game` + `pkg/game/serverapi`, `internal/protocol`.

## Global Constraints

- **Field names verbatim from `server_docs/openapi.json`** (live server ≥ v0.532.0). Never assume names — every struct tag below was extracted from the spec.
- Money / value / liability / escrow / tick fields → `int64`; small counts → `int`; ids / enums / timestamps (`date-time`) → `string`; flags → `bool`. Optional nested actor objects → `*ShipmentActor` (absence = nil). Decode-only structs, so `omitempty` is cosmetic.
- `golangci-lint` must stay clean (no new findings). `go build ./...` **does not** catch mock breakage — adding `GameClient` interface methods breaks the mocks in `pkg/agent` and `pkg/skills`; **run `go test ./...`** (`feedback_gameclient_interface_mocks`).
- Known pre-existing unrelated RED: `pkg/game TestServerCommandsCoveredByClient` (API drift) — ignore; do not try to fix it here.
- Stage only the files each task names. NEVER `git add -A` (runtime `data/**` churn must stay uncommitted). FORBIDDEN: `git stash`.
- Commit trailer, exactly: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`

---

### Task 1: Shipping response structs (`serverapi`)

**Files:**
- Create: `pkg/game/serverapi/responses_shipping.go`
- Test: `pkg/game/serverapi/responses_shipping_test.go`

**Interfaces:**
- Produces: `ShipmentContract`, `ShippingListing`, `ShipmentActor`, `CarrierProfile`, `CarrierCapacity`, `CarrierTierProgress`, `FreightDebt`, `ShipmentTrackingEvent`, and the response wrappers `ShippingListResponse`, `ShippingContractResponse`, `ShippingSettlementResponse`, `ShippingProfileResponse`, `ShippingTrackResponse`, `ShippingDebtPaymentResponse`. Consumed by Task 3 (client methods) and Sub-project B.

- [ ] **Step 1: Write the failing decode test**

Create `pkg/game/serverapi/responses_shipping_test.go`:

```go
package serverapi

import (
	"encoding/json"
	"testing"
)

func TestShippingContractResponseDecodes(t *testing.T) {
	// Shape from openapi.json ShippingContractResponse (action=accept).
	raw := []byte(`{
	  "action":"accept",
	  "contract":{
	    "id":"shp_1","package_id":"pkg_1",
	    "shipper":{"kind":"station","id":"sol_station"},
	    "recipient":{"kind":"station","id":"procyon_station"},
	    "contractor":{"kind":"player","id":"engineer-2"},
	    "origin_base_id":"sol_station","destination_base_id":"procyon_station",
	    "shipping_house_id":"house_1","visibility":"public","service_level":"standard",
	    "status":"in_transit","posted_at":"2026-07-19T20:00:00Z",
	    "listing_expires_at":"2026-07-19T22:00:00Z","accepted_tick":1000,
	    "target_tick":1200,"deadline_tick":1400,
	    "base_reward":5000,"max_speed_bonus":1000,"service_fee":50,
	    "reward_escrow":5000,"speed_bonus_escrow":1000,"failure_debt":2000,
	    "appraised_value":12000,"covered_value":0,"premium":0,"reserved_exposure":2000,
	    "policy_status":"none","insurable":true,"risk_band":"probationary",
	    "route_hops":3,"reputation_eligible":true
	  }
	}`)
	var resp ShippingContractResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Action != "accept" {
		t.Fatalf("action = %q, want accept", resp.Action)
	}
	c := resp.Contract
	if c.ID != "shp_1" || c.PackageID != "pkg_1" {
		t.Fatalf("ids: %q / %q", c.ID, c.PackageID)
	}
	if c.DestinationBaseID != "procyon_station" || c.Status != "in_transit" {
		t.Fatalf("dest/status: %q / %q", c.DestinationBaseID, c.Status)
	}
	if c.DeadlineTick != 1400 || c.TargetTick != 1200 {
		t.Fatalf("ticks: %d / %d", c.DeadlineTick, c.TargetTick)
	}
	if c.BaseReward != 5000 || c.MaxSpeedBonus != 1000 {
		t.Fatalf("reward: %d / %d", c.BaseReward, c.MaxSpeedBonus)
	}
	if c.Shipper == nil || c.Shipper.Kind != "station" || c.Shipper.ID != "sol_station" {
		t.Fatalf("shipper: %+v", c.Shipper)
	}
	if c.Contractor == nil || c.Contractor.ID != "engineer-2" {
		t.Fatalf("contractor: %+v", c.Contractor)
	}
}

func TestShippingListResponseDecodes(t *testing.T) {
	raw := []byte(`{
	  "action":"list","page":1,"per_page":20,"total":1,
	  "empty_reason":"","empty_reason_code":"",
	  "shipments":[{
	    "eligible":true,"reason":"",
	    "contract":{"id":"shp_9","package_id":"pkg_9",
	      "shipper":{"kind":"station","id":"a"},"recipient":{"kind":"station","id":"b"},
	      "origin_base_id":"a","destination_base_id":"b","shipping_house_id":"h",
	      "visibility":"public","service_level":"standard","status":"posted",
	      "posted_at":"2026-07-19T20:00:00Z","listing_expires_at":"2026-07-19T22:00:00Z",
	      "deadline_tick":1400,"base_reward":3000,"max_speed_bonus":0,"service_fee":0,
	      "reward_escrow":3000,"speed_bonus_escrow":0,"failure_debt":1000,
	      "reserved_exposure":1000,"policy_status":"none","insurable":false,
	      "risk_band":"probationary","route_hops":2}
	  }]
	}`)
	var resp ShippingListResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Total != 1 || len(resp.Shipments) != 1 {
		t.Fatalf("total/len: %d / %d", resp.Total, len(resp.Shipments))
	}
	l := resp.Shipments[0]
	if !l.Eligible || l.Contract.ID != "shp_9" || l.Contract.BaseReward != 3000 {
		t.Fatalf("listing: %+v", l)
	}
}

func TestShippingProfileResponseDecodes(t *testing.T) {
	raw := []byte(`{
	  "action":"profile","debt_blocks_acceptance":false,"debt_block_reason":"",
	  "profile":{"actor":{"kind":"player","id":"engineer-2"},"tier":"probationary",
	    "successful_deliveries":3,"delivered_value":15000,"priority_deliveries":0,
	    "returns":0,"breaches":0,"defaults":0,"active_contracts":1,
	    "active_liability":2000,"outstanding_debt":0,"updated_at":"2026-07-19T20:00:00Z"},
	  "capacity":{"active_contracts":1,"active_contracts_unlimited":false,
	    "active_liability":2000,"liability_unlimited":false,"active_contract_limit":3,
	    "aggregate_liability_limit":50000,"remaining_aggregate_liability":48000,
	    "single_package_liability_limit":20000},
	  "progression":{"current_tier":"probationary","next_tier":"licensed",
	    "at_maximum_tier":false,"successful_deliveries":3,"required_successful_deliveries":10,
	    "remaining_successful_deliveries":7,"delivered_value":15000,
	    "required_delivered_value":50000,"remaining_delivered_value":35000},
	  "debts":[]
	}`)
	var resp ShippingProfileResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.DebtBlocksAcceptance {
		t.Fatalf("want not debt-blocked")
	}
	if resp.Profile.Tier != "probationary" || resp.Profile.ActiveContracts != 1 {
		t.Fatalf("profile: %+v", resp.Profile)
	}
	if resp.Capacity.SinglePackageLiabilityLimit != 20000 {
		t.Fatalf("single-package limit: %d", resp.Capacity.SinglePackageLiabilityLimit)
	}
	if resp.Progression.RemainingSuccessfulDeliveries != 7 {
		t.Fatalf("progression: %+v", resp.Progression)
	}
}

func TestShippingSettlementResponseDecodes(t *testing.T) {
	raw := []byte(`{"action":"deliver","carrier_payout":5200,"claim_paid":0,
	  "debt_created":0,"shipper_refund":0,
	  "contract":{"id":"shp_1","package_id":"pkg_1",
	    "shipper":{"kind":"station","id":"a"},"recipient":{"kind":"station","id":"b"},
	    "origin_base_id":"a","destination_base_id":"b","shipping_house_id":"h",
	    "visibility":"public","service_level":"standard","status":"delivered",
	    "posted_at":"2026-07-19T20:00:00Z","listing_expires_at":"2026-07-19T22:00:00Z",
	    "deadline_tick":1400,"base_reward":5000,"max_speed_bonus":1000,"service_fee":50,
	    "reward_escrow":0,"speed_bonus_escrow":0,"failure_debt":0,"reserved_exposure":0,
	    "policy_status":"released","insurable":true,"risk_band":"probationary","route_hops":3}}`)
	var resp ShippingSettlementResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.CarrierPayout != 5200 || resp.Contract.Status != "delivered" {
		t.Fatalf("settlement: payout=%d status=%q", resp.CarrierPayout, resp.Contract.Status)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/game/serverapi/ -run TestShipping -v`
Expected: FAIL — compile error, `ShippingContractResponse` (etc.) undefined.

- [ ] **Step 3: Write the structs**

Create `pkg/game/serverapi/responses_shipping.go`. All field names/types are taken verbatim from `server_docs/openapi.json` (`ShipmentContract`, `Shipping*Response`, `Carrier*`, `FreightDebt`, `ShipmentTrackingEvent`, `ShipmentActor`, `ShippingListing`):

```go
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
	CurrentTier                  string `json:"current_tier"`
	NextTier                     string `json:"next_tier,omitempty"`
	AtMaximumTier                bool   `json:"at_maximum_tier"`
	SuccessfulDeliveries         int    `json:"successful_deliveries"`
	RequiredSuccessfulDeliveries int    `json:"required_successful_deliveries"`
	RemainingSuccessfulDeliveries int   `json:"remaining_successful_deliveries"`
	DeliveredValue               int64  `json:"delivered_value"`
	RequiredDeliveredValue       int64  `json:"required_delivered_value"`
	RemainingDeliveredValue      int64  `json:"remaining_delivered_value"`
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
	ID          string         `json:"id"`
	ShipmentID  string         `json:"shipment_id"`
	PackageID   string         `json:"package_id"`
	Class       string         `json:"class"`
	Custodian   *ShipmentActor `json:"custodian,omitempty"`
	Fingerprint string         `json:"fingerprint"`
	ObservedAt  string         `json:"observed_at"`
	ObservedTick int64         `json:"observed_tick"`
	BaseID      string         `json:"base_id,omitempty"`
	POIID       string         `json:"poi_id,omitempty"`
	ReferenceID string         `json:"reference_id,omitempty"`
	SystemID    string         `json:"system_id,omitempty"`
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
	UpdatedDebts         []FreightDebt       `json:"updated_debts"`
	OutstandingDebts     []FreightDebt       `json:"outstanding_debts"`
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/game/serverapi/ -run TestShipping -v`
Expected: PASS (all four decode tests).

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/game/serverapi/... && \
git add pkg/game/serverapi/responses_shipping.go pkg/game/serverapi/responses_shipping_test.go && \
git commit -m "feat(shipping): serverapi structs for /shipping freight endpoint

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: `storeRawJSON` keys shipping responses by action (`client.go`)

**Files:**
- Modify: `pkg/game/client.go` (the `storeRawJSON` `switch resp.Type` → `case protocol.TypeOK` → `switch action` block, ~`:4234`)
- Test: `pkg/game/client_test.go` (or the existing `storeRawJSON` test file — grep `TestStoreRawJSON`; if none, add `client_shipping_test.go`)

**Interfaces:**
- Consumes: `storeRawJSON` (existing). Produces: shipping `type:ok` responses become retrievable via `Client.GetRawJSON("shipping_"+action)` — the key convention Task 3's methods + Sub-project B rely on.

**Context:** `storeRawJSON` derives a store key from `resp.Payload["action"]` for `type:ok` responses (that is how `get_system` → `"system"` works). Shipping responses carry `action` ∈ {list,get,accept,deliver,return,cancel,profile,track,pay_debt} (the discriminator). Store each under `"shipping_"+action` so keys never collide with other commands' keys.

- [ ] **Step 1: Write the failing test**

Grep first: `grep -rn "func TestStoreRawJSON\|storeRawJSON(" pkg/game/*_test.go`. Mirror the existing storeRawJSON test if present; otherwise create `pkg/game/client_shipping_test.go`:

```go
package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestStoreRawJSONShippingKeysByAction(t *testing.T) {
	c := &Client{latestRawJSON: map[string][]byte{}}
	c.storeRawJSON(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"action": "list",
			"total":  float64(1),
		},
	})
	if raw := c.GetRawJSON("shipping_list"); len(raw) == 0 {
		t.Fatalf("shipping_list not stored")
	}
	c.storeRawJSON(protocol.Response{
		Type:    protocol.TypeOK,
		Payload: map[string]any{"action": "profile", "debt_blocks_acceptance": false},
	})
	if raw := c.GetRawJSON("shipping_profile"); len(raw) == 0 {
		t.Fatalf("shipping_profile not stored")
	}
}
```

> **Implementer:** confirm the `Client` zero-value used here matches how existing storeRawJSON tests construct a client (the real struct may need `latestRawJSON` pre-initialised, which `storeRawJSON` may also lazily create — check the field init in `client.go` and mirror the existing test's construction exactly). Adjust construction, not the assertion.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/game/ -run TestStoreRawJSONShipping -v`
Expected: FAIL — `GetRawJSON("shipping_list")` returns empty (no case yet).

- [ ] **Step 3: Add the shipping cases**

In `storeRawJSON`, inside `case protocol.TypeOK:` → `switch action {`, add a grouped set of cases before `default`:

```go
		case "list", "get", "accept", "deliver", "return", "cancel", "profile", "track", "pay_debt", "quote", "post":
			// /shipping is action-dispatched; the reply's action is the verb.
			// Namespace the key so it can't collide with other commands' keys.
			// (quote/post included so shipper-side reads land somewhere too, even
			// though Sub-project A ships no quote/post client method yet.)
			storeKey = "shipping_" + action
			shouldStore = true
```

> **Implementer:** these action verbs are generic (`get`, `list`), so they MUST be added inside the shipping-namespacing logic, not as bare keys. If any of these verbs is already a `case` in this switch for a different command, do NOT reuse it — resolve by checking `resp.Payload` for a shipping-only marker (e.g. presence of `"contract"`/`"shipments"`/`"debt_blocks_acceptance"`) and only then key as `shipping_*`. Grep the existing `switch action` arms first; report if a collision exists.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/game/ -run TestStoreRawJSONShipping -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/client.go pkg/game/client_shipping_test.go && \
git commit -m "feat(shipping): store /shipping responses under shipping_<action> keys

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Client shipping methods + `GameClient` interface + mocks

**Files:**
- Modify: `pkg/game/client_commands.go` (add methods)
- Modify: `pkg/game/interface.go` (add to `GameClient`, new `// Shipping` group)
- Modify: mock `GameClient` implementations — grep `grep -rln "GameClient" pkg/agent pkg/skills` and any `mock*.go` that implement the interface
- Test: `pkg/game/client_commands_test.go` (or a shipping test file) — a light send-path test if the codebase has a loopback/fake transport; otherwise rely on `go test ./...` compile + the interface `var _ GameClient` assertion

**Interfaces:**
- Consumes: `Client.Submit`/`await` (existing pattern; see `GetActionLog` at `client_commands.go:922`), the `"shipping_"+action` store keys (Task 2), the Task 1 structs.
- Produces: `GameClient` methods `Shipping`, `ShippingList`, `ShippingGet`, `ShippingAccept`, `ShippingDeliver`, `ShippingReturn`, `ShippingCancel`, `ShippingTrack`, `ShippingProfile`, `ShippingPayDebt`. Consumed by Sub-project B and the Task 5 smoke.

- [ ] **Step 1: Add the low-level dispatcher + typed wrappers**

In `pkg/game/client_commands.go`, following the `GetActionLog` pattern exactly (build `protocol.Message`, `Submit(...WithAckOnly()...WithTimeout(SleepMedium))`, `await`, return error):

```go
// Shipping sends a /shipping action with the given payload (the action is
// injected). The reply is cached under "shipping_<action>" (storeRawJSON);
// read it with GetRawJSON and unmarshal into the matching serverapi struct.
func (c *Client) Shipping(ctx context.Context, action string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["action"] = action
	msg := protocol.Message{
		Type:      "shipping",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}

// ShippingList fetches the current station's freight board (docked-only). sort ∈
// {reward,distance,age} (empty = server default reward). Reply: shipping_list.
func (c *Client) ShippingList(ctx context.Context, sort string) error {
	p := map[string]any{}
	if sort != "" {
		p["sort"] = sort
	}
	return c.Shipping(ctx, "list", p)
}

// ShippingGet fetches one contract by id. Reply: shipping_get.
func (c *Client) ShippingGet(ctx context.Context, shipmentID string) error {
	return c.Shipping(ctx, "get", map[string]any{"shipment_id": shipmentID})
}

// ShippingAccept accepts a contract as the given carrier (player|faction). The
// package lands in the carrier's storage at origin. Reply: shipping_accept.
func (c *Client) ShippingAccept(ctx context.Context, shipmentID, carrier string) error {
	return c.Shipping(ctx, "accept", map[string]any{"shipment_id": shipmentID, "carrier": carrier})
}

// ShippingDeliver settles a delivered contract. Reply: shipping_deliver.
func (c *Client) ShippingDeliver(ctx context.Context, shipmentID string) error {
	return c.Shipping(ctx, "deliver", map[string]any{"shipment_id": shipmentID})
}

// ShippingReturn returns a contract to its origin (breach-avoidance escape
// hatch). Reply: shipping_return.
func (c *Client) ShippingReturn(ctx context.Context, shipmentID string) error {
	return c.Shipping(ctx, "return", map[string]any{"shipment_id": shipmentID})
}

// ShippingCancel cancels a contract (breach-avoidance escape hatch). Reply:
// shipping_cancel.
func (c *Client) ShippingCancel(ctx context.Context, shipmentID string) error {
	return c.Shipping(ctx, "cancel", map[string]any{"shipment_id": shipmentID})
}

// ShippingTrack fetches a contract plus its beacon custody events (limit ≤ 200;
// 0 = server default). Reply: shipping_track.
func (c *Client) ShippingTrack(ctx context.Context, shipmentID string, limit int) error {
	p := map[string]any{"shipment_id": shipmentID}
	if limit > 0 {
		p["limit"] = limit
	}
	return c.Shipping(ctx, "track", p)
}

// ShippingProfile fetches this actor's carrier standing, capacity, progression
// and debts. Reply: shipping_profile.
func (c *Client) ShippingProfile(ctx context.Context) error {
	return c.Shipping(ctx, "profile", nil)
}

// ShippingPayDebt pays freight debt (amount ≤ 0 pays the full balance). Reply:
// shipping_pay_debt.
func (c *Client) ShippingPayDebt(ctx context.Context, amount int64) error {
	p := map[string]any{}
	if amount > 0 {
		p["amount"] = amount
	}
	return c.Shipping(ctx, "pay_debt", p)
}
```

- [ ] **Step 2: Add to the `GameClient` interface**

In `pkg/game/interface.go`, add a `// Shipping` group with the exact signatures:

```go
	// Shipping (freight contracts)
	Shipping(ctx context.Context, action string, payload map[string]any) error
	ShippingList(ctx context.Context, sort string) error
	ShippingGet(ctx context.Context, shipmentID string) error
	ShippingAccept(ctx context.Context, shipmentID, carrier string) error
	ShippingDeliver(ctx context.Context, shipmentID string) error
	ShippingReturn(ctx context.Context, shipmentID string) error
	ShippingCancel(ctx context.Context, shipmentID string) error
	ShippingTrack(ctx context.Context, shipmentID string, limit int) error
	ShippingProfile(ctx context.Context) error
	ShippingPayDebt(ctx context.Context, amount int64) error
```

- [ ] **Step 3: Build — expect mock breakage — then fix every mock**

Run: `go build ./...` then `go test ./...`
Expected: build may pass but `go test ./...` FAILS to compile `pkg/agent` and/or `pkg/skills` (their `GameClient` mocks now miss 10 methods). Add the same 10 methods to each mock implementation (a no-op returning `nil`, or recording the call if the mock records calls — match the mock's existing style). Grep: `grep -rln "func.*GameClient\|mockGameClient\|type .*Client struct" pkg/agent pkg/skills`.

- [ ] **Step 4: Verify the interface is satisfied + suite green**

Run: `go test ./pkg/game/ ./pkg/agent/ ./pkg/skills/ ./...`
Expected: PASS (except the known pre-existing `pkg/game TestServerCommandsCoveredByClient` drift RED — verify it is the ONLY failure and is unrelated). Confirm `*Client` satisfies `GameClient` (existing `var _ GameClient = (*Client)(nil)` assertion compiles).

- [ ] **Step 5: Lint + commit**

```bash
golangci-lint run ./pkg/game/... ./pkg/agent/... ./pkg/skills/... && \
git add pkg/game/client_commands.go pkg/game/interface.go <the mock files you changed> && \
git commit -m "feat(shipping): GameClient methods for the /shipping carrier subset

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Bump `BuiltForAPIVersion`

**Files:**
- Modify: `pkg/version/checker.go` (`BuiltForAPIVersion`, currently `"v0.495.1"`)

**Interfaces:** none (constant bump).

- [ ] **Step 1: Confirm the live server version**

Run: `python3 -c "import json;print(json.load(open('server_docs/openapi.json'))['info']['x-gameserver-version'])"`
Expected: prints the current version (e.g. `v0.532.0` or later).

- [ ] **Step 2: Update the constant**

Set `BuiltForAPIVersion` in `pkg/version/checker.go` to that exact string. Update the adjacent comment to note the bump reason (shipping-carrier client added) and date (2026-07-19). Do NOT touch `VersionID` in `pkg/game/constants.go` (that is the client's own semver, unrelated).

- [ ] **Step 3: Build + commit**

```bash
go build ./... && \
git add pkg/version/checker.go && \
git commit -m "chore(version): bump BuiltForAPIVersion for shipping-carrier client

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Live `play_as` smoke — resolve Sub-project B's open questions (operator-run gate)

**Not a code task.** This is the hard gate the spec mandates before any carrier behavior (Sub-project B) or fleet exposure. Run against the LIVE server with a single agent, using the new client methods (or `play_as`'s raw passthrough). Record findings into the Sub-project B design doc.

**Files:**
- Modify (append findings): `docs/superpowers/specs/2026-07-19-shipping-carrier-design.md` (§ Open Questions → record answers)

- [ ] **Step 1: Pick a safe canary + a real contract**

Use one idle mission-runner NOT currently in the live fleet, via `go run ./cmd/tools/play_as <agent>` (see `feedback_play_as_go_run` — do NOT `play_as` a running worker; freeze/stop it first if it is supervised). Dock at a station, then `shipping list` (via a raw `shipping` command with `{"action":"list"}` or the typed method through a small harness).

- [ ] **Step 2: Answer each open question against live payloads**

Record verbatim server payloads for:
1. **Package cargo footprint** — `shipping accept` a small contract, then `view_storage` / `get_cargo`: does the package appear as an item with a quantity/volume? `withdraw_items` it into cargo and read the cargo delta. Determine whether a pre-accept size check is possible and where the size lives.
2. **`deliver` mechanic** — at the destination, does `shipping deliver` consume the package from cargo, or require a deposit into destination storage first? Record the exact precondition.
3. **`return`/`cancel` semantics** — accept a second test contract and, before transit, try `shipping return` then (separately) `shipping cancel`: which is the correct pre-transit release, and does either avoid `failure_debt`? Read `shipping profile.debt_blocks_acceptance` after.
4. **`accept` shape** — confirm `carrier:"player"` is accepted and the package lands in *personal* storage at `origin_base_id` (not faction).

- [ ] **Step 3: Record + gate**

Append the answers (with the raw payloads) to the design doc's Open Questions section, and note any struct-field drift discovered (feed back into Task 1 structs if a field name differs from openapi). **Sub-project B planning does not start until Steps 1-3 are done.** Commit the design-doc update:

```bash
git add docs/superpowers/specs/2026-07-19-shipping-carrier-design.md && \
git commit -m "docs(shipping): record live-smoke answers to carrier open questions

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Final Verification

- [ ] `go build ./...` — clean.
- [ ] `go test ./...` — green except the known pre-existing `pkg/game TestServerCommandsCoveredByClient` drift RED (unrelated).
- [ ] `golangci-lint run` — no new findings.
- [ ] Task 5 smoke answers recorded in the design doc → unblocks the Sub-project B plan (carrier gate + trip + telemetry).

## Self-Review Notes

- **Spec coverage:** Sub-project A of `2026-07-19-shipping-carrier-design.md` = client + serverapi structs + version bump (Tasks 1-4); the smoke gate (§ Open Questions) = Task 5. Sub-project B (carrier behavior) is intentionally a separate plan authored after Task 5.
- **Type consistency:** every struct field name/type is from the openapi extraction; `Shipping*Response` wrappers reference the exact object structs; client methods return `error` and responses are read via `GetRawJSON("shipping_"+action)`, matching `missionFetchActiveMissions`.
- **Field-name risk:** structs are spec-derived, not yet live-verified (same caveat as `responses_passthrough.go`); Task 5 is the first live contact and feeds any drift back into Task 1.
