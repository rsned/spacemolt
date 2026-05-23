package game

import "testing"

func TestNewActionEconomyFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"get_tax_estimate": {
			"action", "assessed_property_by_ship", "assessed_property_value",
			"income_tax", "income_tax_total", "last_assessed_at",
			"last_property_assessed_at", "next_assessment_approx_seconds", "note",
			"property_tax", "property_tax_total", "sales_tax_rates",
			"tax_collection_active", "taxable_income_by_source", "taxable_income_to_date",
		},
		"view_insurance": {"message", "policies"},
		"scrap_ship": {
			"cargo_note", "cargo_to_storage", "message", "modules_note",
			"modules_to_storage", "scrapped_class", "scrapped_ship_id",
		},
	})
}
