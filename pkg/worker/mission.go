package worker

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
	"time"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

const (
	// missionDryPassLimit: consecutive passes with no acceptable work before
	// the worker repositions to another station (spec: hop, don't camp).
	missionDryPassLimit = 3
	// missionRepositionPool: how many nearby stations the reposition cursor
	// rotates through.
	missionRepositionPool = 5
	// missionParkWindow: once a full circuit of the reposition pool comes up
	// dry, boards are globally thin (overnight soak 2026-07-17: 248 fuel-burning
	// hops for zero accepts, ~2.7k cr/h lost). Park at the current station for
	// this long — passes keep re-reading the local board for free, so the
	// worker resumes the instant missions respawn — before hopping again.
	missionParkWindow = 30 * time.Minute
	// missionSurgeMult caps how far the local depth-walk's volume-weighted
	// average fill price may run above the authoritative reference price
	// before the availability gate treats it as a local price surge (the
	// fighter-4 overpay: iron_ore bought locally at 2000 against a ~7 cr
	// reference) and skips the mission instead of buying into it.
	missionSurgeMult = 4.0
	// missionReferenceLookback is the lookback window passed to
	// MissionStore.GetReferencePrice for the surge-ceiling basis.
	missionReferenceLookback = 24 * time.Hour
)

// MissionStore is the subset of *market.Collector the mission runner needs
// (result telemetry + item pricing for the cost gate).
type MissionStore interface {
	RecordMissionResult(ctx context.Context, r market.MissionResult) error
	GetReferenceAsk(ctx context.Context, itemID string) (market.ReferenceAsk, bool, error)
	// GetReferencePrice returns a robust "cheap" cross-station reference
	// (20th-percentile over lookback), used as the surge-ceiling basis for
	// the local depth-walk gate — see missionSurgeMult.
	GetReferencePrice(ctx context.Context, itemID string, lookback time.Duration) (float64, bool, error)
	// RecordFreightResult persists one settled shipping-contract outcome.
	RecordFreightResult(ctx context.Context, r market.FreightResult) error
}

var _ MissionStore = (*market.Collector)(nil)

// missionRunState carries cross-pass memory (dry-pass streak + reposition
// cursor + the set of missions already attempted this session), held by
// WorkerDispatch so it survives between command passes — the shuttleState
// pattern.
type missionRunState struct {
	dry    int
	cursor int
	// hopsDry counts repositions since the last executed work; reaching the
	// pool size means a full circuit found nothing and triggers parking.
	hopsDry int
	// parkedUntil suppresses repositioning (not board reads) until it passes.
	parkedUntil time.Time
	attempted   map[string]bool
	// loggedSkips remembers "<mission id>:<reason>" pairs already printed
	// this session, so a dry board's skip lines print once instead of every
	// pass. A changed reason (e.g. the expiry countdown crossing the floor)
	// is a new key, so it logs again.
	loggedSkips map[string]bool
	// heldFreight is the set of in-flight shipping contracts accepted this
	// session, keyed by contract ID. The live canary (2026-07-20) proved the
	// board read NEVER returns our own in_transit contracts, so this
	// in-memory set is the PRIMARY reconcile source; the profile count in
	// freightReconcileSet is now just the post-restart mismatch detector —
	// the held set itself resumes across restarts via the freight-held.json
	// file (WorkerDispatch.ensureFreightPersistence), so the detector only
	// fires when that file is lost or corrupted. Sub-project C: was a single
	// *ShipmentContract.
	heldFreight map[string]*serverapi.ShipmentContract
	// persistHeld saves the held set to disk after every mutation. nil (tests,
	// non-freight workers, no AgentID) keeps the pre-persistence in-memory-only
	// behavior. Set by WorkerDispatch.ensureFreightPersistence for freight workers.
	persistHeld func([]*serverapi.ShipmentContract)
	// bootstrapLoss is the cumulative freight LOSS (sum of -net over accepted
	// negative-net contracts) this worker has eaten to climb out of the
	// probationary carrier tier. In-memory and per-worker; resets on restart
	// (bounded — the probationary gate and fast advancement keep it small).
	// Positive-net accepts never touch it. Read via bootstrapSpent().
	bootstrapLoss float64
	// smugglingLoss is the cumulative CREDIT loss this worker has eaten on
	// smuggling runs taken below missionMinNet to buy skill XP. Bounds the
	// relaxed floor via effectiveMissionFloor. In-memory and per-worker: a
	// restart forgives it, which is the forgiving direction for a canary whose
	// job is to reach the next level. Profitable runs never touch it.
	smugglingLoss float64
	// loadParks counts, per contract ID, how many passes have parked on an
	// unconfirmed package load. Bounds freightLoadUnconfirmed so a contract we
	// genuinely cannot carry is eventually handed back instead of pinning the
	// worker. In-memory and per-worker; a fresh session starts every contract
	// at zero, which is the forgiving direction.
	loadParks map[string]int
	// lastLoggedTier is the carrier tier last written to the log this session.
	// logTierIfChanged prints the tier the first time it is seen and again on
	// any change (a promotion), giving the tagged log an authoritative
	// per-worker tier without logging it every freight pass.
	lastLoggedTier string
}

// smugglingSpent is the cumulative loss eaten buying smuggling XP. 0 on a nil
// receiver, which keeps the relaxed floor available rather than silently
// reverting a stateless caller to missionMinNet.
func (s *missionRunState) smugglingSpent() float64 {
	if s == nil {
		return 0
	}

	return s.smugglingLoss
}

// noteSmugglingLoss books the loss on a smuggling run accepted below the normal
// floor. Only losses count — a profitable courier costs no budget, so a worker
// that finds paying runs can climb indefinitely. No-op on a nil receiver.
func (s *missionRunState) noteSmugglingLoss(net float64) {
	if s == nil || net >= 0 {
		return
	}
	s.smugglingLoss += -net
}

// noteLoadPark counts one parked pass for a contract whose package load could
// not be confirmed, returning the running total. counted=false on a nil
// receiver (State is optional): with nowhere to count, the caller must not
// park, because an uncounted park would never terminate.
func (s *missionRunState) noteLoadPark(contractID string) (int, bool) {
	if s == nil {
		return 0, false
	}
	if s.loadParks == nil {
		s.loadParks = make(map[string]int)
	}
	s.loadParks[contractID]++

	return s.loadParks[contractID], true
}

// addHeldFreight remembers (or refreshes) a contract we are carrying. No-op
// on a nil receiver (State is optional; tests that don't care omit it).
func (s *missionRunState) addHeldFreight(c *serverapi.ShipmentContract) {
	if s == nil || c == nil {
		return
	}
	if s.heldFreight == nil {
		s.heldFreight = make(map[string]*serverapi.ShipmentContract)
	}
	s.heldFreight[c.ID] = c
	s.saveHeld()
}

// removeHeldFreight forgets one contract after its terminal outcome. No-op
// on a nil receiver.
func (s *missionRunState) removeHeldFreight(id string) {
	if s == nil {
		return
	}
	delete(s.heldFreight, id)
	s.saveHeld()
}

// saveHeld pushes the current held set through the persist callback, if wired.
func (s *missionRunState) saveHeld() {
	if s == nil || s.persistHeld == nil {
		return
	}
	s.persistHeld(s.heldFreightAll())
}

// heldFreightAll returns the held contracts sorted by ID (deterministic
// iteration for feasibility math, logs and tests). Nil on a nil receiver.
func (s *missionRunState) heldFreightAll() []*serverapi.ShipmentContract {
	if s == nil || len(s.heldFreight) == 0 {
		return nil
	}
	ids := make([]string, 0, len(s.heldFreight))
	for id := range s.heldFreight {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*serverapi.ShipmentContract, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.heldFreight[id])
	}
	return out
}

// heldFreightCount is len(held); 0 on a nil receiver.
func (s *missionRunState) heldFreightCount() int {
	if s == nil {
		return 0
	}
	return len(s.heldFreight)
}

// markAttempted records that mission id has been accepted (and recorded,
// win or lose) this session, so a future pass never re-selects it. No-op on
// a nil receiver (State is optional; tests that don't care simply omit it).
func (s *missionRunState) markAttempted(id string) {
	if s == nil {
		return
	}
	if s.attempted == nil {
		s.attempted = make(map[string]bool)
	}
	s.attempted[id] = true
}

// wasAttempted reports whether mission id was already recorded this session.
// Always false on a nil receiver.
func (s *missionRunState) wasAttempted(id string) bool {
	if s == nil {
		return false
	}
	return s.attempted[id]
}

