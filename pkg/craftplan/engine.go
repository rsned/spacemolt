package craftplan

import (
	"context"
	"strings"
	"sync"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

const defaultMax = 100

// Engine computes craftable rows and plan results from a Source.
type Engine struct {
	src Source

	// catalogMu guards catalog cache. Engine is REPL-scoped so contention is
	// nil in practice; mutex is for safety against future concurrent calls.
	catalogMu sync.Mutex
	catalog   map[string]serverapi.Recipe // cached result of src.Recipes()
}

// New constructs an Engine. The Source must be non-nil; methods will panic
// otherwise.
func New(src Source) *Engine {
	if src == nil {
		panic("craftplan.New: Source must be non-nil")
	}
	return &Engine{src: src}
}

// recipes returns the cached catalog, fetching on first call or when refresh
// is true. Callers must not mutate the returned map.
func (e *Engine) recipes(ctx context.Context, refresh bool) (map[string]serverapi.Recipe, error) {
	e.catalogMu.Lock()
	defer e.catalogMu.Unlock()
	if refresh || e.catalog == nil {
		recs, err := e.src.Recipes(ctx, refresh)
		if err != nil {
			return nil, err
		}
		e.catalog = recs
	}
	return e.catalog, nil
}

// stringMatch reports whether needle appears in haystack case-insensitively.
// Empty needle always matches.
func stringMatch(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
