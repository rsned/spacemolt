package worker

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// HaulDesertPatience is how many CONSECUTIVE dry haul passes a hauler tolerates
// before it starts widening its reposition radius.
//
// A pass runs once per tick (~11s observed), so this is roughly five minutes of
// seeing nothing at all. Short enough that a stranded hauler is earning again
// within the same trading session; long enough that an ordinary lull -- the
// scanner's ~20-minute expire/insert cycle briefly emptying the local board --
// does not scatter the fleet across the galaxy chasing a gap that was about to
// close anyway.
const HaulDesertPatience = 30

// desertRadius returns the reposition-distance cap to use after idlePasses
// consecutive dry passes, escalating from base.
//
// The tight radius holds through HaulDesertPatience passes, then DOUBLES each
// further patience window: 5 -> 10 -> 20 -> 40. Doubling rather than stepping
// because the number of systems within n jumps grows with n, so a linear
// widening barely enlarges the visible board while a doubling reliably does.
//
// It is clamped at HaulMaxHaulJumps: past the backstop the radius means nothing
// (no opportunity survives that drop regardless), and an unbounded doubling
// would quietly turn the cap off altogether.
func desertRadius(idlePasses, base int) int {
	if base <= 0 {
		base = DefaultHaulMaxJumps
	}
	if base >= HaulMaxHaulJumps {
		return HaulMaxHaulJumps
	}
	if idlePasses < HaulDesertPatience {
		return base
	}
	r := base
	for range idlePasses / HaulDesertPatience {
		r *= 2
		if r >= HaulMaxHaulJumps {
			return HaulMaxHaulJumps
		}
	}
	return r
}

// haulDesert tracks one worker's run of consecutive dry haul passes so a hauler
// parked in an opportunity desert widens its search instead of idling forever.
// Held per worker process alongside treasuryRescue; a nil receiver disables the
// escalation entirely, which is the behaviour every non-haul fleet keeps.
type haulDesert struct {
	mu   sync.Mutex
	dryN int
}

// dry records a pass that found no reachable opportunity.
func (d *haulDesert) dry() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.dryN++
	d.mu.Unlock()
}

// worked records a pass that claimed something, ending the drought. The radius
// snaps straight back to base rather than decaying: the hauler has just proven
// there is work where it now stands, so there is nothing left to escape.
func (d *haulDesert) worked() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.dryN = 0
	d.mu.Unlock()
}

// radius is the cap this pass should search with, given the drought so far.
func (d *haulDesert) radius(base int) int {
	if d == nil {
		return base
	}
	d.mu.Lock()
	n := d.dryN
	d.mu.Unlock()
	return desertRadius(n, base)
}

// HaulDesertRelocate is the drought length at which a hauler stops waiting for
// the board to come to it and moves to the nearest empire capital.
//
// It is one patience window past HaulDesertPatience so the free option -- a
// wider search radius, costing no fuel and no transit -- always gets its own
// window first. Only a hauler still seeing nothing after that pays to cross the
// map.
const HaulDesertRelocate = HaulDesertPatience * 2

// HaulCapitalPoliceLevel is the police level that marks an empire capital.
// Verified against the live knowledge base 2026-09-20: exactly five systems
// carry it -- sol, haven, krynn, nexus_prime and frontier, one per empire --
// while the next tier down is 80. Reading it from the map beats a hardcoded
// list: it cannot drift as systems are added, and it needs no maintenance when
// an empire's capital moves.
const HaulCapitalPoliceLevel = 100

// capitalSystems returns the ids of the empire capitals in systems.
func capitalSystems(systems []knowledge.System) []string {
	caps := make([]string, 0, 5)
	for _, s := range systems {
		if s.PoliceLevel >= HaulCapitalPoliceLevel && s.ID != "" {
			caps = append(caps, s.ID)
		}
	}
	return caps
}

// nearestCapital returns the closest reachable capital system to current, and
// how many jumps away it is. A capital the hauler is already standing in is not
// a candidate: relocating to where you already are burns a pass and cannot
// change what the board shows.
//
// Outer Rim note: frontier carries the capital police level but its station is
// the MOBILE mobile_capital and may not be there. A hauler that arrives to find
// no station is picked up by the existing station-less recovery, which moves it
// to the nearest real station -- still a better place than the desert it left.
func nearestCapital(graph navigation.JumpGraph, current string, capitals []string) (string, int, bool) {
	targets := make([]string, 0, len(capitals))
	for _, c := range capitals {
		if c != current {
			targets = append(targets, c)
		}
	}
	dist := navigation.BFSJumps(graph, current, targets)
	best, bestHops, found := "", 0, false
	for _, c := range targets {
		// Unreachable is RouteInf, not a missing key: without this an
		// unroutable capital wins as "nearest" and the hauler autopilots
		// at a system it can never arrive at, every pass, forever.
		d, ok := dist[c]
		if !ok || d <= 0 || d >= navigation.RouteInf {
			continue
		}
		if !found || d < bestHops || (d == bestHops && c < best) {
			best, bestHops, found = c, d, true
		}
	}
	return best, bestHops, found
}

// haulDesertEscape records one dry pass and, once the drought outlasts the
// cheap widening window, relocates the hauler to the nearest empire capital.
//
// Widening the radius only changes what the hauler can SEE; it leaves the ship
// in a region the board has abandoned, and the reposition leg out of one is
// long every single time. Capitals and their neighbours trade near-continuously,
// so moving there restores an ordinary short-hop rotation rather than buying one
// distant haul. Observed 2026-09-20: the three 1900-cargo congregations sat at
// player stations whose demand is occasional, and fleet-wide claims fell from
// 651/day to 111 while the board itself stayed healthy.
//
// Returns nil on every failure path: a desert is not an error, and a hauler that
// cannot relocate this pass must simply try again on the next one.
func haulDesertEscape(ctx context.Context, deps HaulDeps, out io.Writer, graph navigation.JumpGraph, systems []knowledge.System, current string) error {
	if deps.Desert == nil {
		return nil
	}
	deps.Desert.dry()
	deps.Desert.mu.Lock()
	n := deps.Desert.dryN
	deps.Desert.mu.Unlock()
	if !relocationDue(n) {
		return nil
	}
	dest, hops, ok := nearestCapital(graph, current, capitalSystems(systems))
	if !ok {
		fmt.Fprintf(out, "haul: %d dry passes in %s but no capital is reachable; staying put\n", n, current) //nolint:errcheck
		return nil
	}
	fmt.Fprintf(out, "haul: %d dry passes in %s; relocating to capital %s (%d jump(s))\n", n, current, dest, hops) //nolint:errcheck
	if err := haulAutopilot(ctx, deps, out, dest, "", nil); err != nil {
		fmt.Fprintf(out, "haul: capital relocation failed: %v; will retry next pass\n", err) //nolint:errcheck
	}
	return nil
}

// relocationDue reports whether a drought this long warrants moving to a
// capital. It stays true for the rest of the drought rather than firing once:
// a relocation can fail (no fuel, transit error) and must be retried, and a
// hauler that arrives somewhere still dry should keep moving.
func relocationDue(idlePasses int) bool { return idlePasses >= HaulDesertRelocate }
