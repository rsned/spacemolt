package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/explore"
	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

const (
	// frontierMinPower is the survey power a system must be surveyed at to
	// count as explored: two Survey Scanner IIs.
	frontierMinPower = 90
	// frontierKits is the repair kits a ship carries on the run.
	frontierKits = 10
	// frontierHullFloor is the hull fraction below which a ship enters no POI.
	frontierHullFloor = 0.5
	// frontierMaxLaunches is how many pathfinder launches at one target may
	// fail to arrive before it is marked unreachable.
	frontierMaxLaunches = 2
	// frontierProgressLY is how much nearer the target a launch must start
	// than the one before it for the earlier launches not to count: a drift
	// captured by charted ground on the way is progress, not a failure.
	frontierProgressLY = 50.0
	// frontierCaptureLY is how near a drift's line a system captures it.
	frontierCaptureLY = 100.0
	// frontierStageLook is how many of the nearest staging systems are
	// checked for a clear line to the target.
	frontierStageLook = 12
	// frontierMaxTries is how many failed tours of one system end it
	// incomplete; frontierMaxStops how many station detours its tour may force.
	frontierMaxTries = 3
	frontierMaxStops = 1
	// frontierLaunchCostEstimate stands in until the first launch is measured
	// and the fleet yaml gives no sweep_launch_cost (a Reserve's cost).
	frontierLaunchCostEstimate = 45
	// DefaultFrontierRun is the shared frontier ledger.
	DefaultFrontierRun = "data/sweep/frontier-run.json"
)

// frontierPass is one call of FrontierExplore: the ship's state, where it is
// saved, and the shared ledger.
type frontierPass struct {
	d    *WorkerDispatch
	s    *frontierState
	run  string // ledger path
	halt bool   // the pass found nothing it can do; wait before the next
}

// FrontierExplore runs one bounded step of the frontier full-explore run:
// fly to the nearest unexplored frontier system in the ship's compass wedge,
// claim its lane-connected cluster, and explore every system of it (survey at
// power 90, every POI visited, relics interacted with), recording each on the
// shared ledger. The state is saved after every step, so the idle loop calls
// it again and again and a restart resumes in place.
//
// Spec: docs/superpowers/specs/2026-11-18-frontier-full-explore-design.md
func (d *WorkerDispatch) FrontierExplore(ctx context.Context) error {
	if !slices.Contains(sweep.Wedges(), d.FrontierWedge) {
		return fmt.Errorf("frontier_explore: frontier_wedge %q is not one of %v", d.FrontierWedge, sweep.Wedges())
	}
	if d.frontierHalt != "" {
		// Stopped for the operator (see frontierPass.tour); a restart retries.
		settle(ctx, game.SleepSendTally)
		return nil
	}
	path := frontierStatePath(d.agentsDir(), d.AgentID)
	s, err := loadFrontierState(path)
	if err != nil {
		return err
	}
	if s.Phase == frontierDone {
		return nil
	}
	if s.LaunchCost <= 0 {
		s.LaunchCost = cmpOr(d.SweepLaunchCost, frontierLaunchCostEstimate)
	}
	f := &frontierPass{d: d, s: s, run: cmpOr(d.FrontierRun, DefaultFrontierRun)}
	err = f.step(ctx)
	s.UpdatedUTC = time.Now().UTC()
	if serr := saveFrontierState(path, s); serr != nil {
		return errors.Join(err, serr)
	}
	if err == nil && f.halt {
		settle(ctx, game.SleepSendTally)
	}
	return err
}

// cmpOr is v, or def when v is not positive.
func cmpOr[T ~string | ~float64](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}

func (f *frontierPass) logf(format string, args ...any) {
	fmt.Fprintf(f.d.Out, "frontier_explore: "+format+"\n", args...) //nolint:errcheck
}

// step runs the phase the ship is in.
func (f *frontierPass) step(ctx context.Context) error {
	loc, err := f.d.sweepLocation(ctx)
	if err != nil {
		return err
	}
	if loc.Location.Drifting() {
		return f.drift(ctx, loc)
	}
	sysID := loc.Location.SystemID
	if st := f.d.Client.GetState(); sysID == "" && st != nil {
		sysID = st.System.ID
	}
	if f.s.Drifted {
		f.landed(ctx, sysID, loc)
	}
	switch f.s.Phase {
	case frontierVerify:
		return f.verify(ctx, sysID)
	case frontierStage:
		return f.stage(ctx, sysID)
	case frontierLaunch:
		return f.launchAtTarget(ctx, sysID)
	case frontierWork:
		return f.work(ctx, sysID, loc)
	case frontierRefuel:
		return f.refuel(ctx, sysID)
	case frontierNext:
		return f.next(ctx, sysID)
	case frontierHome:
		return f.home(ctx, sysID)
	}
	return fmt.Errorf("frontier_explore: unknown phase %q", f.s.Phase)
}

// --- verify ---------------------------------------------------------------

// verify holds the ship until it carries the frontier fit, then picks the
// first target: the system of its wedge nearest charted space.
func (f *frontierPass) verify(ctx context.Context, sysID string) error {
	if why := f.fitProblem(ctx); why != "" {
		f.logf("not starting: %s", why)
		f.halt = true
		return nil
	}
	// Read the survey power before leaving the station: a ship that cannot
	// survey at 90 would only find that out a pathfinder launch away.
	power, err := f.d.frontierSurveyFn()(ctx)
	if err == nil && power < frontierMinPower {
		err = fmt.Errorf("survey power %d is under %d", power, frontierMinPower)
	}
	if err != nil {
		f.logf("not starting: pre-flight survey: %v", err)
		if st := f.d.Client.GetState(); st != nil && !st.Doc && f.d.sweepStationPOI(ctx, sysID) != "" {
			if derr := dockIdempotent(ctx, f.d.Client); derr != nil {
				f.logf("dock: %v", derr)
			}
		}
		f.halt = true
		return nil
	}
	x, y, _ := f.position(ctx, sysID)
	t, err := sweep.OwnClaim(f.run, f.d.AgentID, x, y)
	if err == nil && t == nil {
		t, err = sweep.StartFrontierTarget(f.run, f.d.FrontierWedge)
	}
	if err != nil {
		return err
	}
	if t == nil {
		f.s.Phase = frontierNext
		return f.next(ctx, sysID)
	}
	f.setTarget(t)
	f.logf("fit verified; wedge %s, first target %s (%.0f, %.0f)", f.d.FrontierWedge, t.ID, t.X, t.Y)
	return nil
}

