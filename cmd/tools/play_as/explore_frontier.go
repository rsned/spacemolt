package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// systemEligible reports whether a system is worth travelling to.
//
// Eligibility is deliberately a BOOLEAN, not a score. The ordering comes from
// the breadth-first walk in nextHopToward, which visits in increasing jump
// count, so "the first eligible system found" IS "the nearest eligible system"
// with no distance-versus-staleness weighting to tune.
//
// Ranking candidates by staleness alone was the alternative and it is wrong:
// the least-recently-surveyed system in the galaxy is almost always on the far
// side of it, so an explorer obeying that ordering spends its life in transit
// rather than surveying.
type systemEligible struct {
	// surveyed is when each system was last surveyed, from the KB, so the
	// judgement survives a restart. The old picker held this in a process-local
	// map, which is why a server restart made the explorer re-target the system
	// it had just come from.
	surveyed map[string]time.Time
	// visitedThisRun is the same-run backstop. A survey only lands in the KB
	// when the ship carries a survey scanner; without one, nothing would ever
	// become ineligible and the walk would oscillate between two systems
	// forever.
	visitedThisRun map[string]bool
	now            time.Time
}

// eligible reports whether the system has never been surveyed, or was surveyed
// long enough ago to be worth revisiting.
//
// The threshold is game.FreshnessSystem, the existing "how long system data
// stays current" figure, so no new constant is needed. Re-surveying a day later
// is genuinely useful in any case: deposits deplete and regenerate.
func (e systemEligible) eligible(systemID string) bool {
	if systemID == "" || e.visitedThisRun[systemID] {
		return false
	}
	at, ok := e.surveyed[systemID]
	if !ok {
		return true // never surveyed
	}

	return e.now.Sub(at) >= time.Duration(game.FreshnessSystem)*time.Second
}

// nextHopToward returns the adjacent system to jump to in order to reach the
// nearest eligible system, along with that target and its distance in jumps.
//
// It returns a HOP rather than the target because client.Jump moves one system
// at a time. Re-planning every hop is also the more robust arrangement: the
// walk adapts as systems are surveyed underneath it, without holding a stale
// multi-jump route.
//
// The search is unrestricted: it will happily route THROUGH systems that are
// themselves ineligible. That is the fix for the narrow sections of the map,
// where the old picker refused to re-enter the corridor it had just crossed and
// so walled itself into whichever pocket it was standing in. Recency
// disqualifies a system as a DESTINATION; it must never disqualify it as a
// ROUTE.
func nextHopToward(adjacency map[string][]string, from string, elig systemEligible) (hop, target string, jumps int, ok bool) {
	if from == "" {
		return "", "", 0, false
	}
	type node struct {
		id        string
		firstHop  string
		jumpCount int
	}
	seen := map[string]bool{from: true}
	queue := make([]node, 0, len(adjacency))
	for _, n := range adjacency[from] {
		if seen[n] {
			continue
		}
		seen[n] = true
		queue = append(queue, node{id: n, firstHop: n, jumpCount: 1})
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if elig.eligible(cur.id) {
			return cur.firstHop, cur.id, cur.jumpCount, true
		}
		for _, n := range adjacency[cur.id] {
			if seen[n] {
				continue
			}
			seen[n] = true
			queue = append(queue, node{id: n, firstHop: cur.firstHop, jumpCount: cur.jumpCount + 1})
		}
	}

	return "", "", 0, false
}

// buildAdjacency turns the KB's connection rows into an undirected adjacency
// list. Both directions are added because a row states a link, and a link is
// traversable either way.
func buildAdjacency(conns []knowledge.Connection) map[string][]string {
	adj := make(map[string][]string, len(conns))
	seen := make(map[string]bool, len(conns)*2)
	add := func(a, b string) {
		if a == "" || b == "" || seen[a+"\x00"+b] {
			return
		}
		seen[a+"\x00"+b] = true
		adj[a] = append(adj[a], b)
	}
	for _, c := range conns {
		add(c.FromSystem, c.ToSystem)
		add(c.ToSystem, c.FromSystem)
	}

	return adj
}

