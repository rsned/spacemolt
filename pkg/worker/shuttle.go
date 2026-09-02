package worker

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// DefaultShuttleMaxJumps bounds how far (in jumps) a shuttle will ferry a
// passenger batch. Passenger fares scale with distance (and first-class long-hauls
// pay a large lump plus a speedy-delivery bonus), and the ranking already favors
// the biggest total fare, so this is a generous safety bound — effectively "any
// reachable, non-stronghold destination" for this galaxy — not a turnover throttle.
const DefaultShuttleMaxJumps = 40

// shuttleRepositionThreshold is how many consecutive passes a shuttle may board
// nobody — station barren, or every fare out-competed — before it relocates to a
// neighboring system to hunt fresh passengers instead of idling in place. Kept
// high so the shuttle CAMPS at a populated hub: passenger demand is bursty and
// every fare is contested, so presence over many passes wins more fares than
// wandering off after a couple of dry passes (which historically left the shuttle
// hunting low-demand systems while the hub refilled behind it).
const shuttleRepositionThreshold = 10

// shuttleState carries cross-pass shuttle memory so a single worker's
// idle→idle→reposition cadence survives between standing-loop passes: how many
// consecutive passes boarded nobody, and a rotating cursor over neighbor systems
// so repositioning fans out instead of fixating on one neighbor.
type shuttleState struct {
	mu        sync.Mutex
	dryPasses int
	cursor    int
}

// dry records a boardless pass and returns the running consecutive count.
func (s *shuttleState) dry() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dryPasses++
	return s.dryPasses
}

// boarded resets the dry-pass streak after a successful board.
func (s *shuttleState) boarded() { s.resetDry() }

func (s *shuttleState) resetDry() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.dryPasses = 0
	s.mu.Unlock()
}

// nextNeighbor advances the rotating cursor and returns an index into a neighbor
// list of length n (n must be > 0).
func (s *shuttleState) nextNeighbor(n int) int {
	if s == nil || n <= 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.cursor % n
	s.cursor++
	return i
}

// ShuttleDeps are the injected collaborators for one Shuttle step.
type ShuttleDeps struct {
	Client   game.GameClient
	KB       knowledge.Base
	Out      io.Writer // nil -> io.Discard
	AgentID  string
	MaxJumps int // 0 -> DefaultShuttleMaxJumps
	// Now returns the current wall-clock time (nil -> time.Now). Injected for tests.
	Now func() time.Time
	// Treasury rate-limits faction-treasury rescue withdrawals across passes.
	Treasury *treasuryRescue
	// State carries cross-pass shuttle memory (dry-pass streak + reposition
	// cursor). nil disables repositioning (the shuttle just idles in place).
	State *shuttleState
	// SetActivity publishes the status-page "current activity" string (nil in
	// tests). Set when passengers are boarded, cleared ("") on an idle pass.
	SetActivity func(string)
	// Access is the learned player-station access map. Player-built stations may
	// refuse an outsider's dock and nothing readable says which will, so an
	// unverified one is treated as closed and its fares are declined. nil
	// disables the check entirely (older callers, tests).
	Access *StationAccess
}

func shuttleNow(deps ShuttleDeps) time.Time {
	if deps.Now != nil {
		return deps.Now()
	}
	return time.Now()
}

// shuttleCandidate aggregates the passengers waiting at the current station that
// are bound for one destination station: how many, their combined estimated fare,
// the destination's system, and the jump distance to it.
type shuttleCandidate struct {
	station  string
	system   string
	sysName  string
	fare     int
	pax      int
	jumpDist int
}

