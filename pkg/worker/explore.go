package worker

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// DefaultExploreStaleTicks is the age, in game ticks, past which a visited
// system is worth re-capturing. ~1 day at 10s/tick (the KB system-freshness
// convention): 86400s / 10s = 8640 ticks.
const DefaultExploreStaleTicks int64 = 8640

// exploreClass ranks why a system is worth visiting; lower is higher priority.
type exploreClass int

const (
	classFrontier  exploreClass = iota // connection endpoint not yet a known system
	classUnvisited                     // known but never visited (LastVisitedTick == 0)
	classStale                         // known, visited, but stale
)

// NextExploreTarget picks the next system an explorer should visit from
// currentSystem, ranked by jump distance, preferring (1) undiscovered frontier
// systems — connection endpoints not yet in the KB's systems table — then
// (2) known-but-unvisited systems (LastVisitedTick == 0), then (3) stale known
// systems (nowTick-LastUpdatedTick > staleTicks). ok is false when nothing
// within reach is worth visiting, in which case target is "".
//
// avoid (lower-case system id -> reason, as loadAvoidSystems returns) is
// neither a target nor a way through: Autopilot refuses both, and a picker
// that offers one re-picks it every pass until the overmind quarantines the
// ship (explorer-7 and HR 8832, 2026-11-26).
func NextExploreTarget(ctx context.Context, kb knowledge.Base, currentSystem string, staleTicks, nowTick int64, avoid map[string]string) (string, bool, error) {
	conns, err := kb.GetConnections(ctx)
	if err != nil {
		return "", false, fmt.Errorf("explore: get connections: %w", err)
	}
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return "", false, fmt.Errorf("explore: get systems: %w", err)
	}

	graph := navigation.JumpGraphFromConnections(conns)
	if len(avoid) > 0 {
		graph = withoutAvoided(graph, currentSystem, avoid)
	}

	// Node universe: current system, every graph endpoint, every known system.
	nodeSet := map[string]bool{currentSystem: true}
	for from, tos := range graph {
		nodeSet[from] = true
		for _, to := range tos {
			nodeSet[to] = true
		}
	}
	known := make(map[string]knowledge.System, len(systems))
	for _, s := range systems {
		known[s.ID] = s
		nodeSet[s.ID] = true
	}
	nodes := make([]string, 0, len(nodeSet))
	for id := range nodeSet {
		nodes = append(nodes, id)
	}

	dist := navigation.BFSJumps(graph, currentSystem, nodes)

	var bestID string
	var bestClass exploreClass
	bestDist := navigation.RouteInf
	have := false
	for _, id := range nodes {
		if id == currentSystem {
			continue
		}
		if _, avoided := avoid[strings.ToLower(id)]; avoided {
			continue
		}
		d, ok := dist[id]
		if !ok || d >= navigation.RouteInf {
			continue // unreachable
		}
		sys, isKnown := known[id]
		var class exploreClass
		switch {
		case !isKnown:
			class = classFrontier
		case sys.LastVisitedTick == 0:
			class = classUnvisited
		case nowTick-sys.LastUpdatedTick > staleTicks:
			class = classStale
		default:
			continue // known and fresh — skip
		}
		if !have || outranks(class, d, id, bestClass, bestDist, bestID) {
			bestID, bestClass, bestDist, have = id, class, d, true
		}
	}
	if !have {
		return "", false, nil
	}
	return bestID, true, nil
}

// withoutAvoided drops every edge into or out of an avoided system, except
// out of from: the ship may already sit in one and must be able to leave.
func withoutAvoided(graph navigation.JumpGraph, from string, avoid map[string]string) navigation.JumpGraph {
	out := make(navigation.JumpGraph, len(graph))
	for sys, tos := range graph {
		if _, avoided := avoid[strings.ToLower(sys)]; avoided && sys != from {
			continue
		}
		for _, to := range tos {
			if _, avoided := avoid[strings.ToLower(to)]; !avoided {
				out[sys] = append(out[sys], to)
			}
		}
	}
	return out
}

// outranks reports whether (class,d,id) beats the current best: lower class
// first, then smaller jump distance, then smaller system id (deterministic).
func outranks(class exploreClass, d int, id string, bClass exploreClass, bDist int, bID string) bool {
	if class != bClass {
		return class < bClass
	}
	if d != bDist {
		return d < bDist
	}
	return id < bID
}

// ExploreDeps are the injected collaborators for one Explore step.
type ExploreDeps struct {
	Client     game.GameClient
	KB         knowledge.Base
	Out        io.Writer // progress; nil -> io.Discard
	StaleTicks int64     // 0 -> DefaultExploreStaleTicks
	AgentID    string    // stamped as DetectedBy on the POIs a survey reveals
	// Market receives the market snapshot of every station the tour docks at;
	// nil skips the market.db write (the KB capture still runs).
	Market *market.Collector
	// AvoidSystemsPath is the avoid list (DefaultAvoidSystemsPath when empty).
	AvoidSystemsPath string
}

// Explore performs one exploration step: resolve the current system, choose the
// next target via NextExploreTarget, and autopilot to it (capturing each hop
// via the worker's plain KB capture). When there is no KB, no current system,
// or no reachable frontier, it logs and returns nil so the worker idles and
// retries on the next pass.
func Explore(ctx context.Context, deps ExploreDeps) error {
	out := deps.Out
	if out == nil {
		out = io.Discard
	}
	if deps.KB == nil {
		fmt.Fprintln(out, "explore: no knowledge base; skipping") //nolint:errcheck
		return nil
	}
	stale := deps.StaleTicks
	if stale <= 0 {
		stale = DefaultExploreStaleTicks
	}
	state := deps.Client.GetState()
	// The KB jump graph and systems table key on system IDs (e.g. "haven"),
	// not display names (e.g. "Haven"); state.CurrentSystem is the name, so
	// select on state.System.ID. The name is kept only for human-readable logs.
	if state == nil || state.System.ID == "" {
		fmt.Fprintln(out, "explore: current system unknown; skipping") //nolint:errcheck
		return nil
	}
	currentID := state.System.ID
	label := state.System.Name
	if label == "" {
		label = currentID
	}
	if handled, err := exploreFuelStop(ctx, deps, out, currentID); handled || err != nil {
		return err
	}
	target, ok, err := NextExploreTarget(ctx, deps.KB, currentID, stale, state.CurrentTick, loadAvoidSystems(deps.AvoidSystemsPath))
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintf(out, "explore: nothing to survey reachable from %s; idling\n", label) //nolint:errcheck
		return nil
	}
	fmt.Fprintf(out, "explore: heading to %s\n", target) //nolint:errcheck
	if err := Autopilot(ctx, AutopilotDeps{
		Client:     deps.Client,
		Out:        out,
		KB:         deps.KB,
		OnWaypoint: func(ctx context.Context) error { return KBWaypointCapture(ctx, deps.Client, deps.KB) },
	}, target, ""); err != nil {
		return err
	}
	// Then explore it: a survey, so hidden POIs are found and known ones
	// refreshed, and a tour — every POI captured, every station docked at,
	// captured and refueled. The refuel is what keeps an explorer's tank up.
	return systemTour(ctx, deps, out)
}
