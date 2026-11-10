package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const completeWithUnlocks = `{"command":"complete_mission","result":{"title":"Processing the Data","credits_earned":1000,
"unlocks":[
{"available":true,"mission":{"mission_id":"m_ready","title":"Ready One","base_id":"b","base_name":"First Step Memorial Station","system_id":"first_step","system_name":"First Step"}},
{"available":false,"mission":{"mission_id":"joining_the_investigators","title":"Joining the Investigators","base_name":"First Step Memorial Station","system_name":"First Step"},
 "still_requires":[{"mission":{"mission_id":"the_listening_foundry","title":"The Listening Foundry"},"next_step":{"mission_id":"signal_triage","title":"Signal Triage","base_name":"B One","system_name":"S One"}}]},
{"available":false,"mission":{"mission_id":"m_skill","title":"Skill Gate"}}]}}`

func TestFormatCompleteMission_ShowsUnlocks(t *testing.T) {
	got := formatCompleteMission([]byte(completeWithUnlocks))
	for _, want := range []string{
		"Unlocks:",
		"✓ Ready One (m_ready) — available at First Step Memorial Station, First Step",
		"✗ Joining the Investigators (joining_the_investigators) at First Step Memorial Station, First Step",
		"needs The Listening Foundry (the_listening_foundry) → next: Signal Triage (signal_triage) at B One, S One",
		"✗ Skill Gate (m_skill) — non-mission requirement unmet (accept_mission names it)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatMissions_ShowsLockedHere(t *testing.T) {
	raw := `{"base_name":"First Step Memorial Station","missions":[{"mission_id":"x","type":"delivery","title":"X"}],
"locked_missions":[{"still_requires":[{"mission":{"mission_id":"a","title":"A"},"next_step":{"mission_id":"a","title":"A"}}]}]}`
	got := formatMissions([]byte(raw))
	for _, want := range []string{"Locked here (1):", "needs A (a)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// next_step equal to the prerequisite itself adds nothing; don't repeat it.
	if strings.Contains(got, "→ next: A (a)") {
		t.Errorf("redundant next step in:\n%s", got)
	}
}

func TestCaptureMissionCompletion_KeepsUnlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	if err := captureMissionCompletion(path, "craftsman-1", []byte(completeWithUnlocks)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"unlocks":[`) || !strings.Contains(string(b), "joining_the_investigators") {
		t.Fatalf("ledger dropped unlocks: %s", b)
	}
}

func TestFormatMissions_ShowsChainNext(t *testing.T) {
	raw := `{"missions":[{"chain_next":"parallel_installations","difficulty":6,"mission_id":"observation_points",
"template_id":"observation_points","title":"Observation Points","type":"exploration","rewards":{"credits":10000}}]}`
	got := formatMissions([]byte(raw))
	if !strings.Contains(got, "Chain next:  parallel_installations") {
		t.Fatalf("missing chain_next in:\n%s", got)
	}
}
