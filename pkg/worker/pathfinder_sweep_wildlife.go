package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// sweepLeviathanHere names a leviathan at the ship's location, or "" when
// there is none. Since v0.711.2 wildlife cannot strike on the tick a ship
// arrives, so reading this before any action lets the first command be the
// way out. Leviathans killed 11 ships on 2026-11-13, a third of their attacks
// landing on the arrival tick.
//
// An empty first read is re-read once a tick later: wildlife is placed late
// in the arrival tick, so a get_nearby in that tick can read empty at a
// populated POI (see huntNearbyCreatures). A failed read returns "" and the
// pass goes on: the battle gate still flees a fight that starts.
func (d *WorkerDispatch) sweepLeviathanHere(ctx context.Context) string {
	name, n, err := d.sweepReadLeviathan(ctx)
	if err != nil || name != "" || n > 0 {
		return name
	}
	settle(ctx, game.SleepTick)
	name, _, _ = d.sweepReadLeviathan(ctx)
	return name
}

// sweepReadLeviathan issues one get_nearby and returns the first leviathan
// seen and how many creatures the reply held.
func (d *WorkerDispatch) sweepReadLeviathan(ctx context.Context) (string, int, error) {
	if err := d.Client.GetNearby(ctx); err != nil {
		fmt.Fprintf(d.Out, "pathfinder_sweep: get_nearby: %v\n", err) //nolint:errcheck
		return "", 0, err
	}
	var nearby serverapi.GetNearbyResponse
	if raw := d.Client.GetRawJSON("nearby"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &nearby); err != nil {
			return "", 0, fmt.Errorf("parse nearby: %w", err)
		}
	}
	for _, c := range nearby.Creatures {
		if strings.Contains(strings.ToLower(c.Species+" "+c.Name), "leviathan") {
			if c.Name != "" {
				return c.Name, len(nearby.Creatures), nil
			}
			return c.Species, len(nearby.Creatures), nil
		}
	}
	return "", len(nearby.Creatures), nil
}