// shouldLogSkip reports whether the (id, reason) pair hasn't been logged yet
// this session and records it so it won't fire again. Always true on a nil
// receiver (no cross-pass memory to dedupe against).
func (s *missionRunState) shouldLogSkip(id, reason string) bool {
	if s == nil {
		return true
	}
	key := id + ": " + reason
	if s.loggedSkips[key] {
		return false
	}
	if s.loggedSkips == nil {
		s.loggedSkips = make(map[string]bool)
	}
	s.loggedSkips[key] = true
	return true
}

// addBootstrapSpent adds a bootstrap freight loss to the per-worker tally. No-op
// on a nil receiver (State is optional; tests that don't care omit it).
func (s *missionRunState) addBootstrapSpent(loss float64) {
	if s == nil {
		return
	}
	s.bootstrapLoss += loss
}

// bootstrapSpent is the cumulative bootstrap freight loss this session; 0 on a
// nil receiver.
func (s *missionRunState) bootstrapSpent() float64 {
	if s == nil {
		return 0
	}
	return s.bootstrapLoss
}

// stationHop is one reposition target from the nearest-stations query.
type stationHop struct {
	SystemID string
	POIID    string
}

// MissionDeps are the injected collaborators for one Missions pass.
type MissionDeps struct {
	Client  game.GameClient
	KB      knowledge.Base
	Market  MissionStore
	Out     io.Writer // nil -> io.Discard
	AgentID string
	// MaxJumps caps mission-destination distance (0 -> DefaultMissionMaxJumps).
	MaxJumps int
	// Categories is the board-category allowlist (nil/empty -> delivery only).
	// The learning pool runs {"delivery", "exploration"}.
	Categories []string
	// capture fires a full market capture at the current dock (nil disables).
	// Exploration tours call it at every dock leg — paid coverage sweeps.
	capture func(ctx context.Context) error
	// Treasury rate-limits faction-treasury rescue withdrawals (nil disables).
	Treasury *treasuryRescue
	// FuelPrices supplies captured station fuel prices for the fuel-cost model.
	// nil disables fuel accounting (net == reward - item cost).
	FuelPrices FuelPriceSource
	// Now returns wall-clock time (nil -> time.Now); injected for tests.
	Now func() time.Time
	// State carries the cross-pass dry-streak/reposition memory (nil disables
	// repositioning — tests that don't care simply omit it).
	State *missionRunState
	// HomeStation pins this worker to one base id: a dry pass returns here
	// instead of touring nearby boards. Empty (the default) keeps the roaming
	// behavior every worker had before.
	//
	// Roaming quietly decides a worker's economics. The 2026-07-26 smuggling
	// canaries were configured with a `station` that only the assist role read,
	// so they toured: engineer-1 was found idling in Dheneb at a board-less POI
	// while the courier it was enabled for sat unseen on the Grand Exchange
	// board. A worker enabled for one category has to be where that category is
	// posted.
	HomeStation string
	// nav navigates to (system, poi); nil -> real Autopilot. Injected for tests,
	// mirroring WorkerDispatch.ensureHomeNav.
	nav func(ctx context.Context, system, poi string) error
	// nearbyStations lists reposition targets near the current system; nil ->
	// the galaxy-graph default built inside Missions. Injected for tests.
	nearbyStations func(ctx context.Context, limit int) ([]stationHop, error)
	// sleep is the post-fetch settle delay (nil -> craftPollSleepFunc, the
	// ctx-aware real sleep). Tests inject a zero-delay stand-in so the suite
	// doesn't accumulate real SleepQuick waits — the craftPollSleep pattern.
	sleep func(ctx context.Context, d time.Duration) error
	// SetActivity publishes the status-page "current activity" string (nil in
	// tests). Set when a mission is accepted or a held one resumed, cleared
	// ("") on a dry pass.
	SetActivity func(string)
	// EnableFreight opts this worker into the /shipping carrier path, evaluated
	// co-equally with the mission board. Default false so existing fleets are
	// unchanged until the canary opts in explicitly.
	EnableFreight bool
	// FreightMaxPackages caps concurrent freight contracts (sub-project C
	// multi-package trips). 0 and 1 both mean the v1 single-contract
	// behavior; the cap layers UNDER the server/cargo headroom gates and is
	// never a target. Canary: fighter-4 at 3.
	FreightMaxPackages int
	// FreightBootstrap enables the probationary loss-leader floor relaxation
	// (effectiveFreightFloor). Defaults on wherever EnableFreight is set; a
	// false value forces the normal 500 floor — the kill switch. Inert unless
	// EnableFreight is also true (freightCandidate only runs on the freight path).
	FreightBootstrap bool
}

// missionActivityLabel renders the accepted set as the status-page activity
// line, e.g. "Mission Steel Plate Order" (or "Mission X (+2 more)" for a
// stacked trip). Empty input yields "".
func missionActivityLabel(accepted []missionCandidate) string {
	if len(accepted) == 0 {
		return ""
	}
	label := "Mission " + accepted[0].Entry.Title
	if n := len(accepted) - 1; n > 0 {
		label += fmt.Sprintf(" (+%d more)", n)
	}
	return label
}

// missionCategoryEnabled reports whether the board category is allowlisted.
// A nil/empty Categories keeps v1 behavior: delivery only.
func missionCategoryEnabled(deps MissionDeps, category string) bool {
	if len(deps.Categories) == 0 {
		return category == missionTypeDelivery
	}
	return slices.Contains(deps.Categories, category)
}

func missionNow(deps MissionDeps) time.Time {
	if deps.Now != nil {
		return deps.Now()
	}
	return time.Now()
}

func missionTick(deps MissionDeps) int64 {
	if st := deps.Client.GetState(); st != nil {
		return st.GetTick()
	}
	return 0
}

