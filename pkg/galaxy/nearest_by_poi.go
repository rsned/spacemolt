package galaxy

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// FindNearestByPOIType returns up to `limit` nearest accessible systems that
// contain a POI of the given type, starting from `fromSystem`.
//
// For poiType == "station", only systems with a publicly accessible station
// (PublicAccess == true) and not flagged IsStronghold are included. For any
// other poiType, any system containing a POI of that type is included.
//
// Callers are expected to have already called graph.BuildFromDB.
func FindNearestByPOIType(
	ctx context.Context,
	kb knowledge.Base,
	graph *GalaxyGraph,
	fromSystem string,
	poiType string,
	limit int,
) ([]NearestResult, error) {
	if kb == nil {
		return nil, fmt.Errorf("nearest_by_poi: knowledge base is nil")
	}
	if graph == nil {
		return nil, fmt.Errorf("nearest_by_poi: graph is nil")
	}
	if fromSystem == "" {
		return nil, fmt.Errorf("nearest_by_poi: fromSystem is required")
	}
	if poiType == "" {
		return nil, fmt.Errorf("nearest_by_poi: poiType is required")
	}

	var targets []string
	var err error
	if poiType == "station" {
		targets, err = queryAccessibleStations(ctx, kb)
	} else {
		targets, err = queryPOIsByType(ctx, kb, poiType)
	}
	if err != nil {
		return nil, err
	}

	if len(targets) == 0 {
		return nil, nil
	}

	return graph.FindNearest(fromSystem, targets, limit)
}

// queryAccessibleStations returns system IDs that contain at least one
// publicly accessible station and are not strongholds.
func queryAccessibleStations(ctx context.Context, kb knowledge.Base) ([]string, error) {
	systems, err := kb.GetSystems(ctx)
	if err != nil {
		return nil, fmt.Errorf("get systems: %w", err)
	}

	systemSet := make(map[string]bool)
	for _, sys := range systems {
		if sys.IsStronghold {
			continue
		}
		pois, err := kb.GetPOIs(ctx, sys.ID)
		if err != nil {
			continue
		}
		for _, poi := range pois {
			if poi.Type != "station" {
				continue
			}
			base, err := kb.GetBaseByPOI(ctx, poi.ID)
			if err == nil && base != nil && base.PublicAccess {
				systemSet[sys.ID] = true
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

// queryPOIsByType returns system IDs containing any POI of the given type.
func queryPOIsByType(ctx context.Context, kb knowledge.Base, poiType string) ([]string, error) {
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
			if poi.Type == poiType {
				systemSet[sys.ID] = true
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
