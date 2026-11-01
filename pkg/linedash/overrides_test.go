package linedash

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func writeOverrides(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "line-overrides.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOverridesRenameAndHide(t *testing.T) {
	o, err := LoadOverrides(writeOverrides(t, `
thresholds: { craft_window: 2h }
lines:
  "b:refine_steel": { name: "Steel" }
  "b:grow_sunspindle": { hidden: true }
  "b:sell:void_dust": { category: "Contraband" }
`))
	if err != nil {
		t.Fatal(err)
	}
	if o.Thresholds.CraftWindow != 2*time.Hour || o.Thresholds.StaleData != 2*time.Hour {
		t.Errorf("thresholds = %+v (craft 2h set, others defaulted)", o.Thresholds)
	}
	lines := []Line{{ID: "b:refine_steel", Name: "Steel Plate"}, {ID: "b:grow_sunspindle"}, {ID: "b:sell:void_dust", Category: CategorySales}}
	got, warns := ApplyOverrides(lines, o)
	if len(warns) != 0 {
		t.Errorf("warnings %v", warns)
	}
	if len(got) != 2 || got[0].Name != "Steel" || got[1].Category != "Contraband" {
		t.Errorf("applied = %+v", got)
	}
}

func TestOverridesUnknownLineWarns(t *testing.T) {
	o, err := LoadOverrides(writeOverrides(t, "lines:\n  nope: { name: x }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, warns := ApplyOverrides(nil, o); !slices.Contains(warns, "override names unknown line nope") {
		t.Errorf("warnings = %v", warns)
	}
}

func TestOverridesUnknownKeyErrors(t *testing.T) {
	if _, err := LoadOverrides(writeOverrides(t, "lines:\n  a: { nmae: x }\n")); err == nil {
		t.Error("a misspelt key loaded")
	}
}

func TestOverridesMissingFileIsDefaults(t *testing.T) {
	o, err := LoadOverrides(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil || o.Thresholds != DefaultThresholds() {
		t.Errorf("missing overrides = %+v, %v", o, err)
	}
}

func TestCadenceWarningsFlagsDailyCapture(t *testing.T) {
	lines := []Line{{ID: "l", Nodes: []Node{{ID: "p", Agents: []string{"a1", "a2", "a3"}}}}}
	sched := map[string][]Task{
		"a1": {{Frequency: "daily", Command: "capture_storage"}, {Frequency: "hourly", Command: "capture_action_log"}},
		"a2": {{Frequency: "ten_minutely", Command: "capture_storage"}, {Frequency: "hourly", Command: "capture_action_log"}},
	}
	got := CadenceWarnings(lines, sched)
	if !slices.Contains(got, "a1: needs hourly capture_storage") {
		t.Errorf("warnings = %v", got)
	}
	for _, w := range got {
		if strings.HasPrefix(w, "a2:") || strings.HasPrefix(w, "a3:") {
			t.Errorf("unexpected warning %q (a2 is fine; a3 has no schedule on record)", w)
		}
	}
}
