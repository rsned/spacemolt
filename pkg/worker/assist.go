package worker

import (
	"context"
	"fmt"
	"io"
	"maps"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
	"github.com/rsned/spacemolt/pkg/rescue"
)

// assistTakeoverInterval staggers claim takeover for non-nearest agents: a
// pending rescue unclaimed for rank×interval unlocks the rank-th nearest
// home. Without it, a benched or dead assist slot whose home is nearest to
// the strandee is a claim black hole — every live agent defers to a ghost
// that will never claim (live incident 2026-07-04: three rescues nearest to
// the never-launched assist-krynn sat pending for 40+ minutes while four
// full-tank assists idled at home).
const assistTakeoverInterval = 5 * time.Minute

// RescueMaxAttempts is how many claimed-then-failed tries a record gets before
// it goes terminal and waits for an operator. Sized to let each assist agent
// have one turn (there are five), so a failure caused by one rescuer's state —
// an empty tank, a missing module, a bad route — is retried by the others
// before anyone concludes the record itself is the problem.
const RescueMaxAttempts = 5

// rescueRetryAfter is how long an assister that failed a record stays barred
// from re-claiming it. Long enough that every other assister's takeover rank
// unlocks first (so the retry really does go to someone else), but finite so a
// record cannot strand between "everyone live has failed it" and the terminal
// attempt count.
const rescueRetryAfter = RescueMaxAttempts * assistTakeoverInterval

// assistHomes maps each fixed-capital assist agent to its home system id.
// The claim election in assistElect relies on every agent being able to
// compute all five home distances locally (these four plus the mobile homes
// below). IDs verified 2026-07-03 against data/spacemolt-knowledge.db:
// SELECT id, name FROM systems WHERE name IN ('Haven','Sol','Krynn','Nexus Prime');
var assistHomes = map[string]string{
	"assist-haven": "haven",
	"assist-sol":   "sol",
	"assist-krynn": "krynn",
	"assist-nexus": "nexus_prime",
}

// assistMobileHomes maps assist agents whose home capital is a moving POI to
// that POI id. Outerrim's mobile_capital hyperspace-jumps to another of its
// empire's systems once a day, so its system is resolved per pass via the
// server's find_route (which always knows the POI's current location) instead
// of being hardcoded.
var assistMobileHomes = map[string]string{
	"assist-frontier": "mobile_capital",
}

// resolveAssistHomes returns this pass's agent->home-system map: the static
// capitals plus each mobile home resolved via find_route. A mobile home that
// fails to resolve is dropped for the pass (logged); the resulting brief
// cross-agent disagreement over who is nearest is covered by the queue's CAS
// claim, like any other election race.
func resolveAssistHomes(ctx context.Context, deps AssistDeps) map[string]string {
	homes := make(map[string]string, len(assistHomes)+len(assistMobileHomes))
	maps.Copy(homes, assistHomes)
	for id, poi := range assistMobileHomes {
		sys, err := assistResolveMobile(ctx, deps, poi)
		if err != nil {
			fmt.Fprintf(deps.Out, "assist: resolve %s: %v\n", poi, err) //nolint:errcheck
			continue
		}
		homes[id] = sys
	}
	return homes
}

// assistResolveMobile finds the system a moving POI is currently in: the last
// step of the server's route to it, or our own system when the route is empty
// (we are already there).
func assistResolveMobile(ctx context.Context, deps AssistDeps, poi string) (string, error) {
	route, err := deps.Client.FindRoute(ctx, poi)
	if err != nil {
		return "", err
	}
	if len(route) > 0 {
		return route[len(route)-1].SystemID, nil
	}
	if st := deps.Client.GetState(); st != nil && st.System.ID != "" {
		return st.System.ID, nil
	}
	return "", fmt.Errorf("empty route to %s and current system unknown", poi)
}

// RescueQueue is the slice of rescue.Queue the assist behavior consumes.
type RescueQueue interface {
	List() ([]rescue.Record, error)
	Transition(agentID string, from, to rescue.Status, mutate func(*rescue.Record)) (bool, error)
}

