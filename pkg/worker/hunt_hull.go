package worker

import (
	"context"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// huntCreatureWorkMinTier is the lowest hull tier allowed to take creature work.
const huntCreatureWorkMinTier = 2

// huntCreatureWorkCategory is the catalog category spelling for combat hulls.
// "Combat Support" is a distinct category and does not qualify.
const huntCreatureWorkCategory = "Combat"

// huntCreatureWorkHull reports whether classID is a T2+ combat hull. Any
// lookup error, an unknown class, or a class with no catalog category (legacy
// hulls such as the prospector and drillship) returns false, the safe failure.
func huntCreatureWorkHull(ctx context.Context, kb knowledge.Base, classID string) bool {
	if kb == nil || classID == "" {
		return false
	}
	sc, err := kb.GetShipClass(ctx, classID)
	if err != nil || sc == nil {
		return false
	}
	return sc.Tier >= huntCreatureWorkMinTier && sc.Category == huntCreatureWorkCategory
}
