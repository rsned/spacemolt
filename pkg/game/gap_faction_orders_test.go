package game

import "testing"

func TestGapFactionOrdersFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"faction_create_buy_order":  {"consolidated", "faction_id", "faction_tag", "item", "item_id", "listing_fee", "order_id", "price_each", "quantity", "total_escrowed"},
		"faction_create_sell_order": {"consolidated", "faction_id", "faction_tag", "item", "item_id", "listing_fee", "order_id", "price_each", "quantity"},
		"faction_post_mission":      {"escrowed", "status", "template_id", "title"},
		"faction_cancel_mission":    {"status"},
	})
}
