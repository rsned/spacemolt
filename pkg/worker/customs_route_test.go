package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/customs"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// itemKB answers GetItem from a category map; everything else comes from
// the avoid-test fakeKB.
type itemKB struct {
	*fakeKB
	categories map[string]string
}

func (k itemKB) GetItem(_ context.Context, id string) (*knowledge.CatalogItem, error) {
	return &knowledge.CatalogItem{ID: id, Category: k.categories[id]}, nil
}

var testCategories = map[string]string{"void_dust": "contraband", "iron_ore": "ore"}

func writeCustomsMap(t *testing.T, monitored ...string) string {
	t.Helper()
	m := customs.Map{GeneratedAt: time.Now(), WindowDays: 14, Systems: map[string]customs.SystemStat{}}
	for _, id := range monitored {
		m.Systems[id] = customs.SystemStat{Class: customs.Monitored, Inspections: 5}
	}
	path := filepath.Join(t.TempDir(), "customs.json")
	if err := customs.Write(path, m); err != nil {
		t.Fatal(err)
	}
	return path
}

func cargoFake(items ...string) *fakeClient {
	f := izarRouteFake()
	for _, id := range items {
		f.state.Ship.Cargo = append(f.state.Ship.Cargo, game.CargoItem{ItemID: id, Quantity: 3})
	}
	return f
}

type customsRun struct {
	deps AutopilotDeps
	out  *bytes.Buffer
}

func customsDeps(t *testing.T, f *fakeClient, detour bool, mapPath string) customsRun {
	t.Helper()
	var out bytes.Buffer
	return customsRun{out: &out, deps: AutopilotDeps{
		Client: f, Out: &out,
		KB:                itemKB{fakeKB: avoidKB(false, detour), categories: testCategories},
		AvoidSystemsPath:  filepath.Join(t.TempDir(), "none.json"),
		CustomsMapPath:    mapPath,
		CustomsAlertsPath: filepath.Join(t.TempDir(), "alerts.json"),
	}}
}

func TestContrabandDetoursAroundMonitoredSystem(t *testing.T) {
	f := cargoFake("void_dust")
	r := customsDeps(t, f, true, writeCustomsMap(t, "izar"))
	if err := Autopilot(context.Background(), r.deps, "rigel", ""); err != nil {
		t.Fatal(err)
	}
	if got := jumps(f.calls); !slices.Equal(got, []string{"tidewater", "vega", "rigel"}) {
		t.Errorf("jumps = %v, want the detour around monitored izar", got)
	}
	if !strings.Contains(r.out.String(), "contraband aboard") {
		t.Errorf("output %q does not say contraband is aboard", r.out.String())
	}
}

// A courier mission puts its contraband in the hold at accept_mission, but the
// cached State.Ship.Cargo only changes on the next cargo read. pirate-7 routed
// through Mimosa on that stale empty hold (2026-11-23 20:53) and lost the
// cargo to the Northern Cordon; the customs check must read the hold fresh.
func TestContrabandAcceptedJustNowStillDetours(t *testing.T) {
	f := cargoFake()
	f.onGetCargo = func() {
		f.state.Ship.Cargo = []game.CargoItem{{ItemID: "void_dust", Quantity: 5}}
	}
	r := customsDeps(t, f, true, writeCustomsMap(t, "izar"))
	if err := Autopilot(context.Background(), r.deps, "rigel", ""); err != nil {
		t.Fatal(err)
	}
	if got := jumps(f.calls); !slices.Equal(got, []string{"tidewater", "vega", "rigel"}) {
		t.Errorf("jumps = %v, want the detour around monitored izar", got)
	}
}

func TestCleanCargoIgnoresCustomsMap(t *testing.T) {
	f := cargoFake("iron_ore")
	r := customsDeps(t, f, true, writeCustomsMap(t, "izar"))
	if err := Autopilot(context.Background(), r.deps, "rigel", ""); err != nil {
		t.Fatal(err)
	}
	if got := jumps(f.calls); !slices.Equal(got, []string{"tidewater", "izar", "rigel"}) {
		t.Errorf("jumps = %v, want the server route for a clean ship", got)
	}
}

func readAlertCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f customsAlertsFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range f.Alerts {
		n += a.Count
	}
	return n
}

func TestContrabandNoDetourFailsWithAlert(t *testing.T) {
	f := cargoFake("void_dust")
	r := customsDeps(t, f, false, writeCustomsMap(t, "izar"))
	err := Autopilot(context.Background(), r.deps, "rigel", "")
	if !errors.Is(err, ErrNoCustomsFreeRoute) || !errors.Is(err, ErrRouteThroughAvoided) {
		t.Fatalf("err = %v, want ErrNoCustomsFreeRoute wrapping ErrRouteThroughAvoided", err)
	}
	if got := jumps(f.calls); len(got) != 0 {
		t.Errorf("a blocked contraband run must not fly, jumped %v", got)
	}
	if !strings.Contains(r.out.String(), "no customs-free route to") {
		t.Errorf("first failure output %q lacks the full alert line", r.out.String())
	}
	if n := readAlertCount(t, r.deps.CustomsAlertsPath); n != 1 {
		t.Errorf("alert count = %d, want 1", n)
	}

	r.out.Reset()
	if err := Autopilot(context.Background(), r.deps, "rigel", ""); !errors.Is(err, ErrNoCustomsFreeRoute) {
		t.Fatalf("second err = %v", err)
	}
	if strings.Contains(r.out.String(), "no customs-free route to") {
		t.Errorf("repeat failure must log the short line, got %q", r.out.String())
	}
	if n := readAlertCount(t, r.deps.CustomsAlertsPath); n != 2 {
		t.Errorf("alert count = %d, want 2", n)
	}
}

func TestContrabandMonitoredDestinationStillFlies(t *testing.T) {
	f := cargoFake("void_dust")
	r := customsDeps(t, f, true, writeCustomsMap(t, "rigel"))
	if err := Autopilot(context.Background(), r.deps, "rigel", ""); err != nil {
		t.Fatal(err)
	}
	if got := jumps(f.calls); !slices.Equal(got, []string{"tidewater", "izar", "rigel"}) {
		t.Errorf("jumps = %v, want the normal route into the monitored destination", got)
	}
	if !strings.Contains(r.out.String(), "route endpoint") {
		t.Errorf("output %q does not warn that the endpoint is monitored", r.out.String())
	}
}

// A stub KB (nil embedded Base) would panic on GetItem; with no customs map
// the contraband check must never reach it.
func TestMissingCustomsMapNoKBCalls(t *testing.T) {
	f := cargoFake("void_dust")
	err := Autopilot(context.Background(), AutopilotDeps{
		Client: f, Out: &bytes.Buffer{}, KB: avoidKB(false, true),
		AvoidSystemsPath: filepath.Join(t.TempDir(), "none.json"),
		CustomsMapPath:   filepath.Join(t.TempDir(), "missing.json"),
	}, "rigel", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := jumps(f.calls); !slices.Equal(got, []string{"tidewater", "izar", "rigel"}) {
		t.Errorf("jumps = %v, want the server route with no customs map", got)
	}
}

// A block that recurs after the route reopened is a new episode: the
// operator must see the full line again, not the repeat line.
func TestCustomsAlertIsNewAgainAfterQuietGap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alerts.json")
	t0 := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	if !recordCustomsAlert(path, "a", "x", nil, t0) {
		t.Fatal("first alert must be new")
	}
	if recordCustomsAlert(path, "a", "x", nil, t0.Add(time.Hour)) {
		t.Fatal("hourly retry must be a repeat")
	}
	if !recordCustomsAlert(path, "a", "x", nil, t0.Add(5*time.Hour)) {
		t.Fatal("a block after a 4h gap must be new again")
	}
}

func TestCustomsAlertLinesShowOnlyRecent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alerts.json")
	recordCustomsAlert(path, "old", "x", nil, time.Now().Add(-48*time.Hour))
	recordCustomsAlert(path, "new", "y", nil, time.Now())
	lines := CustomsAlertLines(path)
	if len(lines) != 1 || !strings.Contains(lines[0], "new") {
		t.Fatalf("lines = %v, want only the recent alert", lines)
	}
}