// AssistDeps wires one assist worker: fly rescue fuel to quarantined workers
// from the shared queue, then return to the home capital and re-tank.
type AssistDeps struct {
	Client      game.GameClient
	KB          knowledge.Base
	Queue       RescueQueue
	Out         io.Writer
	AgentID     string
	HomeStation string // station POI id at the home capital (fleet yaml `station`)
	// Navigate overrides Autopilot in tests; nil uses the real thing.
	Navigate func(ctx context.Context, system, poi string) error
	// SetActivity publishes the status-page "current activity" string (nil in
	// tests). Set while rescuing a target, cleared ("") when idle at home.
	SetActivity func(string)
}

func (d AssistDeps) navigate(ctx context.Context, system, poi string) error {
	if d.Navigate != nil {
		return d.Navigate(ctx, system, poi)
	}
	return Autopilot(ctx, AutopilotDeps{Client: d.Client, Out: d.Out}, system, poi)
}

// Assist runs one pass of the assist standing behavior (the standing loop
// re-invokes it): resume an owned claim, else claim the nearest pending
// rescue, else make sure we are docked at home with a full tank.
func Assist(ctx context.Context, deps AssistDeps) error {
	// Clear last pass's activity; runRescue sets it when we take a target.
	publishActivity(deps.SetActivity, "")
	recs, err := deps.Queue.List()
	if err != nil {
		fmt.Fprintf(deps.Out, "assist: queue read: %v\n", err) //nolint:errcheck
		return nil
	}
	for _, r := range recs {
		if r.Status == rescue.StatusClaimed && r.ClaimedBy == deps.AgentID {
			return runRescue(ctx, deps, r)
		}
	}
	if rec, ok := claimNearestPending(ctx, deps, recs); ok {
		return runRescue(ctx, deps, rec)
	}
	return assistEnsureHome(ctx, deps)
}

func claimNearestPending(ctx context.Context, deps AssistDeps, recs []rescue.Record) (rescue.Record, bool) {
	var graph navigation.JumpGraph
	var homes map[string]string
	for _, r := range recs {
		if r.Status != rescue.StatusPending {
			continue
		}
		if r.HasFailed(deps.AgentID) && assistPendingAge(r, time.Now()) < rescueRetryAfter {
			// We already tried this one and failed. A failure re-queues the
			// record so a DIFFERENT assister gets a turn; without this skip the
			// agent that just failed is usually still rank 0 and would
			// immediately re-win the election, retrying forever.
			//
			// The skip expires, though, and must: with fewer live assisters
			// than RescueMaxAttempts, a permanent skip means every live agent
			// ends up in FailedBy, nobody can claim, and the record sits
			// pending forever without ever reaching the terminal alert. It also
			// gives transient failures a second chance — the first attempt on
			// this record died to a six-minute DNS outage, not to anything
			// about the record.
			continue
		}
		if r.SystemID == "" {
			// Silent-forever otherwise: the operator must fill in system_id by
			// hand (enqueue-time resolution failed). Log once per pass so it is
			// visible without auto-failing the record.
			fmt.Fprintf(deps.Out, "assist: pending rescue for %s has no system_id; operator must fill it\n", r.AgentID) //nolint:errcheck
			continue
		}
		if graph == nil {
			if deps.KB == nil {
				return rescue.Record{}, false
			}
			conns, err := deps.KB.GetConnections(ctx)
			if err != nil {
				fmt.Fprintf(deps.Out, "assist: connections: %v\n", err) //nolint:errcheck
				return rescue.Record{}, false
			}
			graph = navigation.JumpGraphFromConnections(conns)
			homes = resolveAssistHomes(ctx, deps)
		}
		if !assistElect(deps.AgentID, homes, r.SystemID, graph, assistPendingAge(r, time.Now())) {
			continue
		}
		ok, err := deps.Queue.Transition(r.AgentID, rescue.StatusPending, rescue.StatusClaimed,
			func(rec *rescue.Record) { rec.ClaimedBy = deps.AgentID })
		if err != nil || !ok {
			continue // raced another rescuer; move on
		}
		r.Status, r.ClaimedBy = rescue.StatusClaimed, deps.AgentID
		return r, true
	}
	return rescue.Record{}, false
}