// Shuttle performs one passenger-ferry step: ensure docked, survey the station's
// waiting passengers, pick the best-paying reachable destination (excluding pirate
// strongholds and anything beyond the jump budget), board whoever fits, fly them
// there, and collect the auto-paid fares on docking — depositing the treasury's
// cut. On any mid-run failure it logs and returns nil so the worker idles and
// retries; aboard passengers auto-deliver whenever the ship next docks at their
// destination, so a partial run is never a hard loss.
func Shuttle(ctx context.Context, deps ShuttleDeps) error {
	// Clear last pass's activity; the board site below sets it when we load fares.
	publishActivity(deps.SetActivity, "")
	out := deps.Out
	if out == nil {
		out = io.Discard
	}
	if deps.KB == nil {
		fmt.Fprintln(out, "shuttle: no knowledge base; skipping") //nolint:errcheck
		return nil
	}
	maxJumps := deps.MaxJumps
	if maxJumps <= 0 {
		maxJumps = DefaultShuttleMaxJumps
	}

	state := deps.Client.GetState()
	if state == nil || state.System.ID == "" {
		fmt.Fprintln(out, "shuttle: current system unknown; idling") //nolint:errcheck
		return nil
	}
	current := state.System.ID

	// Boarding requires being DOCKED AT A STATION. Gate on the docked flag, not on
	// CurrentPOI: a shuttle can sit undocked while CurrentPOI names a non-station POI
	// (a haze/star/resource field a delivery or reposition left it at), which is still
	// un-boardable — ListStationPassengers rejects it with "dock at a station". After
	// a delivery the worker is already docked at the drop-off; otherwise try to dock.
	// A dock that cannot succeed here means we are stranded in a system with no station
	// of its own (a server-restart dropped us mid-run). Route out to the nearest safe
	// station instead of looping forever — the deadlock that stranded johnny_cab.
	if !state.Doc || state.Traveling {
		currentPOI := state.CurrentPOI
		if err := deps.Client.Dock(ctx); err != nil {
			if shuttleRecoverIfStranded(ctx, deps, out, current, currentPOI) {
				return nil
			}
			fmt.Fprintf(out, "shuttle: not docked and dock failed: %v; idling\n", err) //nolint:errcheck
			return nil
		}
		state = deps.Client.GetState()
		if state != nil {
			currentPOI = state.CurrentPOI
		}
		if state == nil || !state.Doc {
			// Dock was accepted but has not landed us at a station (it executes on
			// the next tick). If we are at a non-station POI — a station-less system,
			// or the wrong POI in a station-having system — reposition to a station;
			// otherwise idle and let the pending dock complete next pass.
			if shuttleRecoverIfStranded(ctx, deps, out, current, currentPOI) {
				return nil
			}
			fmt.Fprintln(out, "shuttle: still not docked; idling") //nolint:errcheck
			return nil
		}
	}

	// Docked with no committed run: a low wallet now is genuine fumes, so this is
	// the safe point to pull a treasury rescue before earning the next fares.
	deps.Treasury.maybe(ctx, deps.Client, out, shuttleNow(deps))

	// Deliver anyone already aboard BEFORE hunting new fares. A prior pass may have
	// boarded passengers whose delivery never completed (a worker restart caught it
	// mid-run, or a transit/dock failure), and the manifest lives server-side — so a
	// fresh worker that skips straight to boarding finds its berths full, gets 0 on
	// every load_passenger, and never collects a fare: a silent deadlock. This clears
	// it by finishing the delivery first. Returns delivered=true when it flew/docked
	// this pass, so we re-enter next pass to handle any remaining aboard passengers
	// or resume boarding.
	if delivered, derr := deliverAboardPassengers(ctx, deps, out, current, maxJumps); derr != nil {
		fmt.Fprintf(out, "shuttle: aboard-delivery error: %v; idling\n", derr) //nolint:errcheck
		return nil
	} else if delivered {
		return nil
	}

	resp, err := deps.Client.ListStationPassengers(ctx, "")
	if err != nil {
		fmt.Fprintf(out, "shuttle: list passengers failed: %v; idling\n", err) //nolint:errcheck
		return nil
	}
	if len(resp.Waiting) == 0 {
		return shuttleIdle(ctx, deps, out, current, "no passengers waiting")
	}

	systems, err := deps.KB.GetSystems(ctx)
	if err != nil {
		return fmt.Errorf("shuttle: get systems: %w", err)
	}
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		return fmt.Errorf("shuttle: get connections: %w", err)
	}
	graph := navigation.JumpGraphFromConnections(conns)
	nameToID := buildNameToID(systems)
	strongholdByID := make(map[string]bool, len(systems))
	for _, s := range systems {
		if s.IsStronghold {
			strongholdByID[s.ID] = true
		}
	}

	ranked := rankShuttleDestinations(resp.Waiting, current, graph, nameToID, strongholdByID, maxJumps, out, deps.Access)
	if len(ranked) == 0 {
		return shuttleIdle(ctx, deps, out, current, fmt.Sprintf("no reachable, safe destinations within %d jumps", maxJumps))
	}

	// Board the best destination that accepts passengers. Try EVERY ranked
	// destination, not a fixed top-N: the biggest-fare long-hauls are the most
	// contested and usually out-competed before this tick-deferred load lands, so
	// the shuttle must fall through to winnable cheaper fares rather than give up.
	c, loadResp, ok := boardBestDestination(ctx, deps, ranked, out)
	if !ok {
		return shuttleIdle(ctx, deps, out, current, "no loadable passengers this pass")
	}
	fmt.Fprintf(out, "shuttle: boarded %d for %s (%s, %d jumps) — %d cr booked\n", //nolint:errcheck
		loadResp.Count, c.sysName, c.station, c.jumpDist, loadResp.TotalFare)
	publishActivity(deps.SetActivity, fmt.Sprintf("Hauling %d passengers from %s to %s", loadResp.Count, current, c.station))
	deps.State.boarded()
	return shuttleDeliver(ctx, deps, out, c, loadResp.TotalFare)
}

