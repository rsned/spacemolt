package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
)

// MissionStore is the subset of *market.Collector the mission runner needs
// (result telemetry + item pricing for the cost gate).
type MissionStore interface {
	RecordMissionResult(ctx context.Context, r market.MissionResult) error
	GetReferenceAsk(ctx context.Context, itemID string) (market.ReferenceAsk, bool, error)
}

var _ MissionStore = (*market.Collector)(nil)

// missionRunState carries cross-pass memory (dry-pass streak + reposition
// cursor + the set of missions already attempted this session), held by
// WorkerDispatch so it survives between command passes — the shuttleState
// pattern.
type missionRunState struct {
	dry       int
	cursor    int
	attempted map[string]bool
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
	// A board only exists at a station. Not at one -> dock if possible, else
	// idle this pass (the role's ensure_home/reposition machinery moves us).
	if state.CurrentPOI == "" {
		fmt.Fprintln(out, "missions: not at a station POI; idling") //nolint:errcheck
		return nil
	}
	if !state.Doc {
		if err := deps.Client.Dock(ctx); err != nil {
			fmt.Fprintf(out, "missions: dock failed: %v; idling\n", err) //nolint:errcheck
			return nil
		}
	}

	// Routing substrate (same shape as Haul).
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		return fmt.Errorf("missions: get connections: %w", err)
	}
	graph := navigation.JumpGraphFromConnections(conns)
	current := state.System.ID

	// Default reposition source: nearest accessible stations by the galaxy
	// graph (the same query haul's stranded-recovery uses; it excludes
	// strongholds and non-public stations).
	if deps.nearbyStations == nil {
		deps.nearbyStations = func(ctx context.Context, limit int) ([]stationHop, error) {
			gal := &galaxy.GalaxyGraph{}
			if gerr := gal.BuildFromDB(ctx, deps.KB); gerr != nil {
				return nil, gerr
			}
			near, nerr := galaxy.FindNearestByPOIType(ctx, deps.KB, gal, current, "station", limit)
			if nerr != nil {
				return nil, nerr
			}
			hops := make([]stationHop, 0, len(near))
			for _, n := range near {
				if n.SystemID == current || len(n.POIs) == 0 {
					continue // skip the station we're camping; we want elsewhere
				}
				hops = append(hops, stationHop{SystemID: n.SystemID, POIID: n.POIs[0].ID})
			}
			return hops, nil
		}
	}

	// Resume held missions before accepting new ones: complete what's aboard,
	// abandon what isn't (v1 keeps resume simple; a lost-cargo mission cannot
	// be completed anyway).
	if done := missionResume(ctx, deps, out, current); done {
		return nil
	}

	// Idle and unencumbered: safe point for a treasury top-up before buying.
	deps.Treasury.maybe(ctx, deps.Client, out, missionNow(deps))

	// Read the live board.
	board, baseID, ok := missionReadBoard(ctx, deps, out)
	if !ok || len(board) == 0 {
		fmt.Fprintln(out, "missions: no board entries here") //nolint:errcheck
		return missionDryPass(ctx, deps, out)
	}

	// Distance map to every candidate destination.
	targets := make([]string, 0, len(board))
	for _, e := range board {
		if _, _, _, destSys, shaped := deliverShape(e); shaped {
			targets = append(targets, destSys)
		}
	}
	dist := navigation.BFSJumps(graph, current, targets)

	// Fuel model (same probe as Haul).
	probeTarget := ""
	for _, nb := range graph[current] {
		probeTarget = nb
		break
	}
	fuelPerJump := haulFuelPerJump(ctx, deps.Client, probeTarget)
	priceOf := buildPriceOf(ctx, deps.FuelPrices)
	fuelCostFor := func(jumps int) float64 {
		if fuelPerJump <= 0 || priceOf == nil {
			return 0
		}
		return float64(jumps*fuelPerJump) * priceOf(state.CurrentPOI)
	}
	refAsk := func(itemID string) (float64, bool) {
		ra, found, aerr := deps.Market.GetReferenceAsk(ctx, itemID)
		if aerr != nil || !found {
			return 0, false
		}
		return ra.BestAsk, true
	}

	// Gate + stack.
	var cands []missionCandidate
	for _, e := range board {
		if deps.State.wasAttempted(e.MissionID) {
			fmt.Fprintf(out, "missions: skip %s: already attempted this session\n", e.MissionID) //nolint:errcheck
			continue
		}
		c, reason := buildMissionCandidate(e, dist, refAsk, fuelCostFor)
		if reason != "" {
			fmt.Fprintf(out, "missions: skip %s (%s): %s\n", e.MissionID, e.Title, reason) //nolint:errcheck
			continue
		}
		cands = append(cands, c)
	}
	st := deps.Client.GetState()
	cargoFree := st.Ship.CargoCapacity - st.Ship.CargoUsed
	set := SelectMissionSet(cands, st.GetCredits(), cargoFree, deps.MaxJumps)
	if len(set) == 0 {
		fmt.Fprintln(out, "missions: no acceptable missions on this board") //nolint:errcheck
		return missionDryPass(ctx, deps, out)
	}

	// Accept + provision. A failed accept just drops that mission from the trip;
	// a failed buy abandons the accepted mission (recorded) and drops it.
	acceptedAt, acceptedTick := rfc(missionNow(deps)), missionTick(deps)
	trip := make([]missionCandidate, 0, len(set))
	for _, c := range set {
		if aerr := deps.Client.AcceptMission(ctx, c.Entry.MissionID); aerr != nil {
			fmt.Fprintf(out, "missions: accept %s failed: %v\n", c.Entry.MissionID, aerr) //nolint:errcheck
			continue
		}
		if c.BuyQty > 0 {
			if berr := deps.Client.Buy(ctx, c.ItemID, float64(c.BuyQty)); berr != nil {
				fmt.Fprintf(out, "missions: buy %dx %s for %s failed: %v; abandoning\n", c.BuyQty, c.ItemID, c.Entry.MissionID, berr) //nolint:errcheck
				c.ItemCost = 0 // buy failed, nothing spent
				missionAbandon(ctx, deps, out, c, baseID, acceptedAt, acceptedTick)
				continue
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
		deps.State.dry = 0 // executed real work: reset the dry streak
	}

	// One shared destination system (SelectMissionSet guarantees it). Transit,
	// then complete each mission at its own base within that system.
	dest := trip[0].DestSystem
	fmt.Fprintf(out, "missions: running %d mission(s) to %s (%d jumps, est net %.0f)\n", len(trip), dest, trip[0].Jumps, tripNet(trip)) //nolint:errcheck
	for i, c := range trip {
		if nerr := deps.nav(ctx, dest, c.DestBaseID); nerr != nil {
			fmt.Fprintf(out, "missions: transit to %s failed: %v; %d mission(s) left held for next pass\n", c.DestBaseID, nerr, len(trip)-i) //nolint:errcheck
			return nil // held missions resume on the next pass
		}
		if derr := deps.Client.Dock(ctx); derr != nil {
			fmt.Fprintf(out, "missions: dock at %s failed: %v; held for next pass\n", c.DestBaseID, derr) //nolint:errcheck
			return nil
		}
		missionComplete(ctx, deps, out, c, baseID, acceptedAt, acceptedTick)
	}
	return nil
}

// missionDryPass counts a no-work pass; on the missionDryPassLimit-th
// consecutive one, the worker repositions to the next nearby station
// (rotating cursor) instead of camping a dry board forever. Nil State (no
// cross-pass memory) just idles.
func missionDryPass(ctx context.Context, deps MissionDeps, out io.Writer) error {
	if deps.State == nil {
		return nil
	}
	deps.State.dry++
	if deps.State.dry < missionDryPassLimit {
		return nil
	}
	hops, err := deps.nearbyStations(ctx, missionRepositionPool)
	if err != nil || len(hops) == 0 {
		fmt.Fprintf(out, "missions: reposition lookup failed (%v, %d targets); idling\n", err, len(hops)) //nolint:errcheck
		return nil
	}
	hop := hops[deps.State.cursor%len(hops)]
	deps.State.cursor++
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

// missionResume handles missions held from a previous pass/process: deliverable
// ones (goods aboard) are run to completion; the rest are abandoned so their
// slots free up. Returns true when it acted (the pass ends; the board is read
// fresh next pass).
func missionResume(ctx context.Context, deps MissionDeps, out io.Writer, current string) bool {
	if err := deps.Client.GetActiveMissions(ctx); err != nil {
		fmt.Fprintf(out, "missions: get_active_missions: %v\n", err) //nolint:errcheck
		return false
	}
	_ = deps.sleep(ctx, game.SleepQuick)
	raw := deps.Client.GetRawJSON("active_missions")
	if len(raw) == 0 {
		return false
	}
	var resp serverapi.GetActiveMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		fmt.Fprintf(out, "missions: parse active missions: %v\n", err) //nolint:errcheck
		return false
	}
	if len(resp.Missions) == 0 {
		return false
	}
	acted := false
	for _, m := range resp.Missions {
		r := m.Requirements
		if r == nil || r.DeliverItemID == "" || r.DeliverToBaseID == "" {
			continue // non-deliver active mission: leave it alone (manual/other origin)
		}
		aboard := cargoQty(deps.Client.GetState(), r.DeliverItemID)
		held := missionCandidate{
			Entry: serverapi.MissionBoardEntry{
				MissionID: m.MissionID, TemplateID: m.TemplateID, Type: m.Type, Title: m.Title,
			},
			ItemID: r.DeliverItemID, Qty: r.DeliverQuantity, DestBaseID: r.DeliverToBaseID,
		}
		if aboard >= float64(r.DeliverQuantity) {
			// Deliverable: resolve the destination system via FindRoute (the
			// active-mission payload has no system id) and run it in.
			route, rerr := deps.Client.FindRoute(ctx, r.DeliverToBaseID)
			destSys := current
			if rerr == nil && len(route) > 0 {
				destSys = route[len(route)-1].SystemID
			}
			fmt.Fprintf(out, "missions: resuming held %s (%s) -> %s\n", m.MissionID, m.Title, r.DeliverToBaseID) //nolint:errcheck
			if nerr := deps.nav(ctx, destSys, r.DeliverToBaseID); nerr != nil {
				fmt.Fprintf(out, "missions: resume transit failed: %v; retry next pass\n", nerr) //nolint:errcheck
				return true
			}
			if derr := deps.Client.Dock(ctx); derr != nil {
				fmt.Fprintf(out, "missions: resume dock failed: %v; retry next pass\n", derr) //nolint:errcheck
				return true
			}
			missionComplete(ctx, deps, out, held, "", rfc(missionNow(deps)), missionTick(deps))
		} else {
			fmt.Fprintf(out, "missions: abandoning held %s (%s): cargo %s %.0f/%d\n", m.MissionID, m.Title, r.DeliverItemID, aboard, r.DeliverQuantity) //nolint:errcheck
			missionAbandon(ctx, deps, out, held, "", rfc(missionNow(deps)), missionTick(deps))
		}
		acted = true
	}
	return acted
}

// missionComplete completes c, measuring realized income as the wallet delta
// (the raw router has no complete_mission store key), and records the row.
func missionComplete(ctx context.Context, deps MissionDeps, out io.Writer, c missionCandidate, fromBase, acceptedAt string, acceptedTick int64) {
	before := deps.Client.GetState().GetCredits()
	if err := deps.Client.CompleteMission(ctx, c.Entry.MissionID); err != nil {
		fmt.Fprintf(out, "missions: complete %s failed: %v; held for next pass\n", c.Entry.MissionID, err) //nolint:errcheck
		return
	}
	_ = deps.sleep(ctx, game.SleepQuick) // let the ok response update credits in State
	earned := deps.Client.GetState().GetCredits() - before
	fmt.Fprintf(out, "missions: completed %s (%s): +%.0f cr (expected %.0f)\n", c.Entry.MissionID, c.Entry.Title, earned, c.Reward) //nolint:errcheck
	missionRecord(ctx, deps, out, c, fromBase, acceptedAt, acceptedTick, earned, "completed")
}

// missionAbandon abandons c and records the loss row. c.ItemCost must reflect
// what was actually spent — callers zero it when the buy never happened (a
// failed-buy abandon); it is only nonzero when cargo was actually purchased
// and then stranded (e.g. a resume abandon with cargo already sunk).
func missionAbandon(ctx context.Context, deps MissionDeps, out io.Writer, c missionCandidate, fromBase, acceptedAt string, acceptedTick int64) {
	if err := deps.Client.AbandonMission(ctx, c.Entry.MissionID); err != nil {
		fmt.Fprintf(out, "missions: abandon %s failed: %v\n", c.Entry.MissionID, err) //nolint:errcheck
	}
	missionRecord(ctx, deps, out, c, fromBase, acceptedAt, acceptedTick, 0, "abandoned")
}

func missionRecord(ctx context.Context, deps MissionDeps, out io.Writer, c missionCandidate, fromBase, acceptedAt string, acceptedTick int64, earned float64, outcome string) {
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
