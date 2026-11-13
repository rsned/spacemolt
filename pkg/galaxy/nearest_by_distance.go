package galaxy

import (
	"context"
	"math"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// NearestStationByDistance is the accessible, non-stronghold station system
// closest to map position (x,y) in a straight line: where a pathfinder ship
// ends its sweep. Systems with no coordinates ((0,0)) are skipped; ties go
// to the smaller id. ok is false when there is no candidate.
func NearestStationByDistance(ctx context.Context, kb knowledge.Base, x, y float64) (string, bool, error) {
	ids, err := queryAccessibleStations(ctx, kb, false)
	if err != nil {
		return "", false, err
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return "", false, err
	}
	best, bestD := "", math.Inf(1)
	for _, s := range systems {
		if !want[s.ID] || (s.Position.X == 0 && s.Position.Y == 0) {
			continue
		}
		d := math.Hypot(s.Position.X-x, s.Position.Y-y)
		if d < bestD || (d == bestD && s.ID < best) {
			best, bestD = s.ID, d
		}
	}
	return best, best != "", nil
}
