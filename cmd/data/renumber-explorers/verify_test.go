package main

import (
	"path/filepath"
	"testing"
)

func TestVerifyAgentsBands(t *testing.T) {
	agentsDir := t.TempDir()
	// Correct: slot 1 nebula, slot 9 outerrim.
	mkAgent(t, agentsDir, "explorer-1", "nebula")
	mkAgent(t, agentsDir, "explorer-9", "outerrim")
	if probs := verifyAgents(agentsDir, []string{"explorer-1", "explorer-9"}); len(probs) != 0 {
		t.Fatalf("unexpected problems: %v", probs)
	}
	// Wrong: slot 2 should be nebula, give it crimson.
	mkAgent(t, agentsDir, "explorer-2", "crimson")
	probs := verifyAgents(agentsDir, []string{"explorer-2"})
	if len(probs) == 0 {
		t.Fatal("expected a band mismatch problem")
	}
	_ = filepath.Join // keep import used if trimmed
}
