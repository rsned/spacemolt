package main

import (
	"strings"
	"testing"
)

const missionsWithFixer = `{"missions":[{"mission_id":"m1","type":"delivery","title":"Haul Ore","rewards":{"credits":500}}],
"fixer":{"allowance":2750,"available":1250,"debt":0,"exposure":1500}}`

func TestFormatMissions_ShowsFixer(t *testing.T) {
	got := formatMissions([]byte(missionsWithFixer))
	want := "Fixer: available 1,250 of 2,750 allowance · exposure 1,500 · debt 0"
	if !strings.Contains(got, want) {
		t.Fatalf("missing %q in:\n%s", want, got)
	}
}

func TestFormatMissions_EmptyStillShowsFixer(t *testing.T) {
	got := formatMissions([]byte(`{"missions":[],"base_name":"Sol","fixer":{"allowance":2500,"available":2500,"debt":0,"exposure":0}}`))
	if !strings.Contains(got, "No missions available at Sol") || !strings.Contains(got, "available 2,500 of 2,500") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestFormatActiveMissions_ShowsFixerDebt(t *testing.T) {
	got := formatActiveMissions([]byte(`{"missions":[],"fixer":{"allowance":2500,"available":0,"debt":3000,"exposure":3000}}`))
	for _, want := range []string{"debt 3,000", "⚠ DEBT", `recipient="fixer:tab" credits=3000`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatCompleteMission_ShowsFixer(t *testing.T) {
	raw := `{"command":"complete_mission","result":{"title":"Survey Equipment","credits_earned":5000,
"fixer":{"allowance":2500,"available":2500,"debt":0,"exposure":0},"reputation_changes":{"outerrim":2}}}`
	got := formatCompleteMission([]byte(raw))
	if !strings.Contains(got, "Fixer: available 2,500 of 2,500 allowance") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestFormatFixer_NoAllowanceWarns(t *testing.T) {
	got := formatFixer(&fixerAccount{Allowance: 2500, Available: 0, Exposure: 2500})
	if !strings.Contains(got, "no allowance left") {
		t.Fatalf("got %q", got)
	}
	if formatFixer(nil) != "" {
		t.Fatal("nil account must render nothing")
	}
}
