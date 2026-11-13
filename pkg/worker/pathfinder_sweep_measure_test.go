package worker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

func namedSystemLoc(systemID, name string) []byte {
	b, _ := json.Marshal(map[string]any{"location": map[string]any{
		"system_id": systemID, "system_name": name, "poi_id": systemID + "_poi",
	}})
	return b
}

// explorer-11, 2026-11-13: stopped mid-drift, restarted after the ship had
// landed; its saved reading was from before the stop, and it wrote Ash
// Pearl 160 LY short. A reading more than a tick or two old measures
// nothing.
func TestSweepArrivalStaleReadingSkipsMeasure(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "far_landing", 0, 0, 1, 1)
	s := sweep.NewState(7900, 148, 45)
	s.LaunchCostMeasured, s.Launches, s.Drifted, s.ReadSinceLaunch = true, 1, true, true
	s.LastX, s.LastY, s.LastReadTick = -6123, 3215, 2_400_000-30
	c := inSystem("far_landing", 900)
	d, _, _ := newArrivalDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sys, _ := kb.GetSystem(context.Background(), "far_landing"); sys.Position.X != 0 || sys.Position.Y != 0 {
		t.Fatalf("position = %+v, want a stale reading left unrecorded", sys.Position)
	}
}

// Patient Span, 2026-11-13: a frontier system has no KB row (the map capture
// skips systems without coordinates) and the tour never makes one, so the
// held measurement was dropped. The landing pass creates the row, named.
func TestSweepArrivalCreatesNewSystemRow(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	s := sweep.NewState(7950, 148, 45)
	s.LaunchCostMeasured, s.Launches, s.Drifted, s.ReadSinceLaunch = true, 1, true, true
	s.LastX, s.LastY, s.LastReadTick = -6165, 5004, 2_400_000
	c := inSystem("patient_span", 900)
	c.locs = [][]byte{namedSystemLoc("patient_span", "Patient Span")}
	d, path, _ := newArrivalDispatch(t, c, kb, s)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	sys, _ := kb.GetSystem(context.Background(), "patient_span")
	if sys == nil || sys.Name != "Patient Span" || sys.Position.X != -6165 || sys.Position.Y != 5004 {
		t.Fatalf("patient_span = %+v, want created with the landing reading", sys)
	}
	if got, _, _ := sweep.Load(path); got.PendingMeasureSystem != "" {
		t.Errorf("pending %q still held after the write", got.PendingMeasureSystem)
	}
}

// Each drift reading stamps the game tick it was taken on, so a landing can
// tell a fresh reading from a stale one.
func TestSweepDriftStampsReadingTick(t *testing.T) {
	s := sweep.NewState(7500, 148, 45)
	s.Phase = sweep.PhaseStage
	s.StageRefueled, s.Drifted, s.Launches, s.LaunchCostMeasured = true, true, 1, true
	s.TargetX, s.TargetY = -6259, 3300
	b := round1(sweep.BearingTo(-5894, 3073, -6259, 3300))
	s.PlannedBearing = b
	x, y := back(-6259, 3300, b, 200)
	c := &sweepFakeClient{state: shipWith(900, 1020), launchCost: 45, locs: [][]byte{driftLoc(x, y, b)}}
	c.state.CurrentTick = 2_400_123
	d, path := newSweepDispatch(t, c, stageKB(t), s)
	d.SweepStage = testStage
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := sweep.Load(path); got.LastReadTick != 2_400_123 {
		t.Errorf("LastReadTick = %d, want 2400123", got.LastReadTick)
	}
}