// deliverAboardPassengers checks the ship's manifest and, if anyone is aboard,
// finishes delivering them before the worker boards new fares. It is the fix for
// the berth deadlock: a prior pass's incomplete delivery (a restart caught mid-run,
// or a transit/dock failure) would otherwise leave the berths full forever — every
// load_passenger returns 0 and no fare is ever collected. It delivers ONE
// destination per call (the soonest-to-expire fare) and returns delivered=true so
// the caller re-enters and clears the rest next pass. Aboard passengers whose
// destination is unroutable (unknown/over-budget system, or a stronghold the
// shuttle must not enter) are stranded via unload to free the berths, rather than
// holding them hostage indefinitely. Returns delivered=false (cheaply, no graph
// build) when nobody is aboard.
func deliverAboardPassengers(ctx context.Context, deps ShuttleDeps, out io.Writer, current string, maxJumps int) (bool, error) {
	resp, err := deps.Client.ListPassengers(ctx)
	if err != nil {
		return false, fmt.Errorf("list aboard passengers: %w", err)
	}
	if resp == nil || len(resp.Passengers) == 0 {
		return false, nil // nobody aboard — proceed to boarding
	}

	systems, err := deps.KB.GetSystems(ctx)
	if err != nil {
		return false, fmt.Errorf("get systems: %w", err)
	}
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		return false, fmt.Errorf("get connections: %w", err)
	}
	graph := navigation.JumpGraphFromConnections(conns)
	nameToID := buildNameToID(systems)
	strongholdByID := make(map[string]bool, len(systems))
	for _, s := range systems {
		if s.IsStronghold {
			strongholdByID[s.ID] = true
		}
	}

	target, urgentTicks, routable := mostUrgentAboardDestination(resp.Passengers, current, graph, nameToID, strongholdByID, maxJumps, deps.Access)
	if !routable {
		// Nothing aboard can be routed (unknown/over-budget systems, or
		// stronghold-only destinations the shuttle must not enter). Strand them to
		// free the berths so the shuttle can earn again — holding undeliverable
		// passengers forever is the worse outcome.
		fmt.Fprintf(out, "shuttle: %d aboard but no routable destination; unloading all to free berths\n", len(resp.Passengers)) //nolint:errcheck
		if uerr := deps.Client.UnloadPassenger(ctx, "all"); uerr != nil {
			fmt.Fprintf(out, "shuttle: unload all failed: %v\n", uerr) //nolint:errcheck
		}
		return false, nil
	}

	fmt.Fprintf(out, "shuttle: %d aboard; delivering %d to %s (%s, %d jumps, soonest expires in %d ticks) before boarding\n", //nolint:errcheck
		len(resp.Passengers), target.pax, target.sysName, target.station, target.jumpDist, urgentTicks)
	// bookedFare 0: these fares were booked on an earlier pass, so there is no new
	// treasury cut to take here; the server still collects the passenger's fare on
	// the delivering dock.
	if derr := shuttleDeliver(ctx, deps, out, target, 0); derr != nil {
		return false, derr
	}
	return true, nil
}