// fitProblem is why the ship is not fit for the run, or "".
func (f *frontierPass) fitProblem(ctx context.Context) string {
	if err := f.d.Client.GetShip(ctx); err != nil {
		return fmt.Sprintf("get_ship: %v", err)
	}
	var ship serverapi.GetShipResponse
	if err := json.Unmarshal(unwrapActionResult(f.d.Client.GetRawJSON("ship")), &ship); err != nil {
		return fmt.Sprintf("parse get_ship: %v", err)
	}
	drive, scanners := 0, 0
	for _, m := range ship.Modules {
		switch m.TypeID {
		case sweep.FitDrive:
			drive++
		case sweep.FitScanner:
			scanners++
		}
	}
	switch {
	case drive == 0:
		return "no " + sweep.FitDrive + " fitted"
	case scanners < sweep.FitScannerWant:
		return fmt.Sprintf("%d %s fitted, the run needs %d (survey power %d)", scanners, sweep.FitScanner, sweep.FitScannerWant, frontierMinPower)
	}
	_ = f.d.Client.GetCargo(ctx)
	if st := f.d.Client.GetState(); st == nil || cargoQuantity(st, repairKitItem) < 1 {
		return fmt.Sprintf("no %s aboard (the run carries %d)", repairKitItem, frontierKits)
	}
	return ""
}

// setTarget aims the ship at a ledger system and starts its staging.
func (f *frontierPass) setTarget(t *sweep.FrontierSystem) {
	f.s.Target, f.s.TargetX, f.s.TargetY = t.ID, t.X, t.Y
	f.s.Launches, f.s.LaunchDist = 0, 0
	f.s.Phase = frontierStage
}

// --- getting there ---------------------------------------------------------

// fuelOK reports whether the ship may jump or launch without eating into its
// way back to a station: this launch, one more, and the final jump.
func (f *frontierPass) fuelOK() bool {
	tank, _, cells, premium := sweepFuel(f.d.Client.GetState())
	return sweep.CanContinue(tank, cells, premium, f.s.LaunchCost)
}

// toRefuel turns the ship for a station, to come back to resume afterwards.
func (f *frontierPass) toRefuel(resume, why string) {
	tank, _, cells, premium := sweepFuel(f.d.Client.GetState())
	f.logf("%s (fuel %.0f); detouring to a station, then back to %s", why, sweep.Total(tank, cells, premium), resume)
	f.s.ResumeAt, f.s.Phase = resume, frontierRefuel
}

// nav flies by jump lanes, waiting out a still-settling previous action.
func (f *frontierPass) nav(ctx context.Context, system, poi string) error {
	avoid := f.hostileSystems(system)
	return waitOutPending(ctx, func() error { return f.d.frontierNavFn()(ctx, system, poi, avoid) })
}

// hostileSystems is every system the ledger marks hostile (a leviathan, a
// repeater, a battle), for lane routes to go round; dest is never in it.
func (f *frontierPass) hostileSystems(dest string) map[string]string {
	run, err := sweep.LoadFrontierRun(f.run)
	if err != nil {
		return nil
	}
	var out map[string]string
	for id, ls := range run.Systems {
		if id != dest && ls.Status == sweep.FrontierIncomplete && strings.HasPrefix(ls.Reason, "hostile") {
			if out == nil {
				out = map[string]string{}
			}
			out[id] = "frontier run: " + ls.Reason
		}
	}
	return out
}

// noRoute reports a lane route the server or the avoid list refuses.
func noRoute(err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(err.Error())
	return errors.Is(err, ErrRouteThroughAvoided) || strings.Contains(low, "no route") || sweepUnroutable(err)
}

// navRefused reports a lane trip autopilot will refuse again on every pass:
// no route, or a route it will not fly (fuel, a stronghold, a toll).
func navRefused(err error) bool {
	return noRoute(err) || errors.Is(err, ErrInsufficientRouteFuel) ||
		errors.Is(err, ErrRouteThroughStronghold) || errors.Is(err, ErrGateTollUnaffordable)
}

// targetGone reports a target that is no longer this ship's to fly to:
// finished, written off, or claimed by another ship since it was picked.
func (f *frontierPass) targetGone() bool {
	run, err := sweep.LoadFrontierRun(f.run)
	if err != nil {
		return false
	}
	t := run.Systems[f.s.Target]
	if t == nil {
		return false
	}
	gone := t.Status == sweep.FrontierDone || t.Status == sweep.FrontierIncomplete ||
		(t.Status == sweep.FrontierClaimed && t.Claimant != f.d.AgentID)
	if gone {
		f.logf("target %s is now %s (%s); picking another", t.ID, t.Status, t.Claimant)
	}
	return gone
}

