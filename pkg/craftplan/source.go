package craftplan

import (
	"context"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// Source provides every fact the Engine needs to plan crafts. The real
// implementation in cmd/tools/play_as wraps game.GameClient + *sql.DB; the
// tests in this package use fakeSource so the engine can be exercised
// without spinning up a client or opening a database.
//
// Methods take a context so a slow view_storage call doesn't strand the
// REPL; the engine passes the same context through to every Source call
// for a single Craftable/Plan invocation.
type Source interface {
	// Recipes returns the server's authoritative catalog. The implementation
	// is allowed to cache; refresh=true forces a re-fetch.
	Recipes(ctx context.Context, refresh bool) (map[string]serverapi.Recipe, error)

	// Inventory returns the agent's current cargo + station storage. If
	// includeFaction is true the Faction map is populated; otherwise the
	// implementation MAY leave it nil (engine doesn't read it in that case).
	Inventory(ctx context.Context, includeFaction bool) (Inventory, error)

	// Skills returns the agent's current skill levels keyed by skill_id.
	// Missing skills are treated as level 0 by callers.
	Skills(ctx context.Context) (map[string]int, error)

	// CurrentStationID returns the station_id the agent is docked at, used
	// for the illegal-recipes legality gate and the output header. Returns
	// "" if not docked.
	CurrentStationID(ctx context.Context) (string, error)

	// IllegalAt returns the set of recipe_ids that are illegal to craft at
	// the given stationID. Missing crafting DB returns an empty map and
	// nil error (no recipes are illegal if we can't tell — server enforces
	// at craft time anyway).
	IllegalAt(ctx context.Context, stationID string) (map[string]bool, error)

	// BOM returns flattened bill-of-materials rows for the given recipe IDs,
	// keyed by recipe_id. Only target_type='item' rows are returned. Missing
	// crafting DB returns nil + a wrapped error; the engine surfaces this
	// as "BOM unavailable" when --reachable is requested.
	BOM(ctx context.Context, recipeIDs []string) (map[string][]BOMRow, error)
}