// mostUrgentAboardDestination groups the aboard passengers by destination station,
// drops any whose system is unknown, beyond maxJumps, or a stronghold (the shuttle
// must never fly into one), and returns the routable destination whose most-urgent
// passenger has the fewest ticks remaining — soonest-to-expire fare delivered
// first. routable is false when no aboard passenger has a deliverable destination.
func mostUrgentAboardDestination(aboard []serverapi.AboardPassenger, current string, graph navigation.JumpGraph, nameToID map[string]string, strongholdByID map[string]bool, maxJumps int, access *StationAccess) (cand shuttleCandidate, urgentTicks int, routable bool) {
	type group struct {
		cand     shuttleCandidate
		minTicks int
	}
	groups := make(map[string]*group)
	for _, p := range aboard {
		if p.Destination == "" {
			continue
		}
		sysID := resolveShuttleSystemID(p.DestinationSystem, graph, nameToID)
		if sysID == "" || strongholdByID[sysID] {
			continue // unknown system or stronghold — not a place the shuttle delivers
		}
		if access.Denied(p.Destination) {
			// This station has already refused us. Treating it as unroutable is
			// what routes these passengers to the unload path below, instead of
			// flying there to be turned away a second time.
			continue
		}
		g := groups[p.Destination]
		if g == nil {
			name := p.DestinationName
			if name == "" {
				name = p.DestinationSystem
			}
			g = &group{cand: shuttleCandidate{station: p.Destination, system: sysID, sysName: name}, minTicks: p.TicksRemaining}
			groups[p.Destination] = g
		}
		g.cand.pax++
		g.cand.fare += p.Fare
		if p.TicksRemaining < g.minTicks {
			g.minTicks = p.TicksRemaining
		}
	}
	if len(groups) == 0 {
		return shuttleCandidate{}, 0, false
	}

	sysSet := make(map[string]bool, len(groups))
	for _, g := range groups {
		sysSet[g.cand.system] = true
	}
	targets := make([]string, 0, len(sysSet))
	for s := range sysSet {
		targets = append(targets, s)
	}
	jumps := navigation.BFSJumps(graph, current, targets)

	var best *group
	for _, g := range groups {
		j, ok := jumps[g.cand.system]
		if !ok || j > maxJumps {
			continue // unreachable or beyond the jump budget
		}
		g.cand.jumpDist = j
		if best == nil || g.minTicks < best.minTicks {
			best = g
		}
	}
	if best == nil {
		return shuttleCandidate{}, 0, false
	}
	return best.cand, best.minTicks, true
}

// boardBestDestination tries each ranked destination in order and boards the first
// that accepts at least one passenger, returning that candidate and the load
// response. It tries ALL ranked destinations (not a fixed top-N) so that when the
// premium long-hauls are out-competed for the tick-deferred load, the shuttle
// still falls through to winnable cheaper fares instead of idling. Returns
// ok=false when no destination boarded anyone this pass.
func boardBestDestination(ctx context.Context, deps ShuttleDeps, ranked []shuttleCandidate, out io.Writer) (shuttleCandidate, *serverapi.LoadPassengerResponse, bool) {
	for _, c := range ranked {
		loadResp, lerr := deps.Client.LoadPassenger(ctx, c.station)
		if lerr != nil {
			fmt.Fprintf(out, "shuttle: load for %s failed: %v; trying next\n", c.station, lerr) //nolint:errcheck
			continue
		}
		if loadResp.Count == 0 {
			// 0 boarded means the fares were taken by another ship between the
			// listing and this tick-deferred load, or no free berth of a matching
			// class. Either way, fall through to the next candidate.
			fmt.Fprintf(out, "shuttle: 0 boarded for %s (taken by another, or no matching free berth); ship berths economy=%q business=%q first=%q; trying next\n", //nolint:errcheck
				c.station, loadResp.EconomyBerths, loadResp.BusinessBerths, loadResp.FirstBerths)
			continue
		}
		return c, loadResp, true
	}
	return shuttleCandidate{}, nil, false
}

