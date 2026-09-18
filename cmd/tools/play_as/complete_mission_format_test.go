package main

import (
	"strings"
	"testing"
)

// Verbatim from craftsman-1's Crossing Borders completion, 2026-09-18 07:49.
// The server paid 2557 of an advertised 5500 and moved nebula standing by +3;
// the formatter rendered neither, showing only "+2557 cr".
const crossingBordersReply = `{
  "command":"complete_mission",
  "result":{
    "chain_next":"frontier_extension",
    "credits_earned":2557,
    "credits_promised":5500,
    "credits_shortfall":2943,
    "message":"Nova Terra and Alpha Centauri both confirm delivery.",
    "mission_id":"0b13e1168bc24caaae7c3434db4bd262",
    "reputation_changes":{"nebula":3},
    "skill_xp_gained":{"leadership":15,"navigation":20,"trading":30},
    "title":"Crossing Borders"
  },
  "tick":1915553
}`

func TestFormatCompleteMissionShowsShortfallAndReputation(t *testing.T) {
	got := formatCompleteMission([]byte(crossingBordersReply))

	for _, want := range []string{
		"Crossing Borders",
		"+2557 cr",
		"of 5500 promised",  // the advertised figure must be visible
		"2943 unpaid",       // and so must the gap
		"nebula",            // reputation movement
		"+3",
		"frontier_extension",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// A mission paid in full must not grow a shortfall line.
func TestFormatCompleteMissionFullPaymentIsQuiet(t *testing.T) {
	raw := `{"command":"complete_mission","result":{
	  "title":"First Links","credits_earned":4000,"credits_promised":4000,"credits_shortfall":0,
	  "reputation_changes":{},"skill_xp_gained":{"trading":25}}}`
	got := formatCompleteMission([]byte(raw))
	if strings.Contains(got, "promised") || strings.Contains(got, "unpaid") {
		t.Errorf("paid in full, should not mention a shortfall:\n%s", got)
	}
	if strings.Contains(got, "Reputation") {
		t.Errorf("empty reputation_changes should not print a header:\n%s", got)
	}
	if !strings.Contains(got, "+4000 cr") {
		t.Errorf("credits missing:\n%s", got)
	}
}

// Negative standing must render with its sign, not as a bare number.
func TestFormatCompleteMissionNegativeReputation(t *testing.T) {
	raw := `{"command":"complete_mission","result":{
	  "title":"Off the Books","credits_earned":100,
	  "reputation_changes":{"solarian":-4,"pirates":2}}}`
	got := formatCompleteMission([]byte(raw))
	for _, want := range []string{"solarian", "-4", "pirates", "+2"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