// liveAdjacency replaces the stored adjacency row for the CURRENT system with
// exactly the live get_system connection list. The stored graph can carry
// edges the server no longer honors — an expired wormhole leaves a one-way
// row behind (iron_reach->albireo, 2026-08-31), and buildAdjacency treats
// every row as bidirectional — so the live reply is authoritative for where
// we can jump FROM HERE. Deeper rows stay as stored: the walk re-plans every
// hop, and each system's own live reply overrides in turn when we stand there.
func liveAdjacency(adjacency map[string][]string, from string, connections []game.ConnectionInfo) map[string][]string {
	if adjacency == nil {
		adjacency = make(map[string][]string, 1)
	}
	ids := make([]string, 0, len(connections))
	seen := make(map[string]bool, len(connections))
	for _, c := range connections {
		if c.SystemID == "" || seen[c.SystemID] {
			continue
		}
		seen[c.SystemID] = true
		ids = append(ids, c.SystemID)
	}
	adjacency[from] = ids

	return adjacency
}

// loadFrontier reads the galaxy graph and the survey log from the KB.
//
// Both are whole-table reads, which is affordable: 505 systems and ~2,139
// connections, entirely local, no server calls, once per hop.
func loadFrontier(ctx context.Context, visited map[string]bool) (map[string][]string, systemEligible, error) {
	elig := systemEligible{visitedThisRun: visited, now: time.Now().UTC()}
	if globalKB == nil {
		return nil, elig, fmt.Errorf("knowledge base not loaded")
	}
	conns, err := globalKB.GetConnections(ctx)
	if err != nil {
		return nil, elig, fmt.Errorf("read connections: %w", err)
	}
	surveyed, err := knowledge.SystemsLastSurveyed(ctx, globalKB)
	if err != nil {
		// A missing survey log is not fatal: every system then reads as never
		// surveyed, which is the correct answer for a fresh knowledge base.
		surveyed = nil
	}
	elig.surveyed = surveyed

	return buildAdjacency(conns), elig, nil
}

// cloneAdjacency copies the adjacency map so a live correction applied to one
// row cannot be seen by a caller still holding the stored graph.
func cloneAdjacency(adj map[string][]string) map[string][]string {
	out := make(map[string][]string, len(adj))
	for k, v := range adj {
		out[k] = append([]string(nil), v...)
	}

	return out
}

// frontierMemory remembers the AUTHORITATIVE connection list for every system
// the ship has stood in during this run.
//
// It exists because the stored graph carries edges the server no longer honors
// — phantom rows produced by copied neighbour lists, and one-way rows left by
// expired wormholes. liveAdjacency corrects the row for the system we are
// standing in, but that correction used to be discarded on the next hop, and
// discarding it makes the walk oscillate: each end of a pair believes the
// eligible target lies behind the other.
//
// Observed on explorer-8, 2026-09-14, bouncing horizon <-> first_step for 30+
// hops of a 200-hop run:
//
//	at horizon:    stale gsc_0010 looks 2 jumps away via first_step (phantom)
//	at first_step: live says no gsc_0010 — but horizon's row is stored again,
//	               so stale deep_range looks 2 jumps away via horizon (phantom)
//
// Keeping the corrections is enough to break it: once both rows are known to
// be real, neither phantom target is reachable and the walk routes somewhere it
// can actually get to.
//
// Corrections are per-run, not persisted. The stored graph is shared with the
// rest of the fleet and a single agent's view of one system is not grounds for
// rewriting it — that is a separate repair with its own evidence bar.
type frontierMemory struct {
	rows map[string][]string
}

func newFrontierMemory() *frontierMemory {
	return &frontierMemory{rows: make(map[string][]string)}
}

// learn records what the server says the given system connects to. An empty
// list is ignored: a system genuinely has neighbours, so an empty reply is a
// failed read, and trusting it would strand the walk.
func (m *frontierMemory) learn(systemID string, connections []game.ConnectionInfo) {
	if m == nil || systemID == "" || len(connections) == 0 {
		return
	}
	ids := make([]string, 0, len(connections))
	seen := make(map[string]bool, len(connections))
	for _, c := range connections {
		if c.SystemID == "" || seen[c.SystemID] {
			continue
		}
		seen[c.SystemID] = true
		ids = append(ids, c.SystemID)
	}
	if len(ids) == 0 {
		return
	}
	m.rows[systemID] = ids
}

// adjacency overlays every learned row onto the stored graph, leaving the
// stored graph untouched. `from` is accepted for symmetry with the caller and
// needs no special handling: it was learned on arrival like any other.
func (m *frontierMemory) adjacency(stored map[string][]string, from string) map[string][]string {
	out := cloneAdjacency(stored)
	if m == nil {
		return out
	}
	for id, row := range m.rows {
		out[id] = append([]string(nil), row...)
	}
	_ = from

	return out
}
