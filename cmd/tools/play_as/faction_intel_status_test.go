package main

import (
	"strings"
	"testing"
)

// TestFormatFactionIntelStatus uses the exact live server payload (which
// differs from the OpenAPI spec) to lock the formatter's field bindings.
func TestFormatFactionIntelStatus(t *testing.T) {
	raw := []byte(`{"contributors":2,"coverage_pct":"0.6","intel_level":1,"most_recent_tick":909252,"pois_known":6,"systems_known":3,"top_contributions":2,"top_contributor":"Arthur 'Artificer' Artis","total_systems":505}`)

	out := formatFactionIntelStatus(raw)
	if out == "" {
		t.Fatal("formatter returned empty string")
	}

	for _, want := range []string{
		"Faction Intel Status",
		"Intel level:     1",
		"0.6% (3 / 505 systems)",
		"POIs known:      6",
		"Contributors:    2",
		"Top contributor: Arthur 'Artificer' Artis (2)",
		"tick 909252",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

// TestFormatFactionIntelStatus_ActionResultWrapped confirms the formatter also
// handles an action_result-wrapped frame via unwrapActionResult.
func TestFormatFactionIntelStatus_ActionResultWrapped(t *testing.T) {
	raw := []byte(`{"command":"faction_intel_status","result":{"intel_level":2,"systems_known":10,"total_systems":505,"pois_known":40,"coverage_pct":"2.0"},"tick":1}`)
	out := formatFactionIntelStatus(raw)
	if !strings.Contains(out, "Intel level:     2") || !strings.Contains(out, "2.0% (10 / 505 systems)") {
		t.Errorf("wrapped frame not parsed:\n%s", out)
	}
}

// TestFormatFactionIntelStatus_Empty returns "" on bad JSON so the REPL falls
// back to its default rendering.
func TestFormatFactionIntelStatus_Empty(t *testing.T) {
	if out := formatFactionIntelStatus([]byte(`not json`)); out != "" {
		t.Errorf("expected empty string for bad JSON, got %q", out)
	}
}
