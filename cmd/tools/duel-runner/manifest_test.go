package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestManifestRoundTripAndResume(t *testing.T) {
	p := filepath.Join(t.TempDir(), "manifest.jsonl")
	now := time.Now().UTC()
	recs := []Record{
		{ScenarioID: "S0", Repeat: 1, BattleID: "b1", Started: now, Ended: now, Outcome: "A-fled"},
		{ScenarioID: "S1-ring2", Repeat: 1, BattleID: "b2", Started: now, Ended: now, Outcome: "stalemate"},
		{ScenarioID: "S1-ring2", Repeat: 2, BattleID: "b3", Started: now, Ended: now, Outcome: "stalemate", Void: true},
	}
	for _, r := range recs {
		if err := AppendRecord(p, r); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	done, err := LoadDone(p)
	if err != nil {
		t.Fatalf("LoadDone: %v", err)
	}
	if !done[DoneKey("S0", 1)] || !done[DoneKey("S1-ring2", 1)] {
		t.Errorf("completed duels missing from done set: %v", done)
	}
	// A void record does NOT count as done — it must be re-run.
	if done[DoneKey("S1-ring2", 2)] {
		t.Error("void record counted as done")
	}
}

func TestLoadDoneMissingFileIsEmpty(t *testing.T) {
	done, err := LoadDone(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil {
		t.Fatalf("missing manifest must not error: %v", err)
	}
	if len(done) != 0 {
		t.Errorf("done = %v, want empty", done)
	}
}
