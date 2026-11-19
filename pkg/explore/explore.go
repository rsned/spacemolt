package explore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Deps is what an explore runs against. Client is required; everything else
// has a working default.
//
// The capture steps are functions because the callers differ in what a capture
// includes (play_as also writes intel files and tags its market rows), and the
// package that holds those captures already depends on this one.
type Deps struct {
	Client  game.GameClient
	KB      knowledge.Base // nil skips every KB write
	AgentID string
	Report  Reporter // nil discards

	// Tick is the game tick stamped on what is saved; nil reads client state.
	Tick func() int64
	// Settle waits for a tick-deferred reply to land; nil sleeps (ctx-aware).
	Settle func(ctx context.Context, d time.Duration)
	// HasScanner gates the survey; nil checks the fit and the hull catalog.
	HasScanner func(*game.State) bool
	// UpdateSystem re-reads the current system and saves it; nil only re-reads.
	UpdateSystem func(ctx context.Context) error
	// UpdatePOI captures the POI the ship is at; nil captures nothing.
	UpdatePOI func(ctx context.Context) error
	// StationCapture captures the station the ship is docked at; nil captures
	// nothing.
	StationCapture func(ctx context.Context) error
	// Travel flies to a POI in the current system; nil uses Client.Travel.
	Travel func(ctx context.Context, poiID string) (canceled bool, err error)
	// OnCreatures sees every get_nearby creature list, after it is saved.
	OnCreatures func(creatures []serverapi.NearbyCreature, systemID, poiID string, tick int64)
}

func (d Deps) withDefaults() Deps {
	if d.Report == nil {
		d.Report = discard{}
	}
	if d.Tick == nil {
		client := d.Client
		d.Tick = func() int64 {
			if st := client.GetState(); st != nil {
				return st.GetTick()
			}
			return 0
		}
	}
	if d.Settle == nil {
		d.Settle = func(ctx context.Context, wait time.Duration) {
			select {
			case <-ctx.Done():
			case <-time.After(wait):
			}
		}
	}
	if d.HasScanner == nil {
		kb := d.KB
		d.HasScanner = func(st *game.State) bool { return HasSurveyScanner(context.Background(), kb, st) }
	}
	if d.UpdateSystem == nil {
		client := d.Client
		d.UpdateSystem = func(ctx context.Context) error {
			if err := client.GetSystem(ctx); err != nil {
				return fmt.Errorf("get_system failed: %w", err)
			}
			return nil
		}
	}
	if d.Travel == nil {
		client := d.Client
		d.Travel = func(ctx context.Context, poiID string) (bool, error) {
			res, err := client.Travel(ctx, poiID)
			if err != nil {
				return false, err
			}
			return res != nil && res.Canceled, nil
		}
	}
	return d
}

// Options select what a tour does beyond visiting and capturing every POI.
type Options struct {
	RefuelAtStations bool    // refuel at every station dock
	StopOnUnscanned  bool    // stop at a POI holding a never-scanned species
	EnterHazards     bool    // visit hazardous POIs for one dose instead of skipping them
	HullFloor        float64 // skip POIs while hull/max is at or below this; 0 disables
	Relics           bool    // run interact_poi at relic POIs
	// SkipHazardSeverity skips POIs whose damaging hazard is at or above this
	// severity, unless EnterHazards; 0 skips none.
	SkipHazardSeverity int
	// NoEnterSeverity never enters a POI whose damaging hazard is at or above
	// this severity, EnterHazards or not; 0 disables. A 25%-a-dose POI kills in
	// four ticks, and a visit is rarely over in one.
	NoEnterSeverity int
	// SkipOutposts visits and captures an outpost without docking: outposts
	// refuse outsiders.
	SkipOutposts bool
}

// poiTypeStation is the POI type the tour docks at.
const poiTypeStation = "station"

// Why a tour ended early (Result.Stopped).
const (
	StoppedFuel      = "fuel"      // a hop was refused for fuel
	StoppedBattle    = "battle"    // the ship is in a battle
	StoppedUnscanned = "unscanned" // Options.StopOnUnscanned found a new species
	StoppedHull      = "hull"      // hull at the floor and no way to repair
)

// POIResult is what happened at one POI of the tour.
type POIResult struct {
	ID, Name, Type string
	Visited        bool
	Skipped        string // why it was not visited; "" when visited
}