// stage flies by lanes to the lane-reachable system nearest the target, so
// the pathfinder launch is short; a target the lanes reach is flown to.
func (f *frontierPass) stage(ctx context.Context, sysID string) error {
	if sysID == f.s.Target {
		f.s.Phase = frontierWork
		return nil
	}
	if f.targetGone() {
		f.s.Phase = frontierNext
		return f.next(ctx, sysID)
	}
	if !f.fuelOK() {
		f.toRefuel(f.s.Target, "fuel is under three launches before staging")
		return nil
	}
	systems, err := f.d.KB.GetSystems(ctx)
	if err != nil {
		return fmt.Errorf("frontier_explore: staging: %w", err)
	}
	conns, err := f.d.KB.GetConnections(ctx)
	if err != nil {
		return fmt.Errorf("frontier_explore: staging: %w", err)
	}
	run, err := sweep.LoadFrontierRun(f.run)
	if err != nil {
		return err
	}
	cands := voidStageCandidates(systems, conns, loadAvoidSystems(f.d.AvoidSystemsPath),
		smugglingUnlocked(f.d.Client.GetState()), sysID, f.s.TargetX, f.s.TargetY)
	cands = f.clearStages(cands, systems, run)
	tries := 0
	for _, c := range cands {
		if c.id == sysID || tries >= voidStageTries {
			break
		}
		if ls := run.Systems[c.id]; ls != nil && c.id != f.s.Target && ls.Status == sweep.FrontierIncomplete {
			continue // never stage in a system marked hostile or unreachable
		}
		tries++
		f.logf("staging at %s (%.0f LY from %s) by lanes", c.id, c.dist, f.s.Target)
		err := f.nav(ctx, c.id, "")
		if err == nil {
			f.d.markProgress()
			return nil
		}
		if !navRefused(err) {
			return fmt.Errorf("frontier_explore: staging at %s: %w", c.id, err)
		}
		// Staging is optional: with no usable lane trip the launch is from here.
		f.logf("staging at %s: %v; trying the next nearest", c.id, err)
	}
	f.s.Phase = frontierLaunch
	return f.launchAtTarget(ctx, sysID)
}

// clearStages moves, among the frontierStageLook nearest candidates, those
// whose straight line to the target is clear ahead of those whose drift a
// charted or finished system would capture on the way. Order is otherwise
// kept: nearest first. Unexplored frontier systems do not count as in the
// way, since landing in one is work, not a detour.
func (f *frontierPass) clearStages(cands []voidStageCand, systems []knowledge.System, run *sweep.FrontierRun) []voidStageCand {
	pos := make(map[string]game.Position, len(systems))
	var walls []knowledge.System
	for _, sys := range systems {
		if sys.Position.X == 0 && sys.Position.Y == 0 {
			continue
		}
		pos[sys.ID] = sys.Position
		if ls := run.Systems[sys.ID]; sys.ID != f.s.Target && (ls == nil || ls.Status == sweep.FrontierDone || ls.Status == sweep.FrontierIncomplete) {
			walls = append(walls, sys)
		}
	}
	blocked := func(c voidStageCand) bool {
		o := pos[c.id]
		return slices.ContainsFunc(walls, func(w knowledge.System) bool {
			return w.ID != c.id && sweep.Interrupts(o.X, o.Y, f.s.TargetX, f.s.TargetY, w.Position.X, w.Position.Y, frontierCaptureLY)
		})
	}
	n := min(len(cands), frontierStageLook)
	var clear, rest []voidStageCand
	for _, c := range cands[:n] {
		if blocked(c) {
			rest = append(rest, c)
		} else {
			clear = append(clear, c)
		}
	}
	if len(clear) > 0 && len(rest) > 0 && clear[0].id != cands[0].id {
		f.logf("the line from %s to %s is interrupted; %s has a clear one", cands[0].id, f.s.Target, clear[0].id)
	}
	return slices.Concat(clear, rest, cands[n:])
}

// launchAtTarget launches at the target's ledger position, giving up on a
// target two launches have failed to reach.
func (f *frontierPass) launchAtTarget(ctx context.Context, sysID string) error {
	if sysID == f.s.Target {
		f.s.Phase = frontierWork
		return nil
	}
	if f.targetGone() {
		f.s.Phase = frontierNext
		return f.next(ctx, sysID)
	}
	x, y, ok := f.position(ctx, sysID)
	if ok {
		f.nearer(x, y)
	}
	if f.s.Launches >= frontierMaxLaunches {
		f.unreachable()
		f.s.Phase = frontierNext
		return f.next(ctx, sysID)
	}
	if !f.fuelOK() {
		f.toRefuel(f.s.Target, "fuel is under three launches")
		return nil
	}
	if !ok {
		return fmt.Errorf("frontier_explore: position of %s unknown; cannot plot a bearing to %s", sysID, f.s.Target)
	}
	f.logf("launch %d at %s from %s", f.s.Launches+1, f.s.Target, sysID)
	if err := f.launch(ctx, x, y, f.s.TargetX, f.s.TargetY); err != nil {
		return err // a refused jump is not a launch at the target
	}
	f.launched(x, y)
	return nil
}

// nearer forgets the launches made at the target so far when (x, y), where
// the next would start, is well inside where the last one started.
func (f *frontierPass) nearer(x, y float64) {
	s := f.s
	if s.Launches == 0 || s.LaunchDist <= 0 {
		return
	}
	if d := math.Hypot(s.TargetX-x, s.TargetY-y); d < s.LaunchDist-frontierProgressLY {
		f.logf("%.0f LY from %s, down from %.0f; its launch count starts again", d, s.Target, s.LaunchDist)
		s.Launches = 0
	}
}

// launched counts a launch at the target that started from (x, y).
func (f *frontierPass) launched(x, y float64) {
	f.s.Launches++
	f.s.LaunchDist = math.Hypot(f.s.TargetX-x, f.s.TargetY-y)
}

// unreachable marks the target incomplete on the ledger.
func (f *frontierPass) unreachable() {
	f.logf("%s not reached in %d launches; marked unreachable", f.s.Target, f.s.Launches)
	f.finish(sweep.FrontierSystem{ID: f.s.Target, Status: sweep.FrontierIncomplete, Reason: "unreachable"})
}

// position is the map position of sysID: the KB's, else the ledger's, else
// the last drift reading.
func (f *frontierPass) position(ctx context.Context, sysID string) (x, y float64, ok bool) {
	if sys, err := f.d.KB.GetSystem(ctx, sysID); err == nil && sys != nil && (sys.Position.X != 0 || sys.Position.Y != 0) {
		return sys.Position.X, sys.Position.Y, true
	}
	if run, err := sweep.LoadFrontierRun(f.run); err == nil {
		if ls := run.Systems[sysID]; ls != nil && (ls.X != 0 || ls.Y != 0) {
			return ls.X, ls.Y, true
		}
	}
	return f.s.LastX, f.s.LastY, f.s.LastX != 0 || f.s.LastY != 0
}

