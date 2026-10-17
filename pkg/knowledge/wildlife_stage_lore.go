package knowledge

import (
	"context"
	"fmt"
)

// WildlifeStageLoreRow is one species' lore at one growth stage.
//
// Stage "" is MATURE, consistent with wildlife_observations. DisplayName keeps
// the stage-qualified name exactly as the scan reported it, so an entry can be
// shown in the server's own words rather than a reconstruction.
type WildlifeStageLoreRow struct {
	Species     string
	Stage       string
	DisplayName string
	Description string
	ScannedUTC  string
	// MaxHull is the hull reported by a SCAN of this stage, ratcheted up
	// within the stage. Distinct in provenance from the observed population
	// range, which WildlifeStageHistogram derives from wildlife_observations.
	MaxHull int
}

// UpsertWildlifeStageLore records a description against its growth stage.
//
// A blank description is ignored rather than stored: a scan that revealed no
// lore must not erase lore already on file for that stage. A row with a hull
// but no lore is still worth keeping, so hull alone is enough to write.
func (kb *SQLiteKB) UpsertWildlifeStageLore(ctx context.Context, r WildlifeStageLoreRow) error {
	if r.Species == "" || (r.Description == "" && r.MaxHull == 0) {
		return nil
	}
	if _, err := kb.db.ExecContext(ctx, `
		INSERT INTO wildlife_stage_lore (species, stage, display_name, description, scanned_utc, max_hull)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(species, stage) DO UPDATE SET
			display_name = CASE WHEN excluded.display_name <> '' THEN excluded.display_name ELSE wildlife_stage_lore.display_name END,
			description  = CASE WHEN excluded.description <> '' THEN excluded.description ELSE wildlife_stage_lore.description END,
			scanned_utc  = excluded.scanned_utc,
			max_hull     = MAX(excluded.max_hull, wildlife_stage_lore.max_hull)
	`, r.Species, r.Stage, r.DisplayName, r.Description, r.ScannedUTC, r.MaxHull); err != nil {
		return fmt.Errorf("upsert wildlife stage lore %s/%s: %w", r.Species, r.Stage, err)
	}

	return nil
}

// WildlifeStageLore returns every stage's lore for one species, youngest-named
// first is NOT attempted: stage order is not hardcoded anywhere, so rows come
// back ordered by stage name with mature ("") first. Join against
// WildlifeStageHistogram to order by hull, which gives growth order for free.
func (kb *SQLiteKB) WildlifeStageLore(ctx context.Context, species string) ([]WildlifeStageLoreRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT species, stage, display_name, description, scanned_utc, max_hull
		FROM wildlife_stage_lore WHERE species = ? ORDER BY max_hull, stage`, species)
	if err != nil {
		return nil, fmt.Errorf("wildlife stage lore %s: %w", species, err)
	}
	defer func() { _ = rows.Close() }()

	var out []WildlifeStageLoreRow
	for rows.Next() {
		var r WildlifeStageLoreRow
		if err := rows.Scan(&r.Species, &r.Stage, &r.DisplayName, &r.Description,
			&r.ScannedUTC, &r.MaxHull); err != nil {
			return nil, fmt.Errorf("scan wildlife stage lore: %w", err)
		}
		out = append(out, r)
	}

	return out, rows.Err()
}