// Result is the outcome of one system's explore.
type Result struct {
	SystemID    string
	SystemName  string
	SurveyPower int
	Revealed    int
	POIs        []POIResult
	Relics      int      // relic interactions attempted
	Stopped     string   // "" when the tour finished, else a Stopped* reason
	Unscanned   []string // species behind a StoppedUnscanned
	StoppedAt   string   // POI id where the tour stopped
}

// SkippedIDs lists the POIs the tour did not visit.
func (r Result) SkippedIDs() []string {
	var ids []string
	for _, p := range r.POIs {
		if !p.Visited {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

// System explores the system the ship is in: it surveys first, so the hidden
// POIs the survey reveals are in the route, then tours every POI.
func System(ctx context.Context, d Deps, o Options) (Result, error) {
	return Tour(ctx, d, o, Survey(ctx, d))
}

// Tour visits every POI of the current system in nearest-neighbour order,
// capturing each one, and docks at every station for a full capture. sr is the
// survey that preceded it; a zero SurveyResult is fine.
//
// It returns an error only when there is nothing to tour or a hop was
// interrupted; an early stop is reported in Result.Stopped.
func Tour(ctx context.Context, d Deps, o Options, sr SurveyResult) (Result, error) {
	d = d.withDefaults()
	res := Result{SurveyPower: sr.Power, Revealed: sr.Revealed}

	// Record the system itself (not only its POIs): a system without a station
	// is otherwise never saved. Only the read can fail the tour.
	if err := d.UpdateSystem(ctx); err != nil {
		if strings.Contains(err.Error(), "get_system failed") {
			return res, err
		}
		d.Report.Printf("  (update_system failed: %v)\n", err)
	}

	state := d.Client.GetState()
	if state == nil || state.System.ID == "" {
		return res, fmt.Errorf("no system data available")
	}
	res.SystemID, res.SystemName = state.System.ID, state.System.Name
	pois := state.System.POIs
	if len(pois) == 0 {
		return res, fmt.Errorf("no POIs in system %s", state.System.Name)
	}

	route := PlanRoute(pois, state.CurrentPOI)
	reportRoute(d, state, route)
	res.POIs = make([]POIResult, len(route))
	for i, p := range route {
		res.POIs[i] = POIResult{ID: p.ID, Name: p.Name, Type: p.Type}
	}

	startTime := time.Now()
	t := &tour{d: d, o: o, route: route, res: &res}
	if st := d.Client.GetState(); st != nil {
		t.doseSeen = st.LastHazardDose.At
	}
	stop, err := t.run(ctx)
	if err != nil {
		return res, err
	}
	if stop {
		return res, nil
	}

	_ = d.Client.GetStatus(ctx)
	d.Report.Printf("\nExploration of %s complete: %d POIs in %s\n",
		state.System.Name, len(route), formatDuration(int(time.Since(startTime).Seconds())))
	return res, nil
}

// run visits the route, retries the POIs skipped at the hull floor once, and
// leaves the ship off any hazard. stop means Result.Stopped says why the tour
// ended early.
func (t *tour) run(ctx context.Context) (stop bool, err error) {
	for i := range t.route {
		if stop, err = t.visit(ctx, i); stop || err != nil {
			break
		}
	}
	if err == nil && !stop && len(t.floorSkipped) > 0 {
		// One more try: a station stop on the way may have repaired the hull.
		retry := t.floorSkipped
		t.floorSkipped = nil
		for _, i := range retry {
			t.res.POIs[i].Skipped = ""
			if stop, err = t.visit(ctx, i); stop || err != nil {
				break
			}
		}
	}
	if err != nil {
		return false, err
	}
	if t.res.Stopped != StoppedBattle {
		t.leaveHazard(ctx)
		t.topUpAfterDose(ctx)
	}
	if !stop && len(t.floorSkipped) > 0 {
		t.d.Report.Printf("  Hull at the floor and nothing left to repair with — %d POI(s) not visited\n", len(t.floorSkipped))
		t.res.Stopped, t.res.StoppedAt = StoppedHull, t.route[t.floorSkipped[0]].ID
		return true, nil
	}
	return stop, nil
}

// skip records and logs a POI the tour will not visit.
func (t *tour) skip(i int, why string) {
	t.res.POIs[i].Skipped = why
	t.d.Report.Printf("  skipped %s: %s\n", t.route[i].Name, why)
}

// visit travels to route[i] and captures it, unless a rule skips it.
func (t *tour) visit(ctx context.Context, i int) (stop bool, err error) {
	d, o, poi, res := t.d, t.o, t.route[i], t.res
	if err := ctx.Err(); err != nil {
		return false, err
	}
	st := d.Client.GetState()
	if st != nil && st.InBattle {
		d.Report.Printf("  In a battle — stopping the tour at %s\n", poi.Name)
		res.Stopped, res.StoppedAt = StoppedBattle, poi.ID
		return true, nil
	}

	if damaging(poi) && o.NoEnterSeverity > 0 && poi.Hazard.Severity >= o.NoEnterSeverity {
		t.skip(i, fmt.Sprintf("%s hazard severity %d (%d%% a dose)", poi.Hazard.Type, poi.Hazard.Severity, poi.Hazard.DosePct()))
		return false, nil
	}
	if damaging(poi) && !o.EnterHazards && o.SkipHazardSeverity > 0 && poi.Hazard.Severity >= o.SkipHazardSeverity {
		t.skip(i, fmt.Sprintf("%s hazard severity %d", poi.Hazard.Type, poi.Hazard.Severity))
		return false, nil
	}
	// The hull floor. A station is exempt: docking is how a hull gets repaired.
	// A damaging POI needs its dose on top, so the visit does not itself take
	// the hull under the floor.
	if o.HullFloor > 0 && poi.Type != poiTypeStation {
		need, why := o.HullFloor, ""
		if damaging(poi) {
			need += float64(poi.Hazard.DosePct()) / 100
			why = fmt.Sprintf(", %s costs %d%% a dose", poi.Hazard.Type, poi.Hazard.DosePct())
		}
		frac, _ := readHull(ctx, d)
		if frac <= need {
			t.leaveHazard(ctx)
			frac, _ = repairToFloor(ctx, d, need)
		}
		if frac <= need {
			t.skip(i, "hull "+hullPct(frac)+why)
			t.floorSkipped = append(t.floorSkipped, i)
			return false, nil
		}
	}

	if st != nil && st.CurrentPOI == poi.ID {
		d.Report.Printf("[%d/%d] Already at %s\n", i+1, len(t.route), poi.Name)
	} else {
		d.Report.Printf("[%d/%d] Traveling to %s (%s)...\n", i+1, len(t.route), poi.Name, poi.Type)
		canceled, err := d.Travel(ctx, poi.ID)
		if err != nil {
			d.Report.Printf("  Travel failed: %v\n", err)
			res.POIs[i].Skipped = "travel failed: " + err.Error()
			if insufficientFuel(err) {
				d.Report.Printf("  Out of fuel for in-system travel — stopping the tour\n")
				res.Stopped, res.StoppedAt = StoppedFuel, poi.ID
				markUnvisited(res.POIs, "tour stopped: fuel")
				return true, nil
			}
			// Back off a full tick before the next attempt — a fast retry
			// loop here triggers server rate-limiting when Travel returns
			// prematurely on a state-flag desync.
			d.Settle(ctx, game.SleepTick)
			return false, nil
		}
		if canceled {
			d.Report.Printf("  Travel interrupted!\n")
			return false, fmt.Errorf("explore interrupted at POI %d/%d", i+1, len(t.route))
		}
		if raw := d.Client.GetRawJSON("_last"); raw != nil {
			d.Report.Raw(RawExplore, raw)
		}
	}
	if !damaging(poi) {
		t.topUpAfterDose(ctx)
	}

	// Now at this POI: capture other players here, then the wildlife the same
	// reply lists. This is the only headcount that names a POI —
	// survey_system's census is system-wide — so it ties a species to a
	// habitat.
	Sightings(ctx, d, d.Client.GetNearby, "get_nearby")
	unscanned := WildlifeAtPOI(ctx, d, poi.ID, poi.Type)
	// Halt BEFORE the POI's dock/update work: creatures drift or despawn, so
	// the moment of sighting is the moment to hand control back.
	if o.StopOnUnscanned && len(unscanned) > 0 {
		res.Stopped, res.StoppedAt, res.Unscanned = StoppedUnscanned, poi.ID, unscanned
		return true, nil
	}

	outpost := poi.HasBase && poi.BaseType == game.BaseTypeOutpost
	if poi.Type == poiTypeStation && (!o.SkipOutposts || !outpost) {
		stationStop(ctx, d, o, poi)
	} else if d.UpdatePOI != nil {
		if err := d.UpdatePOI(ctx); err != nil {
			d.Report.Printf("  (update_poi failed: %v)\n", err)
		}
	}
	res.POIs[i].Visited = true
	if o.Relics && relicStep(ctx, d, poi) {
		res.Relics++
	}

	// Second wildlife reading, now that the POI's work has put real time
	// between the two samples.
	unscanned = WildlifeSecondLook(ctx, d, poi.ID, poi.Type)
	if o.StopOnUnscanned && len(unscanned) > 0 {
		res.Stopped, res.StoppedAt, res.Unscanned = StoppedUnscanned, poi.ID, unscanned
		return true, nil
	}
	return false, nil
}

// stationStop docks at the station the ship is at, runs the full capture,
// refuels when asked, and undocks. A refused dock just moves the tour on.
func stationStop(ctx context.Context, d Deps, o Options, poi game.POI) {
	collect := func() {
		if raw := d.Client.GetRawJSON("_last"); raw != nil {
			d.Report.Raw(RawExplore, raw)
		}
	}
	d.Report.Printf("  Docking at %s...\n", poi.Name)
	if err := d.Client.Dock(ctx); err != nil {
		d.Report.Printf("  Dock failed: %v (continuing)\n", err)
		return
	}
	d.Settle(ctx, game.SleepQuick)
	collect()
	if d.StationCapture != nil {
		if err := d.StationCapture(ctx); err != nil {
			d.Report.Printf("  (update_all failed: %v)\n", err)
		}
	}
	full := false
	if st := d.Client.GetState(); st != nil {
		f, m := st.GetFuel()
		full = m > 0 && f >= m
	}
	if o.RefuelAtStations && !full {
		d.Report.Printf("  Refueling...\n")
		// Tank-full is an expected "error" here, not worth surfacing.
		if err := d.Client.Refuel(ctx); err != nil && !tankFull(err) {
			d.Report.Printf("  Refuel warning: %v\n", err)
		}
		d.Settle(ctx, game.SleepQuick)
		collect()
	}
	d.Report.Printf("  Undocking...\n")
	if err := d.Client.Undock(ctx); err != nil {
		d.Report.Printf("  Undock failed: %v\n", err)
	}
	d.Settle(ctx, game.SleepTick)
	collect()
}

// reportRoute prints the planned route with distances and tick estimates.
func reportRoute(d Deps, state *game.State, route []game.POI) {
	speed := state.Ship.Speed
	if speed <= 0 {
		speed = 1
	}
	d.Report.Printf("\nExploring %d POIs in %s:\n\n", len(route), state.System.Name)
	d.Report.Printf("  %-3s %-28s %-16s %10s %10s\n", "#", "POI", "Type", "Dist (AU)", "Est. Ticks")
	d.Report.Printf("  %s\n", strings.Repeat("-", 71))

	var totalDist float64
	var totalTicks int
	prevPos := POIPosition(state.System.POIs, state.CurrentPOI)
	for i, poi := range route {
		dist := Distance(prevPos, poi.Position)
		ticks := max(int(math.Ceil(dist/speed)), 1)
		if i == 0 && poi.ID == state.CurrentPOI {
			dist, ticks = 0, 0
		}
		totalDist += dist
		totalTicks += ticks

		marker := ""
		if poi.Type == poiTypeStation {
			marker = " *"
		}
		d.Report.Printf("  %-3d %-28s %-16s %9.1f %9d%s\n",
			i+1, truncateName(poi.Name, 28), poi.Type, dist, ticks, marker)
		prevPos = poi.Position
	}
	d.Report.Printf("  %s\n", strings.Repeat("-", 71))
	d.Report.Printf("  %-48s %9.1f %9d\n", "Total", totalDist, totalTicks)
	d.Report.Printf("\n  Est. time: ~%s (* = station, will dock for full update)\n\n", formatDuration(totalTicks*10))
}

func markUnvisited(pois []POIResult, why string) {
	for i := range pois {
		if !pois[i].Visited && pois[i].Skipped == "" {
			pois[i].Skipped = why
		}
	}
}

// insufficientFuel reports whether a travel was refused for fuel.
func insufficientFuel(err error) bool {
	s := err.Error()
	return strings.Contains(s, "no_fuel") || strings.Contains(s, "nsufficient fuel")
}

// tankFull reports whether a refuel was refused because the tank is full.
func tankFull(err error) bool {
	var se *game.ServerError
	if errors.As(err, &se) && se.Code == "tank_full" {
		return true
	}
	var goal *game.GoalReachedError
	if errors.As(err, &goal) && goal.Code == "tank_full" {
		return true
	}
	return strings.Contains(err.Error(), "tank_full")
}

// truncateName truncates a string to maxLen runes, adding "..." if needed.
func truncateName(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// formatDuration renders seconds as "45s", "3m" or "3m 20s".
func formatDuration(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	m, s := seconds/60, seconds%60
	if s == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dm %ds", m, s)
}
