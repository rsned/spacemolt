package worker

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/rsned/spacemolt/pkg/customs"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// contrabandGate says which systems a ship carrying contraband may fly
// THROUGH. Any system may be the route's destination (Autopilot exempts the
// endpoints); the origin is never gated, the ship is already there.
type contrabandGate struct {
	// noTransit is never flown through: the operator's avoid list and the
	// strongholds this agent may not enter.
	noTransit map[string]string
	// customs is flown through only as an outbound exit (see outboundExit).
	customs map[string]string
	// empire maps a lower-case system id to its empire; "" is unaligned or
	// unknown.
	empire map[string]string
}

// outboundExit reports whether passing prev -> sys -> next leaves sys's
// empire: customs scans ships entering an empire, not ships leaving it
// (operator, 2026-11-24). Treasure Cache (Nebula) -> Pollux (Nebula,
// monitored) -> Sadalmelik (unaligned) is an exit; arriving at Pollux from
// Sadalmelik, or passing through it between two Nebula systems, is not. A
// system with no known empire is never an exit.
func (g contrabandGate) outboundExit(prev, sys, next string) bool {
	e := g.empire[strings.ToLower(sys)]
	return e != "" && g.empire[strings.ToLower(prev)] == e && g.empire[strings.ToLower(next)] != e
}

// passable reports whether the ship may fly prev -> sys -> next.
func (g contrabandGate) passable(prev, sys, next string) bool {
	k := strings.ToLower(sys)
	if _, stop := g.noTransit[k]; stop {
		return false
	}
	if _, monitored := g.customs[k]; monitored {
		return g.outboundExit(prev, sys, next)
	}
	return true
}

// gateHop is a search state: whether the ship may leave cur depends on
// where it came from.
type gateHop struct{ prev, cur string }

// contrabandSearch is a fewest-jumps search from src over hops, so a
// monitored system is crossed only on an outbound exit. It returns the
// jump count of the first arrival at each system and a path builder for it.
func contrabandSearch(graph navigation.JumpGraph, src string, gate contrabandGate) (map[string]int, func(dst string) []string) {
	start := gateHop{cur: src}
	parent := map[gateHop]gateHop{start: {}}
	depth := map[gateHop]int{start: 0}
	first := map[string]gateHop{src: start}
	for queue := []gateHop{start}; len(queue) > 0; queue = queue[1:] {
		h := queue[0]
		for _, next := range slices.Sorted(slices.Values(graph[h.cur])) {
			if h != start && !gate.passable(h.prev, h.cur, next) {
				continue
			}
			nh := gateHop{prev: h.cur, cur: next}
			if _, seen := parent[nh]; seen {
				continue
			}
			parent[nh], depth[nh] = h, depth[h]+1
			if _, reached := first[next]; !reached {
				first[next] = nh
			}
			queue = append(queue, nh)
		}
	}
	dist := make(map[string]int, len(first))
	for sys, h := range first {
		dist[sys] = depth[h]
	}
	pathTo := func(dst string) []string {
		h, ok := first[dst]
		if !ok {
			return nil
		}
		var path []string
		for ; h != start; h = parent[h] {
			path = append(path, h.cur)
		}
		path = append(path, src)
		slices.Reverse(path)
		return path
	}
	return dist, pathTo
}

// contrabandDist is navigation.BFSJumps for a contraband courier: targets
// reachable only through a gated system come back as RouteInf.
func contrabandDist(graph navigation.JumpGraph, current string, targets []string, gate contrabandGate) map[string]int {
	dist, _ := contrabandSearch(graph, current, gate)
	for _, t := range targets {
		if _, ok := dist[t]; !ok {
			dist[t] = navigation.RouteInf
		}
	}
	return dist
}

// systemEmpires maps lower-case system ids to empires from the KB. A nil KB
// or a failed read gives none, so no monitored system counts as an exit.
func systemEmpires(ctx context.Context, kb knowledge.Base) map[string]string {
	if kb == nil {
		return nil
	}
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(systems))
	for _, s := range systems {
		out[strings.ToLower(s.ID)] = strings.ToLower(s.Empire)
	}
	return out
}

// contrabandGateFor is the gate Autopilot applies with contraband aboard:
// the avoid list never, customs-monitored systems only on an outbound exit.
// It lets the mission runner refuse a courier before accepting it instead of
// discovering at the undock that no route exists (pirate-7/9, 2026-11-24: an
// hour parked holding hot_cell for Ironhearth, reachable only through
// the_rampart or leviathan systems). A missing or unreadable customs map
// gates nothing, matching customsAvoid.
func contrabandGateFor(ctx context.Context, kb knowledge.Base, customsMapPath, avoidSystemsPath string) contrabandGate {
	gate := contrabandGate{noTransit: loadAvoidSystems(avoidSystemsPath)}
	if customsMapPath == "" {
		customsMapPath = customs.DefaultMapPath
	}
	m, err := customs.Load(customsMapPath)
	if err != nil {
		return gate
	}
	gate.customs = map[string]string{}
	for id, why := range m.Monitored() {
		gate.customs[strings.ToLower(id)] = why
	}
	gate.empire = systemEmpires(ctx, kb)
	return gate
}

// customsEntriesOnRoute lists, by display name, the monitored systems the
// route passes through other than as an outbound exit. route excludes the
// origin; the destination is never listed (customsAvoid exempts it).
func customsEntriesOnRoute(origin string, route []game.RouteStep, gate contrabandGate) []string {
	var hits []string
	for i := 0; i+1 < len(route); i++ {
		step := route[i]
		if _, monitored := gate.customs[strings.ToLower(step.SystemID)]; !monitored {
			continue
		}
		prev := origin
		if i > 0 {
			prev = route[i-1].SystemID
		}
		if !gate.outboundExit(prev, step.SystemID, route[i+1].SystemID) {
			hits = append(hits, cmp.Or(step.Name, step.SystemID))
		}
	}
	return hits
}

// contrabandDetour plans the fewest-jumps route from src to dst through the
// gate. blocked strongholds (by id or name) join the gate's noTransit. It
// returns the hops after src, or nil when none exists.
func contrabandDetour(ctx context.Context, kb knowledge.Base, src, dst string, gate contrabandGate, blocked map[string]bool) ([]game.RouteStep, error) {
	if kb == nil {
		return nil, errors.New("no KB to plan a detour")
	}
	conns, err := kb.GetConnections(ctx)
	if err != nil {
		return nil, err
	}
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return nil, err
	}
	gate.noTransit = maps.Clone(gate.noTransit)
	if gate.noTransit == nil {
		gate.noTransit = map[string]string{}
	}
	names := make(map[string]string, len(systems))
	for _, s := range systems {
		names[s.ID] = s.Name
		if blocked[s.ID] || (s.Name != "" && blocked[s.Name]) {
			gate.noTransit[strings.ToLower(s.ID)] = "stronghold"
		}
	}
	_, pathTo := contrabandSearch(navigation.JumpGraphFromConnections(conns), src, gate)
	path := pathTo(dst)
	if len(path) < 2 {
		return nil, nil
	}
	steps := make([]game.RouteStep, 0, len(path)-1)
	for _, id := range path[1:] {
		steps = append(steps, game.RouteStep{SystemID: id, Name: cmp.Or(names[id], id)})
	}
	return steps, nil
}
