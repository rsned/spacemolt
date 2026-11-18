package explore

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// Repair kits: one kit restores KitHull hull when used from cargo (repair,
// away from a repair service).
const (
	KitItem = "repair_kit"
	KitHull = 50
)

// kitCount is how many repair kits the ship carries.
func kitCount(st *game.State) int {
	if st == nil {
		return 0
	}
	n := 0.0
	for _, it := range st.Ship.Cargo {
		if it.ItemID == KitItem {
			n += it.Quantity
		}
	}
	return int(n)
}

// hullFrac is hull/max from client state, or 1 when the hull is unknown.
func hullFrac(st *game.State) float64 {
	if st == nil || st.Ship.MaxHull <= 0 {
		return 1
	}
	return st.Ship.Hull / st.Ship.MaxHull
}

// hullPct renders a hull fraction as a whole percentage, rounded down so a
// hull just under the floor never reads as above it.
func hullPct(frac float64) string {
	return fmt.Sprintf("%d%%", int(math.Floor(frac*100)))
}

// readHull refreshes status and cargo and returns the hull fraction and kits.
func readHull(ctx context.Context, d Deps) (float64, int) {
	_ = d.Client.GetStatus(ctx)
	_ = d.Client.GetCargo(ctx)
	st := d.Client.GetState()
	return hullFrac(st), kitCount(st)
}

// useKit repairs once from cargo and reports whether the hull rose.
func useKit(ctx context.Context, d Deps) bool {
	before := d.Client.GetState().Ship.Hull
	if err := d.Client.Repair(ctx); err != nil {
		d.Report.Printf("  Repair from kits failed: %v\n", err)
		return false
	}
	d.Settle(ctx, game.SleepTick)
	readHull(ctx, d)
	after := d.Client.GetState().Ship.Hull
	d.Report.Printf("  Used a repair kit: hull %.0f -> %.0f\n", before, after)
	return after > before
}

// repairToFloor uses repair kits from cargo until the hull is above floor or
// the kits run out. It returns the hull fraction and kits left.
func repairToFloor(ctx context.Context, d Deps, floor float64) (frac float64, kitsLeft int) {
	frac, kitsLeft = readHull(ctx, d)
	for frac <= floor && kitsLeft > 0 {
		if !useKit(ctx, d) {
			break
		}
		frac, kitsLeft = readHull(ctx, d)
	}
	return frac, kitsLeft
}

// topUpAfterDose uses kits while a whole kit's worth of hull is missing. It
// runs once the ship is clear of the hazard that dosed it: a repair costs a
// tick, and a tick on a steady hazard is another dose.
func (t *tour) topUpAfterDose(ctx context.Context) {
	st := t.d.Client.GetState()
	if st == nil || !st.LastHazardDose.At.After(t.doseSeen) {
		return
	}
	t.doseSeen = st.LastHazardDose.At
	readHull(ctx, t.d)
	for {
		st = t.d.Client.GetState()
		if st.Ship.MaxHull-st.Ship.Hull < KitHull || kitCount(st) == 0 {
			return
		}
		if !useKit(ctx, t.d) {
			return
		}
	}
}

// damaging reports whether a POI's hazard can hurt the hull.
func damaging(p game.POI) bool { return p.Hazard != nil && p.Hazard.Damaging() }

// atHazard returns the damaging-hazard POI the undocked ship sits at, if any.
func (t *tour) atHazard() (game.POI, bool) {
	st := t.d.Client.GetState()
	if st == nil || st.IsDocked() {
		return game.POI{}, false
	}
	for _, p := range t.route {
		if p.ID == st.CurrentPOI {
			return p, damaging(p)
		}
	}
	return game.POI{}, false
}

// leaveHazard moves the ship to the nearest POI without a damaging hazard
// when it is sitting on one. Staying is another dose every tick.
func (t *tour) leaveHazard(ctx context.Context) {
	here, on := t.atHazard()
	if !on {
		return
	}
	var best *game.POI
	for i, p := range t.route {
		if damaging(p) {
			continue
		}
		if best == nil || Distance(here.Position, p.Position) < Distance(here.Position, best.Position) {
			best = &t.route[i]
		}
	}
	if best == nil {
		t.d.Report.Printf("  No safe POI in this system to leave %s for\n", here.Name)
		return
	}
	t.d.Report.Printf("  Leaving hazard %s for %s\n", here.Name, best.Name)
	if _, err := t.d.Travel(ctx, best.ID); err != nil {
		t.d.Report.Printf("  Leaving %s failed: %v\n", here.Name, err)
	}
}

// tour is the state of one Tour call.
type tour struct {
	d     Deps
	o     Options
	route []game.POI
	res   *Result

	floorSkipped []int     // route indexes skipped at the hull floor
	doseSeen     time.Time // the last hazard dose already answered with kits
}
