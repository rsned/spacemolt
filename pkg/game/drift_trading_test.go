package game

import "testing"

func TestDriftTradingFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"estimate_purchase": {"sales_tax", "sales_tax_rate_bps", "subtotal"},
		"sell":              {"smuggling_level_up", "smuggling_xp"},
		"view_orders":       {"item_filter", "order_type", "search_term"},
		"buy_listed_ship":   {"old_ship_id"},
		"accept_mission":    {"expires_at", "template_id", "type"},
	})
}