// shuttleIdle logs a boardless-pass reason and, once the consecutive dry streak
// reaches shuttleRepositionThreshold, relocates the shuttle to a neighboring
// system so it does not idle forever at a station with no boardable fares.
// Always returns nil — a boardless pass is never a hard failure.
func shuttleIdle(ctx context.Context, deps ShuttleDeps, out io.Writer, current, reason string) error {
	fmt.Fprintf(out, "shuttle: %s; idling\n", reason) //nolint:errcheck
	if deps.State.dry() >= shuttleRepositionThreshold {
		deps.State.resetDry()
		repositionShuttle(ctx, deps, out, current)
	}
	return nil
}

// repositionShuttle relocates the shuttle to a reachable, non-stronghold neighbor
// system to hunt fresh passengers after several boardless passes, rotating across
// neighbors so repeated repositions fan out. Best-effort: any failure logs and
// leaves the shuttle in place (the next pass simply tries the current station
// again).
func repositionShuttle(ctx context.Context, deps ShuttleDeps, out io.Writer, current string) {
	systems, err := deps.KB.GetSystems(ctx)
	if err != nil {
		fmt.Fprintf(out, "shuttle: reposition aborted (systems: %v)\n", err) //nolint:errcheck
		return
	}
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		fmt.Fprintf(out, "shuttle: reposition aborted (connections: %v)\n", err) //nolint:errcheck
		return
	}
	graph := navigation.JumpGraphFromConnections(conns)
	stronghold := make(map[string]bool, len(systems))
	nameByID := make(map[string]string, len(systems))
	for _, s := range systems {
		nameByID[s.ID] = s.Name
		if s.IsStronghold {
			stronghold[s.ID] = true
		}
	}
	neighbors := make([]string, 0, len(graph[current]))
	for _, n := range graph[current] {
		if n != "" && !stronghold[n] {
			neighbors = append(neighbors, n)
		}
	}
	if len(neighbors) == 0 {
		fmt.Fprintln(out, "shuttle: no safe neighbor to reposition to; staying put") //nolint:errcheck
		return
	}
	sort.Strings(neighbors) // deterministic ordering for the rotating cursor
	pick := neighbors[deps.State.nextNeighbor(len(neighbors))]
	name := nameByID[pick]
	if name == "" {
		name = pick
	}

	pois, err := deps.KB.GetPOIs(ctx, pick)
	if err != nil {
		fmt.Fprintf(out, "shuttle: reposition aborted (pois for %s: %v)\n", name, err) //nolint:errcheck
		return
	}
	station := freshestStation(pois)
	if station == "" {
		fmt.Fprintf(out, "shuttle: no known station in %s; staying put\n", name) //nolint:errcheck
		return
	}

	fmt.Fprintf(out, "shuttle: repositioning to %s after %d boardless passes\n", name, shuttleRepositionThreshold) //nolint:errcheck
	err = Autopilot(ctx, AutopilotDeps{
		Client:     deps.Client,
		Out:        out,
		OnWaypoint: func(ctx context.Context) error { return KBWaypointCapture(ctx, deps.Client, deps.KB) },
	}, pick, station)
	if err != nil {
		fmt.Fprintf(out, "shuttle: reposition transit to %s failed: %v\n", name, err) //nolint:errcheck
		return
	}
	if err := deps.Client.Dock(ctx); err != nil {
		fmt.Fprintf(out, "shuttle: reposition dock at %s failed: %v\n", name, err) //nolint:errcheck
		return
	}
	fmt.Fprintf(out, "shuttle: repositioned to %s; will hunt passengers next pass\n", name) //nolint:errcheck
}

