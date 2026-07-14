package game

import (
	"encoding/json"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// Live payloads captured 2026-07-14 from the running server (v0.495.x). Both
// tripped the API monitor: get_faction_tax_estimate had no response struct at
// all, and get_tax_estimate had grown five market-profit fields the struct
// silently dropped. These samples pin both.
const (
	liveFactionTaxEstimate = `{"action":"get_faction_tax_estimate","deductible_expenses_to_date":0,` +
		`"domicile":"nebula","faction_id":"e727c0e918d994c72db2978fe5b18edc",` +
		`"faction_name":"Crafting Collective","income_tax":[],"income_tax_total":0,` +
		`"last_assessed_at":1783877320,"loss_carryforward_applied":789070,"net_taxable_profit":0,` +
		`"next_assessment_approx_seconds":604800,"note":"No faction tax assessed.",` +
		`"tax_collection_active":true,"tax_prepaid":0,"taxable_income_to_date":0}`

	liveTaxEstimate = `{"action":"get_tax_estimate",` +
		`"assessed_property_by_ship":[{"ship_id":"96d7bcdb122adda656f96678fae2cd46","value":4300}],` +
		`"assessed_property_value":2446177,"income_tax":[],"income_tax_total":0,` +
		`"last_assessed_at":1783877320,"last_property_assessed_at":1783877320,` +
		`"market_cost_of_goods_deducted":55100,"market_loss_carryforward":552384,` +
		`"market_sales_to_date":55100,"next_assessment_approx_seconds":604800,` +
		`"property_tax":[{"assessed_value":2446177,"empire":"nebula","owed":6115,"rate_bps":25}],` +
		`"property_tax_total":6115,` +
		`"sales_tax_rates":[{"empire":"nebula","rate_bps":50,"reason":"citizen"}],` +
		`"tax_collection_active":true,"tax_prepaid":0,` +
		`"taxable_income_by_source":[{"amount":55100,"category":"market"}],` +
		`"taxable_income_to_date":0,"taxable_market_income":0}`
)

// Every key the server actually sends must be covered by the response struct's
// JSON tags. An uncovered key is exactly what the API monitor flags — and,
// worse, it decodes silently to the zero value rather than erroring.
func TestTaxEstimateStructsCoverLivePayloads(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		action string
		sample string
	}{
		{"get_tax_estimate", liveTaxEstimate},
		{"get_faction_tax_estimate", liveFactionTaxEstimate},
	} {
		t.Run(tc.action, func(t *testing.T) {
			t.Parallel()

			expected, known := expectedFieldsForAction(tc.action)
			if !known {
				t.Fatalf("%s has no entry in actionResponseTypes", tc.action)
			}

			var payload map[string]any
			if err := json.Unmarshal([]byte(tc.sample), &payload); err != nil {
				t.Fatalf("unmarshal sample: %v", err)
			}

			for key := range payload {
				if !expected[key] {
					t.Errorf("live field %q is not covered by the %s response struct", key, tc.action)
				}
			}
		})
	}
}

// The market-profit fields must actually bind, not merely be tolerated: taxable
// market income is sales minus cost of goods, so a dropped field would misstate
// the liability as zero.
func TestGetTaxEstimateDecodesMarketFields(t *testing.T) {
	t.Parallel()

	var resp serverapi.GetTaxEstimateResponse
	if err := json.Unmarshal([]byte(liveTaxEstimate), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.MarketSalesToDate != 55100 {
		t.Errorf("MarketSalesToDate = %d, want 55100", resp.MarketSalesToDate)
	}
	if resp.MarketCostOfGoodsDeducted != 55100 {
		t.Errorf("MarketCostOfGoodsDeducted = %d, want 55100", resp.MarketCostOfGoodsDeducted)
	}
	if resp.MarketLossCarryforward != 552384 {
		t.Errorf("MarketLossCarryforward = %d, want 552384", resp.MarketLossCarryforward)
	}
}
