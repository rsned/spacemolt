package assets

import (
	"context"
	"testing"
	"time"
)

// TestCarrierFromGoldenPayload decodes a real shipping action=profile body.
// The tier-progress block is the part that matters most: remaining_* is what
// turns "still in probation" into "needs 3 more deliveries and 180 value".
func TestCarrierFromGoldenPayload(t *testing.T) {
	raw := []byte(`{"action":"profile","debt_blocks_acceptance":false,"debt_block_reason":"",
	 "profile":{"actor":{"kind":"player","id":"engineer-2"},"tier":"probationary",
	  "successful_deliveries":2,"delivered_value":70,"priority_deliveries":0,
	  "returns":0,"breaches":0,"defaults":0,"active_contracts":1,
	  "active_liability":500,"outstanding_debt":0,"updated_at":"2026-08-01T00:00:00Z"},
	 "capacity":{"active_contracts":1,"active_contracts_unlimited":false,
	  "active_contract_limit":3,"active_liability":500,"liability_unlimited":false,
	  "aggregate_liability_limit":10000,"remaining_aggregate_liability":9500,
	  "single_package_liability_limit":2000},
	 "progression":{"current_tier":"probationary","next_tier":"licensed",
	  "at_maximum_tier":false,"successful_deliveries":2,
	  "required_successful_deliveries":5,"remaining_successful_deliveries":3,
	  "delivered_value":70,"required_delivered_value":250,
	  "remaining_delivered_value":180}}`)

	c, ok, err := CarrierFrom(raw)
	if err != nil || !ok {
		t.Fatalf("CarrierFrom: ok=%v err=%v", ok, err)
	}
	if c.Tier != "probationary" || c.SuccessfulDeliveries != 2 {
		t.Errorf("tier=%q deliveries=%d", c.Tier, c.SuccessfulDeliveries)
	}
	if c.RemainingSuccessfulDeliveries != 3 || c.RemainingDeliveredValue != 180 {
		t.Errorf("remaining = %d deliveries / %d value, want 3 / 180",
			c.RemainingSuccessfulDeliveries, c.RemainingDeliveredValue)
	}
	if c.RemainingAggregateLiability != 9500 || c.ActiveContractLimit != 3 {
		t.Errorf("capacity = %d liability / %d contracts",
			c.RemainingAggregateLiability, c.ActiveContractLimit)
	}
	if c.DebtBlocksAcceptance {
		t.Error("debt_blocks_acceptance must be false")
	}
}

// TestCarrierFromEmptyIsNotAnError pins that an absent cache entry means
// "not captured", not a failure.
func TestCarrierFromEmptyIsNotAnError(t *testing.T) {
	c, ok, err := CarrierFrom(nil)
	if err != nil || ok {
		t.Fatalf("CarrierFrom(nil) = %+v, %v, %v; want zero, false, nil", c, ok, err)
	}
}

// TestUpsertCarrierRoundTrip pins persistence and that a re-capture updates
// rather than duplicating.
func TestUpsertCarrierRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	c := Carrier{Tier: "probationary", SuccessfulDeliveries: 2, RemainingSuccessfulDeliveries: 3}
	if err := st.UpsertCarrier(ctx, "abc123", c, now); err != nil {
		t.Fatalf("first: %v", err)
	}
	c.Tier = "licensed"
	c.RemainingSuccessfulDeliveries = 0
	if err := st.UpsertCarrier(ctx, "abc123", c, now.Add(time.Hour)); err != nil {
		t.Fatalf("second: %v", err)
	}

	var (
		n    int
		tier string
	)
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_carrier`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("agent_carrier rows = %d, want 1", n)
	}
	if err := st.DB().QueryRowContext(ctx,
		`SELECT tier FROM agent_carrier WHERE player_id = ?`, "abc123").Scan(&tier); err != nil {
		t.Fatalf("read: %v", err)
	}
	if tier != "licensed" {
		t.Errorf("tier = %q, want licensed", tier)
	}
}