// freshestStation returns the id of the station POI in pois observed most
// recently, or "" when pois holds no station.
//
// Ordering is the whole point. GetPOIs sorts by name, so the obvious "take the
// first station" pick is really "take the alphabetically first station" — and
// an alphabetically-early station our KB has not refreshed in a million ticks
// is very likely one the server has since deleted. RememberPOI is upsert-only
// and never prunes a POI that has vanished server-side, so those ghosts sit in
// the table forever looking exactly like live stations.
//
// Frontier is the case that cost us a shuttle: the server dropped Expedition
// Launch and Scout Docks some time after 2026-03-19 (both are present in the
// archived get_system captures), leaving Mobile Capital as its only real
// station. Name order picked "Expedition Launch" — last observed at tick 19
// against Mobile Capital's 1775261 — travel answered "Unknown destination:
// expedition_launch", and johnny_cab retried that same phantom every pass until
// the stall watchdog quarantined it. Preferring the freshest observation picks
// a station we have actually seen recently, and degrades sanely if the KB is
// wrong: the strand recovery simply tries again next pass.
func freshestStation(pois []knowledge.POI) string {
	best, bestTick := "", int64(-1)
	for _, p := range pois {
		if p.Type != "station" {
			continue
		}
		if p.LastUpdatedTick > bestTick {
			best, bestTick = p.ID, p.LastUpdatedTick
		}
	}
	return best
}

// shuttleAlreadyAtStation reports whether the shuttle is already sitting at the
// target station POI, so its undocked state is merely a pending dock (it just
// arrived) rather than a strand. In that case recovery must NOT re-issue travel —
// let the pending dock complete. Any other undocked position (a non-station POI in
// this system, or a different system) is a real strand to recover from.
func shuttleAlreadyAtStation(destSystem, stationPOI, current, currentPOI string) bool {
	return destSystem == current && stationPOI != "" && stationPOI == currentPOI
}

// shuttleRecoverIfStranded routes the shuttle to the nearest accessible, non-stronghold
// station when it is undocked somewhere it cannot board. It is the shuttle's escape hatch
// from the deadlock a server-restart mid-run disconnect leaves it in: parked undocked at
// a POI that is not a station — either a system with no station at all (Maplevale, where
// Dock() fails with "No base at this location") or a non-station POI in a system that DOES
// have a station elsewhere (Frontier's drifters_haze gas cloud, where the loop instead
// spins on ListStationPassengers' "dock at a station"). Both leave the standing loop
// unable to reach boarding. It uses the same nearest-accessible-station query as the
// hauler recovery and databot (galaxy.FindNearestByPOIType, which excludes strongholds and
// non-public stations), then autopilots to that station POI — which may be a different POI
// in the CURRENT system (intra-system reposition) or another system entirely.
//
// currentPOI is the shuttle's present POI: when it already equals the target station POI
// the dock is merely pending (recovery is a no-op, returns false so the caller idles and
// lets it complete). Returns true when it issued a relocation (so the caller ends the
// pass), false when no station is reachable at all or recovery could not run. Best-effort
// throughout: any failure logs and the shuttle retries next pass. Aboard passengers are
// left in place; once docked at the destination station the normal deliverAboardPassengers
// path clears the berths, so recovery stays focused on reaching a station.
func shuttleRecoverIfStranded(ctx context.Context, deps ShuttleDeps, out io.Writer, current, currentPOI string) bool {
	galGraph := &galaxy.GalaxyGraph{}
	if err := galGraph.BuildFromDB(ctx, deps.KB); err != nil {
		fmt.Fprintf(out, "shuttle: recovery graph build failed: %v\n", err) //nolint:errcheck
		return false
	}
	near, err := galaxy.FindNearestByPOIType(ctx, deps.KB, galGraph, current, "station", 1)
	if err != nil {
		fmt.Fprintf(out, "shuttle: nearest-station lookup failed: %v\n", err) //nolint:errcheck
		return false
	}
	if len(near) == 0 {
		return false // no station known anywhere reachable — nothing to do but idle
	}
	dest := near[0]
	name := dest.SystemName
	if name == "" {
		name = dest.SystemID
	}

	// Resolve the destination station POI. FindNearestByPOIType leaves POIs nil for
	// POI-type lookups, so query the KB directly (as repositionShuttle does).
	station := ""
	if pois, perr := deps.KB.GetPOIs(ctx, dest.SystemID); perr == nil {
		station = freshestStation(pois)
	}

	// Already at the target station POI: the dock is just pending, not a strand.
	if shuttleAlreadyAtStation(dest.SystemID, station, current, currentPOI) {
		return false
	}

	if dest.SystemID == current {
		fmt.Fprintf(out, "shuttle: undocked at non-station POI %q in %s; repositioning to its station %s\n", //nolint:errcheck
			currentPOI, current, station)
	} else {
		fmt.Fprintf(out, "shuttle: stranded in station-less %s; relocating to nearest safe station %s (%s, %d jump(s))\n", //nolint:errcheck
			current, name, dest.SystemID, dest.Hops)
	}
	err = Autopilot(ctx, AutopilotDeps{
		Client:     deps.Client,
		Out:        out,
		OnWaypoint: func(ctx context.Context) error { return KBWaypointCapture(ctx, deps.Client, deps.KB) },
	}, dest.SystemID, station)
	if err != nil {
		fmt.Fprintf(out, "shuttle: relocation transit to %s failed: %v; will retry next pass\n", name, err) //nolint:errcheck
		return true
	}
	if derr := deps.Client.Dock(ctx); derr != nil {
		fmt.Fprintf(out, "shuttle: relocation dock at %s failed: %v\n", name, derr) //nolint:errcheck
	}
	return true
}

