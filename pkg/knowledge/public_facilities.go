package knowledge

import "context"

// PublicFacility is a captured public crafting facility at a station — who
// runs it, what recipe it produces, and its labor cost/level.
type PublicFacility struct {
	StationID    string
	FacilityID   string
	RecipeID     string
	FacilityName string
	Category     string
	OwnerFaction string
	Level        int
	LaborCost    int
	LastSeenTick int
	DetailsJSON  string // raw captured payload, forward-compat (write-only; not populated by queries)
}

// UpsertPublicFacilities inserts or refreshes public_facilities rows, keyed on
// (station_id, facility_id). Every row is stored as public=1 — this table only
// holds public facilities.
func (kb *SQLiteKB) UpsertPublicFacilities(ctx context.Context, rows []PublicFacility) error {
	return kb.inTx(ctx, func(tx txer) error {
		for _, r := range rows {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO public_facilities
					(station_id, facility_id, recipe_id, facility_name, category, level, labor_cost, owner_faction, public, details_json, last_seen_tick)
				VALUES (?,?,?,?,?,?,?,?,1,?,?)
				ON CONFLICT(station_id, facility_id) DO UPDATE SET
					recipe_id     = excluded.recipe_id,
					facility_name = excluded.facility_name,
					category      = excluded.category,
					level         = excluded.level,
					labor_cost    = excluded.labor_cost,
					owner_faction = excluded.owner_faction,
					public        = excluded.public,
					details_json  = excluded.details_json,
					last_seen_tick = excluded.last_seen_tick`,
				r.StationID, r.FacilityID, r.RecipeID, r.FacilityName, r.Category, r.Level, r.LaborCost,
				r.OwnerFaction, r.DetailsJSON, r.LastSeenTick); err != nil {
				return err
			}
		}
		return nil
	})
}

// FacilitiesForRecipe returns public production facilities that can craft
// recipeID, most-recently-seen first.
func (kb *SQLiteKB) FacilitiesForRecipe(ctx context.Context, recipeID string) ([]PublicFacility, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT station_id, facility_id, recipe_id, facility_name, category, owner_faction, level, labor_cost, last_seen_tick
		FROM public_facilities
		WHERE recipe_id = ? AND public = 1 AND category = 'production'
		ORDER BY last_seen_tick DESC`, recipeID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []PublicFacility
	for rows.Next() {
		var f PublicFacility
		if err := rows.Scan(&f.StationID, &f.FacilityID, &f.RecipeID, &f.FacilityName, &f.Category,
			&f.OwnerFaction, &f.Level, &f.LaborCost, &f.LastSeenTick); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
