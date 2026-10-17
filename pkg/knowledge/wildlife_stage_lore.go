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
}

// UpsertWildlifeStageLore records a description against its growth stage.
//
// A blank description is ignored rather than stored: a scan that revealed no
// lore must not erase lore already on file for that stage.
func (kb *SQLiteKB) UpsertWildlifeStageLore(ctx context.Context, r WildlifeStageLoreRow) error {
	if r.Species == "" || r.Description == "" {
		return nil
	}
	if _, err := kb.db.ExecContext(ctx, `
		INSERT INTO wildlife_stage_lore (species, stage, display_name, description, scanned_utc)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(species, stage) DO UPDATE SET
			display_name = CASE WHEN excluded.display_name <> '' THEN excluded.display_name ELSE wildlife_stage_lore.display_name END,
			description  = excluded.description,
			scanned_utc  = excluded.scanned_utc
	`, r.Species, r.Stage, r.DisplayName, r.Description, r.ScannedUTC); err != nil {
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
		SELECT species, stage, display_name, description, scanned_utc
		FROM wildlife_stage_lore WHERE species = ? ORDER BY stage`, species)
	if err != nil {
		return nil, fmt.Errorf("wildlife stage lore %s: %w", species, err)
	}
	defer func() { _ = rows.Close() }()

	var out []WildlifeStageLoreRow
	for rows.Next() {
		var r WildlifeStageLoreRow
		if err := rows.Scan(&r.Species, &r.Stage, &r.DisplayName, &r.Description, &r.ScannedUTC); err != nil {
			return nil, fmt.Errorf("scan wildlife stage lore: %w", err)
		}
		out = append(out, r)
	}

	return out, rows.Err()
}