// Missions performs one mission-runner pass: complete any resumable active
// missions, read the local board, accept + provision a stackable deliver set,
// run it to the shared destination system, and record every outcome. Mirrors
// Haul's resilience contract: mid-run failures log and return nil so the
// worker idles and retries; the pass never kills the worker loop.
func Missions(ctx context.Context, deps MissionDeps) error {
	out := deps.Out
	if out == nil {
		out = io.Discard
	}
	// Clear last pass's activity; the resume and accept sites below set it when
	// a mission is in hand, so a dry pass reports blank.
	publishActivity(deps.SetActivity, "")
	// A disconnected worker cannot accept, deliver, or route missions; running the
	// pass would only spin failing commands against a dead connection. Skip it and
	// let the reconnect gate restore the connection (the standing loop paces retries).
	if !deps.Client.IsConnected() {
		fmt.Fprintln(out, "missions: game connection down; skipping pass until reconnected") //nolint:errcheck
		return nil
	}
	if deps.Market == nil {
		fmt.Fprintln(out, "missions: market collector not configured; skipping") //nolint:errcheck
		return nil
	}
	if deps.KB == nil {
		fmt.Fprintln(out, "missions: no knowledge base; skipping") //nolint:errcheck
		return nil
	}
	if deps.nav == nil {
		deps.nav = func(ctx context.Context, system, poi string) error {
			return Autopilot(ctx, AutopilotDeps{Client: deps.Client, Out: out}, system, poi)
		}
	}
	if deps.sleep == nil {
		deps.sleep = craftPollSleepFunc
	}
	state := deps.Client.GetState()
	if state == nil || state.System.ID == "" {
		fmt.Fprintln(out, "missions: current system unknown; skipping") //nolint:errcheck
		return nil
	}
	current := state.System.ID

	// hopsTo resolves jump distance to a destination base via the server's
	// router. Shared — not redeclared — by the reconcile block, candidate
	// evaluation, and every take-freight call site below: it is pure plumbing
	// over ctx+deps.
	hopsTo := func(destBaseID string) (int, bool) {
		return missionHopsToBase(ctx, deps, destBaseID)
	}

	// Fuel model (same probe as Haul), built once per pass and memoized, and
	// self-sufficient: it fetches its own connections lazily on first call
	// rather than capturing the pass's `graph` below, because the freight
	// reconcile block runs before that graph is fetched (reconcile must
	// precede EVERY early return, including one from a failed connections
	// fetch). On a GetConnections error it logs once and degrades to the
	// zero-cost model (fail-open, mirroring the existing fuelPerJump<=0
	// branch) instead of aborting the pass. With EnableFreight false the
	// first caller is still the mission-candidate loop further down, exactly
	// where the probe has always happened.
	var fuelCostFor func(jumps int) float64
	ensureFuelModel := func() func(jumps int) float64 {
		if fuelCostFor != nil {
			return fuelCostFor
		}
		probeTarget := ""
		conns, cerr := deps.KB.GetConnections(ctx)
		if cerr != nil {
			fmt.Fprintf(out, "fuel model unavailable: %v; pricing fuel at 0\n", cerr) //nolint:errcheck
			fuelCostFor = func(int) float64 { return 0 }
			return fuelCostFor
		}
		localGraph := navigation.JumpGraphFromConnections(conns)
		for _, nb := range localGraph[current] {
			probeTarget = nb
			break
		}
		fuelPerJump := haulFuelPerJump(ctx, deps.Client, probeTarget)
		priceOf := buildPriceOf(ctx, deps.FuelPrices)
		fuelCostFor = func(jumps int) float64 {
			if fuelPerJump <= 0 || priceOf == nil {
				return 0
			}
			return float64(jumps*fuelPerJump) * priceOf(state.CurrentPOI)
		}
		return fuelCostFor
	}

	// Freight reconcile runs before EVERY early return in this pass -- true
	// again now that it sits above the connections/graph fetch below rather
	// than after it: a transient KB failure there must not preempt reconcile.
	// A restart loses the in-memory task, and an orphaned in-flight package
	// breaches in silence. Worse, missionRecoverToStation and missionResume
	// further down can both end the pass, and missionDryPass's reposition
	// logic will happily fly the worker AWAY from its freight destination
	// while it is still carrying the package. Reconcile itself needs neither
	// the galaxy graph nor the fuel model: hopsTo above hits the server's
	// router directly, and the chain run below prices fuel through
	// ensureFuelModel's self-sufficient lazy probe, which degrades to
	// zero-cost on its own KB failure rather than blocking reconcile.
	if deps.EnableFreight {
		if held := freightReconcileSet(ctx, deps, out); len(held) > 0 {
			// Carrying package(s) legitimately owns the pass. The chain run
			// re-prices every deadline from here with fresh hops (v1's
			// RouteHops-is-total-hops conservatism note carries over: rows
			// with outcome returned_inflight can be this bound, not a real
			// deadline problem). Whether the server refreshes RouteHops to
			// REMAINING hops on an in_transit contract is UNVERIFIED; the
			// canary answers it. Do not "fix" this by guessing remaining hops
			// or by switching to AcceptedTick-style optimism.
			publishActivity(deps.SetActivity, fmt.Sprintf("Freight chain: %d package(s)", len(held)))
			step, ferr := freightChainRun(ctx, deps, func(ctx context.Context, baseID string) error {
				return missionNavToBase(ctx, deps, baseID)
			}, hopsTo, ensureFuelModel(), out)
			if step == freightStepStuck {
				return nil // live undischarged contract: park the pass
			}
			return ferr
		}
	}

	// Routing substrate (same shape as Haul) for the BFS distance table used
	// by board-driven candidate evaluation further down. Freight reconcile
	// above does not depend on this: it already ran using hopsTo and
	// ensureFuelModel's own lazy connections fetch.
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		return fmt.Errorf("missions: get connections: %w", err)
	}
	graph := navigation.JumpGraphFromConnections(conns)

	// Default reposition source: nearest accessible stations by the galaxy
	// graph (the same query haul's stranded-recovery uses; it excludes
	// strongholds and non-public stations). Initialized before recovery so the
	// recovery escape can reuse it to leave a station-less system.
	if deps.nearbyStations == nil {
		deps.nearbyStations = func(ctx context.Context, limit int) ([]stationHop, error) {
			gal := &galaxy.GalaxyGraph{}
			if gerr := gal.BuildFromDB(ctx, deps.KB); gerr != nil {
				return nil, gerr
			}
			// +1: the query's own accessible-station set always includes
			// `current` (Hops 0, filtered out below), so asking for exactly
			// `limit` would waste one slot on the system we're leaving.
			near, nerr := galaxy.FindNearestByPOIType(ctx, deps.KB, gal, current, "station", limit+1)
			if nerr != nil {
				return nil, nerr
			}
			hops := make([]stationHop, 0, len(near))
			for _, n := range near {
				if n.SystemID == current {
					continue // skip the station we're camping; we want elsewhere
				}
				// NearestResult.POIs is only populated for resource lookups
				// (pkg/galaxy/types.go), never for POI-type lookups like
				// "station" — every result would otherwise be filtered out
				// here (the bug: "0 targets" on every dry pass). Resolve the
				// actual station POI id from the KB instead.
				poiID, perr := missionStationPOI(ctx, deps.KB, n.SystemID)
				if perr != nil || poiID == "" {
					continue // KB gap or no accessible station POI in this system
				}
				hops = append(hops, stationHop{SystemID: n.SystemID, POIID: poiID})
			}
			return hops, nil
		}
	}

	// A board only exists at a station. Undocked anywhere else — open space
	// (empty POI after an interrupted jump) or a non-dockable POI (parked at
	// an ice field) — recovers to a known public station: the missionrunner
	// role has no ensure_home behavior, so idling here would strand the worker
	// permanently (all live-observed 2026-07-16/07-17).
	needRecovery := state.CurrentPOI == ""
	if !needRecovery && !state.Doc {
		if err := deps.Client.Dock(ctx); err != nil {
			fmt.Fprintf(out, "missions: dock failed: %v; recovering to a station\n", err) //nolint:errcheck
			needRecovery = true
		}
	}
	if needRecovery {
		if done := missionRecoverToStation(ctx, deps, out, &state, current); done {
			return nil
		}
	}

	// Pirate-stronghold guard (the Dross Citadel incident: a live mission targeted a
	// stronghold and destroyed the worker flying there). Mirrors Haul's protections.
	systems, err := deps.KB.GetSystems(ctx)
	if err != nil {
		return fmt.Errorf("missions: get systems: %w", err)
	}
	strongholds := buildStrongholdRefs(systems)

	// Danger-aware galaxy graph for the route-through check below (mirrors Haul).
	// Best-effort: on a build failure, only the endpoint guard (strongholds[dest])
	// remains in force this pass.
	galGraph := &galaxy.GalaxyGraph{}
	if gerr := galGraph.BuildFromDB(ctx, deps.KB); gerr != nil {
		fmt.Fprintf(out, "missions: galaxy graph build failed: %v; route-safety disabled this pass\n", gerr) //nolint:errcheck
		galGraph = nil
	}

	// Resume held missions before accepting new ones: complete what's aboard,
	// abandon what isn't (v1 keeps resume simple; a lost-cargo mission cannot
	// be completed anyway).
	if done := missionResume(ctx, deps, out, current, strongholds); done {
		return nil
	}

	// Shed leftover cargo when we happen to be docked at home_base. Most pool
	// agents were mining bots in a past life and still haul old ore that just
	// starves the hold for deliver missions (and can strand a spent worker on
	// top of sellable goods). Passive by design — only acts when a mission or
	// reposition already parked us home, never diverts there.
	missionUnloadAtHomeBase(ctx, deps, out)

	// Idle and unencumbered: safe point for a treasury top-up before buying.
	deps.Treasury.maybe(ctx, deps.Client, out, missionNow(deps))

	// Freight candidate selection (the reconcile half ran far above). Evaluated
	// BEFORE the board read so the dry paths below can fall through to it: an
	// empty or fully-gated board is precisely where freight is worth the most,
	// because it converts a dry pass into a paying one. Any freight failure
	// degrades to "no freight this pass" — it must never be a new way for the
	// pass to fail.
	var freightBest *freightCand
	var freightHeldStops []chainStop
	if deps.EnableFreight {
		// Fresh state read, NOT the snapshot taken at the top of the pass:
		// GetState returns a clone, and both missionResume and
		// missionUnloadAtHomeBase have since freed hold space. Judging a worker
		// that just shed 400 units of ore as still full would skip freight on
		// the exact pass it finally has room for a package.
		//
		// freightHeldStops is empty whenever this point is reached — a
		// non-empty held set owned the pass in the reconcile block above —
		// but passing it keeps the code honest if that invariant ever changes.
		freightHeldStops = freightChainStops(ctx, deps, hopsTo, out)
		in := freightInputs{
			CargoFree:   float64(cargoFreeSpace(deps.Client.GetState())),
			FuelCostFor: ensureFuelModel(),
			HopsTo:      hopsTo,
			Held:        freightHeldStops,
			NowTick:     missionTick(deps),
		}
		cand, skip := freightCandidate(ctx, deps, in, out)
		if cand == nil {
			fmt.Fprintf(out, "freight: no candidate (%s)\n", skip) //nolint:errcheck
		}
		freightBest = cand
	}

	// Read the live board.
	board, baseID, ok := missionReadBoard(ctx, deps, out)
	if !ok || len(board) == 0 {
		fmt.Fprintln(out, "missions: no board entries here") //nolint:errcheck
		// An empty board is a prime freight opportunity, not a dry pass.
		// ensureFuelModel is passed unevaluated (not called) — Go evaluates
		// call arguments eagerly, so calling it here would probe the server
		// (GetConnections + FindRoute) even when freightBest is nil, i.e. on
		// every EnableFreight=false pass. missionTakeFreight only invokes the
		// builder once cand is confirmed non-nil.
		return missionFreightOrDry(ctx, deps, freightBest, freightHeldStops, hopsTo, ensureFuelModel, out)
	}

	// Distance map to every candidate destination.
	targets := make([]string, 0, len(board))
	for _, e := range board {
		// Shape-only pass to collect routing targets: pass the widest gate so a
		// smuggling destination gets a distance entry. Whether the worker may
		// actually take it is decided by the category switch below, not here.
		if _, _, _, destSys, shaped := deliverShape(e, true); shaped {
			targets = append(targets, destSys)
		}
	}
	dist := navigation.BFSJumps(graph, current, targets)

	fuelCostFor = ensureFuelModel()
	refAsk := func(itemID string) (float64, bool) {
		ra, found, aerr := deps.Market.GetReferenceAsk(ctx, itemID)
		if aerr != nil || !found {
			return 0, false
		}
		return ra.BestAsk, true
	}

	// Gate + stack.
	var cands []missionCandidate
	var exploreCands []missionCandidate
	exploreEnabled := missionCategoryEnabled(deps, missionTypeExploration)
	deliveryEnabled := missionCategoryEnabled(deps, missionTypeDelivery)
	smugglingEnabled := missionCategoryEnabled(deps, missionTypeSmuggling)
	// One pairwise distance table covers every exploration entry on the board.
	var explorePair func(a, b string) int
	if exploreEnabled {
		var allLegs []missionLeg
		for _, e := range board {
			if legs, ok := exploreShape(e); ok {
				allLegs = append(allLegs, legs...)
			}
		}
		explorePair = missionPairDist(ctx, deps, current, allLegs)
	}
	// Smuggling couriers are posted in same-destination batches (a board will
	// offer three runs to Frontier Station at once), and SelectMissionSet stacks
	// by DestSystem, so one trip carries the lot. Count the cohort per
	// destination up front; buildMissionCandidate spreads the trip fuel across
	// it, which is what lets a batch clear a floor none of its members can clear
	// alone. Cheap: one extra deliverShape pass over the board.
	smugglingCohort := map[string]int{}
	if smugglingEnabled {
		for _, e := range board {
			if e.Type != missionTypeSmuggling {
				continue
			}
			if _, _, _, destSystem, ok := deliverShape(e, true); ok {
				smugglingCohort[destSystem]++
			}
		}
	}
	for _, e := range board {
		if deps.State.wasAttempted(e.MissionID) {
			if deps.State.shouldLogSkip(e.MissionID, "already attempted") {
				fmt.Fprintf(out, "missions: skip %s: already attempted this session\n", e.MissionID) //nolint:errcheck
			}
			continue
		}
		var c missionCandidate
		var reason string
		switch {
		case e.Type == missionTypeExploration && exploreEnabled:
			c, reason = buildExploreCandidate(e, current, explorePair, fuelCostFor)
		case e.Type == missionTypeSmuggling && smugglingEnabled:
			_, _, _, destSystem, _ := deliverShape(e, true)
			c, reason = buildMissionCandidate(e, dist, refAsk, fuelCostFor, true, smugglingCohort[destSystem],
				effectiveMissionFloor(true, deps.State.smugglingSpent(), missionSmugglingXPBudget))
		case e.Type == missionTypeDelivery && deliveryEnabled:
			c, reason = buildMissionCandidate(e, dist, refAsk, fuelCostFor, false, 1, missionMinNet)
		default:
			continue // category not allowlisted for this worker
		}
		if reason != "" {
			if deps.State.shouldLogSkip(e.MissionID, reason) {
				fmt.Fprintf(out, "missions: skip %s (%s): %s\n", e.MissionID, e.Title, reason) //nolint:errcheck
			}
			continue
		}
		// Stronghold endpoint: a KB-derived skip, not a fault of the mission entry
		// itself, so it does not consume the mission's one-shot attempted slot.
		// Exploration candidates check every leg, deliveries their destination.
		if hold, ok := missionStrongholdHop(strongholds, galGraph, current, c); !ok {
			if deps.State.shouldLogSkip(e.MissionID, "stronghold "+hold) {
				fmt.Fprintf(out, "missions: skip %s: %s is (or routes through) a pirate stronghold\n", e.MissionID, hold) //nolint:errcheck
			}
			continue
		}
		if c.Legs != nil {
			exploreCands = append(exploreCands, c)
		} else {
			cands = append(cands, c)
		}
	}
	st := deps.Client.GetState()
	cargoFree := st.Ship.CargoCapacity - st.Ship.CargoUsed
	set := SelectMissionSet(cands, st.GetCredits(), cargoFree, deps.MaxJumps)
	// THE ranking point: freight, the stacked mission trip, and exploration are
	// compared here, together, once. Exploration used to be compared against the
	// mission trip alone and return immediately, so a 12k-net freight contract
	// silently lost to a 600-net exploration tour — inverting the spec, in which
	// exploration is the FALLBACK. Exploration now wins only when it out-nets
	// both of the others.
	//
	// An empty `set` nets 0, so this doubles as the "no acceptable missions"
	// decision: freight or exploration takes the pass if either has anything to
	// offer, and only a genuinely empty slate falls through to missionDryPass.
	//
	// NOTE: tripNet(set) here is the PRE-availability-gate net, i.e. an upper
	// bound on what the mission trip will really earn. Freight is therefore
	// ranked conservatively against missions; when the gate later guts the set,
	// the gate-empty path below hands the pass back to freight.
	exploreBest := bestExploreCandidate(exploreCands)
	missionNet := tripNet(set)
	freightNet, exploreNet := 0.0, 0.0
	if freightBest != nil {
		freightNet = freightBest.Net
	}
	if exploreBest != nil {
		exploreNet = exploreBest.Net
	}
	switch {
	// Freight takes ties against exploration, per "exploration is the fallback".
	case freightBest != nil && freightNet >= exploreNet && freightNet > missionNet:
		fmt.Fprintf(out, "freight: taking %s (net %.0f) over the mission trip (net %.0f) and exploration (net %.0f)\n", freightBest.Contract.ID, freightNet, missionNet, exploreNet) //nolint:errcheck
		step, ferr := missionTakeFreight(ctx, deps, freightBest, freightHeldStops, hopsTo, ensureFuelModel, out)
		switch step {
		case freightStepProceed:
			return ferr
		case freightStepStuck:
			return nil // undischarged contract; park rather than fly elsewhere
		}
		// Released — fall through to missions/exploration below.
		freightBest = nil
	case exploreBest != nil && exploreNet > missionNet:
		return missionAcceptAndExplore(ctx, deps, out, *exploreBest, current, state.CurrentPOI, baseID, strongholds)
	}
	if len(set) == 0 {
		fmt.Fprintln(out, "missions: no acceptable missions on this board") //nolint:errcheck
		return missionDryPass(ctx, deps, out)
	}

	// Availability gate: never accept a mission whose cargo can't actually be
	// acquired at this station — accept-then-abandon churn pollutes telemetry
	// and pays any abandon penalty for nothing (live-observed: PHASE 5's
	// optical_fiber_bundle re-attempted on every worker restart). One
	// view_storage + view_market pair covers the whole batch; the storage map
	// is reused by the withdraw step below.
	storage := map[string]float64{}
	needBuy := false
	for _, c := range set {
		if c.BuyQty > 0 {
			needBuy = true
			break
		}
	}
	if needBuy {
		fetched, serr := missionFetchStorage(ctx, deps)
		if serr != nil {
			fmt.Fprintf(out, "missions: view_storage: %v; assuming empty storage\n", serr) //nolint:errcheck
		} else if fetched != nil {
			storage = fetched
		}
		ladders, merr := missionFetchMarketLadders(ctx, deps)
		if merr != nil || ladders == nil {
			// nil ladders = raw store not settled: no data is not "no stock";
			// gating on it would wrongly mark acquirable missions attempted.
			fmt.Fprintf(out, "missions: view_market unavailable (%v); availability gate skipped this pass\n", merr) //nolint:errcheck
		} else {
			// Working copies so each candidate is priced against what its BUY
			// step will actually see. SelectMissionSet stacks same-destination
			// missions with NO item-level dedup, so two "deliver iron_ore to Z"
			// candidates share the same ladder and storage; pricing both against
			// the un-decremented top-of-book lets candidate B pass surge/net
			// checks at a cheap avg that candidate A's buy will consume, then B
			// walks the wall unchecked at buy time (the fighter-4 overpay). The
			// gate iterates `set` in the same order the buy step will, so
			// decrementing these working copies as candidates are admitted
			// exactly mirrors the sequential buy. The REAL storage/ladders maps
			// are left untouched — the buy step re-reads/decrements real storage.
			workStorage := maps.Clone(storage)
			if workStorage == nil {
				workStorage = map[string]float64{}
			}
			workLadders := make(map[string][]market.AskLevel, len(ladders))
			acquirable := make([]missionCandidate, 0, len(set))
			for _, c := range set {
				if c.BuyQty <= 0 {
					acquirable = append(acquirable, c)
					continue
				}
				lad, seen := workLadders[c.ItemID]
				if !seen {
					lad = ladders[c.ItemID] // first same-item candidate reads the real ladder
				}
				usedStore := min(workStorage[c.ItemID], float64(c.BuyQty))
				marketQty := float64(c.BuyQty) - usedStore
				localCost, _, avgFill, enough := market.CostToAcquire(lad, marketQty)
				if marketQty > 0 && !enough {
					fmt.Fprintf(out, "missions: skip %s (%s): market here can't fill %.0f %s\n", c.Entry.MissionID, c.Entry.Title, marketQty, c.ItemID) //nolint:errcheck
					deps.State.markAttempted(c.Entry.MissionID)                                                                                         // don't re-price it every pass this session
					continue                                                                                                                            // skipped: buys nothing, so consume nothing
				}
				if ref, ok, _ := deps.Market.GetReferencePrice(ctx, c.ItemID, missionReferenceLookback); ok && avgFill > missionSurgeMult*ref {
					fmt.Fprintf(out, "missions: skip %s (%s): %s avg %.0f > %.1fx reference %.0f (surge)\n", c.Entry.MissionID, c.Entry.Title, c.ItemID, avgFill, missionSurgeMult, ref) //nolint:errcheck
					deps.State.markAttempted(c.Entry.MissionID)
					continue
				}
				net := c.Reward - localCost - c.FuelCost
				if net < missionMinNet {
					fmt.Fprintf(out, "missions: skip %s (%s): local net %.0f below floor %.0f (item cost %.0f)\n", c.Entry.MissionID, c.Entry.Title, net, missionMinNet, localCost) //nolint:errcheck
					deps.State.markAttempted(c.Entry.MissionID)
					continue
				}
				// ItemCost carries avgFill*BuyQty (the market avg price applied to
				// the FULL buy qty), not localCost (which only covers the
				// market-bought portion when storage covers part of BuyQty).
				// Downstream unitCost := ItemCost/BuyQty must equal avgFill so
				// that only-the-bought-units costing (remaining*unitCost) prices
				// them at the real market rate rather than an understated blend.
				// c.Net still uses the true economic cost (localCost — storage
				// units are free) and is unaffected by this.
				c.ItemCost, c.Net = avgFill*float64(c.BuyQty), net
				// Admitted: consume the depth + storage this candidate will take
				// so the NEXT same-item candidate prices against the residual.
				// comma-ok read above means an emptied ladder (nil) is honored,
				// not re-read from the full real ladder.
				workLadders[c.ItemID] = market.ConsumeAsks(lad, marketQty)
				workStorage[c.ItemID] -= usedStore
				acquirable = append(acquirable, c)
			}
			set = acquirable
			if len(set) == 0 {
				fmt.Fprintln(out, "missions: no acquirable missions on this board") //nolint:errcheck
				// A fully-gated board is the other prime freight opportunity —
				// and the candidate is already computed, so discarding it here
				// would waste the whole evaluation. Freight lost the ranking
				// above against the PRE-gate mission net; now that the gate has
				// emptied the set, it is the only thing left.
				return missionFreightOrDry(ctx, deps, freightBest, freightHeldStops, hopsTo, ensureFuelModel, out)
			}
		}
	}

	// Accept. A failed accept just drops that mission from the trip.
	acceptedAt, acceptedTick := rfc(missionNow(deps)), missionTick(deps)
	accepted := make([]missionCandidate, 0, len(set))
	for _, c := range set {
		if aerr := deps.Client.AcceptMission(ctx, c.Entry.MissionID); aerr != nil {
			fmt.Fprintf(out, "missions: accept %s failed: %v\n", c.Entry.MissionID, aerr) //nolint:errcheck
			continue
		}
		// Book the XP purchase only once the server has actually granted the
		// run: a rejected accept (skill_required, gone, taken) buys nothing and
		// must not consume budget.
		if c.Entry.Type == missionTypeSmuggling && c.Net < 0 {
			deps.State.noteSmugglingLoss(c.Net)
			fmt.Fprintf(out, "missions: smuggling %s taken at net %.0f for XP; budget spent %.0f/%.0f\n", //nolint:errcheck
				c.Entry.MissionID, c.Net, deps.State.smugglingSpent(), missionSmugglingXPBudget)
		}
		accepted = append(accepted, c)
	}
	if len(accepted) == 0 {
		return missionDryPass(ctx, deps, out)
	}
	publishActivity(deps.SetActivity, missionActivityLabel(accepted))

	// Resolve each acceptance's real (hex) active-mission id before touching
	// cargo: board ids are template-ish, and complete_mission/abandon_mission
	// with a template id 404 with mission_not_found once the server has
	// created the active instance. AcceptMission's own Submit already blocks
	// for that instance's action_result, but the server has been observed
	// needing one more tick before get_active_missions reflects it — settle
	// before querying.
	_ = deps.sleep(ctx, game.SleepTick)
	actives, aerr := missionFetchActiveMissions(ctx, deps)
	if aerr != nil {
		fmt.Fprintf(out, "missions: resolve active ids: get_active_missions: %v; %d accepted mission(s) held for next pass\n", aerr, len(accepted)) //nolint:errcheck
		return nil                                                                                                                                  // ids unresolved; nothing safe to buy/complete/abandon this pass — missionResume picks these up next pass
	}
	accepted = resolveActiveMissionIDs(accepted, actives)
	resolvable := make([]missionCandidate, 0, len(accepted))
	for _, c := range accepted {
		if c.ActiveID == "" {
			fmt.Fprintf(out, "missions: accept %s (%s): could not resolve active mission id; dropping (no abandon issued — id unknown)\n", c.Entry.MissionID, c.Entry.Title) //nolint:errcheck
			continue
		}
		resolvable = append(resolvable, c)
	}
	if len(resolvable) == 0 {
		return missionDryPass(ctx, deps, out)
	}

	// Provision: withdraw pre-stocked station storage before buying anything
	// (the engineer-1 smoke: 50 iron_ore was sitting in storage while
	// acquisition bought blind and failed item_not_available). The storage
	// map comes from the availability gate above (one view_storage per batch;
	// a tick stale by now, but withdraw failures degrade to buying anyway).
	trip := make([]missionCandidate, 0, len(resolvable))
	for _, c := range resolvable {
		if c.BuyQty > 0 {
			unitCost := c.ItemCost / float64(c.BuyQty) // guarded: this branch only runs when BuyQty > 0
			withdrawn := 0.0
			if avail := storage[c.ItemID]; avail > 0 {
				take := min(avail, float64(c.BuyQty))
				if werr := deps.Client.WithdrawItems(ctx, c.ItemID, take); werr != nil {
					fmt.Fprintf(out, "missions: withdraw %.0fx %s for %s failed: %v; buying full remainder\n", take, c.ItemID, c.Entry.Title, werr) //nolint:errcheck
				} else {
					_ = deps.sleep(ctx, game.SleepQuick)
					withdrawn = take
					storage[c.ItemID] -= take
				}
			}
			remaining := c.BuyQty - int(withdrawn)
			c.ItemCost = float64(remaining) * unitCost // withdrawn units cost 0; only bought units count
			if remaining > 0 {
				if berr := deps.Client.Buy(ctx, c.ItemID, float64(remaining)); berr != nil {
					fmt.Fprintf(out, "missions: buy %dx %s for %s failed: %v; abandoning\n", remaining, c.ItemID, c.Entry.Title, berr) //nolint:errcheck
					if withdrawn > 0 {
						fmt.Fprintf(out, "missions: %.0f already-withdrawn %s unit(s) remain in cargo after abandon\n", withdrawn, c.ItemID) //nolint:errcheck
					}
					c.ItemCost = 0 // buy failed, nothing spent
					missionAbandon(ctx, deps, out, c, baseID, acceptedAt, acceptedTick, "buy_failed")
					continue
				}
			}
		}
		trip = append(trip, c)
	}
	if len(trip) == 0 {
		// Every accepted candidate was abandoned (all buys failed): counts as
		// a dry pass so reposition still fires, even though selection wasn't
		// empty.
		return missionDryPass(ctx, deps, out)
	}
	if deps.State != nil {
		// Executed real work: reset the dry streak and the circuit/park state.
		deps.State.dry = 0
		deps.State.hopsDry = 0
		deps.State.parkedUntil = time.Time{}
	}

	// One shared destination system (SelectMissionSet guarantees it). Transit,
	// then complete each mission at its own base within that system.
	dest := trip[0].DestSystem
	fmt.Fprintf(out, "missions: running %d mission(s) to %s (%d jumps, est net %.0f)\n", len(trip), dest, trip[0].Jumps, tripNet(trip)) //nolint:errcheck
	for i, c := range trip {
		if nerr := deps.nav(ctx, dest, c.DestBaseID); nerr != nil {
			fmt.Fprintf(out, "missions: transit to %s failed: %v; %d mission(s) left held for next pass\n", c.DestBaseID, nerr, len(trip)-i) //nolint:errcheck
			return nil                                                                                                                       // held missions resume on the next pass
		}
		if derr := deps.Client.Dock(ctx); derr != nil {
			fmt.Fprintf(out, "missions: dock at %s failed: %v; held for next pass\n", c.DestBaseID, derr) //nolint:errcheck
			return nil
		}
		missionComplete(ctx, deps, out, c, baseID, acceptedAt, acceptedTick)
	}
	return nil
}