// rankShuttleDestinations groups the waiting passengers by destination station,
// drops destinations that are pirate strongholds (most ships are destroyed flying
// there), player stations we cannot prove will admit us, unreachable, or beyond
// maxJumps, and returns the survivors sorted by total estimated fare
// (descending). Skipped strongholds and closed stations are logged once.
//
// The access check is at BOARDING for a reason: a fare accepted for a station
// that refuses the dock cannot be completed or abandoned cheaply -- live on
// 2026-08-11 the shuttle took a passenger for Fortress Blackthorn and then held
// them while circling, until an operator unloaded them by hand.
func rankShuttleDestinations(waiting []serverapi.StationPassenger, current string, graph navigation.JumpGraph, nameToID map[string]string, strongholdByID map[string]bool, maxJumps int, out io.Writer, access *StationAccess) []shuttleCandidate {
	agg := make(map[string]*shuttleCandidate)
	loggedClosed := make(map[string]bool)
	for _, p := range waiting {
		if p.Destination == "" {
			continue
		}
		sysID := resolveShuttleSystemID(p.DestinationSystem, graph, nameToID)
		if sysID == "" {
			continue // unknown system — cannot route
		}
		if !access.Deliverable(p.Destination) {
			// Player station that has not proven it will admit us. Logged so a
			// declined fare is visible as a decision rather than a silent gap.
			if !loggedClosed[p.Destination] {
				fmt.Fprintf(out, "shuttle: skipping unverified player station %s (%s); no proven dock access\n", //nolint:errcheck
					cmp.Or(p.DestinationName, p.DestinationSystem), p.Destination)
				loggedClosed[p.Destination] = true
			}

			continue
		}
		c := agg[p.Destination]
		if c == nil {
			c = &shuttleCandidate{station: p.Destination, system: sysID, sysName: p.DestinationName}
			if c.sysName == "" {
				c.sysName = p.DestinationSystem
			}
			agg[p.Destination] = c
		}
		c.fare += p.EstimatedFare
		c.pax++
	}

	// One BFS over the distinct candidate systems.
	sysSet := make(map[string]bool)
	for _, c := range agg {
		sysSet[c.system] = true
	}
	targets := make([]string, 0, len(sysSet))
	for s := range sysSet {
		targets = append(targets, s)
	}
	jumps := navigation.BFSJumps(graph, current, targets)

	loggedStronghold := make(map[string]bool)
	ranked := make([]shuttleCandidate, 0, len(agg))
	for _, c := range agg {
		if strongholdByID[c.system] {
			if !loggedStronghold[c.system] {
				fmt.Fprintf(out, "shuttle: skipping stronghold destination %s (%s)\n", c.sysName, c.station) //nolint:errcheck
				loggedStronghold[c.system] = true
			}
			continue
		}
		j, ok := jumps[c.system]
		if !ok || j > maxJumps {
			continue // unreachable or beyond the jump budget
		}
		c.jumpDist = j
		ranked = append(ranked, *c)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].fare != ranked[j].fare {
			return ranked[i].fare > ranked[j].fare
		}
		return ranked[i].jumpDist < ranked[j].jumpDist // tie-break: nearer first
	})
	return ranked
}

