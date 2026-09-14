package assets

import (
	"context"
	"testing"
	"time"
)

// realTaxReplyV605 is the get_tax_estimate reply captured live from explorer-8
// on 2026-09-14, the day v0.605.0 landed. The agent held ~136k credits and was
// carrying a 9,267-credit crimson bounty anyway — comfortably able to pay, and
// in debt regardless. That is the case that motivated capturing this at all,
// since nothing in the scalar totals hints at it. Settled the same day with a
// bare `pay_bounty`, which took the whole 9,267 and restored standing to 20.
const realTaxReplyV605 = `{
  "action": "get_tax_estimate",
  "assessed_property_value": 829935,
  "property_tax_total": 8299,
  "income_tax_total": 0,
  "taxable_income_to_date": 0,
  "market_sales_to_date": 0,
  "market_cost_of_goods_deducted": 0,
  "market_loss_carryforward": 434933,
  "taxable_market_income": 0,
  "tax_prepaid": 0,
  "next_assessment_approx_seconds": 604800,
  "tax_collection_active": true,
  "inactivity_exempt": false,
  "outstanding_bounties": [{"empire": "crimson", "bounty": 9267}]
}`

func TestTaxEstimateFrom_CapturesOutstandingBounties(t *testing.T) {
	got, ok, err := TaxEstimateFrom([]byte(realTaxReplyV605))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if got.OutstandingBountyTotal != 9267 {
		t.Errorf("OutstandingBountyTotal = %d, want 9267", got.OutstandingBountyTotal)
	}
	if len(got.Bounties) != 1 || got.Bounties[0].Empire != "crimson" ||
		got.Bounties[0].Bounty != 9267 {
		t.Errorf("Bounties = %+v", got.Bounties)
	}
	if got.InactivityExempt {
		t.Error("InactivityExempt = true for an active agent")
	}
}

// A pre-v0.605 reply has no bounty fields at all. It must read as "no debt
// observed", not as a decode failure that discards the whole capture.
func TestTaxEstimateFrom_PreV605ReplyStillParses(t *testing.T) {
	got, ok, err := TaxEstimateFrom([]byte(realTaxReply))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if got.OutstandingBountyTotal != 0 || len(got.Bounties) != 0 {
		t.Errorf("invented debt from a reply that has none: %+v", got)
	}
}

func TestTaxEstimateFrom_InactivityExempt(t *testing.T) {
	raw := `{"action":"get_tax_estimate","inactivity_exempt":true,"tax_collection_active":true}`
	got, ok, err := TaxEstimateFrom([]byte(raw))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !got.InactivityExempt {
		t.Error("InactivityExempt = false")
	}
}

// TestTaxBountiesRoundTrip: the per-empire rows must behave like agent_tax_ships
// — a replace-set, so that paying a bounty makes the row GO AWAY. A bounty that
// lingers after payment is worse than not capturing it, because it would send us
// chasing debts that are already settled.
func TestTaxBountiesRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)

	t.Cleanup(func() {})
	if err := st.ReplaceTaxBounties(ctx, "p1", []TaxBounty{
		{Empire: "crimson", Bounty: 9267},
		{Empire: "solarian", Bounty: 400},
	}, now); err != nil {
		t.Fatalf("ReplaceTaxBounties: %v", err)
	}
	rows, err := st.TaxBounties(ctx, "p1")
	if err != nil {
		t.Fatalf("TaxBounties: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rows), rows)
	}

	// Crimson paid off; only solarian remains outstanding.
	if err := st.ReplaceTaxBounties(ctx, "p1", []TaxBounty{
		{Empire: "solarian", Bounty: 400},
	}, now); err != nil {
		t.Fatalf("ReplaceTaxBounties (after payment): %v", err)
	}
	rows, err = st.TaxBounties(ctx, "p1")
	if err != nil {
		t.Fatalf("TaxBounties: %v", err)
	}
	if len(rows) != 1 || rows[0].Empire != "solarian" {
		t.Errorf("a paid bounty survived the recapture: %+v", rows)
	}
}

// TestTaxShortfalls_CountsOutstandingDebt is the behaviour change that makes the
// capture worth scheduling. An agent whose NEXT levy is affordable looked fine
// to the old query even while carrying debt from an earlier cycle. Debt already
// owed is what gets an agent detained, so it belongs in the total due.
func TestTaxShortfalls_CountsOutstandingDebt(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)

	if err := st.UpsertIdentity(ctx, Identity{PlayerID: "p1", AgentID: "broke-1"}, now); err != nil {
		t.Fatalf("UpsertIdentity: %v", err)
	}
	if err := st.UpsertProfile(ctx, Profile{PlayerID: "p1", Empire: "crimson", Credits: 500, CapturedAt: now}); err != nil {
		t.Fatalf("UpsertProfile: %v", err)
	}
	// Next levy is 100 — comfortably affordable. The 9,267 already owed is not.
	if err := st.UpsertTax(ctx, "p1", TaxEstimate{
		PropertyTaxTotal:       100,
		OutstandingBountyTotal: 9267,
	}, now); err != nil {
		t.Fatalf("UpsertTax: %v", err)
	}

	got, err := st.TaxShortfalls(ctx)
	if err != nil {
		t.Fatalf("TaxShortfalls: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("an agent with 9267 of unpayable debt was not reported: %+v", got)
	}
	if got[0].OutstandingBounty != 9267 {
		t.Errorf("OutstandingBounty = %d, want 9267", got[0].OutstandingBounty)
	}
	if got[0].TotalDue != 9367 {
		t.Errorf("TotalDue = %d, want 9367 (levy + debt)", got[0].TotalDue)
	}
	if got[0].Shortfall != 8867 {
		t.Errorf("Shortfall = %d, want 8867", got[0].Shortfall)
	}
}
