package galaxy

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// FindNearestByResource returns up to `limit` nearest accessible systems that
// contain at least one POI with a non-depleted entry for the given resource_id
// in the poi_resources table, starting from `fromSystem`.
//
// Callers are expected to have already called graph.BuildFromDB.
func FindNearestByResource(
	ctx context.Context,
	kb knowledge.Base,
	graph *GalaxyGraph,
	fromSystem string,
	resourceID string,
	limit int,
) ([]NearestResult, error) {
	if kb == nil {
		return nil, fmt.Errorf("nearest_by_resource: knowledge base is nil")
	}
	if graph == nil {
		return nil, fmt.Errorf("nearest_by_resource: graph is nil")
	}
	if fromSystem == "" {
		return nil, fmt.Errorf("nearest_by_resource: fromSystem is required")
	}
	if resourceID == "" {
		return nil, fmt.Errorf("nearest_by_resource: resourceID is required")
	}

	poisBySystem, err := querySystemsWithResource(ctx, kb, resourceID)
	if err != nil {
		return nil, err
	}
	if len(poisBySystem) == 0 {
		return nil, nil
	}

	targets := make([]string, 0, len(poisBySystem))
	for id := range poisBySystem {
		targets = append(targets, id)
	}

	results, err := graph.FindNearest(fromSystem, targets, limit)
	if err != nil {
		return nil, err
	}
	for i := range results {
		results[i].POIs = poisBySystem[results[i].SystemID]
	}
	return results, nil
}

// querySystemsWithResource returns the matching POIs grouped by system ID
// for any POI whose Resources list includes resourceID with Remaining > 0.
// Systems with no matching POI are absent from the map.
func querySystemsWithResource(ctx context.Context, kb knowledge.Base, resourceID string) (map[string][]NearestPOI, error) {
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return nil, fmt.Errorf("get systems: %w", err)
	}

	out := make(map[string][]NearestPOI)
	for _, sys := range systems {
		pois, err := kb.GetPOIs(ctx, sys.ID)
		if err != nil {
			continue
		}
		for _, poi := range pois {
			for _, res := range poi.Resources {
				if res.ResourceID != resourceID {
					continue
				}
				if res.Remaining <= 0 {
					continue
				}
				out[sys.ID] = append(out[sys.ID], NearestPOI{
					ID:        poi.ID,
					Name:      poi.Name,
					Remaining: res.Remaining,
				})
				break
			}
		}
	}
	return out, nil
}
