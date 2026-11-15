package knowledge

import (
	"context"
	"fmt"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// FactionIntelStore keeps a faction's intel pool (faction_query_intel) in the
// KB, where any tool or agent can query it.
type FactionIntelStore interface {
	StoreFactionIntel(ctx context.Context, factionID string, entries []serverapi.IntelEntry, queryTick int64, capturedUTC string) error
}

// StoreFactionIntel implements FactionIntelStore. Each entry replaces that
// system's intel rows (a deposit gone from intel is gone here). POI hazards are
// also recorded in pois via RecordPOIHazard so hazard checks see them.
func (kb *SQLiteKB) StoreFactionIntel(ctx context.Context, factionID string, entries []serverapi.IntelEntry, queryTick int64, capturedUTC string) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store faction intel: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	for _, e := range entries {
		for _, q := range []string{`faction_intel_pois`, `faction_intel_resources`, `faction_intel_connections`} {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+q+` WHERE faction_id = ? AND system_id = ?`, factionID, e.SystemID); err != nil { // #nosec G202 -- fixed table names
				return fmt.Errorf("clear %s %s: %w", q, e.SystemID, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO faction_intel_systems
			(faction_id, system_id, name, empire, police_level, description, submitted_by, submitter_name, submitted_at_tick, auto_synced, query_tick, captured_utc)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			factionID, e.SystemID, e.Name, e.Empire, e.PoliceLevel, e.Description, e.SubmittedBy, e.SubmitterName,
			e.SubmittedAtTick, b2i(e.AutoSynced), queryTick, capturedUTC); err != nil {
			return fmt.Errorf("intel system %s: %w", e.SystemID, err)
		}
		for _, c := range e.Connections {
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO faction_intel_connections (faction_id, system_id, to_system_id, distance) VALUES (?, ?, ?, ?)`,
				factionID, e.SystemID, c.SystemID, c.Distance); err != nil {
				return fmt.Errorf("intel lane %s-%s: %w", e.SystemID, c.SystemID, err)
			}
		}
		for _, p := range e.POIs {
			ht, hs := "", 0
			if p.Hazard != nil {
				ht, hs = p.Hazard.Type, p.Hazard.Severity
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO faction_intel_pois
				(faction_id, poi_id, system_id, name, type, class, description, base_id, base_name, deep_core, hazard_type, hazard_severity, position_x, position_y, read_at_tick)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				factionID, p.ID, e.SystemID, p.Name, p.Type, p.Class, p.Description, p.BaseID, p.BaseName, b2i(p.DeepCore),
				ht, hs, p.Position.X, p.Position.Y, p.ReadAtTick); err != nil {
				return fmt.Errorf("intel poi %s: %w", p.ID, err)
			}
			for _, r := range p.Resources {
				if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO faction_intel_resources
					(faction_id, poi_id, system_id, resource_id, richness, remaining, max_remaining, depletion_percent, read_at_tick)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					factionID, p.ID, e.SystemID, r.ResourceID, r.Richness, r.Remaining, r.MaxRemaining, r.DepletionPercent, p.ReadAtTick); err != nil {
					return fmt.Errorf("intel resource %s/%s: %w", p.ID, r.ResourceID, err)
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store faction intel: %w", err)
	}
	for _, e := range entries {
		for _, p := range e.POIs {
			if p.Hazard == nil || p.Hazard.Severity <= 0 {
				continue
			}
			seen := p.ReadAtTick
			if seen == 0 {
				seen = e.SubmittedAtTick
			}
			if err := kb.RecordPOIHazard(ctx, POI{ID: p.ID, SystemID: e.SystemID, Name: p.Name, Type: p.Type,
				Position: game.Position{X: p.Position.X, Y: p.Position.Y}, HazardType: p.Hazard.Type, HazardSeverity: p.Hazard.Severity, LastUpdatedTick: seen}); err != nil {
				return err
			}
		}
	}
	return nil
}
