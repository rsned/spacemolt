package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"
)

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
	// RentalFeePerRun is the credit fee a renter pays per production run
	// (server: production.rental_fee_per_run). The per-unit price
	// (output_price_per_unit) and throughput are preserved in DetailsJSON.
	RentalFeePerRun int
	LastSeenTick    int
	// LastSeenUTC is the wall-clock capture time (RFC3339). Prefer it over
	// LastSeenTick for staleness: the GameClock only ever syncs forward, so
	// tick deltas understate real age. Stamped by the upsert when left empty.
	LastSeenUTC string
	DetailsJSON string // raw captured payload, forward-compat; holds production.ticks_per_run etc.
}

// upsertPublicFacilityRow writes one row, keyed on (station_id, facility_id).
// Shared by UpsertPublicFacilities and ReplacePublicFacilitiesAtStation so the
// two cannot drift on which columns a refresh updates.
func upsertPublicFacilityRow(ctx context.Context, tx txer, r PublicFacility, now string) error {
	seenUTC := r.LastSeenUTC
	if seenUTC == "" {
		seenUTC = now
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO public_facilities
			(station_id, facility_id, recipe_id, facility_name, category, level, rental_fee_per_run, owner_faction, public, details_json, last_seen_tick, last_seen_utc)
		VALUES (?,?,?,?,?,?,?,?,1,?,?,?)
		ON CONFLICT(station_id, facility_id) DO UPDATE SET
			recipe_id     = excluded.recipe_id,
			facility_name = excluded.facility_name,
			category      = excluded.category,
			level         = excluded.level,
			rental_fee_per_run = excluded.rental_fee_per_run,
			owner_faction = excluded.owner_faction,
			public        = excluded.public,
			details_json  = excluded.details_json,
			last_seen_tick = excluded.last_seen_tick,
			last_seen_utc  = excluded.last_seen_utc`,
		r.StationID, r.FacilityID, r.RecipeID, r.FacilityName, r.Category, r.Level, r.RentalFeePerRun,
		r.OwnerFaction, r.DetailsJSON, r.LastSeenTick, seenUTC)
	return err
}

// UpsertPublicFacilities inserts or refreshes public_facilities rows, keyed on
// (station_id, facility_id). Every row is stored as public=1 — this table only
// holds public facilities.
//
// This is insert-only: it can add and refresh, never remove. A caller holding a
// station's COMPLETE facility listing should use ReplacePublicFacilitiesAtStation
// instead, so a dismantled facility stops being reported as live.
func (kb *SQLiteKB) UpsertPublicFacilities(ctx context.Context, rows []PublicFacility) error {
	now := time.Now().UTC().Format(time.RFC3339)
	return kb.inTx(ctx, func(tx txer) error {
		for _, r := range rows {
			if err := upsertPublicFacilityRow(ctx, tx, r, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplacePublicFacilitiesAtStation makes the catalog for one station match what
// a scrape just saw: rows are upserted, and any row still on file for that
// station that the scrape did NOT return is deleted. It returns how many rows
// were pruned.
//
// Why this exists: the table is keyed (station_id, facility_id) and was only
// ever written by upsert, so a facility that got dismantled — or made private —
// stayed on file forever, still answering "you can build this recipe here". A
// live audit found 280 such rows across 49 stations, the oldest 43 days behind,
// overstating facility-only recipe coverage by 7 recipes.
//
// This is only safe because a station's facility listing was measured to be
// COMPLETE, not truncated: crimson_war_citadel returns all 104 of its rows in
// one capture, and the two largest stations return different counts (231 and
// 223), so there is no server-side cap that would make a full listing look like
// a shrunken one. If that ever changes, this method starts deleting live rows.
//
// The whole thing is one transaction, so the station is never momentarily
// empty for a concurrent reader, and a mid-write failure leaves the previous
// catalog intact rather than a half-pruned one.
//
// DANGEROUS IF MISUSED: this deletes. Only call it with a station's complete
// listing. Handing it a partial or failed scrape erases live facilities — the
// same shape as the catalog refresh that erased the legacy mining hulls. The
// completeness judgement belongs to the caller; see
// upsertPublicFromFacilityList, which only reaches this path when the response
// decoded, named its station, and carried at least one facility section.
func (kb *SQLiteKB) ReplacePublicFacilitiesAtStation(ctx context.Context, stationID string, rows []PublicFacility) (int, error) {
	if stationID == "" {
		return 0, fmt.Errorf("replace public facilities: empty station id")
	}
	// A foreign row means two scrapes got mixed together: the delete would be
	// scoped to one station while the insert wrote another. Refuse the call
	// rather than apply half of it.
	for _, r := range rows {
		if r.StationID != stationID {
			return 0, fmt.Errorf("replace public facilities at %q: facility %q belongs to station %q", stationID, r.FacilityID, r.StationID)
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	var pruned int
	err := kb.inTx(ctx, func(tx txer) error {
		for _, r := range rows {
			if err := upsertPublicFacilityRow(ctx, tx, r, now); err != nil {
				return err
			}
		}
		// With no rows the scrape saw nothing public here, and every row on
		// file for the station is stale — the delete is unqualified by design.
		query := `DELETE FROM public_facilities WHERE station_id = ?`
		args := []any{stationID}
		if len(rows) > 0 {
			// Station facility counts are small (single digits in practice),
			// so an inline NOT IN stays far under SQLite's parameter limit.
			placeholders := make([]string, len(rows))
			for i, r := range rows {
				placeholders[i] = "?"
				args = append(args, r.FacilityID)
			}
			query += ` AND facility_id NOT IN (` + strings.Join(placeholders, ",") + `)`
		}
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		pruned = int(n)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return pruned, nil
}

// FacilitiesForRecipe returns public production facilities that can craft
// recipeID, most-recently-seen first.
func (kb *SQLiteKB) FacilitiesForRecipe(ctx context.Context, recipeID string) ([]PublicFacility, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT station_id, facility_id, recipe_id, facility_name, category, owner_faction, level, rental_fee_per_run, last_seen_tick, last_seen_utc, details_json
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
			&f.OwnerFaction, &f.Level, &f.RentalFeePerRun, &f.LastSeenTick, &f.LastSeenUTC, &f.DetailsJSON); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
