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

	targets, err := querySystemsWithResource(ctx, kb, resourceID)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, nil
	}

	return graph.FindNearest(fromSystem, targets, limit)
}

// querySystemsWithResource returns system IDs that contain at least one POI
// whose Resources list includes resourceID with Remaining > 0.
func querySystemsWithResource(ctx context.Context, kb knowledge.Base, resourceID string) ([]string, error) {
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return nil, fmt.Errorf("get systems: %w", err)
	}

	systemSet := make(map[string]bool)
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
				systemSet[sys.ID] = true
				break
			}
			if systemSet[sys.ID] {
				break
			}
		}
	}

	result := make([]string, 0, len(systemSet))
	for id := range systemSet {
		result = append(result, id)
	}
	return result, nil
}