// launch burns cells if the tank is low, launches (or redirects) from (x, y)
// at (ax, ay) and measures the first launch's real cost.
func (f *frontierPass) launch(ctx context.Context, x, y, ax, ay float64) error {
	c, s := f.d.Client, f.s
	tank, maxTank, cells, premium := sweepFuel(c.GetState())
	for _, r := range sweep.Refills(tank, maxTank, cells, premium, s.LaunchCost) {
		if err := c.UseItem(ctx, r.Item, r.Qty); err != nil {
			f.logf("use %d %s: %v", r.Qty, r.Item, err)
		}
	}
	_ = c.GetShip(ctx) // the cached tank lags use_item; measure from the real level
	before, _, _, _ := sweepFuel(c.GetState())
	bearing := math.Round(sweep.BearingTo(x, y, ax, ay)*10) / 10
	if bearing >= 360 {
		bearing -= 360
	}
	res, err := c.Jump(ctx, strconv.FormatFloat(bearing, 'f', 1, 64))
	if err != nil {
		return fmt.Errorf("frontier_explore: launch on %.1f: %w", bearing, err)
	}
	if res == nil || !res.Drifting {
		return fmt.Errorf("frontier_explore: jump %.1f did not start a drift (is a pathfinder_drive fitted?)", bearing)
	}
	if err := c.GetShip(ctx); err == nil && !s.LaunchCostMeasured {
		after, _, _, _ := sweepFuel(c.GetState())
		if cost, ok := sweep.MeasuredLaunchCost(before, after); ok {
			s.LaunchCost, s.LaunchCostMeasured = cost, true
			f.logf("measured launch cost %.0f fuel", cost)
		}
	}
	s.AimX, s.AimY, s.PlannedBearing = ax, ay, bearing
	s.Drifted, s.ReadSinceLaunch = true, false
	f.d.markProgress()
	return nil
}

// drift polls the drift once a tick until the ship lands, the drift goes
// wrong (a storm turned it, or it passed its aim), or the pass budget ends.
func (f *frontierPass) drift(ctx context.Context, loc serverapi.LocationResponse) error {
	s := f.s
	for range sweepPassBudget {
		l := loc.Location
		x, y := l.TransitX, l.TransitY
		if x != s.LastX || y != s.LastY {
			f.d.markProgress()
		}
		s.LastX, s.LastY, s.Drifted, s.ReadSinceLaunch = x, y, true, true
		if gs := f.d.Client.GetState(); gs != nil {
			s.LastReadTick = gs.CurrentTick
		}
		turned := bearingDiff(l.TransitBearing, s.PlannedBearing) > sweepStormTolerance
		passed := math.Hypot(s.AimX-x, s.AimY-y) > sweep.DriftPerTick && sweep.Reached(x, y, s.AimX, s.AimY, l.TransitBearing)
		if turned || passed {
			return f.missed(ctx, x, y, l.TransitBearing)
		}
		settle(ctx, game.SleepTick)
		if ctx.Err() != nil {
			break
		}
		var err error
		if loc, err = f.d.sweepLocation(ctx); err != nil {
			return err
		}
		if !loc.Location.Drifting() {
			return nil // the next pass handles the landing
		}
	}
	return nil //nolint:nilerr // a cancelled pass just ends; the state is saved by the caller
}

// missed re-aims a drift that will not arrive: at the target again, at the
// next target once this one has had its launches, or at a station.
func (f *frontierPass) missed(ctx context.Context, x, y, bearing float64) error {
	s := f.s
	f.logf("drift missed its aim (bearing %.1f, plotted %.1f) at (%.0f, %.0f)", bearing, s.PlannedBearing, x, y)
	tank, _, cells, premium := sweepFuel(f.d.Client.GetState())
	if sweep.Total(tank, cells, premium) < s.LaunchCost {
		f.stranded(ctx, "adrift")
		return nil
	}
	if s.Phase == frontierRefuel || s.Phase == frontierHome {
		return f.headForStation(ctx, x, y, "")
	}
	f.nearer(x, y)
	if s.Launches < frontierMaxLaunches && f.fuelOK() {
		s.Phase = frontierLaunch
		f.logf("launch %d at %s from the drift", s.Launches+1, s.Target)
		if err := f.launch(ctx, x, y, s.TargetX, s.TargetY); err != nil {
			return err
		}
		f.launched(x, y)
		return nil
	}
	if s.Launches >= frontierMaxLaunches {
		f.unreachable()
		t, err := f.pickTarget(x, y)
		if err != nil {
			return err
		}
		if t == nil {
			f.logf("nothing left to explore; heading home")
			s.Phase, s.Target = frontierHome, ""
			return f.headForStation(ctx, x, y, "")
		}
		f.setTarget(t)
		if f.fuelOK() {
			s.Phase = frontierLaunch
			f.logf("next target %s; launch 1 from the drift", t.ID)
			if err := f.launch(ctx, x, y, t.X, t.Y); err != nil {
				return err
			}
			f.launched(x, y)
			return nil
		}
	}
	f.toRefuel(s.Target, "fuel is under three launches adrift")
	return f.headForStation(ctx, x, y, "")
}