// assistElect reports whether agentID should claim a rescue in strandSystemID
// that has been pending for age: the nearest home (ties broken by
// lexicographically smaller agent id) claims immediately, and each
// assistTakeoverInterval of age unlocks the next-nearest rank, so an absent
// agent's home only delays the claim rather than sinking it. Deterministic
// per (record, time), so all agents agree without talking; the queue's CAS
// claim covers any leftover race.
func assistElect(agentID string, homes map[string]string, strandSystemID string, graph navigation.JumpGraph, age time.Duration) bool {
	mySys, ok := homes[agentID]
	if !ok {
		return false
	}
	targets := make([]string, 0, len(homes))
	for _, sys := range homes {
		targets = append(targets, sys)
	}
	dist := navigation.BFSJumps(graph, strandSystemID, targets)
	my, ok := dist[mySys]
	if !ok || my >= navigation.RouteInf {
		return false
	}
	rank := 0
	for id, sys := range homes {
		if id == agentID {
			continue
		}
		d, ok := dist[sys]
		if !ok || d >= navigation.RouteInf {
			continue
		}
		if d < my || (d == my && id < agentID) {
			rank++
		}
	}
	return age >= time.Duration(rank)*assistTakeoverInterval
}

// assistPendingAge is how long the record has been waiting UNCLAIMED, which is
// what the takeover ladder is widening on — not how long ago it was filed.
// PendingSince is re-stamped every time the record enters pending; RequestedAt
// is the fallback for records written before that field existed.
//
// Reading RequestedAt was a live bug: a two-day-old record satisfied
// `age >= rank*assistTakeoverInterval` for EVERY rank, so nearest-home ranking
// was defeated outright and whichever assister polled first won. A Nexus Prime
// rescue went to the Krynn assister (20 jumps) while the Nexus Prime one sat
// idle in the strand system, and a Sol rescue went to Starfall's assister
// while Sol's own sat docked at the strandee's exact POI.
//
// An unparsable timestamp counts as infinitely old — a suboptimal claimant
// beats a silent stall on a hand-edited record.
func assistPendingAge(rec rescue.Record, now time.Time) time.Duration {
	stamp := rec.PendingSince
	if stamp == "" {
		stamp = rec.RequestedAt
	}
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return 1 << 62
	}
	return now.Sub(t)
}

