package linedash

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const fixturePath = "testdata/lines_moonshine.yaml"

func TestLoadLinesMoonshineFixture(t *testing.T) {
	cfg, err := LoadLines(fixturePath)
	if err != nil {
		t.Fatalf("LoadLines: %v", err)
	}
	if len(cfg.Lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(cfg.Lines))
	}
	l := cfg.Lines[0]
	if l.ID != "moonshine" {
		t.Errorf("id = %q", l.ID)
	}
	if len(l.Nodes) != 7 {
		t.Errorf("nodes = %d, want 7", len(l.Nodes))
	}
	if len(l.Edges) != 6 {
		t.Errorf("edges = %d, want 6", len(l.Edges))
	}
	if cfg.Thresholds != DefaultThresholds() {
		t.Errorf("thresholds = %+v, want defaults %+v", cfg.Thresholds, DefaultThresholds())
	}
	if cfg.Thresholds.HaulWindow != 4*time.Hour {
		t.Errorf("HaulWindow = %v", cfg.Thresholds.HaulWindow)
	}
	wantLabels := []string{"haul consigned", "haul consigned", "craft", "craft", "consign", "haul consigned"}
	for i, e := range l.Edges {
		if e.Label != wantLabels[i] {
			t.Errorf("edge %d label = %q, want %q", i, e.Label, wantLabels[i])
		}
	}
}

func TestLoadLinesRealFileMatchesFixture(t *testing.T) {
	realCfg, err := LoadLines(filepath.Join("..", "..", "data", "overmind", "lines.yaml"))
	if err != nil {
		t.Fatalf("real: %v", err)
	}
	fix, err := LoadLines(fixturePath)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if !reflect.DeepEqual(realCfg, fix) {
		t.Error("data/overmind/lines.yaml differs from testdata/lines_moonshine.yaml")
	}
}

func TestLoadLinesDefaultsAbsentThresholds(t *testing.T) {
	cfg, err := LoadLines(writeTemp(t, "thresholds: { haul_window: 9h }\n"+validLine("a")))
	if err != nil {
		t.Fatal(err)
	}
	d := DefaultThresholds()
	d.HaulWindow = 9 * time.Hour
	if cfg.Thresholds != d {
		t.Errorf("got %+v, want %+v", cfg.Thresholds, d)
	}
}

func TestLoadLinesSharedNodeIdsAllowedAcrossLines(t *testing.T) {
	y := "lines:\n" + lineBody("one") + lineBody("two")
	if _, err := LoadLines(writeTemp(t, y)); err != nil {
		t.Fatalf("shared ids across lines should load: %v", err)
	}
}

func TestValidateRejectsDanglingEdge(t *testing.T) {
	y := strings.Replace(validLine("a"), "to: voss_output", "to: nowhere", 1)
	expectLoadError(t, y, "nowhere")
}

func TestValidateRejectsEnergyCrystal(t *testing.T) {
	for _, item := range []string{"energy_crystal", "starshine"} {
		y := strings.Replace(validLine("a"), "items: [pirate_moonshine]", "items: ["+item+"]", 1)
		expectLoadError(t, y, item)
	}
	y := strings.Replace(validLine("a"), "kind: consign }", "kind: consign, item: energy_crystal }", 1)
	expectLoadError(t, y, "energy_crystal")
}

func TestValidateRejectsProduceWithoutRecipe(t *testing.T) {
	y := strings.Replace(validLine("a"), "recipe: r, ", "", 1)
	expectLoadError(t, y, "recipe")
}

func TestValidateRules(t *testing.T) {
	base := validLine("a")
	tests := []struct{ name, from, to, want string }{
		{"dup node id", "id: voss_output, type: store", "id: still, type: store", "duplicate"},
		{"bad type", "type: store", "type: bogus", "type"},
		{"zero units", "units_per_run: 6", "units_per_run: 0", "units_per_run"},
		{"zero batch", "batch_runs: 60", "batch_runs: 0", "batch_runs"},
		{"no inputs", "inputs_per_run: { water_ice: 10 }", "inputs_per_run: {}", "inputs_per_run"},
		{"produce no station", "station: s1, recipe", "recipe", "station"},
		{"holder both", "{ agent: bob, station: s1 }", "{ agent: bob, faction: CRFT, station: s1 }", "holder"},
		{"holder neither", "{ agent: bob, station: s1 }", "{ station: s1 }", "holder"},
		{"capacity", "capacity_per_day: 10", "capacity_per_day: 0", "capacity_per_day"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(base, tc.from) {
				t.Fatalf("test setup: %q not in base", tc.from)
			}
			expectLoadError(t, strings.Replace(base, tc.from, tc.to, 1), tc.want)
		})
	}
}

// lineBody is one valid line, indented to sit under "lines:".
func lineBody(id string) string {
	return `  - id: ` + id + `
    name: N
    output_item: pirate_moonshine
    capacity_per_day: 10
    nodes:
      - { id: still, type: produce, name: S, items: [pirate_moonshine], station: s1, recipe: r, units_per_run: 6, batch_runs: 60, inputs_per_run: { water_ice: 10 } }
      - { id: voss_output, type: store, name: O, items: [pirate_moonshine], holders: [ { agent: bob, station: s1 } ] }
    edges:
      - { from: still, to: voss_output, kind: craft }
      - { from: voss_output, to: still, kind: consign }
`
}

func validLine(id string) string { return "lines:\n" + lineBody(id) }

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "lines.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func expectLoadError(t *testing.T, y, want string) {
	t.Helper()
	_, err := LoadLines(writeTemp(t, y))
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err, want)
	}
}
