package sweep

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := StatePath(dir, "explorer-3")
	s := NewState(7550, 148, 23)
	s.Phase = PhaseExplore
	s.TargetX, s.TargetY, s.TargetDeg, s.PlannedBearing = 1, 2, 143, 270.5
	s.LaunchCostMeasured = true
	s.Launches = 4
	s.LastX, s.LastY, s.Drifted = -4790.2, -6130.1, true
	s.ArrivalToured, s.ExploreJumps = true, 3
	s.Gaps = []Gap{{FromDeg: 148, ToDeg: 143, Reason: "storm"}}
	s.AddUnfinished("whitecap")
	s.HomeSystem = "frontier"
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(path)
	if err != nil || !ok {
		t.Fatalf("Load = %v, %v", ok, err)
	}
	if got.UpdatedUTC == "" {
		t.Error("Save did not stamp UpdatedUTC")
	}
	got.UpdatedUTC, s.UpdatedUTC = "", ""
	if !reflect.DeepEqual(got, s) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, s)
	}
}

func TestNewStateStartsLap(t *testing.T) {
	s := NewState(7500, 148, 45)
	if s.Phase != PhaseLap || s.ProgressDeg != 148 || s.StartDeg != 148 || s.Ring != 7500 || s.LaunchCost != 45 {
		t.Fatalf("NewState = %+v", s)
	}
}

func TestLoadMissing(t *testing.T) {
	s, ok, err := Load(filepath.Join(t.TempDir(), "nope", "sweep_state.json"))
	if s != nil || ok || err != nil {
		t.Fatalf("Load(missing) = %v, %v, %v", s, ok, err)
	}
}

func TestSaveAtomicLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := StatePath(dir, "explorer-5")
	if err := Save(path, NewState(7600, 148, 41)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "sweep_state.json" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("dir holds %v, want only sweep_state.json", names)
	}
}

func TestAddUnfinishedDedup(t *testing.T) {
	s := NewState(7500, 148, 45)
	s.AddUnfinished("a")
	s.AddUnfinished("a")
	if !reflect.DeepEqual(s.Unfinished, []string{"a"}) {
		t.Fatalf("Unfinished = %v", s.Unfinished)
	}
}
