package main

import (
	"testing"
	"time"
)

// The opening banner reported "406 of 505 systems surveyed (80%)" while the
// walk was steering by a 24-hour freshness window that made 425 of those 505
// eligible to visit. Both numbers were right; they answered different
// questions, and the one on screen was not the one driving the run. An operator
// reading 80% concludes there are 99 systems left when there are 425.
func TestCoverageCounts_SeparatesEverSurveyedFromFresh(t *testing.T) {
	now := time.Date(2026, 9, 14, 20, 0, 0, 0, time.UTC)
	fresh := now.Add(-2 * time.Hour)
	stale := now.Add(-14 * 24 * time.Hour)

	surveyed := map[string]time.Time{
		"a": fresh, "b": fresh,
		"c": stale, "d": stale, "e": stale,
	}

	ever, freshN, eligible := coverageCounts(10, surveyed, now)
	if ever != 5 {
		t.Errorf("ever = %d, want 5", ever)
	}
	if freshN != 2 {
		t.Errorf("fresh = %d, want 2", freshN)
	}
	// 10 systems, 2 of them fresh: everything else is worth visiting.
	if eligible != 8 {
		t.Errorf("eligible = %d, want 8", eligible)
	}
}

// The live numbers from explorer-8 on 2026-09-14, so the reported figures match
// what the walk actually had in front of it.
func TestCoverageCounts_LiveFigures(t *testing.T) {
	now := time.Now().UTC()
	surveyed := make(map[string]time.Time, 406)
	for i := range 406 {
		if i < 80 {
			surveyed[string(rune('A'+i%26))+string(rune('a'+i/26))] = now.Add(-1 * time.Hour)
			continue
		}
		surveyed[string(rune('A'+i%26))+string(rune('a'+i/26))+"x"] = now.Add(-14 * 24 * time.Hour)
	}
	_, fresh, eligible := coverageCounts(505, surveyed, now)
	if fresh != 80 {
		t.Errorf("fresh = %d, want 80", fresh)
	}
	if eligible != 425 {
		t.Errorf("eligible = %d, want 425 (505 - 80)", eligible)
	}
}