// headForStation launches from (x, y) at the nearest station system,
// preferring one whose desk was last seen with fuel, never `leaving`.
func (f *frontierPass) headForStation(ctx context.Context, x, y float64, leaving string) error {
	d := f.d
	unlocked := smugglingUnlocked(d.Client.GetState())
	notLeaving := func(sys string) bool { return sys != leaving }
	id, ok, err := galaxy.NearestStationByDistance(ctx, d.KB, x, y, unlocked, d.sweepFuelledFilter(ctx, notLeaving))
	if err == nil && !ok {
		id, ok, err = galaxy.NearestStationByDistance(ctx, d.KB, x, y, unlocked, notLeaving)
	}
	if err != nil || !ok {
		return fmt.Errorf("frontier_explore: no station to head for from (%.0f,%.0f): %v", x, y, err)
	}
	sys, err := d.KB.GetSystem(ctx, id)
	if err != nil || sys == nil {
		return fmt.Errorf("frontier_explore: station system %s: %v", id, err)
	}
	f.logf("launching at the station system %s", id)
	return f.launch(ctx, x, y, sys.Position.X, sys.Position.Y)
}

// stranded asks for a tow, once, and waits.
func (f *frontierPass) stranded(ctx context.Context, where string) {
	f.halt = true
	if f.s.Distress {
		return
	}
	tank, _, cells, premium := sweepFuel(f.d.Client.GetState())
	f.logf("STRANDED %s with %.0f fuel (a launch costs %.0f) and no station in reach; asking for a tow", where, sweep.Total(tank, cells, premium), f.s.LaunchCost)
	if err := f.d.Client.DistressSignal(ctx, "tow"); err != nil {
		f.logf("distress signal: %v", err)
		return
	}
	f.s.Distress = true
}

// landed records a drift's landing and decides what the system is to the run.
func (f *frontierPass) landed(ctx context.Context, sysID string, loc serverapi.LocationResponse) {
	d, s := f.d, f.s
	var tick int64
	if gs := d.Client.GetState(); gs != nil {
		tick = gs.CurrentTick
	}
	fresh := s.ReadSinceLaunch && s.LastReadTick > 0 && tick > 0 && tick-s.LastReadTick <= sweepMeasureMaxAgeTicks
	if rec, ok := d.KB.(knowledge.SystemPositionRecorder); ok && fresh && sysID != "" {
		if wrote, err := rec.RecordMeasuredSystemPosition(ctx, sysID, loc.Location.SystemName, s.LastX, s.LastY); err != nil {
			f.logf("record position of %s: %v", sysID, err)
		} else if wrote {
			f.logf("%s measured at (%.1f, %.1f)", sysID, s.LastX, s.LastY)
		}
	}
	if n, err := knowledge.CaptureHostilesNearby(ctx, d.KB, loc.Location.NearbyRepeaters, sysID, loc.Location.POIID, d.AgentID, tick); err != nil {
		f.logf("hostile capture at %s: %v", sysID, err)
	} else if n > 0 {
		f.logf("%d repeater(s) recorded at %s", n, sysID)
	}
	d.sweepCollectChat(ctx, sysID)
	d.sweepCaptureFn()(ctx, sysID)
	s.Drifted = false
	d.markProgress()
	f.logf("drift landed in %s (phase %s, target %s)", sysID, s.Phase, s.Target)

	if s.Phase != frontierLaunch {
		return
	}
	switch {
	case sysID == s.Target:
		s.Phase = frontierWork
	case d.frontierChartedFn()(ctx, sysID):
		f.logf("%s is charted ground; relaunching at %s", sysID, s.Target)
	default:
		// A frontier system on the way: work it. The target stays todo and is
		// picked again when this cluster is finished.
		in := sweep.FrontierInput{ID: sysID, Name: loc.Location.SystemName}
		in.X, in.Y, _ = f.position(ctx, sysID)
		if err := sweep.AddSystem(f.run, in, "", d.AgentID, time.Now()); err != nil {
			f.logf("ledger: add %s: %v", sysID, err)
		}
		f.logf("captured by %s on the way to %s; exploring it first", sysID, s.Target)
		s.Target, s.TargetX, s.TargetY, s.Launches, s.LaunchDist = sysID, in.X, in.Y, 0, 0
		s.Cluster, s.Done = nil, nil
		s.Phase = frontierWork
	}
}

// --- working a cluster -----------------------------------------------------

// work claims the cluster the ship is in and explores it one step per pass:
// tour the system it is in, or fly by lanes to the next unfinished one.
func (f *frontierPass) work(ctx context.Context, sysID string, loc serverapi.LocationResponse) error {
	d, s := f.d, f.s
	run, err := sweep.LoadFrontierRun(f.run)
	if err != nil {
		return err
	}
	// The ledger is the record: a restart between its write and the state's
	// must not tour a system twice.
	sync := func() {
		for _, id := range s.Cluster {
			if ls := run.Systems[id]; ls != nil && (ls.Status == sweep.FrontierDone || ls.Status == sweep.FrontierIncomplete) {
				f.markDone(id)
			}
		}
	}
	pending := func() string {
		for _, id := range s.Cluster {
			if !slices.Contains(s.Done, id) {
				return id
			}
		}
		return ""
	}
	sync()
	// Outside the cluster with part of it unfinished (a lane trip cut short,
	// a restart mid-hop): go back to it rather than claim where the ship is.
	if !slices.Contains(s.Cluster, sysID) && pending() == "" {
		cur := run.Systems[sysID]
		// Written off as unreachable, yet the ship is in it: explore it.
		if cur != nil && cur.Status == sweep.FrontierIncomplete && strings.HasPrefix(cur.Reason, "unreachable") {
			if _, err = sweep.ReopenUnreachable(f.run, time.Now(), sysID); err != nil {
				return err
			}
			f.logf("%s was marked %s, but the ship is in it; exploring it", sysID, cur.Reason)
			cur.Status = sweep.FrontierTodo
		}
		switch {
		case cur == nil && d.frontierChartedFn()(ctx, sysID):
			f.logf("%s is charted ground; nothing to explore here", sysID)
			s.Phase = frontierNext
			return nil
		case cur == nil:
			in := sweep.FrontierInput{ID: sysID, Name: loc.Location.SystemName}
			in.X, in.Y, _ = f.position(ctx, sysID)
			if err = sweep.AddSystem(f.run, in, "", d.AgentID, time.Now()); err != nil {
				return err
			}
		case cur.Status == sweep.FrontierDone || cur.Status == sweep.FrontierIncomplete:
			f.logf("%s is already %s on the ledger; moving on", sysID, cur.Status)
			s.Phase = frontierNext
			return nil
		}
		ids, ok, cerr := sweep.ClaimCluster(f.run, d.AgentID, sysID, time.Now())
		if cerr != nil {
			return cerr
		}
		if !ok {
			f.logf("%s is claimed by another ship; leaving it", sysID)
			s.Phase = frontierNext
			return nil
		}
		s.Cluster, s.Done = ids, nil
		f.logf("claimed the cluster of %s: %d system(s) %v", sysID, len(ids), ids)
		if run, err = sweep.LoadFrontierRun(f.run); err != nil {
			return err
		}
		sync()
	}
	if slices.Contains(s.Cluster, sysID) && !slices.Contains(s.Done, sysID) {
		return f.tour(ctx, sysID, loc)
	}
	next := pending()
	if next == "" {
		f.logf("cluster finished: %d system(s)", len(s.Cluster))
		s.Cluster, s.Done = nil, nil
		s.Phase = frontierNext
		return nil
	}
	if !f.fuelOK() {
		f.toRefuel(next, "fuel is under three launches before the lane jump to "+next)
		return nil
	}
	f.logf("lanes to %s (%d of %d in the cluster done)", next, len(s.Done), len(s.Cluster))
	err = f.nav(ctx, next, "")
	switch {
	case err == nil:
		d.markProgress()
	case errors.Is(err, ErrInsufficientRouteFuel):
		f.toRefuel(next, fmt.Sprintf("the lane trip to %s was refused for fuel (%v)", next, err))
		return nil
	case navRefused(err):
		f.logf("%s is unroutable from %s (%v); marked incomplete", next, sysID, err)
		f.finish(sweep.FrontierSystem{ID: next, Status: sweep.FrontierIncomplete, Reason: "unreachable by lane"})
		f.markDone(next)
		return nil
	}
	return err
}

