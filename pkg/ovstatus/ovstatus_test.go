package ovstatus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

func writeStatus(t *testing.T, dir, name string, sf balances.StatusFile) string {
	t.Helper()
	p := filepath.Join(dir, name)
	data, err := json.Marshal(sf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestRenderTOCAndAlphabeticalRows(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 28, 21, 40, 0, 0, time.UTC)
	captured := now.Add(-20 * time.Second).Format(time.RFC3339)

	haul := writeStatus(t, dir, "haul.json", balances.StatusFile{
		CapturedAt: captured,
		Workers: []balances.LiveRecord{
			{AgentID: "zulu", Role: "hauler", System: "Sol", POI: "sol_central",
				Credits: 1234567, Docked: false, StandingBehavior: "hauler",
				Healthy: true, Seen: true, LastSeen: captured},
			{AgentID: "alpha", Role: "hauler", System: "Krynn", POI: "war_citadel",
				Credits: 500, Docked: true, StandingBehavior: "hauler",
				ActiveTaskID: "t-9", Healthy: true, Seen: true, LastSeen: captured},
		},
	})
	mb := writeStatus(t, dir, "mb.json", balances.StatusFile{
		CapturedAt: captured,
		Workers: []balances.LiveRecord{
			{AgentID: "marketbot_sol", Role: "resident", System: "Sol", POI: "sol_station",
				Credits: 489, Docked: true, StandingBehavior: "resident",
				Healthy: true, Seen: true, LastSeen: captured},
		},
	})

	out := Render([]Source{{Name: "Haul", Path: haul}, {Name: "Marketbots", Path: mb}}, 300, now)

	// Table of contents has both overminds with counts.
	if !strings.Contains(out, ">Haul</a> <span class=\"count\">(2)</span>") {
		t.Errorf("TOC missing Haul(2):\n%s", out)
	}
	if !strings.Contains(out, ">Marketbots</a> <span class=\"count\">(1)</span>") {
		t.Errorf("TOC missing Marketbots(1)")
	}

	// Alphabetical ordering within Haul: alpha before zulu.
	ai := strings.Index(out, ">alpha<")
	zi := strings.Index(out, ">zulu<")
	if ai < 0 || zi < 0 || ai > zi {
		t.Errorf("rows not alphabetical: alpha=%d zulu=%d", ai, zi)
	}

	// Credits formatted with separators; active task surfaced; position joined.
	for _, want := range []string{"1,234,567", "task t-9", "Krynn / war_citadel", "auto-refresh 5m"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRenderMissingFileAndStale(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 28, 21, 40, 0, 0, time.UTC)
	old := now.Add(-10 * time.Minute).Format(time.RFC3339)

	stale := writeStatus(t, dir, "stale.json", balances.StatusFile{
		CapturedAt: old,
		Workers: []balances.LiveRecord{
			{AgentID: "ghost", Role: "shuttle", System: "Haven", POI: "haven_star",
				Credits: 10, StandingBehavior: "shuttle", Healthy: false,
				Restarts: 3, Seen: true, LastSeen: old},
		},
	})

	out := Render([]Source{
		{Name: "Shuttle", Path: stale},
		{Name: "Missing", Path: filepath.Join(dir, "nope.json")},
	}, 0, now)

	if !strings.Contains(out, "⚠ unhealthy") || !strings.Contains(out, "⚠ stale") {
		t.Errorf("expected unhealthy+stale flags:\n%s", out)
	}
	if !strings.Contains(out, "3 restarts") {
		t.Errorf("expected restart count")
	}
	if !strings.Contains(out, "No status file yet") {
		t.Errorf("expected missing-file notice for Missing overmind")
	}
	if !strings.Contains(out, "auto-refresh off") {
		t.Errorf("refresh=0 should render as off")
	}
	// refresh=0 must not emit a meta-refresh tag.
	if strings.Contains(out, "http-equiv=\"refresh\"") {
		t.Errorf("refresh=0 should not emit meta refresh")
	}
}

func TestNameText(t *testing.T) {
	if got := nameText(balances.LiveRecord{AgentID: "alpha"}); got != "alpha" {
		t.Errorf("nameText without tag = %q, want %q", got, "alpha")
	}
	if got := nameText(balances.LiveRecord{AgentID: "alpha", FactionTag: "YSMT"}); got != "alpha (YSMT)" {
		t.Errorf("nameText with tag = %q, want %q", got, "alpha (YSMT)")
	}
	// Server pads tags to a fixed width; the suffix must render trimmed.
	if got := nameText(balances.LiveRecord{AgentID: "alpha", FactionTag: " DB "}); got != "alpha (DB)" {
		t.Errorf("nameText with padded tag = %q, want %q", got, "alpha (DB)")
	}
}

func TestFormatCredits(t *testing.T) {
	cases := map[float64]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -2500: "-2,500"}
	for in, want := range cases {
		if got := formatCredits(in); got != want {
			t.Errorf("formatCredits(%v) = %q, want %q", in, got, want)
		}
	}
}
