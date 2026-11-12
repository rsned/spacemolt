package main

import (
	"strings"
	"testing"
)

// v0.695.0: rewards.piloting_xp is shown on offered and active missions.
func TestMissionViewsShowPilotingXP(t *testing.T) {
	offered := `{"missions":[{"mission_id":"m1","type":"delivery","title":"Run","rewards":{"credits":500,"piloting_xp":42}}]}`
	if got := formatMissions([]byte(offered)); !strings.Contains(got, "piloting:") || !strings.Contains(got, "42 xp") {
		t.Errorf("get_missions lacks piloting xp:\n%s", got)
	}
	active := `{"missions":[{"mission_id":"m1","type":"delivery","title":"Run","rewards":{"credits":500,"piloting_xp":42}}]}`
	if got := formatActiveMissions([]byte(active)); !strings.Contains(got, "piloting:") || !strings.Contains(got, "42 xp") {
		t.Errorf("active missions lack piloting xp:\n%s", got)
	}
}