// missionRecoverToStation gets an undocked worker to a dockable station so a
// board can be read. It first tries a public station in the current system;
// if none is known (a station-less "lawless" system — live 2026-07-17:
// engineer-2 stranded 15 min at wealth_lane_ice_belt after a reposition jump
// timed out mid-route), it escapes to the nearest accessible station in
// another system via the reposition machinery rather than idling until the
// stall watchdog kills it. Returns true when the pass should end (recovery
// navigated, or an escape/transit failed and will retry next pass); false
// only when the worker is already at a usable local station and the pass may
// continue. *state is refreshed after a successful in-system recovery.
func missionRecoverToStation(ctx context.Context, deps MissionDeps, out io.Writer, state **game.State, current string) bool {
	poi, perr := missionStationPOI(ctx, deps.KB, current)
	if perr == nil && poi != "" {
		fmt.Fprintf(out, "missions: not at a station POI; recovering to %s/%s\n", current, poi) //nolint:errcheck
		if nerr := deps.nav(ctx, current, poi); nerr != nil {
			fmt.Fprintf(out, "missions: recovery transit failed: %v; retry next pass\n", nerr) //nolint:errcheck
			return true
		}
		if st := deps.Client.GetState(); st != nil {
			*state = st // refresh the pre-recovery snapshot (POI/docked changed)
		}
		if !(*state).Doc {
			if derr := deps.Client.Dock(ctx); derr != nil {
				fmt.Fprintf(out, "missions: dock after recovery failed: %v; retry next pass\n", derr) //nolint:errcheck
				return true
			}
		}
		return false // docked at the local station; continue the pass
	}
	// No station in this system: escape to the nearest one elsewhere instead of
	// idling (the reposition query already excludes strongholds/non-public).
	hops, herr := deps.nearbyStations(ctx, missionRepositionPool)
	if herr != nil || len(hops) == 0 {
		fmt.Fprintf(out, "missions: stranded in %s with no reachable station (err=%v, %d targets); retry next pass\n", current, herr, len(hops)) //nolint:errcheck
		return true
	}
	hop := hops[0]
	fmt.Fprintf(out, "missions: no station in %s; escaping to %s/%s\n", current, hop.SystemID, hop.POIID) //nolint:errcheck
	if nerr := deps.nav(ctx, hop.SystemID, hop.POIID); nerr != nil {
		fmt.Fprintf(out, "missions: escape transit failed: %v; retry next pass\n", nerr) //nolint:errcheck
		return true
	}
	if derr := deps.Client.Dock(ctx); derr != nil {
		fmt.Fprintf(out, "missions: escape dock failed: %v; retry next pass\n", derr) //nolint:errcheck
	}
	return true // re-read the board fresh at the new station next pass
}

