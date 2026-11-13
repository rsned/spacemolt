package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SystemPositionRecorder stores a system position measured by a pathfinder
// drift (the last transit_x/transit_y reading before arrival). It writes
// only where the stored position is an estimate or a (0,0) placeholder,
// never over map coordinates, and reports whether it wrote.
type SystemPositionRecorder interface {
	RecordMeasuredSystemPosition(ctx context.Context, systemID string, x, y float64) (bool, error)
}

// RecordMeasuredSystemPosition implements SystemPositionRecorder. An
// unknown system is (false, nil).
func (kb *SQLiteKB) RecordMeasuredSystemPosition(ctx context.Context, systemID string, x, y float64) (bool, error) {
	var px, py float64
	var source string
	err := kb.db.QueryRowContext(ctx,
		`SELECT position_x, position_y, position_source FROM systems WHERE id = ?`, systemID).Scan(&px, &py, &source)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("measured position %s: %w", systemID, err)
	}
	if source == "api" && (px != 0 || py != 0) {
		return false, nil
	}
	if _, err = kb.db.ExecContext(ctx,
		`UPDATE systems SET position_x = ?, position_y = ?, position_source = 'measured' WHERE id = ?`,
		x, y, systemID); err != nil {
		return false, fmt.Errorf("measured position %s: %w", systemID, err)
	}
	return true, nil
}

// RecordMeasuredSystemPosition implements SystemPositionRecorder. The
// memory KB keeps no position source, so only a (0,0) placeholder is
// replaced.
func (kb *MemoryKB) RecordMeasuredSystemPosition(_ context.Context, systemID string, x, y float64) (bool, error) {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	s, ok := kb.systems[systemID]
	if !ok || s.Position.X != 0 || s.Position.Y != 0 {
		return false, nil
	}
	s.Position.X, s.Position.Y = x, y
	return true, nil
}
