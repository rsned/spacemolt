package main

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}

	return t
}

// TestHighestRungWinsOverRecordCount is the trap this report exists to avoid.
// mission_results does not reach back forever, so an agent that cleared
// an_introduction before the table was capturing has a row for that rung and
// nothing before it. Counting rows would call it "1 of 3, next across_the_line"
// and send an already-unlocked agent back to the giver for work it finished.
func TestHighestRungWinsOverRecordCount(t *testing.T) {
	got := Compute([]string{"miner-1"}, []Completion{
		{AgentID: "miner-1", TemplateID: "an_introduction", FinishedAt: at("2026-08-11T22:53:23Z")},
	})
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	if got[0].Step != 3 {
		t.Errorf("Step = %d; one record of the LAST rung still means all three are behind it", got[0].Step)
	}
	if !got[0].Unlocked {
		t.Error("an_introduction is the rung that grants the unlock; agent reported as still locked")
	}
	if got[0].Next != "" {
		t.Errorf("Next = %q; an unlocked agent is owed nothing further", got[0].Next)
	}
}

// TestPartwayAgentIsToldTheNextRung — the actionable half of the report.
func TestPartwayAgentIsToldTheNextRung(t *testing.T) {
	got := Compute([]string{"prophet-2"}, []Completion{
		{AgentID: "prophet-2", TemplateID: "no_questions_asked", FinishedAt: at("2026-08-13T18:00:00Z")},
		{AgentID: "prophet-2", TemplateID: "across_the_line", FinishedAt: at("2026-08-13T21:11:23Z")},
	})
	p := got[0]
	if p.Step != 2 || p.Last != "across_the_line" {
		t.Errorf("Step/Last = %d/%q, want 2/across_the_line", p.Step, p.Last)
	}
	if p.Next != "an_introduction" {
		t.Errorf("Next = %q, want an_introduction — this is the rung that grants the unlock", p.Next)
	}
	if p.Unlocked {
		t.Error("reported unlocked one rung early")
	}
	if !p.LastAt.Equal(at("2026-08-13T21:11:23Z")) {
		t.Errorf("LastAt = %v; want the time of the HIGHEST rung, not the first", p.LastAt)
	}
}

// TestAgentWithNoCompletionsStillAppears. "Has not started" is the most useful
// line in the report; dropping the row would silently hide every idle agent and
// make an untouched pool look complete.
func TestAgentWithNoCompletionsStillAppears(t *testing.T) {
	got := Compute([]string{"pirate-3"}, nil)
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1 — an agent with no progress must still be listed", len(got))
	}
	if got[0].Step != 0 || got[0].Next != "no_questions_asked" {
		t.Errorf("got Step=%d Next=%q, want 0/no_questions_asked", got[0].Step, got[0].Next)
	}
	if got[0].Unlocked {
		t.Error("an agent with no completions cannot be unlocked")
	}
}

// TestOutsidersAndUnrelatedMissionsAreIgnored. The pool runs ordinary courier
// and delivery work alongside the chain, and other fleets share the table.
func TestOutsidersAndUnrelatedMissionsAreIgnored(t *testing.T) {
	got := Compute([]string{"miner-9"}, []Completion{
		{AgentID: "trader-10", TemplateID: "an_introduction", FinishedAt: at("2026-08-13T21:05:53Z")},
		{AgentID: "miner-9", TemplateID: "smuggling_courier_abc", FinishedAt: at("2026-08-13T20:00:00Z")},
		{AgentID: "miner-9", TemplateID: "edge_of_known_space_reconnaissance", FinishedAt: at("2026-08-13T21:53:43Z")},
	})
	if len(got) != 1 || got[0].AgentID != "miner-9" {
		t.Fatalf("got %+v; a non-pool agent leaked into the report", got)
	}
	if got[0].Step != 0 {
		t.Errorf("Step = %d; courier and exploration work is not chain progress", got[0].Step)
	}
}

// TestFurthestAlongSortsFirst so the agents about to land the unlock are what
// you read first.
func TestFurthestAlongSortsFirst(t *testing.T) {
	got := Compute([]string{"a", "b", "c"}, []Completion{
		{AgentID: "b", TemplateID: "across_the_line", FinishedAt: at("2026-08-13T21:00:00Z")},
		{AgentID: "c", TemplateID: "an_introduction", FinishedAt: at("2026-08-13T20:00:00Z")},
	})
	order := []string{got[0].AgentID, got[1].AgentID, got[2].AgentID}
	want := []string{"c", "b", "a"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v (furthest along first)", order, want)
		}
	}
}
