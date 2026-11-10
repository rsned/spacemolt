package game

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// HazardDose is one hazard_warning push as the client received it.
type HazardDose struct {
	serverapi.HazardWarning
	// Tick is the game tick when the push arrived (0 when unknown); At is the
	// wall-clock receive time.
	Tick int64
	At   time.Time
}

// clone deep-copies the dose so State.Clone never shares the Hazard pointer.
func (d HazardDose) clone() HazardDose {
	if d.Hazard != nil {
		h := *d.Hazard
		d.Hazard = &h
	}
	return d
}

// HullFrac is the hull left after the dose as a fraction of max hull, or 1
// when the push did not carry a max.
func (d HazardDose) HullFrac() float64 {
	if d.MaxHull <= 0 {
		return 1
	}
	return float64(d.Hull) / float64(d.MaxHull)
}

// HazardDoseRecent is how long a hazard_warning counts as "the ship is being
// dosed right now". Steady hazards dose every tick, so a dose older than a
// few ticks means the ship has left or docked.
const HazardDoseRecent = 3 * SleepTick

// Recent reports whether the dose arrived within HazardDoseRecent of now.
func (d HazardDose) Recent(now time.Time) bool {
	return !d.At.IsZero() && now.Sub(d.At) <= HazardDoseRecent
}

// formatHazardDose renders the operational log line for one dose.
func formatHazardDose(d HazardDose) string {
	kind, sev := "unknown", 0
	if d.Hazard != nil {
		kind, sev = d.Hazard.Type, d.Hazard.Severity
	}
	where := d.POIName
	if where == "" {
		where = d.POIID
	}
	line := fmt.Sprintf("☢️  HAZARD DOSE: %s severity %d at %s — hull %d/%d (%.0f%%), dose %d%% of max",
		kind, sev, where, d.Hull, d.MaxHull, d.HullFrac()*100, d.DosePct)
	if d.Message != "" {
		line += " — " + d.Message
	}
	return line
}

// handleHazardWarning records a hazard_warning push: the hull figure it
// carries is authoritative, the dose goes to State.LastHazardDose, and the
// line goes to the operational log (never debug, which production discards).
func (c *Client) handleHazardWarning(payload map[string]any) {
	var w serverapi.HazardWarning
	if data, err := json.Marshal(payload); err == nil {
		_ = json.Unmarshal(data, &w)
	}
	c.mu.Lock()
	d := HazardDose{HazardWarning: w, Tick: c.state.CurrentTick, At: time.Now()}
	c.state.LastHazardDose = d
	if w.MaxHull > 0 {
		c.state.Hull = float64(w.Hull)
		c.state.MaxHull = float64(w.MaxHull)
		c.state.Ship.Hull = float64(w.Hull)
		c.state.Ship.MaxHull = float64(w.MaxHull)
	}
	c.mu.Unlock()
	c.logEvent(formatHazardDose(d))
}