// missionDryPass counts a no-work pass; on the missionDryPassLimit-th
// consecutive one, the worker repositions to the next nearby station
// (rotating cursor) instead of camping a dry board forever. Nil State (no
// cross-pass memory) just idles.
func missionDryPass(ctx context.Context, deps MissionDeps, out io.Writer) error {
	if deps.State == nil {
		return nil
	}
	if !deps.State.parkedUntil.IsZero() && missionNow(deps).Before(deps.State.parkedUntil) {
		return nil // parked: camp the local board instead of burning fuel
	}
	deps.State.dry++
	if deps.State.dry < missionDryPassLimit {
		return nil
	}
	// Pinned worker: its assigned board IS the assignment, so a dry pass waits
	// there rather than touring. Only a mission trip or freight leg should ever
	// carry it away, and then it comes straight back.
	if deps.HomeStation != "" {
		st := deps.Client.GetState()
		// docked_at_base rides on the PLAYER object; the ship payload has no
		// such field, so reading it off the ship silently never matches.
		if st != nil && st.Doc && st.Player.DockedAtBase == deps.HomeStation {
			deps.State.dry = 0
			deps.State.parkedUntil = missionNow(deps).Add(missionParkWindow)
			fmt.Fprintf(out, "missions: %d dry passes at pinned station %s; parking for %s\n", missionDryPassLimit, deps.HomeStation, missionParkWindow) //nolint:errcheck

			return nil
		}
		deps.State.dry = 0
		fmt.Fprintf(out, "missions: %d dry passes; returning to pinned station %s\n", missionDryPassLimit, deps.HomeStation) //nolint:errcheck

		return missionNavToBase(ctx, deps, deps.HomeStation)
	}
	hops, err := deps.nearbyStations(ctx, missionRepositionPool)
	if err != nil || len(hops) == 0 {
		fmt.Fprintf(out, "missions: reposition lookup failed (%v, %d targets); idling\n", err, len(hops)) //nolint:errcheck
		return nil
	}
	if deps.State.hopsDry >= len(hops) {
		// A full circuit of the pool found no work: every reachable board is
		// dry. Park here — passes keep polling the local board for free.
		deps.State.hopsDry = 0
		deps.State.dry = 0
		deps.State.parkedUntil = missionNow(deps).Add(missionParkWindow)
		fmt.Fprintf(out, "missions: full reposition circuit dry; parking at current station for %s\n", missionParkWindow) //nolint:errcheck
		return nil
	}
	hop := hops[deps.State.cursor%len(hops)]
	deps.State.cursor++
	deps.State.hopsDry++
	deps.State.dry = 0
	fmt.Fprintf(out, "missions: %d dry passes; repositioning to %s/%s\n", missionDryPassLimit, hop.SystemID, hop.POIID) //nolint:errcheck
	if nerr := deps.nav(ctx, hop.SystemID, hop.POIID); nerr != nil {
		fmt.Fprintf(out, "missions: reposition transit failed: %v; retry next pass\n", nerr) //nolint:errcheck
		return nil
	}
	if derr := deps.Client.Dock(ctx); derr != nil {
		fmt.Fprintf(out, "missions: reposition dock failed: %v\n", derr) //nolint:errcheck
	}
	return nil
}