func (f *frontierPass) markDone(id string) {
	if !slices.Contains(f.s.Done, id) {
		f.s.Done = append(f.s.Done, id)
	}
}

// finish writes a system's outcome to the ledger.
func (f *frontierPass) finish(fs sweep.FrontierSystem) {
	if err := sweep.FinishSystem(f.run, f.d.AgentID, fs, time.Now()); err != nil {
		f.logf("ledger: %v", err)
	}
}

// tour explores the system the ship is in and records the outcome.
func (f *frontierPass) tour(ctx context.Context, sysID string, loc serverapi.LocationResponse) error {
	d := f.d
	hostile := func(what string) {
		f.logf("HOSTILE at %s (%s); not touring, marked incomplete", sysID, what)
		f.finish(sweep.FrontierSystem{ID: sysID, Status: sweep.FrontierIncomplete, Reason: "hostile: " + what})
		f.markDone(sysID)
	}
	// A leviathan or a repeater first: the first command is the way out.
	if lev := d.sweepLeviathanHere(ctx); lev != "" {
		hostile(lev)
		return nil
	}
	if n := len(loc.Location.NearbyRepeaters); n > 0 {
		hostile(fmt.Sprintf("%d repeater(s)", n))
		return nil
	}
	s := f.s
	res, err := d.frontierTourFn()(ctx)
	if err != nil {
		if s.TrySystem != sysID {
			s.TrySystem, s.Tries = sysID, 0
		}
		s.Tries++
		if s.Tries < frontierMaxTries {
			return fmt.Errorf("frontier_explore: explore %s (try %d of %d): %w", sysID, s.Tries, frontierMaxTries, err)
		}
		f.logf("%s could not be explored in %d tries (%v); marked incomplete", sysID, s.Tries, err)
		f.finish(sweep.FrontierSystem{ID: sysID, Status: sweep.FrontierIncomplete, Reason: fmt.Sprintf("tour failed %d times: %v", s.Tries, err)})
		f.markDone(sysID)
		s.TrySystem, s.Tries = "", 0
		return nil
	}
	d.markProgress()
	visited := 0
	var why []string
	for _, p := range res.POIs {
		if p.Visited {
			visited++
		} else if p.Skipped != "" && !slices.Contains(why, p.Skipped) {
			why = append(why, p.Skipped)
		}
	}
	out := sweep.FrontierSystem{ID: sysID, SurveyPower: res.SurveyPower, POIsSeen: len(res.POIs),
		POIsVisited: visited, Relics: res.Relics, Skipped: res.SkippedIDs()}
	switch {
	case res.Stopped == explore.StoppedBattle:
		if berr := d.Client.Battle(ctx, "stance", map[string]any{"stance": battleStanceFlee}); berr != nil && !notInBattle(berr) {
			f.logf("flee stance: %v", berr)
		}
		f.logf("HOSTILE at %s (battle); fleeing, marked incomplete", sysID)
		out.Status, out.Reason = sweep.FrontierIncomplete, "hostile: battle"
	case res.SurveyPower < frontierMinPower:
		// A scanner is offline or missing: nothing surveyed from here on
		// would count, so stop until the operator looks.
		d.frontierHalt = fmt.Sprintf("survey power %d at %s is under %d", res.SurveyPower, sysID, frontierMinPower)
		f.logf("STOPPED: %s; %s is not marked done. Check the scanners, then restart the worker.", d.frontierHalt, sysID)
		return nil
	case res.Stopped == explore.StoppedHull || res.Stopped == explore.StoppedFuel:
		what := map[string]string{
			explore.StoppedHull: fmt.Sprintf("hull at the %d%% floor with no repair kits", int(frontierHullFloor*100)),
			explore.StoppedFuel: "out of fuel for in-system travel",
		}[res.Stopped]
		if s.StopSystem != sysID {
			s.StopSystem, s.Stops = sysID, 0
		}
		if s.Stops < frontierMaxStops {
			// One station stop per system: the tour starts over on return.
			s.Stops++
			f.toRefuel(sysID, what+" at "+sysID)
			return nil
		}
		// It stopped again after a station stop: this system costs more than
		// a full hull or tank. Record what was reached and move on.
		out.Status, out.Reason = sweep.FrontierIncomplete, fmt.Sprintf("%s again after a station stop; %d POI(s) not visited", what, len(out.Skipped))
	case len(out.Skipped) > 0:
		out.Status, out.Reason = sweep.FrontierIncomplete, fmt.Sprintf("%d POI(s) not visited: %s", len(out.Skipped), strings.Join(why, "; "))
	default:
		out.Status = sweep.FrontierDone
	}
	f.finish(out)
	f.markDone(sysID)
	s.StopSystem, s.Stops, s.TrySystem, s.Tries = "", 0, "", 0
	f.logf("%s %s: survey power %d, %d/%d POIs, %d relic(s)%s", sysID, out.Status, out.SurveyPower, visited, len(res.POIs), res.Relics,
		map[bool]string{true: " — " + out.Reason, false: ""}[out.Reason != ""])
	f.discoverLanes(ctx, sysID)
	return nil
}

