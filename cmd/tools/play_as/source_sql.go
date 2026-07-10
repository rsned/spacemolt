package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/craftbrain"
	"github.com/rsned/spacemolt/pkg/finditem"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// craftbrainSource adapts the knowledge DB + market collector to
// craftbrain.Source. Read-only: it never writes.
type craftbrainSource struct {
	kb           *knowledge.SQLiteKB
	col          *market.Collector
	originSystem string

	recipeCache map[string]knowledge.RecipeDef
	graph       navigation.JumpGraph
	sysCache    map[string]string
}

func newCraftbrainSource(kb *knowledge.SQLiteKB, col *market.Collector, originSystem string) *craftbrainSource {
	return &craftbrainSource{kb: kb, col: col, originSystem: originSystem, sysCache: map[string]string{}}
}

func (s *craftbrainSource) Recipes(ctx context.Context) (map[string]knowledge.RecipeDef, error) {
	if s.recipeCache != nil {
		return s.recipeCache, nil
	}
	defs, err := s.kb.GetRecipes(ctx) // hydrates Inputs + Outputs in bulk
	if err != nil {
		return nil, err
	}
	out := make(map[string]knowledge.RecipeDef, len(defs))
	for _, d := range defs {
		out[d.ID] = d
	}
	s.recipeCache = out
	return out, nil
}

// Facilities returns known public production sites for recipeID. Every row is
// run through ParseProduction: site.go trusts OutputPerRun/TicksPerRun to
// already be populated and ParseProduction is what guarantees OutputPerRun
// >= 1, so skipping it would silently degrade per-instance throughput instead
// of failing loudly.
func (s *craftbrainSource) Facilities(ctx context.Context, recipeID string) ([]craftbrain.Facility, error) {
	rows, err := s.kb.FacilitiesForRecipe(ctx, recipeID)
	if err != nil {
		return nil, err
	}
	out := make([]craftbrain.Facility, 0, len(rows))
	for _, r := range rows {
		f := craftbrain.Facility{
			StationID:       r.StationID,
			FacilityID:      r.FacilityID,
			Level:           r.Level,
			RentalFeePerRun: r.RentalFeePerRun,
			LastSeenUTC:     r.LastSeenUTC,
		}
		// A malformed payload must not kill the plan; ParseProduction leaves
		// safe defaults (OutputPerRun=1) on parse failure.
		_ = f.ParseProduction(r.DetailsJSON)
		out = append(out, f)
	}
	return out, nil
}

// OnHand sums both pools: personal storage (storage_snapshots +
// storage_snapshot_items) and faction storage (faction_storage_items). Each
// holding keeps its holder so Executor B knows whom to ask.
//
// Both queries GROUP BY the holding's (Holder, BaseID) key and carry an
// explicit ORDER BY:
//   - storage_snapshots is upsert-only (UNIQUE(agent_id, base_id)), so there
//     is at most one snapshot row per (agent, base) already; the GROUP BY on
//     the join additionally collapses any accidental duplicate item rows
//     within one snapshot (storage_snapshot_items has no uniqueness
//     constraint of its own) so a single Holding always comes out per
//     (agent_id, base_id) pair.
//   - faction_storage_items is keyed PRIMARY KEY(faction_id, base_id,
//     item_id) and DELETE+INSERT per (faction_id, base_id), so it already
//     holds current state; the GROUP BY on base_id additionally collapses
//     the rare case of two different factions holding stock at the same
//     base_id (Holder is uniformly "" for the faction pool, so distinct
//     base_id is what the (Holder, BaseID) contract requires to be unique).
//
// The explicit ORDER BY matters because the engine's own comparator sorts
// candidates on (jumps, BaseID, Holder, Qty) but never CapturedAt: two
// otherwise-identical holdings would tie under that comparator, and without
// a deterministic source order which one gets consumed (and therefore
// whether the emitted node is flagged stale) would depend on sort algorithm
// internals rather than being reproducible.
func (s *craftbrainSource) OnHand(ctx context.Context, itemID string) ([]craftbrain.Holding, error) {
	var out []craftbrain.Holding

	personalRows, err := s.kb.DB().QueryContext(ctx, `
		SELECT ss.agent_id, ss.base_id, SUM(ssi.quantity) AS qty, ss.captured_at
		FROM storage_snapshots ss
		JOIN storage_snapshot_items ssi ON ssi.snapshot_id = ss.id
		WHERE ssi.item_id = ?
		GROUP BY ss.agent_id, ss.base_id, ss.captured_at
		HAVING SUM(ssi.quantity) > 0
		ORDER BY ss.agent_id, ss.base_id`, itemID)
	if err != nil {
		return nil, err
	}
	func() {
		defer func() { _ = personalRows.Close() }()
		for personalRows.Next() {
			var agentID, baseID, capturedAt string
			var qty float64
			if scanErr := personalRows.Scan(&agentID, &baseID, &qty, &capturedAt); scanErr != nil {
				err = scanErr
				return
			}
			out = append(out, craftbrain.Holding{
				Holder:     agentID,
				BaseID:     baseID,
				Qty:        int(qty),
				CapturedAt: parseUTCOrZero(capturedAt),
			})
		}
		if scanErr := personalRows.Err(); scanErr != nil {
			err = scanErr
		}
	}()
	if err != nil {
		return nil, err
	}

	factionRows, err := s.kb.DB().QueryContext(ctx, `
		SELECT fsi.base_id, SUM(fsi.quantity) AS qty, MAX(fsi.captured_utc) AS captured_utc
		FROM faction_storage_items fsi
		WHERE fsi.item_id = ?
		GROUP BY fsi.base_id
		HAVING SUM(fsi.quantity) > 0
		ORDER BY fsi.base_id`, itemID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = factionRows.Close() }()
	for factionRows.Next() {
		var baseID, capturedUTC string
		var qty float64
		if err := factionRows.Scan(&baseID, &qty, &capturedUTC); err != nil {
			return nil, err
		}
		out = append(out, craftbrain.Holding{
			BaseID:     baseID, // Holder "" = faction pool
			Qty:        int(qty),
			CapturedAt: parseUTCOrZero(capturedUTC),
		})
	}
	return out, factionRows.Err()
}

