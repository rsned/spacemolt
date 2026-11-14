package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/sweep"
)

var now = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// holey is a 300 LY lattice over ±7500 with one 1200 LY hole at (1500,1500).
func holey() []sweep.Point {
	var pts []sweep.Point
	for x := -7500.0; x <= 7500; x += 300 {
		for y := -7500.0; y <= 7500; y += 300 {
			if x >= 1200 && x <= 1800 && y >= 1200 && y <= 1800 {
				continue
			}
			pts = append(pts, sweep.Point{X: x, Y: y})
		}
	}
	return pts
}

func TestGenerateWritesAFreshRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "void-run.json")
	if err := generate(holey(), path, now); err != nil {
		t.Fatal(err)
	}
	r, err := sweep.LoadVoidRun(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Zones) != 1 || len(r.Zones[0].Waypoints) == 0 {
		t.Fatalf("run %+v, want one zone with a square wave", r)
	}
}

func TestGenerateRefusesAnActiveRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "void-run.json")
	if err := generate(holey(), path, now); err != nil {
		t.Fatal(err)
	}
	if _, err := sweep.ClaimZone(path, "explorer-1", 90, nil, now); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	err := generate(holey(), path, now)
	if err == nil || !strings.Contains(err.Error(), "archive") {
		t.Fatalf("err %v, want a refusal telling the operator to archive the run", err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("generate changed an active run")
	}
}

func TestRenderMapShowsFlownLanes(t *testing.T) {
	r := sweep.NewVoidRun(sweep.ComputeZones(holey(), sweep.DefaultZoneOpts()), 100, now)
	if got := renderMap(r, holey()); strings.Contains(got, `class="flown"`) {
		t.Fatal("a run with nothing flown draws flown lanes")
	}
	r.Zones[0].Next = 2
	r.Zones[0].Found = []string{"hidden_one"}
	got := renderMap(r, holey())
	for _, want := range []string{`class="flown"`, "V01", "hidden_one", "<title>"} {
		if !strings.Contains(got, want) {
			t.Errorf("map lacks %q", want)
		}
	}
}