// missionStationPOI returns the id of an accessible (public, non-stronghold)
// station POI in systemID, or "" if none is known to the KB. Mirrors the
// selection galaxy.FindNearestByPOIType's internal queryAccessibleStations
// already applied to pick systemID in the first place — see the
// nearbyStations default closure above for why NearestResult.POIs can't be
// used directly.
func missionStationPOI(ctx context.Context, kb knowledge.Base, systemID string) (string, error) {
	pois, err := kb.GetPOIs(ctx, systemID)
	if err != nil {
		return "", err
	}
	for _, p := range pois {
		if p.Type != "station" {
			continue
		}
		base, berr := kb.GetBaseByPOI(ctx, p.ID)
		if berr == nil && base != nil && base.PublicAccess {
			return p.ID, nil
		}
	}
	return "", nil
}

// tripNet sums the estimated net of a selected trip (for the log line).
func tripNet(trip []missionCandidate) float64 {
	total := 0.0
	for _, c := range trip {
		total += c.Net
	}
	return total
}

// missionReadBoard fetches and parses the local mission board. ok=false on any
// fetch/parse problem (logged), so the pass idles rather than erroring out.
func missionReadBoard(ctx context.Context, deps MissionDeps, out io.Writer) ([]serverapi.MissionBoardEntry, string, bool) {
	if err := deps.Client.GetMissions(ctx); err != nil {
		fmt.Fprintf(out, "missions: get_missions: %v\n", err) //nolint:errcheck
		return nil, "", false
	}
	_ = deps.sleep(ctx, game.SleepQuick)
	raw := deps.Client.GetRawJSON("missions")
	if len(raw) == 0 {
		fmt.Fprintln(out, "missions: get_missions returned no data") //nolint:errcheck
		return nil, "", false
	}
	var resp serverapi.GetMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		fmt.Fprintf(out, "missions: parse board: %v\n", err) //nolint:errcheck
		return nil, "", false
	}
	return resp.Missions, resp.BaseID, true
}

