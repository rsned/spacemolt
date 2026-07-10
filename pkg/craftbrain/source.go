package craftbrain

import (
	"context"

	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Source provides every fact the Engine needs. The real implementation lives
// in cmd/tools/play_as and wraps the knowledge DB + market collector; tests
// use an in-memory fake.
//
// Every method is read-only. The Engine never mutates game or KB state.
type Source interface {
	// Recipes returns the whole recipe graph keyed by recipe_id, with Inputs
	// and Outputs hydrated.
	Recipes(ctx context.Context) (map[string]knowledge.RecipeDef, error)

	// Facilities returns known public production sites for recipeID, with
	// production details already parsed. Empty means none are known — which
	// signifies "unknown", not "impossible".
	Facilities(ctx context.Context, recipeID string) ([]Facility, error)

	// OnHand returns stock of itemID the fleet holds, attributed by holder.
	OnHand(ctx context.Context, itemID string) ([]Holding, error)

	// Buyable returns market sellers of itemID with at least qty depth.
	Buyable(ctx context.Context, itemID string, qty int) ([]finditem.Result, error)

	// SystemOf resolves a station_id (a base_id) to its system_id. Returns
	// "" when unknown.
	SystemOf(ctx context.Context, stationID string) (string, error)

	// Jumps returns hop distance from fromSystem to each of toSystems.
	Jumps(ctx context.Context, fromSystem string, toSystems []string) (map[string]int, error)

	// Coverage reports catalog breadth for the plan footer.
	Coverage(ctx context.Context) (Coverage, error)
}

// Engine plans crafts from a Source.
type Engine struct {
	src Source
}

// New constructs an Engine. src must be non-nil.
func New(src Source) *Engine {
	if src == nil {
		panic("craftbrain.New: Source must be non-nil")
	}
	return &Engine{src: src}
}
