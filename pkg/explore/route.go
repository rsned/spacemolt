package explore

import (
	"math"

	"github.com/rsned/spacemolt/pkg/game"
)

// PlanRoute orders POIs using nearest-neighbor heuristic starting from startPOI.
// If startPOI is in the list, it becomes the first entry. Otherwise, the first POI is.
func PlanRoute(pois []game.POI, startPOI string) []game.POI {
	if len(pois) <= 1 {
		result := make([]game.POI, len(pois))
		copy(result, pois)
		return result
	}

	remaining := make(map[int]bool, len(pois))
	for i := range pois {
		remaining[i] = true
	}

	startIdx := 0
	for i, poi := range pois {
		if poi.ID == startPOI {
			startIdx = i
			break
		}
	}

	route := []game.POI{pois[startIdx]}
	curPos := pois[startIdx].Position
	delete(remaining, startIdx)

	// Nearest-neighbor greedy. Ties go to the lower index, so the route is
	// the same on every run.
	for len(remaining) > 0 {
		bestIdx := -1
		bestDist := math.MaxFloat64

		for idx := range remaining {
			d := Distance(curPos, pois[idx].Position)
			if d < bestDist || (d == bestDist && idx < bestIdx) {
				bestDist = d
				bestIdx = idx
			}
		}

		route = append(route, pois[bestIdx])
		curPos = pois[bestIdx].Position
		delete(remaining, bestIdx)
	}

	return route
}

// Distance returns the Euclidean distance between two positions.
func Distance(a, b game.Position) float64 {
	return math.Hypot(b.X-a.X, b.Y-a.Y)
}

// POIPosition finds the position of a POI by ID in a list, falling back to
// the first POI's.
func POIPosition(pois []game.POI, poiID string) game.Position {
	for _, poi := range pois {
		if poi.ID == poiID {
			return poi.Position
		}
	}
	if len(pois) > 0 {
		return pois[0].Position
	}
	return game.Position{}
}
