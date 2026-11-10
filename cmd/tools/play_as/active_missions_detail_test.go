package main

import (
	"strings"
	"testing"
)

// craftsman-1's live get_active_missions reply, 2026-11-10, plus the
// in_cargo/in_storage counts the server sends on deliver objectives.
const activeChartsReply = `{"fixer":{"allowance":2500,"available":2500,"debt":0,"exposure":0},"max_missions":5,"missions":[{"accepted_at":"2026-11-10T18:34:27Z","description":"Collect historical survey charts from Alpha Centauri and deliver calibration circuits to Sirius Observatory for the Records Division's galactic atlas project.","difficulty":3,"expires_in_ticks":60471,"giver":{"name":"Archivist Yuen","title":"Sol Central Records Division"},"issuing_base":"Confederacy Central Command","issuing_base_id":"confederacy_central_command","issuing_system_id":"sol","issuing_system_name":"Sol","mission_id":"696fd332dd0fcded2db177a70095d957","objectives":[{"completed":false,"current":0,"description":"Collect historical survey charts at Alpha Centauri","required":1,"system_id":"alpha_centauri","system_name":"Alpha Centauri","target_base":"alpha_centauri_colonial_station","target_base_name":"Alpha Centauri Colonial Station","type":"dock_at_base"},{"completed":false,"current":0,"description":"Deliver calibration circuits to Sirius Observatory","item_id":"circuit_board","item_name":"Circuit Board","in_cargo":2,"in_storage":40,"required":5,"system_id":"sirius","system_name":"Sirius","target_base":"sirius_observatory_station","target_base_name":"Sirius Observatory Station","type":"deliver_item"}],"percent_complete":0,"rewards":{"credits":3500,"reputation":2,"skill_xp":{"exploration":45}},"template_id":"gathering_the_old_charts","title":"Gathering the Old Charts","type":"exploration"}],"total_count":1}`

func TestFormatActiveMissions_Details(t *testing.T) {
	got := formatActiveMissions([]byte(activeChartsReply))
	for _, want := range []string{
		"Giver:       Archivist Yuen — Sol Central Records Division",
		"Issued at:   Confederacy Central Command (Sol)",
		"☐ [dock_at_base] Collect historical survey charts at Alpha Centauri [0/1]",
		"→ Alpha Centauri Colonial Station, Alpha Centauri",
		"☐ [deliver_item] Deliver calibration circuits to Sirius Observatory (circuit_board) [0/5]",
		"→ Sirius Observatory Station, Sirius · have 2 in cargo, 40 in storage",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatActiveMissions_SequentialCommunityRestitution(t *testing.T) {
	raw := `{"missions":[{"mission_id":"m1","title":"Pay Back","type":"delivery","difficulty":1,
"sequential_objectives":true,"restitution_relief":1500,
"community":true,"community_percent":42.6,"community_progress":{"iron_ore":"425/1000"},
"objectives":[{"description":"Do A","type":"dock_at_base","required":1}]}]}`
	got := formatActiveMissions([]byte(raw))
	for _, want := range []string{
		"Objectives (in order):",
		"Restitution: up to 1,500 cr debt relief",
		"Community:   43% — iron_ore 425/1000",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
