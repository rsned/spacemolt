package worker

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

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

	// Boarding requires being docked. After a delivery the worker is already
	// docked at the drop-off; on a cold start it may be in space — try once to dock.
	if state.CurrentPOI == "" || state.Traveling {
		if err := deps.Client.Dock(ctx); err != nil {
			fmt.Fprintf(out, "shuttle: not docked and dock failed: %v; idling\n", err) //nolint:errcheck
			return nil
		}
		state = deps.Client.GetState()
		if state == nil || state.CurrentPOI == "" {
			fmt.Fprintln(out, "shuttle: still not docked; idling") //nolint:errcheck
			return nil
		}
	}
	current := state.System.ID

	// Docked with no committed run: a low wallet now is genuine fumes, so this is
	// the safe point to pull a treasury rescue before earning the next fares.
	deps.Treasury.maybe(ctx, deps.Client, out, shuttleNow(deps))

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

	ranked := rankShuttleDestinations(resp.Waiting, current, graph, nameToID, strongholdByID, maxJumps, out)
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
	deps.State.boarded()
	return shuttleDeliver(ctx, deps, out, c, loadResp.TotalFare)
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
	station := ""
	for _, p := range pois {
		if p.Type == "station" {
			station = p.ID
			break
		}
	}
	if station == "" {
		fmt.Fprintf(out, "shuttle: no known station in %s; staying put\n", name) //nolint:errcheck
		return
	}

	fmt.Fprintf(out, "shuttle: repositioning to %s after %d boardless passes\n", name, shuttleRepositionThreshold) //nolint:errcheck
	err = Autopilot(ctx, AutopilotDeps{
		Client: deps.Client,
		Out:    out,
		OnWaypoint: func(ctx context.Context) error {
			if deps.KB == nil {
				return nil
			}
			if err := KBUpdateSystem(ctx, deps.Client, deps.KB, ""); err != nil {
				return err
			}
			return KBUpdatePOI(ctx, deps.Client, deps.KB, "")
		},
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

// rankShuttleDestinations groups the waiting passengers by destination station,
// drops destinations that are pirate strongholds (most ships are destroyed flying
// there), unreachable, or beyond maxJumps, and returns the survivors sorted by
// total estimated fare (descending). Skipped strongholds are logged once.
func rankShuttleDestinations(waiting []serverapi.StationPassenger, current string, graph navigation.JumpGraph, nameToID map[string]string, strongholdByID map[string]bool, maxJumps int, out io.Writer) []shuttleCandidate {
	agg := make(map[string]*shuttleCandidate)
	for _, p := range waiting {
		if p.Destination == "" {
			continue
		}
		sysID := resolveShuttleSystemID(p.DestinationSystem, graph, nameToID)
		if sysID == "" {
			continue // unknown system — cannot route
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
		Client: deps.Client,
		Out:    out,
		OnWaypoint: func(ctx context.Context) error {
			if deps.KB == nil {
				return nil
			}
			if err := KBUpdateSystem(ctx, deps.Client, deps.KB, ""); err != nil {
				return err
			}
			return KBUpdatePOI(ctx, deps.Client, deps.KB, "")
		},
	}, c.system, c.station)
	if err != nil {
		fmt.Fprintf(out, "shuttle: transit to %s failed: %v; passengers stay aboard\n", c.sysName, err) //nolint:errcheck
		return nil
	}
	// Autopilot travels to the station POI but leaves the ship undocked; dock to
	// trigger the automatic passenger delivery + fare collection.
	if err := deps.Client.Dock(ctx); err != nil {
		fmt.Fprintf(out, "shuttle: dock at %s failed: %v; passengers stay aboard\n", c.station, err) //nolint:errcheck
		return nil
	}
	// Passengers bound here have now auto-delivered. Contribute the treasury's cut
	// of the booked fare to the shared fund.
	depositProfitShare(ctx, deps.Client, out, float64(bookedFare))
	if err := deps.Client.Refuel(ctx); err != nil {
		fmt.Fprintf(out, "shuttle: refuel at %s failed: %v\n", c.station, err) //nolint:errcheck
	}
	fmt.Fprintf(out, "shuttle: delivered to %s; ready for next run\n", c.sysName) //nolint:errcheck
	return nil
}