func runRescue(ctx context.Context, deps AssistDeps, rec rescue.Record) error {
	// fail re-queues the rescue for a different assister rather than killing it.
	// A terminal `failed` record is a deadlock: pollRescues releases quarantine
	// only on `done` (or on the record being deleted), and a quarantined worker
	// has no process, so it can never rescue itself — failed means stranded
	// forever. Live 2026-07-29: two workers sat quarantined for one and two days
	// on records that failed once, back when no assister had a refueling pump.
	//
	// Only after RescueMaxAttempts distinct tries does the record go terminal,
	// which is the signal that the cause is not the assister (a missing module,
	// a bounty, a stale POI) and needs an operator.
	fail := func(stage string, err error) error {
		fmt.Fprintf(deps.Out, "assist: rescue %s failed at %s: %v\n", rec.AgentID, stage, err) //nolint:errcheck
		next, attempts := rescue.StatusPending, rec.Attempts+1
		if attempts >= RescueMaxAttempts {
			next = rescue.StatusFailed
		}
		if _, terr := deps.Queue.Transition(rec.AgentID, rescue.StatusClaimed, next,
			func(r *rescue.Record) {
				r.Error = stage + ": " + err.Error()
				r.Attempts = attempts
				r.ClaimedBy = ""
				if !r.HasFailed(deps.AgentID) {
					r.FailedBy = append(r.FailedBy, deps.AgentID)
				}
			}); terr != nil {
			fmt.Fprintf(deps.Out, "assist: mark %s %s: %v\n", next, rec.AgentID, terr) //nolint:errcheck
		}
		if next == rescue.StatusFailed {
			fmt.Fprintf(deps.Out, "assist: rescue %s GIVING UP after %d attempts (%s); operator needed\n", //nolint:errcheck
				rec.AgentID, attempts, strings.Join(append(rec.FailedBy, deps.AgentID), ","))
		} else {
			fmt.Fprintf(deps.Out, "assist: rescue %s re-queued for another assister (attempt %d/%d)\n", //nolint:errcheck
				rec.AgentID, attempts, RescueMaxAttempts)
		}
		return assistEnsureHome(ctx, deps)
	}
	fmt.Fprintf(deps.Out, "assist: rescuing %s at %s/%s (%d fuel)\n", rec.AgentID, rec.SystemID, rec.POI, rec.RescueFuel) //nolint:errcheck
	publishActivity(deps.SetActivity, "Rescuing "+rec.AgentID)
	if err := deps.navigate(ctx, rec.SystemID, rec.POI); err != nil {
		// Mid-hyperspace-jump the server answers find_route with "You are
		// not in a system" — a worker restarted in transit hits this on its
		// first resumed pass (live incident 2026-07-04: assist-haven
		// restarted mid-jump, rescue wrongly marked failed). The claim is
		// still ours; retry on the next standing-loop pass instead.
		if strings.Contains(err.Error(), "not in a system") {
			fmt.Fprintf(deps.Out, "assist: rescue %s: in transit (%v); retrying next pass\n", rec.AgentID, err) //nolint:errcheck
			return nil
		}
		return fail("travel", err)
	}
	qty := rescueFuelQty(ctx, deps, rec)
	if qty <= 0 {
		// The rescuer cannot spare fuel without risking its own trip home (or
		// the strandee is already full). Release the claim so a fuller or
		// nearer rescuer takes it, and head home to re-tank.
		fmt.Fprintf(deps.Out, "assist: rescue %s: nothing to spare after home reserve; releasing claim\n", rec.AgentID) //nolint:errcheck
		if _, err := deps.Queue.Transition(rec.AgentID, rescue.StatusClaimed, rescue.StatusPending,
			func(r *rescue.Record) { r.ClaimedBy = "" }); err != nil {
			fmt.Fprintf(deps.Out, "assist: release %s: %v\n", rec.AgentID, err) //nolint:errcheck
		}
		return assistEnsureHome(ctx, deps)
	}
	if err := deps.Client.RefuelShip(ctx, rec.TargetUsername, qty); err != nil {
		return fail("refuel", err)
	}
	if ok, err := deps.Queue.Transition(rec.AgentID, rescue.StatusClaimed, rescue.StatusDone,
		func(r *rescue.Record) { r.RescueFuel = qty }); err != nil || !ok {
		fmt.Fprintf(deps.Out, "assist: mark done %s: ok=%v err=%v\n", rec.AgentID, ok, err) //nolint:errcheck
	}
	fmt.Fprintf(deps.Out, "assist: rescued %s (+%d fuel to %s)\n", rec.AgentID, qty, rec.TargetUsername) //nolint:errcheck
	return assistEnsureHome(ctx, deps)
}

// rescuerHome returns this assist agent's home system id: the static capital,
// or the current location of its mobile capital. ok is false when neither is
// configured or the mobile home cannot be resolved this pass.
func rescuerHome(ctx context.Context, deps AssistDeps) (string, bool) {
	if home, ok := assistHomes[deps.AgentID]; ok {
		return home, true
	}
	if poi, mobile := assistMobileHomes[deps.AgentID]; mobile {
		if home, err := assistResolveMobile(ctx, deps, poi); err == nil {
			return home, true
		}
	}
	return "", false
}

