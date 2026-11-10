package main

import (
	"strings"
	"testing"
)

func TestFormatCompleteMission_RecipesUnlocked(t *testing.T) {
	got := formatCompleteMission([]byte(`{"mission_id":"m1","title":"Teach","credits_earned":100,"recipes_unlocked":["build_power_cell"]}`))
	if !strings.Contains(got, "recipe:") || !strings.Contains(got, "build_power_cell (now craftable)") {
		t.Fatalf("recipes not rendered:\n%s", got)
	}
	// A completion that teaches nothing but recipes still shows a Rewards block.
	got = formatCompleteMission([]byte(`{"mission_id":"m2","title":"Lore","recipes_unlocked":["x"]}`))
	if !strings.Contains(got, "Rewards:") || !strings.Contains(got, "x (now craftable)") {
		t.Fatalf("recipe-only completion:\n%s", got)
	}
}
