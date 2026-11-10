package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// Environmental hazards (v0.687.0): a POI's hazard doses an undocked ship
// every tick (radiation, tidal_shear, scar_dust) or on every mining action
// (tidal_shear, charged_seam, debris), and an emptied hull is a lost ship.
// Nothing in a scripted loop notices: `loop 100 mine` mines through every
// hazard_warning until the ship dies. The gate below runs in front of every
// standing/scheduled/task command (StandingDeps.dispatch) and gets the ship
// out, and refuses to fly into or mine at a POI known to be dangerous.

// ErrHazardEscape aborts the enclosing loops: the ship was being dosed and has
// been moved off the hazard POI. Fatal under -f, like a rate limit -- mining on
// regardless is exactly what kills the ship.
var ErrHazardEscape = errors.New("hazard: left a dangerous POI")

// ErrHazardRefused is a command refused because its POI carries a known
// high-severity hazard. Also loop-fatal: retrying cannot make the POI safe.
var ErrHazardRefused = errors.New("hazard: refused a dangerous POI")

const (
	// hazardLeaveHullFrac is the hull fraction below which even a severity-1
	// dose (1% of max hull a tick) is reason to leave.
	hazardLeaveHullFrac = 0.70
	// hazardLeaveSeverity is the severity at which any dose means leave now:
	// moderate (3%/tick) already costs a third of the hull in ten ticks.
	hazardLeaveSeverity = 2
	// hazardAvoidSeverity is the known severity we refuse to enter or mine at:
	// high (8%) and lethal (25%) are a dozen and four doses from death.
	hazardAvoidSeverity = 3
)

// hazardShouldLeave reports whether the ship is being dosed right now at the
// POI it still sits at, badly enough to leave.
func hazardShouldLeave(st *game.State, now time.Time) (bool, string) {
	if st == nil || st.Doc || st.InBattle {
		return false, ""
	}
	d := st.LastHazardDose
	if !d.Recent(now) {
		return false, ""
	}
	if d.POIID != "" && st.CurrentPOI != "" && d.POIID != st.CurrentPOI {
		return false, "" // the dose was at a POI we have already left
	}
	sev := 0
	kind := "hazard"
	if d.Hazard != nil {
		sev, kind = d.Hazard.Severity, d.Hazard.Type
	}
	if sev >= hazardLeaveSeverity || d.HullFrac() < hazardLeaveHullFrac {
		return true, fmt.Sprintf("%s severity %d at %s, hull %d/%d", kind, sev, nonEmpty(d.POIName, d.POIID), d.Hull, d.MaxHull)
	}
	return false, ""
}

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// systemPOI finds a POI of the ship's current system by id or name.
func systemPOI(st *game.State, ref string) *game.POI {
	if st == nil || ref == "" {
		return nil
	}
	for i := range st.System.POIs {
		p := &st.System.POIs[i]
		if p.ID == ref || strings.EqualFold(p.Name, ref) || (p.BaseID != "" && p.BaseID == ref) {
			return p
		}
	}
	return nil
}

// hazardRefuge plans the way off the current POI: dock where we are if it has
// a base; else the nearest hazard-free base POI in the system (travel + dock);
// else the nearest hazard-free POI. Nil when the system offers nowhere safe.
func hazardRefuge(st *game.State) [][]string {
	cur := systemPOI(st, st.CurrentPOI)
	if cur != nil && cur.HasBase && !cur.Hazard.Damaging() {
		return [][]string{{"dock"}}
	}
	var bestBase, bestAny *game.POI
	dBase, dAny := math.Inf(1), math.Inf(1)
	for i := range st.System.POIs {
		p := &st.System.POIs[i]
		if cur != nil && p.ID == cur.ID {
			continue
		}
		if p.Hazard.Damaging() {
			continue
		}
		d := 0.0
		if cur != nil {
			d = math.Hypot(p.Position.X-cur.Position.X, p.Position.Y-cur.Position.Y)
		}
		if p.HasBase && d < dBase {
			bestBase, dBase = p, d
		}
		if d < dAny {
			bestAny, dAny = p, d
		}
	}
	switch {
	case bestBase != nil:
		return [][]string{{"travel", bestBase.ID}, {"dock"}}
	case bestAny != nil:
		return [][]string{{"travel", bestAny.ID}}
	}
	return nil
}

// hazardRefusal reports why tokens must not run: travel to, or mining at, a
// POI whose known hazard is high or lethal. Empty when the command may run.
func hazardRefusal(st *game.State, tokens []string) string {
	if st == nil || len(tokens) == 0 {
		return ""
	}
	var target *game.POI
	switch strings.ToLower(tokens[0]) {
	case "travel":
		if len(tokens) < 2 {
			return ""
		}
		target = systemPOI(st, tokens[1])
	case "mine":
		if st.Doc {
			return ""
		}
		target = systemPOI(st, st.CurrentPOI)
	default:
		return ""
	}
	if target == nil || target.Hazard == nil || !target.Hazard.Damaging() {
		return ""
	}
	if target.Hazard.Severity < hazardAvoidSeverity {
		return ""
	}
	return fmt.Sprintf("%s has a %s hazard of severity %d (%d%% of max hull per dose)",
		nonEmpty(target.Name, target.ID), target.Hazard.Type, target.Hazard.Severity, target.Hazard.DosePct())
}

// isMovement reports whether tokens already take the ship off its POI or into
// a dock, so the escape need not pre-empt them.
func isMovement(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	switch strings.ToLower(tokens[0]) {
	case "travel", "jump", "dock", "go", "goto", "travel_to":
		return true
	}
	return false
}

// hazardGate runs in front of one command. It returns a non-nil error when the
// command must not run: ErrHazardEscape after moving the ship off a POI that is
// dosing it, or ErrHazardRefused for a command aimed at a known deadly POI.
func hazardGate(ctx context.Context, out io.Writer, st *game.State, now time.Time,
	tokens []string, run func(context.Context, []string) error,
) error {
	if why := hazardRefusal(st, tokens); why != "" {
		fmt.Fprintf(out, "☢️  hazard: refusing %q: %s\n", strings.Join(tokens, " "), why) //nolint:errcheck
		return fmt.Errorf("%w: %s", ErrHazardRefused, why)
	}
	leave, why := hazardShouldLeave(st, now)
	if !leave || isMovement(tokens) {
		return nil
	}
	plan := hazardRefuge(st)
	if plan == nil {
		fmt.Fprintf(out, "☢️  hazard: %s — no safe POI known in this system; continuing\n", why) //nolint:errcheck
		return nil
	}
	fmt.Fprintf(out, "☢️  hazard: %s — leaving before %q\n", why, strings.Join(tokens, " ")) //nolint:errcheck
	for _, step := range plan {
		if err := run(ctx, step); err != nil {
			fmt.Fprintf(out, "☢️  hazard: escape step %q failed: %v\n", strings.Join(step, " "), err) //nolint:errcheck
			return fmt.Errorf("%w (%s; escape step %q failed: %w)", ErrHazardEscape, why, strings.Join(step, " "), err)
		}
	}
	fmt.Fprintf(out, "☢️  hazard: escaped (%s)\n", why) //nolint:errcheck
	return fmt.Errorf("%w: %s", ErrHazardEscape, why)
}
