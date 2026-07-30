package rescue

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Rescue-fuel sizing (spec: 5 per jump to the nearest station + one jump's
// slack for the intra-system leg; floor 10; fallback when routing fails).
const (
	// FuelPerJump is the LAST-RESORT per-jump burn, used only when the real
	// rate is unknown. Actual burn is hull-dependent — ceil(scale^1.5 * speed)
	// — so this flat value is wrong for every ship, over-reserving for small
	// hulls and under-reserving badly for large ones. Callers that hold a live
	// client must pass the measured rate instead; see haulFuelPerJump, which
	// prefers the server's own fuel_per_jump from a find_route probe.
	FuelPerJump  = 5
	FuelBuffer   = 5
	FuelMin      = 10
	FuelFallback = 25
)

func fuelForHops(hops int) int {
	f := FuelPerJump*hops + FuelBuffer
	if f < FuelMin {
		return FuelMin
	}
	return f
}

// TransferQuantity sizes a rescue fuel transfer at refuel time: fill the
// strandee's remaining tank capacity, but never give away more than the
// rescuer can spare after reserving fuel to fly home (hopsHome jumps at
// perJump each, plus FuelBuffer). Both terms clamp at zero, so a rescuer
// that cannot cover its own trip home returns 0 — the caller then declines the
// transfer rather than stranding itself.
//
// perJump is the rescuer's measured per-jump burn; <= 0 falls back to the flat
// FuelPerJump. Passing the real rate matters: with the flat 5, a rescuer that
// actually burns ~2.85/jump reserved 105 fuel for a 20-hop trip home where ~62
// was needed, concluded it had "nothing to spare", and abandoned the rescue
// after flying the whole way (live 2026-07-29, assist-krynn). The same flat
// value under-reserves for a large hull, which strands the rescuer instead.
func TransferQuantity(strandeeMaxFuel, strandeeFuel, rescuerFuel, hopsHome, perJump int) int {
	if perJump <= 0 {
		perJump = FuelPerJump
	}
	need := max(strandeeMaxFuel-strandeeFuel, 0)
	spare := max(rescuerFuel-(perJump*hopsHome+FuelBuffer), 0)
	return min(need, spare)
}

// ResolveUsername reads the agent's in-game username from
// <agentsDir>/<agentID>/credentials.json. In-game usernames differ from the
// on-disk agent aliases; refuel --target needs the in-game one.
func ResolveUsername(agentsDir, agentID string) (string, error) {
	creds, err := game.LoadCredentials(filepath.Join(agentsDir, agentID))
	if err != nil {
		return "", fmt.Errorf("rescue: credentials for %s: %w", agentID, err)
	}
	return creds.Username, nil
}

// ResolveSystemID maps a system display name (what worker heartbeats carry,
// e.g. "First Step") or an id to the KB system id. Exact id match wins;
// otherwise a case-insensitive name match.
func ResolveSystemID(systems []knowledge.System, nameOrID string) (string, bool) {
	for _, s := range systems {
		if s.ID == nameOrID {
			return s.ID, true
		}
	}
	for _, s := range systems {
		if strings.EqualFold(s.Name, nameOrID) {
			return s.ID, true
		}
	}
	return "", false
}

// FuelForSystem sizes the rescue transfer for a strandee in systemID: BFS to
// the nearest station-bearing system, then fuelForHops. On any routing
// failure the caller should fall back to FuelFallback.
func FuelForSystem(ctx context.Context, kb knowledge.Base, systemID string) (int, error) {
	graph := &galaxy.GalaxyGraph{}
	if err := graph.BuildFromDB(ctx, kb); err != nil {
		return FuelFallback, fmt.Errorf("rescue: build graph: %w", err)
	}
	near, err := galaxy.FindNearestByPOIType(ctx, kb, graph, systemID, "station", 1)
	if err != nil {
		return FuelFallback, fmt.Errorf("rescue: nearest station from %s: %w", systemID, err)
	}
	if len(near) == 0 {
		return FuelFallback, fmt.Errorf("rescue: no station reachable from %s", systemID)
	}
	return fuelForHops(near[0].Hops), nil
}
