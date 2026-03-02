package strategy

import (
	"testing"
)

func TestCheckpointSaveRestore(t *testing.T) {
	cp := &SkillCheckpoint{
		SkillName:   "mine",
		CurrentStep: "mine_loop",
		StepState: map[string]any{
			"cargo_pct":   0.52,
			"mining_site": "belt-42",
		},
	}

	if cp.SkillName != "mine" {
		t.Errorf("SkillName = %q, want %q", cp.SkillName, "mine")
	}
	if cp.CurrentStep != "mine_loop" {
		t.Errorf("CurrentStep = %q, want %q", cp.CurrentStep, "mine_loop")
	}
	if cp.IsEmpty() {
		t.Error("expected non-empty checkpoint")
	}
}

func TestCheckpointEmpty(t *testing.T) {
	cp := &SkillCheckpoint{}
	if !cp.IsEmpty() {
		t.Error("expected empty checkpoint")
	}
}

func TestCheckpointClear(t *testing.T) {
	cp := &SkillCheckpoint{
		SkillName:   "mine",
		CurrentStep: "mine_loop",
		StepState:   map[string]any{"cargo_pct": 0.5},
	}
	cp.Clear()
	if !cp.IsEmpty() {
		t.Error("expected empty checkpoint after Clear()")
	}
}
