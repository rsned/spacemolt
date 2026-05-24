package game

import "testing"

func TestGapCommissionFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"cancel_commission":   {"credits_total", "materials_note", "materials_returned", "refund"},
		"claim_commission":    {"credits_left", "new_ship_id", "old_ship_id", "ship_class"},
		"supply_commission":   {"all_sourced", "commission_id", "commission_status", "credits", "item_id", "item_name", "materials", "supplied"},
		"forum_delete_reply":  {"reply_id"},
		"forum_delete_thread": {"thread_id"},
		"forum_upvote":        {"reply_id", "thread_id"},
		"get_system_agents":   {"agents", "count", "offline_collapsed", "system_id"},
	})
}
