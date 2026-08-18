package assets

import (
	"context"
	"testing"
	"time"
)

// realTaxReply mirrors a get_tax_estimate reply observed on 2026-08-18,
// reconstructed from the rendered output plus the field names in
// serverapi.GetTaxEstimateResponse. assessed_property_by_ship is a LIST of
// {ship_id, value} — that is the shape play_as decodes and the shape the live
// reply rendered from. The two ship values sum to assessed_property_value,
// which is the invariant that makes the per-ship table trustworthy.
const realTaxReply = `{
  "action": "get_tax_estimate",
  "assessed_property_by_ship": [
    {"ship_id": "c63763d53539dd8cdde94211d64916d9", "value": 28705},
    {"ship_id": "f7827df3e87c7b26df365bd227de62ee", "value": 46928}
  ],
  "assessed_property_value": 75633,
  "property_tax_total": 378,
  "income_tax_total": 0,
  "taxable_income_to_date": 0,
  "market_sales_to_date": 0,
  "market_cost_of_goods_deducted": 0,
  "taxable_market_income": 0,
  "tax_prepaid": 0,
  "next_assessment_approx_seconds": 604800,
  "tax_collection_active": true,
  "sales_tax_rates": [{"empire":"solarian","rate_bps":100,"reason":"citizen"},{"empire":"crimson","rate_bps":400,"reason":"foreign-lowest"}]
}`

func TestTaxEstimateFrom_RealReply(t *testing.T) {
	got, ok, err := TaxEstimateFrom([]byte(realTaxReply))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if got.AssessedPropertyValue != 75633 || got.PropertyTaxTotal != 378 {
		t.Errorf("totals = %d/%d", got.AssessedPropertyValue, got.PropertyTaxTotal)
	}
	if !got.CollectionActive {
		t.Error("collection_active lost")
	}
	if got.NextAssessmentSeconds != 604800 {
		t.Errorf("next assessment = %ds, want a week", got.NextAssessmentSeconds)
	}
	if len(got.Ships) != 2 {
		t.Fatalf("ships = %+v", got.Ships)
	}
	// Highest value first: the question this answers is which hull to move.
	if got.Ships[0].Value != 46928 || got.Ships[1].Value != 28705 {
		t.Errorf("ship order = %+v, want descending by value", got.Ships)
	}
	var sum int64
	for _, s := range got.Ships {
		sum += s.Value
	}
	if sum != got.AssessedPropertyValue {
		t.Errorf("ship values sum to %d, assessed %d — the per-ship table would not reconcile",
			sum, got.AssessedPropertyValue)
	}
	if got.SalesTaxRates == "" {
		t.Error("sales tax rates dropped; they are the only record of the foreign/citizen spread")
	}
}

// TestTaxEstimateFrom_EmptyIsNotZeroOwed is the distinction that matters: an
// absent reply must not be recorded as "owes nothing".
func TestTaxEstimateFrom_EmptyIsNotZeroOwed(t *testing.T) {
	_, ok, err := TaxEstimateFrom(nil)
	if err != nil || ok {
		t.Errorf("ok=%v err=%v, want a silent not-ok", ok, err)
	}
}

// TestTaxShipsFrom_AcceptsMapShape: the live shape is a list (see realTaxReply);
// a keyed map is also accepted, and an unrecognised shape must cost the ship
// breakdown only, never the scalar totals.
func TestTaxShipsFrom_AcceptsMapShape(t *testing.T) {
	got, ok, err := TaxEstimateFrom([]byte(`{
		"assessed_property_value": 300, "property_tax_total": 3,
		"assessed_property_by_ship": {"a":200,"b":100}}`))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(got.Ships) != 2 || got.Ships[0].ShipID != "a" {
		t.Errorf("ships = %+v", got.Ships)
	}

	// An unrecognised shape keeps the totals and drops only the breakdown.
	odd, ok, err := TaxEstimateFrom([]byte(`{"assessed_property_value": 300,
		"property_tax_total": 3, "assessed_property_by_ship": "surprise"}`))
	if err != nil || !ok {
		t.Fatalf("odd shape: ok=%v err=%v", ok, err)
	}
	if odd.AssessedPropertyValue != 300 || odd.PropertyTaxTotal != 3 {
		t.Errorf("totals lost to an unknown per-ship shape: %+v", odd)
	}
	if len(odd.Ships) != 0 {
		t.Errorf("invented ships from an unknown shape: %+v", odd.Ships)
	}
}

// TestTaxShortfalls_FindsAgentsThatCannotPay is what the capture exists for.
func TestTaxShortfalls_FindsAgentsThatCannotPay(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Now()

	type row struct {
		player, agent, empire string
		credits               float64
		property, income      int64
	}
	for _, r := range []row{
		{"p1", "explorer-7", "crimson", 0, 2256, 0},     // cannot pay any of it
		{"p2", "hauler-3", "outerrim", 50000, 120, 300}, // comfortable
		{"p3", "miner-3", "solarian", 40, 78, 0},        // just short
	} {
		if err := st.UpsertIdentity(ctx, Identity{PlayerID: r.player, AgentID: r.agent}, now); err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertProfile(ctx, Profile{
			PlayerID: r.player, Empire: r.empire, Credits: r.credits, CapturedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertTax(ctx, r.player, TaxEstimate{
			PropertyTaxTotal: r.property, IncomeTaxTotal: r.income,
		}, now); err != nil {
			t.Fatal(err)
		}
	}

	got, err := st.TaxShortfalls(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("shortfalls = %+v, want the two that cannot pay", got)
	}
	if got[0].AgentID != "explorer-7" || got[0].Shortfall != 2256 {
		t.Errorf("worst = %+v, want explorer-7 short by its whole levy", got[0])
	}
	if got[1].AgentID != "miner-3" || got[1].Shortfall != 38 {
		t.Errorf("second = %+v, want miner-3 short by 38", got[1])
	}
}
