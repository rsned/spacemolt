package main

import (
	"strings"
	"testing"
)

// realTaxEstimate is a get_tax_estimate reply captured live on 2026-08-18, with
// the market-profit fields and a per-empire property_tax breakdown added from
// serverapi.GetTaxEstimateResponse — the seven fields the previous formatter
// silently dropped.
const realTaxEstimate = `{
  "action": "get_tax_estimate",
  "assessed_property_by_ship": [
    {"ship_id": "c63763d53539dd8cdde94211d64916d9", "value": 28705},
    {"ship_id": "f7827df3e87c7b26df365bd227de62ee", "value": 46928}
  ],
  "assessed_property_value": 75633,
  "property_tax_total": 378,
  "property_tax": [{"empire": "solarian", "value": 75633, "rate_bps": 50, "owed": 378, "paid": 378, "unpaid": 0}],
  "income_tax_total": 0,
  "taxable_income_to_date": 0,
  "taxable_income_by_source": [],
  "market_sales_to_date": 412000,
  "market_cost_of_goods_deducted": 390000,
  "market_loss_carryforward": 12000,
  "taxable_market_income": 10000,
  "tax_prepaid": 250,
  "last_assessed_at": 1786910902,
  "next_assessment_approx_seconds": 604800,
  "tax_collection_active": true,
  "sales_tax_rates": [
    {"empire": "solarian", "rate_bps": 100, "reason": "citizen"},
    {"empire": "crimson", "rate_bps": 400, "reason": "foreign-lowest"}
  ]
}`

// TestFormatGetTaxEstimate_ShowsTheFieldsTheOldFormatterDropped is the
// regression. The previous version declared a private mirror of the wire struct
// and omitted the entire market profit basis, tax_prepaid, and the per-empire
// breakdowns — which for a trading agent is the whole reason the bill is what it
// is.
func TestFormatGetTaxEstimate_ShowsTheFieldsTheOldFormatterDropped(t *testing.T) {
	got := formatGetTaxEstimate([]byte(realTaxEstimate))
	if got == "" {
		t.Fatal("formatter returned nothing")
	}
	for _, want := range []string{
		"412000", // market_sales_to_date
		"390000", // market_cost_of_goods_deducted
		"12000",  // market_loss_carryforward
		"10000",  // taxable_market_income
		"250",    // tax_prepaid
		"Property tax by empire",
		"0.50%",                // property_tax rate_bps rendered as a percent
		"2026-08-16 20:08 UTC", // last_assessed_at — the levy seen in the action log
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q:\n%s", want, got)
		}
	}
	// The pre-existing sections must survive the rewrite.
	for _, want := range []string{"Tax Estimate (ACTIVE)", "75633", "378",
		"Sales tax rates", "citizen *", "~7d 0h", "c63763d53539dd8cdde94211d64916d9"} {
		if !strings.Contains(got, want) {
			t.Errorf("regression: lost %q:\n%s", want, got)
		}
	}
}

// realFactionTaxEstimate is the get_faction_tax_estimate reply captured live on
// 2026-08-18. It had NO formatter at all before this, so it printed as raw JSON.
const realFactionTaxEstimate = `{
  "action": "get_faction_tax_estimate",
  "deductible_expenses_to_date": 177424,
  "domicile": "solarian",
  "faction_id": "5b565d6303a0bea2eaf559129c1686f5",
  "faction_name": "Data Bots",
  "income_tax": [],
  "income_tax_total": 0,
  "last_assessed_at": 1786910902,
  "loss_carryforward_applied": 972924,
  "net_taxable_profit": 0,
  "next_assessment_approx_seconds": 604800,
  "note": "No faction tax assessed: no taxable profit has accrued since the last cycle and no debt is carried forward.",
  "tax_collection_active": true,
  "tax_prepaid": 0,
  "taxable_income_to_date": 0
}`

func TestFormatFactionTaxEstimate(t *testing.T) {
	got := formatFactionTaxEstimate([]byte(realFactionTaxEstimate))
	for _, want := range []string{
		"Faction Tax Estimate (ACTIVE)",
		"Data Bots",
		"5b565d6303a0bea2eaf559129c1686f5",
		"solarian",
		"177424", // deductible expenses
		"972924", // loss carryforward — nearly 1M, and the reason nothing is owed
		"No faction tax assessed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output is missing %q:\n%s", want, got)
		}
	}
	// An empty income_tax list must not print an empty table.
	if strings.Contains(got, "Income tax by empire") {
		t.Errorf("rendered an empty breakdown table:\n%s", got)
	}
}

// TestFormatTaxBreakdown_UnknownShapeIsShownNotDropped: the row shape is
// undocumented, and silently dropping it is the exact failure being fixed.
func TestFormatTaxBreakdown_UnknownShapeIsShownNotDropped(t *testing.T) {
	got := formatTaxBreakdown("Income tax by empire", []byte(`{"solarian": 42}`))
	if !strings.Contains(got, "42") {
		t.Errorf("an unrecognised breakdown shape was dropped: %q", got)
	}
	if out := formatTaxBreakdown("Income tax by empire", []byte(`[]`)); out != "" {
		t.Errorf("empty breakdown should render nothing, got %q", out)
	}
}
