package supervisor

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/control"
)

// TestApplyStatusProgressMarkAdvancesClock pins the drift exemption. A
// Pathfinder sweep ship drifting between systems keeps the same system, POI
// and credits for 15+ minutes; live 2026-11-13 the watchdog restarted ten
// drifting explorers and quarantined explorer-4 as fuel-dead mid-drift. The
// worker's Progress counter moving is forward motion.
func TestApplyStatusProgressMarkAdvancesClock(t *testing.T) {
	f := NewFleet()
	t0 := time.Unix(100000, 0)
	st := control.Status{System: "Crooked Lamp", POI: "bent_dark", Credits: 10, Progress: 1}
	f.ApplyStatus("explorer-4", st, t0)
	st.Progress = 2
	t1 := t0.Add(20 * time.Minute)
	f.ApplyStatus("explorer-4", st, t1)
	if w := f.get("explorer-4"); !w.LastProgress.Equal(t1) {
		t.Errorf("LastProgress = %v, want %v (progress mark moved)", w.LastProgress, t1)
	}
	f.ApplyStatus("explorer-4", st, t1.Add(20*time.Minute))
	if w := f.get("explorer-4"); !w.LastProgress.Equal(t1) {
		t.Errorf("LastProgress = %v, want %v (progress mark unchanged)", w.LastProgress, t1)
	}
}
