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
