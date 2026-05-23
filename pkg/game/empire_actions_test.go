package game

import "testing"

func TestNewActionEmpireFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"citizenship": {
			"citizenship", "citizenships", "empire_id", "empires", "fee_paid",
			"fee_refunded", "message", "origin", "pending_petitions", "petition",
			"petition_id", "recent_decisions", "renounced", "rules", "status",
		},
		"get_empire_info": {"action", "empires"},
		"petition":        {"empire_id", "empire_name", "message"},
	})
}
