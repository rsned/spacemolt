package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// ErrRouteThroughStronghold is returned by Autopilot when the computed route
// enters a pirate stronghold that THIS agent cannot survive. Callers should
// treat it like ErrInsufficientRouteFuel: nothing moved, the agent is still at
// the origin, so release any claim and re-plan rather than retrying.
var ErrRouteThroughStronghold = errors.New("route passes through a pirate stronghold")

// strongholdsOnRoute returns the stronghold systems the route enters, in route
// order, named for a log line.
//
// An empty ref set means "this agent may work strongholds" (it holds the pirate
// unlock) and the function is then a no-op — see strongholdRefsFor, which
// returns nil exactly in that case.
//
// Both the id and the name of each step are checked because strongholds are
// dual-named and buildStrongholdRefs registers both spellings; an id-only match
// silently lets a route through when the ref set happens to carry the other
// form. Each step contributes at most one entry even though both of its
// spellings are usually present in the set.
func strongholdsOnRoute(route []game.RouteStep, strongholds map[string]bool) []string {
	if len(strongholds) == 0 {
		return nil
	}
	var hits []string
	for _, step := range route {
		switch {
		case step.SystemID != "" && strongholds[step.SystemID]:
		case step.Name != "" && strongholds[step.Name]:
		default:
			continue
		}
		name := step.Name
		if name == "" {
			name = step.SystemID
		}
		hits = append(hits, name)
	}

	return hits
}

// routeStrongholdError builds the refusal error, naming both the destination
// and every blocking hop so a worker log says which system was refused without
// the reader re-deriving the route.
func routeStrongholdError(targetSystem string, blocking []string) error {
	return fmt.Errorf("%w: route to %s enters %s (complete the pirate unlock to fly it)",
		ErrRouteThroughStronghold, targetSystem, strings.Join(blocking, ", "))
}

// strongholdRefsForRoute resolves the stronghold references this agent must
// avoid on this pass, reading the live standings so an agent that banks the
// unlock mid-run picks it up on its next route.
//
// A nil KB disables the gate. That is deliberate: this is the shared movement
// layer, and hard-failing every caller that has not been wired yet would ground
// the fleet. It is LOUD instead of silent — the whole point of moving the check
// down here is that a role which forgets it should be visible, and the previous
// design failed by being quiet. A KB we cannot read falls back to blocking every
// stronghold we last knew about, matching haul.go: guessing locked costs one
// skipped route, guessing unlocked costs the ship.
func strongholdRefsForRoute(ctx context.Context, kb knowledge.Base, client game.GameClient, out io.Writer) map[string]bool {
	if kb == nil {
		fmt.Fprintln(out, "autopilot: NO STRONGHOLD GUARD — this caller passed no KB; route is unchecked") //nolint:errcheck
		return nil
	}
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		fmt.Fprintf(out, "autopilot: stronghold guard: read systems: %v (treating all known strongholds as blocked)\n", err) //nolint:errcheck
		return buildStrongholdRefs(systems)
	}

	return strongholdRefsFor(client.GetState(), systems)
}