// discoverLanes adds the frontier systems this system's lanes lead to. Lanes
// into charted space are not followed.
func (f *frontierPass) discoverLanes(ctx context.Context, sysID string) {
	d, s := f.d, f.s
	st := d.Client.GetState()
	if st == nil || st.System.ID != sysID {
		return
	}
	hx, hy, _ := f.position(ctx, sysID)
	for _, c := range st.System.Connections {
		if c.SystemID == "" || c.SystemID == sysID {
			continue
		}
		if d.frontierChartedFn()(ctx, c.SystemID) {
			f.logf("lane %s -> %s leads into charted space; not followed", sysID, c.SystemID)
			continue
		}
		in := sweep.FrontierInput{ID: c.SystemID, Name: c.Name, X: hx, Y: hy}
		if sys, err := d.KB.GetSystem(ctx, c.SystemID); err == nil && sys != nil && (sys.Position.X != 0 || sys.Position.Y != 0) {
			in.X, in.Y = sys.Position.X, sys.Position.Y
		}
		if err := sweep.AddSystem(f.run, in, sysID, d.AgentID, time.Now()); err != nil {
			f.logf("ledger: add %s: %v", c.SystemID, err)
		}
	}
	// Claim what the new lanes joined on. Another ship's claim among them
	// leaves the cluster as it was: those systems are theirs.
	ids, ok, err := sweep.ClaimCluster(f.run, d.AgentID, sysID, time.Now())
	if err != nil || !ok {
		return
	}
	for _, id := range ids {
		if !slices.Contains(s.Cluster, id) {
			s.Cluster = append(s.Cluster, id)
			f.logf("lane found: %s joins the cluster", id)
		}
	}
}

// --- stations --------------------------------------------------------------

// refuel fills the tank, repairs and restocks kits at a station in this
// system and turns back for ResumeAt; with no station here it goes to one.
func (f *frontierPass) refuel(ctx context.Context, sysID string) error {
	d, s := f.d, f.s
	poi := d.sweepStationPOI(ctx, sysID)
	if poi == "" {
		return f.toStation(ctx, sysID)
	}
	if err := f.dockAt(ctx, sysID, poi); err != nil {
		return err
	}
	withdrawRepairKits(ctx, d.Client, frontierKits-int(cargoQuantity(d.Client.GetState(), repairKitItem)), d.Out)
	s.Distress = false
	if st := d.Client.GetState(); st != nil && st.Ship.MaxHull > 0 && float64(st.Ship.Hull) <= frontierHullFloor*float64(st.Ship.MaxHull) {
		// Going back out at the floor only repeats the stop. Docked is safe:
		// hold here and try the repair again next pass.
		f.logf("hull %v/%v is still at the %d%% floor after the station at %s; holding here until it is repaired",
			st.Ship.Hull, st.Ship.MaxHull, int(frontierHullFloor*100), sysID)
		f.halt = true
		return nil
	}
	if !f.fuelOK() {
		// A dry desk: try the next station by lanes before waiting here.
		f.logf("the station at %s left the tank under three launches", sysID)
		return f.toStation(ctx, sysID)
	}
	run, err := sweep.LoadFrontierRun(f.run)
	if err != nil {
		return err
	}
	resume := run.Systems[s.ResumeAt]
	s.ResumeAt = ""
	if resume == nil || resume.Status == sweep.FrontierDone || resume.Status == sweep.FrontierIncomplete {
		s.Phase = frontierNext
		return nil
	}
	f.setTarget(resume)
	f.logf("refuelled at %s; back to %s", sysID, resume.ID)
	return nil
}

// dockAt flies to a station in the system the ship is in, docks, refuels and
// repairs.
func (f *frontierPass) dockAt(ctx context.Context, sysID, poi string) error {
	d := f.d
	if st := d.Client.GetState(); st == nil || (!st.Doc && st.Ship.DockedAtBase == "") {
		if err := f.nav(ctx, sysID, poi); err != nil {
			return fmt.Errorf("frontier_explore: reach station at %s: %w", sysID, err)
		}
		waitOutHold(ctx, d.Out, d.Client)
		if err := waitOutPending(ctx, func() error { return dockIdempotent(ctx, d.Client) }); err != nil {
			return fmt.Errorf("frontier_explore: dock at %s: %w", sysID, err)
		}
	}
	return exploreRefuelDocked(ctx, ExploreDeps{Client: d.Client, KB: d.KB}, d.Out)
}