// missionRouteClear reports whether the shortest path from -> to avoids every
// pirate stronghold, modeled on haul.go's filterStrongholdRoutes clear()
// closure. pathOf is GalaxyGraph.FindPath, injected so this is unit-testable
// without a populated graph. A lookup error, or either endpoint empty/equal,
// is treated as clear so a graph gap never blocks a mission run — the
// endpoint guard (strongholds[dest] in the caller) remains the backstop.
func missionRouteClear(pathOf func(from, to string, weighted bool) (galaxy.Route, error), strongholds map[string]bool, from, to string) bool {
	if from == "" || to == "" || from == to {
		return true
	}
	route, err := pathOf(from, to, false)
	if err != nil {
		return true // no path / unknown system — do not block on a lookup failure
	}
	for _, sys := range route.Path {
		if strongholds[sys] {
			return false
		}
	}
	return true
}

// missionUnloadAtHomeBase clears leftover, non-mission cargo whenever the worker
// is docked at its own home_base. Most pool agents were mining bots in a past
// life and still carry ore that starves the hold for deliver missions and,
// once a worker spends down, can strand it broke on top of sellable goods.
// It runs at the idle point after missionResume, where anything still aboard is
// leftover (not mission cargo), so clearing the whole hold is safe: sell each
// item into a local buy order for credits, then DepositAllItems to sweep
// whatever had no buyer into home storage — the hold is always freed. Passive
// by design: it never diverts the worker home, only acts when a mission or
// reposition already parked it there. Best-effort; sell/deposit errors are
// logged and the cargo simply waits for the next home visit.
func missionUnloadAtHomeBase(ctx context.Context, deps MissionDeps, out io.Writer) {
	state := deps.Client.GetState()
	if state == nil || state.Player.HomeBase == "" || state.CurrentPOI != state.Player.HomeBase {
		return
	}
	var items []game.CargoItem
	for _, c := range state.Ship.Cargo {
		if c.Quantity > 0 {
			items = append(items, c)
		}
	}
	if len(items) == 0 {
		return
	}
	sold := 0
	for _, it := range items {
		if err := deps.Client.Sell(ctx, it.ItemID, it.Quantity); err != nil {
			continue // no local buyer (or transient) -> caught by the deposit sweep
		}
		sold++
		fmt.Fprintf(out, "missions: sold %.0f %s at home base %s\n", it.Quantity, it.ItemID, state.CurrentPOI) //nolint:errcheck
	}
	// Sweep whatever did not sell into home storage so the hold is always freed
	// for mission cargo. No-op server-side when the hold is already empty.
	if err := deps.Client.DepositAllItems(ctx); err != nil {
		fmt.Fprintf(out, "missions: home-base deposit of leftover cargo failed: %v\n", err) //nolint:errcheck
		return
	}
	fmt.Fprintf(out, "missions: unloaded leftover cargo at home base %s (%d/%d item type(s) sold, rest deposited)\n", state.CurrentPOI, sold, len(items)) //nolint:errcheck
}

// missionResume handles missions held from a previous pass/process: deliverable
// ones (goods aboard) are run to completion; the rest are abandoned so their
// slots free up. strongholds gates deliverable resumes whose resolved
// destination system is a pirate stronghold (abandoned instead of flown to).
// Returns true when it acted (the pass ends; the board is read fresh next pass).
func missionResume(ctx context.Context, deps MissionDeps, out io.Writer, current string, strongholds map[string]bool) bool {
	actives, err := missionFetchActiveMissions(ctx, deps)
	if err != nil {
		fmt.Fprintf(out, "missions: get_active_missions: %v\n", err) //nolint:errcheck
		return false
	}
	if len(actives) == 0 {
		return false
	}
	acted := false
	for _, m := range actives {
		if m.Type == missionTypeExploration && missionCategoryEnabled(deps, missionTypeExploration) {
			if missionResumeExplore(ctx, deps, out, m, current, strongholds) {
				acted = true
			}
			continue
		}
		// Resume smuggling only for a worker still allowlisted for it. If the
		// operator revokes the category mid-run the mission is left alone
		// rather than abandoned: it stays on the books for manual handling,
		// which is the same treatment any other unrecognised active gets.
		if !missionDeliverType(m.Type, missionCategoryEnabled(deps, missionTypeSmuggling)) || len(m.Objectives) != 1 {
			continue // non-single-leg-delivery active mission: leave it alone (manual/other origin)
		}
		o := m.Objectives[0]
		if o.Type != missionObjectiveDeliver {
			continue // not a deliver objective: leave it alone (manual/other origin)
		}
		remaining := o.Required - o.Current
		aboard := cargoQty(deps.Client.GetState(), o.ItemID)
		held := missionCandidate{
			Entry: serverapi.MissionBoardEntry{
				MissionID: m.MissionID, TemplateID: m.TemplateID, Type: m.Type, Title: m.Title,
			},
			// m.MissionID is already the server's real active-mission id
			// (this list comes straight from get_active_missions), so no
			// resolution step is needed here — unlike a freshly accepted trip.
			ActiveID: m.MissionID,
			ItemID:   o.ItemID, Qty: max(remaining, 0), DestBaseID: o.TargetBase,
		}
		// An objective can be satisfied with no cargo aboard: storage at the
		// target base counts toward delivery (the actives wire reports it as
		// "in_storage"). A completed/covered objective still needs an explicit
		// complete_mission at the target base to claim the reward.
		if o.Completed || remaining <= 0 || aboard >= float64(remaining) {
			// Deliverable: the objective already carries the destination system;
			// fall back to FindRoute only when it's missing (defensive — the
			// live wire always populates it for deliver_item).
			destSys := o.SystemID
			if destSys == "" {
				route, rerr := deps.Client.FindRoute(ctx, o.TargetBase)
				destSys = current
				if rerr == nil && len(route) > 0 {
					destSys = route[len(route)-1].SystemID
				}
			}
			if strongholds[destSys] {
				fmt.Fprintf(out, "missions: abandoning held %s (%s): destination %s is a pirate stronghold\n", m.MissionID, m.Title, destSys) //nolint:errcheck
				missionAbandon(ctx, deps, out, held, "", rfc(missionNow(deps)), missionTick(deps), "stronghold_destination")
				acted = true
				continue
			}
			fmt.Fprintf(out, "missions: resuming held %s (%s) -> %s\n", m.MissionID, m.Title, o.TargetBase) //nolint:errcheck
			publishActivity(deps.SetActivity, "Mission "+m.Title)
			if nerr := deps.nav(ctx, destSys, o.TargetBase); nerr != nil {
				fmt.Fprintf(out, "missions: resume transit failed: %v; retry next pass\n", nerr) //nolint:errcheck
				return true
			}
			if derr := deps.Client.Dock(ctx); derr != nil {
				fmt.Fprintf(out, "missions: resume dock failed: %v; retry next pass\n", derr) //nolint:errcheck
				return true
			}
			missionComplete(ctx, deps, out, held, "", rfc(missionNow(deps)), missionTick(deps))
		} else {
			fmt.Fprintf(out, "missions: abandoning held %s (%s): cargo %s %.0f/%d\n", m.MissionID, m.Title, o.ItemID, aboard, remaining) //nolint:errcheck
			missionAbandon(ctx, deps, out, held, "", rfc(missionNow(deps)), missionTick(deps), "cargo_lost")
		}
		acted = true
	}
	return acted
}

