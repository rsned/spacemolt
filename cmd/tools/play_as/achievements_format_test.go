package main

import (
	"strings"
	"testing"
)

// achievementsSample is a trimmed slice of a real get_achievements payload: one
// earned entry, one locked-with-progress entry, one hidden/secret entry, plus
// the summary roll-up.
const achievementsSample = `{"achievements":[` +
	`{"category":"combat","description":"Destroy your first enemy ship.","earned":false,"hidden":false,"id":"first_blood","name":"First Blood","points":10,"progress":{"current":0,"target":1}},` +
	`{"category":"combat","description":"???","earned":false,"hidden":true,"id":"nine_lives","name":"???","points":15},` +
	`{"category":"commerce","description":"Complete 50 trades.","earned":true,"earned_at":"2026-06-17T01:41:05Z","hidden":false,"id":"open_for_business","name":"Open For Business","points":15},` +
	`{"category":"commerce","description":"Complete 1,000 trades.","earned":false,"hidden":false,"id":"high_volume_trader","name":"High-Volume Trader","points":25,"progress":{"current":604,"target":1000}}` +
	`],"message":"Your achievements.","summary":{"earned":1,"points":15,"total":4}}`

// TestFormatGetAchievements_RendersSummaryAndEntries guards that the styled
// formatter surfaces the summary headline, category sections, earned/locked
// marks, points, and progress counters.
func TestFormatGetAchievements_RendersSummaryAndEntries(t *testing.T) {
	out := formatGetAchievements([]byte(achievementsSample))

	for _, want := range []string{
		"1 / 4 earned · 15 points", // summary headline
		"COMBAT (0/2)",
		"COMMERCE (1/2)",
		"Open For Business",
		"High-Volume Trader",
		"[604/1000 · 60%]",    // progress column
		"nine_lives (secret)", // hidden entry shows id + (secret)
		"Complete 50 trades.", // description column
		"✓",                   // earned mark
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// TestFormatGetAchievements_UnwrapsActionResult guards that achievements nested
// in an action_result frame still render.
func TestFormatGetAchievements_UnwrapsActionResult(t *testing.T) {
	raw := []byte(`{"command":"get_achievements","tick":7,"result":` + achievementsSample + `}`)
	out := formatGetAchievements(raw)
	if !strings.Contains(out, "Open For Business") {
		t.Errorf("action_result frame not unwrapped:\n%s", out)
	}
}

// TestFormatGetAchievements_Empty guards that an empty/unparseable payload
// returns "" so the caller falls back to raw JSON.
func TestFormatGetAchievements_Empty(t *testing.T) {
	if out := formatGetAchievements([]byte(`{}`)); out != "" {
		t.Errorf("empty payload should yield no styled output, got:\n%s", out)
	}
}