// toStation moves toward a station: by lanes when one is lane-reachable, by
// a pathfinder launch otherwise. A ship that can do neither asks for a tow.
func (f *frontierPass) toStation(ctx context.Context, sysID string) error {
	d := f.d
	graph := &galaxy.GalaxyGraph{}
	if err := graph.BuildFromDB(ctx, d.KB); err == nil {
		if to := d.sweepLaneHome(ctx, graph, sysID); to != "" {
			f.logf("lanes to the station system %s", to)
			err := f.nav(ctx, to, "")
			if err == nil {
				d.markProgress()
				return nil
			}
			if !navRefused(err) {
				return err
			}
			f.logf("no lane route to %s (%v)", to, err)
		}
	}
	tank, _, cells, premium := sweepFuel(d.Client.GetState())
	x, y, ok := f.position(ctx, sysID)
	if !ok || sweep.Total(tank, cells, premium) < f.s.LaunchCost {
		f.stranded(ctx, "at "+sysID)
		return nil
	}
	return f.headForStation(ctx, x, y, sysID)
}

// next picks the nearest unexplored system: in the ship's wedge while one is
// left there, then anywhere. With nothing left it goes home.
func (f *frontierPass) next(ctx context.Context, sysID string) error {
	x, y, _ := f.position(ctx, sysID)
	t, err := f.pickTarget(x, y)
	if err != nil {
		return err
	}
	f.s.Cluster, f.s.Done = nil, nil
	if t == nil {
		f.logf("nothing left to explore; heading home")
		f.s.Phase, f.s.Target = frontierHome, ""
		return nil
	}
	f.setTarget(t)
	f.logf("next target %s (wedge %s, %.0f LY away)", t.ID, t.Wedge, math.Hypot(t.X-x, t.Y-y))
	return nil
}

// pickTarget is the ship's own nearest unfinished claim, so none is left
// orphaned, else the nearest todo system (its wedge first).
func (f *frontierPass) pickTarget(x, y float64) (*sweep.FrontierSystem, error) {
	t, err := sweep.OwnClaim(f.run, f.d.AgentID, x, y)
	if err != nil || t != nil {
		return t, err
	}
	return sweep.NextFrontierTarget(f.run, f.d.FrontierWedge, x, y)
}

// home returns to Conjunction Junction and ends the run. A ship that cannot
// route there ends at the station it reached instead.
func (f *frontierPass) home(ctx context.Context, sysID string) error {
	d, s := f.d, f.s
	end := func(poi string) error {
		if err := f.dockAt(ctx, sysID, poi); err != nil {
			return err
		}
		if err := sweep.ReleaseAgent(f.run, d.AgentID, time.Now()); err != nil {
			f.logf("ledger: %v", err)
		}
		s.Phase = frontierDone
		f.logf("run complete; docked at %s", sysID)
		return nil
	}
	if sysID == huntHomeSystem {
		return end(huntHomePOI)
	}
	err := f.nav(ctx, huntHomeSystem, "")
	if err == nil {
		d.markProgress()
		return nil
	}
	if !navRefused(err) {
		return err
	}
	if poi := d.sweepStationPOI(ctx, sysID); poi != "" {
		f.logf("no lane route home from %s (%v); ending the run here", sysID, err)
		return end(poi)
	}
	return f.toStation(ctx, sysID)
}

// --- seams -----------------------------------------------------------------

// frontierTourFn explores the system the ship is in. A survey that did not
// run is an error (the next pass tries again), not a low-power reading.
func (d *WorkerDispatch) frontierTourFn() func(context.Context) (explore.Result, error) {
	if d.frontierTour != nil {
		return d.frontierTour
	}
	return func(ctx context.Context) (explore.Result, error) {
		deps := workerExploreDeps(ExploreDeps{Client: d.Client, KB: d.KB, Out: d.Out, AgentID: d.AgentID, Market: d.Market})
		sr := explore.Survey(ctx, deps)
		if !sr.Ran {
			return explore.Result{}, fmt.Errorf("survey did not run: %s", sr.Problem)
		}
		if sr.Power < frontierMinPower {
			return explore.Result{SurveyPower: sr.Power, Revealed: sr.Revealed}, nil
		}
		res, err := explore.Tour(ctx, deps, explore.Options{RefuelAtStations: true, EnterHazards: true,
			HullFloor: frontierHullFloor, Relics: true}, sr)
		if err != nil && strings.Contains(err.Error(), "no POIs in system") {
			// Surveyed at full power and nothing there: explored, with no POIs.
			return res, nil
		}
		return res, err
	}
}

// frontierSurveyFn reads the survey power where the ship is, undocking first
// (a docked ship cannot survey). The caller docks again.
func (d *WorkerDispatch) frontierSurveyFn() func(context.Context) (int, error) {
	if d.frontierSurvey != nil {
		return d.frontierSurvey
	}
	return func(ctx context.Context) (int, error) {
		if st := d.Client.GetState(); st != nil && (st.Doc || st.Ship.DockedAtBase != "") {
			if err := d.Client.Undock(ctx); err != nil {
				return 0, fmt.Errorf("undock: %w", err)
			}
			settle(ctx, game.SleepUndock)
		}
		sr := explore.Survey(ctx, workerExploreDeps(ExploreDeps{Client: d.Client, KB: d.KB, Out: d.Out, AgentID: d.AgentID}))
		if !sr.Ran {
			return 0, fmt.Errorf("survey did not run: %s", sr.Problem)
		}
		return sr.Power, nil
	}
}

// frontierNavFn flies by jump lanes, refusing routes through the systems in
// avoid (on top of the operator's avoid file).
func (d *WorkerDispatch) frontierNavFn() func(ctx context.Context, system, poi string, avoid map[string]string) error {
	if d.frontierNav != nil {
		return d.frontierNav
	}
	return func(ctx context.Context, system, poi string, avoid map[string]string) error {
		return Autopilot(ctx, AutopilotDeps{Client: d.Client, Out: d.Out, KB: d.KB,
			AvoidSystemsPath: d.AvoidSystemsPath, ExtraAvoid: avoid}, system, poi)
	}
}

// frontierChartedFn reports whether a system is charted (map) ground, which
// the run neither explores nor follows lanes into.
func (d *WorkerDispatch) frontierChartedFn() func(context.Context, string) bool {
	if d.frontierCharted != nil {
		return d.frontierCharted
	}
	return d.sweepAPISystem
}
