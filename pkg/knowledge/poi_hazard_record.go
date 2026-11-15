package knowledge

import (
	"context"
	"fmt"
)

// POIHazardRecorder records a POI's environmental hazard from a source that
// knows the hazard but not the rest of the POI's read state (faction intel:
// no hidden flag, no full read). Only the hazard columns of a known POI move;
// an unknown POI is added so the hazard is on file before any ship lands.
type POIHazardRecorder interface {
	RecordPOIHazard(ctx context.Context, poi POI) error
}

// RecordPOIHazard implements POIHazardRecorder. poi.LastUpdatedTick is the
// tick the hazard was observed; it becomes hazard_seen_tick, never the POI's
// own last_updated_tick, so a later real read still wins on everything else.
func (kb *SQLiteKB) RecordPOIHazard(ctx context.Context, poi POI) error {
	if poi.HazardSeverity <= 0 || poi.ID == "" {
		return nil
	}
	_, err := kb.db.ExecContext(ctx, `
		INSERT INTO pois (id, system_id, name, type, description, position_x, position_y, last_updated_tick, hazard_type, hazard_severity, hazard_seen_tick)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			hazard_type = CASE WHEN excluded.hazard_seen_tick >= pois.hazard_seen_tick THEN excluded.hazard_type ELSE pois.hazard_type END,
			hazard_severity = CASE WHEN excluded.hazard_seen_tick >= pois.hazard_seen_tick THEN excluded.hazard_severity ELSE pois.hazard_severity END,
			hazard_seen_tick = MAX(pois.hazard_seen_tick, excluded.hazard_seen_tick)
	`, poi.ID, poi.SystemID, poi.Name, poi.Type, poi.Description, poi.Position.X, poi.Position.Y,
		poi.HazardType, poi.HazardSeverity, poi.LastUpdatedTick)
	if err != nil {
		return fmt.Errorf("record hazard %s: %w", poi.ID, err)
	}
	return nil
}

// RecordPOIHazard implements POIHazardRecorder for the in-memory KB.
func (kb *MemoryKB) RecordPOIHazard(_ context.Context, poi POI) error {
	if poi.HazardSeverity <= 0 || poi.ID == "" {
		return nil
	}
	kb.mu.Lock()
	defer kb.mu.Unlock()
	if p, ok := kb.pois[poi.ID]; ok {
		p.HazardType, p.HazardSeverity = poi.HazardType, poi.HazardSeverity
		return nil
	}
	n := poi
	n.LastUpdatedTick = 0
	kb.pois[poi.ID] = &n
	return nil
}
