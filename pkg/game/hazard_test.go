package game

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// A hazard dose must reach the operational log (debug is discarded in
// production), refresh the hull, and be kept on State for the worker gate.
func TestHazardWarningRecordedAndLogged(t *testing.T) {
	var buf bytes.Buffer
	c := facilityEventClient(&buf)
	c.state.CurrentTick = 42
	c.handleResponse(protocol.Response{
		Type: protocol.TypeHazardWarning,
		Payload: map[string]any{
			"poi_id": "the_maw", "poi_name": "The Maw",
			"hazard":   map[string]any{"type": "radiation", "severity": float64(4)},
			"hull":     float64(50),
			"max_hull": float64(200),
			"dose_pct": float64(25),
			"message":  "Radiation is cooking your hull.",
		},
	})
	got := buf.String()
	for _, want := range []string{"HAZARD DOSE", "radiation severity 4", "The Maw", "hull 50/200 (25%)"} {
		if !strings.Contains(got, want) {
			t.Errorf("event log %q missing %q", got, want)
		}
	}
	st := c.GetState()
	d := st.LastHazardDose
	if d.POIID != "the_maw" || d.Hazard == nil || d.Hazard.Severity != 4 || d.Tick != 42 {
		t.Fatalf("LastHazardDose = %+v", d)
	}
	if !d.Recent(time.Now()) || d.Recent(time.Now().Add(time.Hour)) {
		t.Error("Recent window wrong")
	}
	if st.Hull != 50 || st.Ship.Hull != 50 || st.Ship.MaxHull != 200 {
		t.Errorf("hull not refreshed: state %v ship %v/%v", st.Hull, st.Ship.Hull, st.Ship.MaxHull)
	}
	// Clone must not share the hazard pointer.
	st.LastHazardDose.Hazard.Severity = 1
	if c.GetState().LastHazardDose.Hazard.Severity != 4 {
		t.Error("State.Clone shares LastHazardDose.Hazard")
	}
}