// resolveShuttleSystemID maps a passenger's destination_system (which may be a raw
// system id or a display name) to a graph system id, or "" when it cannot be
// resolved against known systems.
func resolveShuttleSystemID(ds string, graph navigation.JumpGraph, nameToID map[string]string) string {
	if ds == "" {
		return ""
	}
	if _, ok := graph[ds]; ok {
		return ds
	}
	if id := nameToID[ds]; id != "" {
		return id
	}
	return ""
}

// shuttleDeliver flies the boarded passengers to their destination, docks (which
// auto-delivers them and collects their fares), deposits the treasury's cut of the
// booked fare, and tops off fuel for the next run. Transit/dock failures leave the
// passengers aboard (they auto-deliver on a later dock) and idle without depositing.
func shuttleDeliver(ctx context.Context, deps ShuttleDeps, out io.Writer, c shuttleCandidate, bookedFare int) error {
	err := Autopilot(ctx, AutopilotDeps{
		Client:     deps.Client,
		Out:        out,
		OnWaypoint: func(ctx context.Context) error { return KBWaypointCapture(ctx, deps.Client, deps.KB) },
	}, c.system, c.station)
	if err != nil {
		fmt.Fprintf(out, "shuttle: transit to %s failed: %v; passengers stay aboard\n", c.sysName, err) //nolint:errcheck
		return nil
	}
	// Autopilot travels to the station POI but leaves the ship undocked; dock to
	// trigger the automatic passenger delivery + fare collection.
	dockErr := deps.Client.Dock(ctx)
	// Learn from the attempt either way. This is the ONLY place the fleet finds
	// out whether a player station admits us, so a refusal recorded here is what
	// stops the next pass -- and every other shuttle sharing the map -- from
	// selling the same undeliverable fare again.
	deps.Access.RecordDock(c.station, dockErr)
	if dockErr != nil {
		fmt.Fprintf(out, "shuttle: dock at %s failed: %v; passengers stay aboard\n", c.station, dockErr) //nolint:errcheck
		return nil
	}
	// Passengers bound here have now auto-delivered. Contribute the treasury's cut
	// of the booked fare to the shared fund.
	depositProfitShare(ctx, deps.Client, out, float64(bookedFare))
	// The shuttle already refuels at every drop-off, so learning whether the
	// station HAS a fuel desk is free here -- and it is the fact that stranded
	// engineer-5 at a station that admitted it perfectly well.
	// The station attempt is recorded on its own: a cargo-cell fallback that
	// reported success would write "sells fuel" against a station whose desk is
	// dry, and this record is what later routing trusts.
	refuelErr := RefuelStationAndSync(ctx, deps.Client, out, "shuttle")
	deps.Access.RecordRefuel(c.station, refuelErr)
	if refuelErr != nil {
		fmt.Fprintf(out, "shuttle: refuel at %s failed: %v\n", c.station, refuelErr) //nolint:errcheck
		// A dry desk still leaves the shuttle needing fuel for the next run, and
		// unlike the probe it has somewhere to be.
		if deskIsDry(refuelErr) {
			if cerr := deps.Client.RefuelFromCargo(ctx, fuelCellItemID, 1); cerr != nil {
				fmt.Fprintf(out, "shuttle: no cargo cells to fall back on either: %v\n", cerr) //nolint:errcheck
			} else {
				syncShipState(ctx, deps.Client, out, "shuttle")
			}
		}
	}
	fmt.Fprintf(out, "shuttle: delivered to %s; ready for next run\n", c.sysName) //nolint:errcheck
	return nil
}
