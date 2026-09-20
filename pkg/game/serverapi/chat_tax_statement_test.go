package serverapi

import (
	"encoding/json"
	"testing"
)

// The weekly tax bill arrives as a private chat_message from the Interstellar
// Revenue Service, and the prose body is only a summary: the machine-readable
// breakdown rides alongside it in tax_statement -- the same block
// get_tax_estimate returns as latest_statement. ChatMessage dropped it
// entirely, so a reader could see "391372 owed" but not which empire, which
// brackets, or which ships were assessed.
//
// Captured live 2026-09-20 (tick 1935360), trimmed to two ships.
func TestChatMessageDecodesTaxStatement(t *testing.T) {
	const payload = `{
		"channel": "private",
		"content": "Weekly personal tax statement. Taxable income: 8010214 credits ...",
		"empire_official": true,
		"sender": "Interstellar Revenue Service",
		"sender_id": "npc_authority_revenue",
		"tax_statement": {
			"assessed_at": "2026-09-20T21:52:17.943886991Z",
			"inactivity_exempt": false,
			"income": [{
				"brackets": [
					{"income_in_bracket": 450000, "lower_bound": 50000, "rate_bps": 200, "tax_from_bracket": 9000, "upper_bound": 500000},
					{"income_in_bracket": 7510214, "lower_bound": 500000, "rate_bps": 500, "tax_from_bracket": 375510}
				],
				"credit": 0,
				"empire": "nebula",
				"gross": 384510,
				"owed": 384510,
				"paid": 384510,
				"rate_bps": 480,
				"unpaid": 0
			}],
			"income_by_category": {"market": 8117463, "mission": 47557},
			"income_gross": 8165020,
			"market_deduction": 154806,
			"market_purchases": 154806,
			"paid_from_prepaid": 0,
			"paid_from_wallet": 391372,
			"preview": false,
			"property": [{"brackets": null, "empire": "nebula", "owed": 6862, "paid": 6862, "rate_bps": 25, "unpaid": 0}],
			"property_value": 2744883,
			"refund": 0,
			"ships": [
				{"ship_id": "e7b84425c5fbabe7196aafcdbaca63c5", "value": 284588},
				{"ship_id": "24eeda6909e3cfb6044a87b33f1b9ee3", "value": 1973}
			],
			"taxable_income": 8010214,
			"tick": 1935360,
			"total_owed": 391372,
			"total_paid": 391372,
			"total_unpaid": 0
		}
	}`

	var got ChatMessage
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	st := got.TaxStatement
	if st == nil {
		t.Fatal("TaxStatement is nil, want the decoded weekly statement")
	}
	if st.TotalOwed != 391372 || st.TotalPaid != 391372 || st.TotalUnpaid != 0 {
		t.Errorf("totals = owed %d paid %d unpaid %d, want 391372/391372/0",
			st.TotalOwed, st.TotalPaid, st.TotalUnpaid)
	}
	if st.TaxableIncome != 8010214 {
		t.Errorf("TaxableIncome = %d, want 8010214", st.TaxableIncome)
	}
	if st.MarketDeduction != 154806 {
		t.Errorf("MarketDeduction = %d, want 154806", st.MarketDeduction)
	}
	if st.PaidFromWallet != 391372 || st.PaidFromPrepaid != 0 {
		t.Errorf("paid from wallet/prepaid = %d/%d, want 391372/0",
			st.PaidFromWallet, st.PaidFromPrepaid)
	}
	if st.Tick != 1935360 {
		t.Errorf("Tick = %d, want 1935360", st.Tick)
	}
	if st.Preview {
		t.Error("Preview = true, want false on a settled statement")
	}
	if st.IncomeByCategory["market"] != 8117463 || st.IncomeByCategory["mission"] != 47557 {
		t.Errorf("IncomeByCategory = %v, want market 8117463 and mission 47557", st.IncomeByCategory)
	}

	if len(st.Income) != 1 {
		t.Fatalf("len(Income) = %d, want 1", len(st.Income))
	}
	inc := st.Income[0]
	if inc.Empire != "nebula" {
		t.Errorf("Income[0].Empire = %q, want %q", inc.Empire, "nebula")
	}
	if inc.Owed != 384510 || inc.Paid != 384510 {
		t.Errorf("Income[0] owed/paid = %d/%d, want 384510/384510", inc.Owed, inc.Paid)
	}
	if inc.RateBPS != 480 {
		t.Errorf("Income[0].RateBPS = %d, want 480 (the effective rate)", inc.RateBPS)
	}
	if len(inc.Brackets) != 2 {
		t.Fatalf("len(Income[0].Brackets) = %d, want 2", len(inc.Brackets))
	}
	// The top bracket is open-ended: upper_bound is absent, not zero.
	top := inc.Brackets[1]
	if top.LowerBound != 500000 || top.UpperBound != 0 {
		t.Errorf("top bracket bounds = %d..%d, want 500000..open", top.LowerBound, top.UpperBound)
	}
	if top.RateBPS != 500 || top.TaxFromBracket != 375510 {
		t.Errorf("top bracket = %d bps -> %d, want 500 -> 375510", top.RateBPS, top.TaxFromBracket)
	}

	if len(st.Property) != 1 || st.Property[0].Owed != 6862 {
		t.Errorf("Property = %+v, want one nebula line owing 6862", st.Property)
	}
	if st.PropertyValue != 2744883 {
		t.Errorf("PropertyValue = %d, want 2744883", st.PropertyValue)
	}
	if len(st.Ships) != 2 {
		t.Fatalf("len(Ships) = %d, want 2", len(st.Ships))
	}
	if st.Ships[0].ShipID != "e7b84425c5fbabe7196aafcdbaca63c5" || st.Ships[0].Value != 284588 {
		t.Errorf("Ships[0] = %+v, want the 284588-credit hull", st.Ships[0])
	}
}
