package game

import (
	"strings"
	"testing"
)

// Hazard doses must stand out in a play_as terminal the way server API
// changes do (operator 2026-11-14, after craftsman-1's Concordance died at
// Crush Vector Star with one plain-text warning on screen).
func TestAlertTagStyling(t *testing.T) {
	if got := alertTag("HAZARD", true); got != "\x1b[1;93;41m[HAZARD]\x1b[0m " {
		t.Errorf("tty tag = %q", got)
	}
	if got := alertTag("HAZARD", false); got != "[HAZARD] " {
		t.Errorf("plain tag = %q", got)
	}
	if got := alertTag("SERVER API CHANGE", true); got != "\x1b[1;93;41m[SERVER API CHANGE]\x1b[0m " {
		t.Errorf("api-change tag = %q", got)
	}
}

func TestHazardDoseLineCarriesTag(t *testing.T) {
	line := formatHazardDose(HazardDose{})
	if !strings.HasPrefix(line, "[HAZARD] ") || !strings.Contains(line, "HAZARD DOSE:") {
		t.Errorf("dose line %q, want [HAZARD] tag and the HAZARD DOSE marker", line)
	}
}
