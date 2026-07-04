package worker

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
	"github.com/rsned/spacemolt/pkg/rescue"
)

// assistHomes maps each assist agent to its home capital's system id. The set
// is fixed (one agent per empire capital); the claim election in assistElect
// relies on every agent being able to compute all five distances locally.
// IDs verified 2026-07-03 against data/spacemolt-knowledge.db:
// SELECT id, name FROM systems WHERE name IN ('Haven','Sol','Krynn','Frontier','Nexus Prime');
var assistHomes = map[string]string{
	"assist-haven":    "haven",
	"assist-sol":      "sol",
	"assist-krynn":    "krynn",
	"assist-frontier": "frontier",
	"assist-nexus":    "nexus_prime",
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
	for _, r := range recs {
		if r.Status != rescue.StatusPending || r.SystemID == "" {
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
		}
		if !assistElect(deps.AgentID, assistHomes, r.SystemID, graph) {
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

// assistElect reports whether agentID should claim a rescue in strandSystemID:
// its home is (one of) the nearest homes, ties broken by lexicographically
// smaller agent id. Deterministic per record, so all five agents agree without
// talking; the queue's CAS claim covers any leftover race.
func assistElect(agentID string, homes map[string]string, strandSystemID string, graph navigation.JumpGraph) bool {
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
	for id, sys := range homes {
		if id == agentID {
			continue
		}
		d, ok := dist[sys]
		if !ok || d >= navigation.RouteInf {
			continue
		}
		if d < my || (d == my && id < agentID) {
			return false
		}
	}
	return true
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