// rescueFuelQty computes how much fuel to transfer to the strandee: fill its
// tank, capped by what the rescuer can spare after reserving its trip home
// (rescue.TransferQuantity). Falls back to the record's enqueue-time
// RescueFuel estimate when live state, the KB, or the home route is
// unavailable, so a transfer is never blocked on missing data.
//
// The home reserve is priced at this ship's MEASURED per-jump burn, not the
// flat rescue.FuelPerJump: the flat 5 made a rescuer that burns ~2.85/jump
// reserve 105 fuel for a 20-hop trip home, decide it had nothing to spare, and
// abandon the rescue it had just flown 20 jumps to reach.
func rescueFuelQty(ctx context.Context, deps AssistDeps, rec rescue.Record) int {
	st := deps.Client.GetState()
	if st == nil || deps.KB == nil {
		return rec.RescueFuel
	}
	home, ok := rescuerHome(ctx, deps)
	if !ok {
		return rec.RescueFuel
	}
	conns, err := deps.KB.GetConnections(ctx)
	if err != nil {
		return rec.RescueFuel
	}
	graph := navigation.JumpGraphFromConnections(conns)
	dist := navigation.BFSJumps(graph, rec.SystemID, []string{home})
	hops, ok := dist[home]
	if !ok || hops >= navigation.RouteInf {
		return rec.RescueFuel
	}
	// We are standing at the strandee, so probe the route we actually have to
	// fly back; haulFuelPerJump prefers the server's own fuel_per_jump and
	// falls back to the ship-class formula. 0 means "unknown", which
	// TransferQuantity turns back into the flat constant.
	perJump := haulFuelPerJump(ctx, deps.Client, deps.HomeStation)
	return rescue.TransferQuantity(int(rec.MaxFuel), int(rec.Fuel), int(st.Fuel), hops, perJump)
}

// assistEnsureHome parks the rescuer docked at its home capital with a full
// tank so the next rescue starts fresh. Best-effort: failures log and return
// nil so the standing loop retries next pass.
func assistEnsureHome(ctx context.Context, deps AssistDeps) error {
	home, ok := assistHomes[deps.AgentID]
	if !ok {
		if poi, mobile := assistMobileHomes[deps.AgentID]; mobile {
			var err error
			if home, err = assistResolveMobile(ctx, deps, poi); err != nil {
				// Transient: stay put and retry next pass.
				fmt.Fprintf(deps.Out, "assist: resolve home %s: %v\n", poi, err) //nolint:errcheck
				return nil
			}
			ok = true
		}
	}
	if !ok || deps.HomeStation == "" {
		fmt.Fprintf(deps.Out, "assist: no home configured for %s\n", deps.AgentID) //nolint:errcheck
		return nil
	}
	st := deps.Client.GetState()
	// Already parked and docked at the home POI: nothing to do.
	if st != nil && st.CurrentPOI == deps.HomeStation && st.Doc {
		return nil
	}
	// Travel only when we are not already sitting at the home POI. Autopilot
	// always issues a travel to the target POI, which auto-undocks the ship;
	// re-traveling to a POI we already occupy would undock us every pass,
	// thrashing undock<->dock forever so the dock never sticks (and the ship
	// never re-tanks). Critical for mobile_capital, which must be boarded
	// (docked) to ride its once-a-day hyperspace jump — an undocked rescuer is
	// left behind when the capital jumps.
	if st == nil || st.CurrentPOI != deps.HomeStation {
		if err := deps.navigate(ctx, home, deps.HomeStation); err != nil {
			fmt.Fprintf(deps.Out, "assist: return home: %v\n", err) //nolint:errcheck
			return nil
		}
	}
	if err := deps.Client.Dock(ctx); err != nil && !strings.Contains(err.Error(), "Already docked") {
		fmt.Fprintf(deps.Out, "assist: dock home: %v\n", err) //nolint:errcheck
		return nil
	}
	// Sync after refuelling: a rescuer that thinks its tank is still empty
	// declines every rescue it wins (rescueFuelQty sees the stale fuel and
	// returns 0), which is silent and indistinguishable from having no work.
	if err := RefuelAndSync(ctx, deps.Client, deps.Out, "assist"); err != nil {
		fmt.Fprintf(deps.Out, "assist: home refuel: %v\n", err) //nolint:errcheck
	}
	return nil
}