// missionComplete completes c (using its resolved ActiveID — the board id
// 404s once the active instance exists) and records the row. Realized income
// prefers the complete_mission action_result's credits_earned (the actual
// server figure) over the wallet delta: parseActionResult has no "action"
// case for complete_mission, so State.Credits is never updated by this
// response and the delta this measured against `before` is frequently a
// stale/zero read racing whatever else touches the wallet meanwhile.
func missionComplete(ctx context.Context, deps MissionDeps, out io.Writer, c missionCandidate, fromBase, acceptedAt string, acceptedTick int64) {
	before := deps.Client.GetState().GetCredits()
	if err := deps.Client.CompleteMission(ctx, c.ActiveID); err != nil {
		fmt.Fprintf(out, "missions: complete %s failed: %v; held for next pass\n", c.ActiveID, err) //nolint:errcheck
		return
	}
	_ = deps.sleep(ctx, game.SleepTick) // settle: let the complete_mission action_result land in the raw JSON store
	earned, source := missionCreditsEarned(deps, c, before)
	fmt.Fprintf(out, "missions: completed %s (%s): +%.0f cr (expected %.0f) [source: %s]\n", c.Entry.MissionID, c.Entry.Title, earned, c.Reward, source) //nolint:errcheck
	missionRecord(ctx, deps, out, c, fromBase, acceptedAt, acceptedTick, earned, "completed", "")
}

// missionCreditsEarned returns the realized reward for a just-completed
// mission plus which source it came from ("action_result" or "delta"), for
// the log line. It prefers the complete_mission action_result's
// credits_earned field (wire-verified shape: {"command":"complete_mission",
// "result":{"credits_earned":N,"mission_id":"...",...}}, cached by the raw
// JSON store under the "complete_mission" key — see unwrapActionResult in
// craft_node.go for the same wrapper shape on a different command). Falls
// back to the wallet delta when the payload is missing, unparsable, or
// reports a different mission id (a stale frame from an earlier complete
// call still sitting in the store).
func missionCreditsEarned(deps MissionDeps, c missionCandidate, before float64) (earned float64, source string) {
	delta := deps.Client.GetState().GetCredits() - before
	raw := deps.Client.GetRawJSON("complete_mission")
	if len(raw) == 0 {
		return delta, "delta"
	}
	var res serverapi.CompleteMissionResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &res); err != nil {
		return delta, "delta"
	}
	if res.MissionID != c.ActiveID {
		return delta, "delta" // stale frame from a previous complete_mission call
	}
	return float64(res.CreditsEarned), "action_result"
}

// missionAbandon abandons c (using its resolved ActiveID) and records the
// loss row. c.ItemCost must reflect what was actually spent — callers zero
// it when the buy never happened (a failed-buy abandon); it is only nonzero
// when cargo was actually purchased and then stranded (e.g. a resume abandon
// with cargo already sunk).
// missionAbandon abandons c and records the outcome. reason is a stable,
// machine-readable cause slug (see docs/superpowers/specs/
// 2026-07-17-mission-abandon-catalog.md) so abandons are queryable from
// mission_results instead of log archaeology.
func missionAbandon(ctx context.Context, deps MissionDeps, out io.Writer, c missionCandidate, fromBase, acceptedAt string, acceptedTick int64, reason string) {
	if err := deps.Client.AbandonMission(ctx, c.ActiveID); err != nil {
		fmt.Fprintf(out, "missions: abandon %s failed: %v\n", c.ActiveID, err) //nolint:errcheck
	}
	missionRecord(ctx, deps, out, c, fromBase, acceptedAt, acceptedTick, 0, "abandoned", reason)
}

// missionFetchActiveMissions fetches and parses get_active_missions. A nil
// error with a nil/empty slice means "no active missions" (or no data yet);
// callers that need to distinguish a fetch failure check the error.
func missionFetchActiveMissions(ctx context.Context, deps MissionDeps) ([]serverapi.ActiveMission, error) {
	if err := deps.Client.GetActiveMissions(ctx); err != nil {
		return nil, err
	}
	_ = deps.sleep(ctx, game.SleepQuick)
	raw := deps.Client.GetRawJSON("active_missions")
	if len(raw) == 0 {
		return nil, nil
	}
	var resp serverapi.GetActiveMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse active missions: %w", err)
	}
	return resp.Missions, nil
}

// missionFetchStorage fetches and parses view_storage into an item-id ->
// quantity map for the current station, one call for a whole accept batch.
// A nil map with a nil error means "no data" (empty storage, or the raw
// store hadn't settled) — not a fetch failure.
func missionFetchStorage(ctx context.Context, deps MissionDeps) (map[string]float64, error) {
	if err := deps.Client.ViewStorage(ctx); err != nil {
		return nil, err
	}
	_ = deps.sleep(ctx, game.SleepQuick)
	raw := deps.Client.GetRawJSON("storage")
	if len(raw) == 0 {
		return nil, nil
	}
	var resp serverapi.ViewStorageResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse storage: %w", err)
	}
	items := make(map[string]float64, len(resp.Items))
	for _, it := range resp.Items {
		items[it.ItemID] = it.Quantity
	}
	return items, nil
}

// missionFetchMarketLadders fetches and parses a full view_market into
// per-item ascending-by-price sell ladders for the current station, one call
// for a whole accept batch. Only positive-price, positive-quantity sell
// orders count. A nil map with a nil error means "no data" (raw store not
// settled) — not a fetch failure; callers must not gate on it.
func missionFetchMarketLadders(ctx context.Context, deps MissionDeps) (map[string][]market.AskLevel, error) {
	if err := deps.Client.ViewMarket(ctx, map[string]any{}); err != nil {
		return nil, err
	}
	_ = deps.sleep(ctx, game.SleepQuick)
	raw := deps.Client.GetRawJSON("market")
	if len(raw) == 0 {
		return nil, nil
	}
	var resp struct {
		Items []serverapi.ViewMarketItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse market: %w", err)
	}
	ladders := make(map[string][]market.AskLevel, len(resp.Items))
	for _, it := range resp.Items {
		var lvls []market.AskLevel
		for _, o := range it.SellOrders {
			// Skip empties AND the server's not-for-sale sentinel (999999 and its
			// 1000000 sibling). GetAskLadder/GetReferencePrice strip it in SQL via
			// notForSaleSQL; the mission ladder must match or a reached sentinel
			// corrupts enoughDepth and skip reasons.
			if o.PriceEach <= 0 || o.Quantity <= 0 || o.PriceEach >= market.NotForSalePrice {
				continue
			}
			lvls = append(lvls, market.AskLevel{PriceEach: o.PriceEach, Quantity: o.Quantity})
		}
		slices.SortFunc(lvls, func(a, b market.AskLevel) int { return cmp.Compare(a.PriceEach, b.PriceEach) })
		ladders[it.ItemID] = lvls
	}
	return ladders, nil
}

func missionRecord(ctx context.Context, deps MissionDeps, out io.Writer, c missionCandidate, fromBase, acceptedAt string, acceptedTick int64, earned float64, outcome, reason string) {
	deps.State.markAttempted(c.Entry.MissionID) // never re-select this mission this session
	now := missionNow(deps)
	r := market.MissionResult{
		AgentID:        deps.AgentID,
		MissionID:      c.Entry.MissionID,
		TemplateID:     c.Entry.TemplateID,
		MissionType:    c.Entry.Type,
		Title:          c.Entry.Title,
		FromBaseID:     fromBase,
		ToBaseID:       c.DestBaseID,
		ItemID:         c.ItemID,
		Qty:            float64(c.Qty),
		ExpectedReward: c.Reward,
		CreditsEarned:  earned,
		ItemCost:       c.ItemCost,
		FuelCost:       c.FuelCost,
		Jumps:          c.Jumps,
		Outcome:        outcome,
		Reason:         reason,
		AcceptedAt:     acceptedAt,
		FinishedAt:     rfc(now),
		AcceptedTick:   acceptedTick,
		FinishedTick:   missionTick(deps),
		CreatedAt:      rfc(now),
	}
	if err := deps.Market.RecordMissionResult(ctx, r); err != nil {
		fmt.Fprintf(out, "missions: record result %s: %v\n", c.Entry.MissionID, err) //nolint:errcheck
	}
}
