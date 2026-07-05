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

// assistPendingAge is how long the record has been waiting since it was
// filed. An unparsable timestamp counts as infinitely old — a suboptimal
// claimant beats a silent stall on a hand-edited record.
func assistPendingAge(rec rescue.Record, now time.Time) time.Duration {
	t, err := time.Parse(time.RFC3339, rec.RequestedAt)
	if err != nil {
		return 1 << 62
	}
	return now.Sub(t)
}

func runRescue(ctx context.Context, deps AssistDeps, rec rescue.Record) error {
	fail := func(stage string, err error) error {
		fmt.Fprintf(deps.Out, "assist: rescue %s failed at %s: %v\n", rec.AgentID, stage, err) //nolint:errcheck
		if _, terr := deps.Queue.Transition(rec.AgentID, rescue.StatusClaimed, rescue.StatusFailed,
			func(r *rescue.Record) { r.Error = stage + ": " + err.Error() }); terr != nil {
			fmt.Fprintf(deps.Out, "assist: mark failed %s: %v\n", rec.AgentID, terr) //nolint:errcheck
		}
		return assistEnsureHome(ctx, deps)
	}
	fmt.Fprintf(deps.Out, "assist: rescuing %s at %s/%s (%d fuel)\n", rec.AgentID, rec.SystemID, rec.POI, rec.RescueFuel) //nolint:errcheck
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
	if err := deps.Client.RefuelShip(ctx, rec.TargetUsername, rec.RescueFuel); err != nil {
		return fail("refuel", err)
	}
	if ok, err := deps.Queue.Transition(rec.AgentID, rescue.StatusClaimed, rescue.StatusDone, nil); err != nil || !ok {
		fmt.Fprintf(deps.Out, "assist: mark done %s: ok=%v err=%v\n", rec.AgentID, ok, err) //nolint:errcheck
	}
	fmt.Fprintf(deps.Out, "assist: rescued %s (+%d fuel to %s)\n", rec.AgentID, rec.RescueFuel, rec.TargetUsername) //nolint:errcheck
	return assistEnsureHome(ctx, deps)
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
	if st := deps.Client.GetState(); st != nil && st.System.ID == home && st.Doc {
		return nil
	}
	if err := deps.navigate(ctx, home, deps.HomeStation); err != nil {
		fmt.Fprintf(deps.Out, "assist: return home: %v\n", err) //nolint:errcheck
		return nil
	}
	if err := deps.Client.Dock(ctx); err != nil && !strings.Contains(err.Error(), "Already docked") {
		fmt.Fprintf(deps.Out, "assist: dock home: %v\n", err) //nolint:errcheck
		return nil
	}
	if err := deps.Client.Refuel(ctx); err != nil {
		fmt.Fprintf(deps.Out, "assist: home refuel: %v\n", err) //nolint:errcheck
	}
	return nil
}