func (s *craftbrainSource) Buyable(ctx context.Context, itemID string, qty int) ([]finditem.Result, error) {
	if s.col == nil {
		return nil, nil // no market data -> caller degrades to BLOCKED
	}
	return finditem.Find(ctx, s.col, s.kb, itemID, float64(qty), s.originSystem, 5)
}

// SystemOf resolves a station_id to a system. station_id is a bases.id, whose
// bases.poi_id points at pois.id; only pois carries system_id. Fall back to
// treating the id as a poi id, since some callers pass one.
func (s *craftbrainSource) SystemOf(ctx context.Context, stationID string) (string, error) {
	if stationID == "" {
		return "", nil
	}
	if v, ok := s.sysCache[stationID]; ok {
		return v, nil
	}
	var sys string
	err := s.kb.DB().QueryRowContext(ctx, `
		SELECT p.system_id FROM bases b JOIN pois p ON p.id = b.poi_id WHERE b.id = ?`, stationID).Scan(&sys)
	if errors.Is(err, sql.ErrNoRows) {
		err = s.kb.DB().QueryRowContext(ctx, `SELECT system_id FROM pois WHERE id = ?`, stationID).Scan(&sys)
	}
	if errors.Is(err, sql.ErrNoRows) {
		s.sysCache[stationID] = ""
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("system of %s: %w", stationID, err)
	}
	s.sysCache[stationID] = sys
	return sys, nil
}

func (s *craftbrainSource) Jumps(ctx context.Context, fromSystem string, toSystems []string) (map[string]int, error) {
	if s.graph == nil {
		conns, err := s.kb.GetConnections(ctx)
		if err != nil {
			return nil, err
		}
		s.graph = navigation.JumpGraphFromConnections(conns)
	}
	return navigation.BFSJumps(s.graph, fromSystem, toSystems), nil
}

// Coverage measures catalog breadth. Errors propagate: this footer is what
// tells the operator whether a BLOCKED node means "not swept yet" or "truly
// impossible". Silently reporting "0 stations, 0/0 recipes" on a failed query
// would read as "the catalog is empty" -- a lie at exactly the moment the
// operator is deciding whether to trust a BLOCKED node.
func (s *craftbrainSource) Coverage(ctx context.Context) (craftbrain.Coverage, error) {
	var c craftbrain.Coverage
	db := s.kb.DB()
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT station_id) FROM public_facilities`).Scan(&c.Stations); err != nil {
		return c, fmt.Errorf("coverage stations: %w", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM recipes WHERE facility_only = 1`).Scan(&c.FacilityOnlyTotal); err != nil {
		return c, fmt.Errorf("coverage facility_only total: %w", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT r.id) FROM recipes r
		JOIN public_facilities f ON f.recipe_id = r.id
		WHERE r.facility_only = 1`).Scan(&c.FacilityOnlyCovered); err != nil {
		return c, fmt.Errorf("coverage facility_only covered: %w", err)
	}
	return c, nil
}

var _ craftbrain.Source = (*craftbrainSource)(nil)

// parseUTCOrZero parses an RFC3339 stamp, returning the zero time when the
// column is empty (rows written by a bin/worker predating 21e60dc).
func parseUTCOrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
