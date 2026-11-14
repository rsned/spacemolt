package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/rsned/spacemolt/pkg/game"
)

// SystemPositionRecorder stores a system position measured by a pathfinder
// drift (the last transit_x/transit_y reading before arrival). The first
// measurement stands: it writes where the system is unknown (creating the
// row, named name), or where the stored position is an estimate or a (0,0)
// placeholder, never over map coordinates or an earlier measurement, and
// reports whether it wrote. Later landings are caught anywhere inside the
// capture radius, tens of LY off, so they must not replace the first.
type SystemPositionRecorder interface {
	RecordMeasuredSystemPosition(ctx context.Context, systemID, name string, x, y float64) (bool, error)
}

// isOriginPlaceholder reports a (0,0) reading for any system but Sol: the
// API's placeholder for an uncharted system, never a real position.
func isOriginPlaceholder(systemID string, x, y float64) bool {
	return x == 0 && y == 0 && systemID != "sol"
}

// RecordMeasuredSystemPosition implements SystemPositionRecorder.
func (kb *SQLiteKB) RecordMeasuredSystemPosition(ctx context.Context, systemID, name string, x, y float64) (bool, error) {
	if isOriginPlaceholder(systemID, x, y) {
		return false, nil
	}
	var px, py float64
	var source string
	err := kb.db.QueryRowContext(ctx,
		`SELECT position_x, position_y, position_source FROM systems WHERE id = ?`, systemID).Scan(&px, &py, &source)
	if errors.Is(err, sql.ErrNoRows) {
		// A frontier system reached by drift: the map capture never made a
		// row for it (it has no coordinates), so the measurement does.
		if name == "" {
			name = systemID
		}
		if _, err = kb.db.ExecContext(ctx,
			`INSERT INTO systems (id, name, position_x, position_y, position_source) VALUES (?, ?, ?, ?, 'measured')`,
			systemID, name, x, y); err != nil {
			return false, fmt.Errorf("measured position %s: %w", systemID, err)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("measured position %s: %w", systemID, err)
	}
	if source == "measured" || (source == "api" && (px != 0 || py != 0)) {
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
// memory KB keeps no position source: an unknown system is created and only
// a (0,0) placeholder is replaced.
func (kb *MemoryKB) RecordMeasuredSystemPosition(ctx context.Context, systemID, name string, x, y float64) (bool, error) {
	if isOriginPlaceholder(systemID, x, y) {
		return false, nil
	}
	kb.mu.Lock()
	s, ok := kb.systems[systemID]
	if ok {
		defer kb.mu.Unlock()
		if s.Position.X != 0 || s.Position.Y != 0 {
			return false, nil
		}
		s.Position.X, s.Position.Y = x, y
		return true, nil
	}
	kb.mu.Unlock()
	if name == "" {
		name = systemID
	}
	if err := kb.RememberSystem(ctx, System{ID: systemID, Name: name, Position: game.Position{X: x, Y: y}}); err != nil {
		return false, err
	}
	return true, nil
}

// SystemPositionSourcer reports where a system's coordinates came from:
// "api" (map, exact), "estimate" or "measured"; "" for an unknown system.
type SystemPositionSourcer interface {
	SystemPositionSource(ctx context.Context, systemID string) (string, error)
}

// SystemPositionSource implements SystemPositionSourcer.
func (kb *SQLiteKB) SystemPositionSource(ctx context.Context, systemID string) (string, error) {
	var src string
	err := kb.db.QueryRowContext(ctx, `SELECT position_source FROM systems WHERE id = ?`, systemID).Scan(&src)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("position source %s: %w", systemID, err)
	}
	return src, nil
}
