package assets

import (
	"context"
	"fmt"
	"time"
)

// CargoRow is one item line in a ship's hold.
//
// Name and UnitSize are best-effort: the cargo wire carries them, but a hold
// read off cached client state has only the id and quantity. They default empty
// rather than blocking the capture, because the manifest's whole point is
// knowing WHAT is aboard — a nameless row still answers that.
type CargoRow struct {
	ShipID   string
	ItemID   string
	Name     string
	Quantity float64
	UnitSize int
}

// ReplaceCargo swaps in the full manifest for one ship's hold. Rows absent from
// the new set are deleted: a hold that empties must read empty, since carrying a
// stale manifest forward would report contraband already delivered.
//
// Scoped to (playerID, shipID) rather than the whole player, because an agent
// owns many hulls and capturing the active one must not wipe what we know about
// the others.
func (s *Store) ReplaceCargo(ctx context.Context, playerID, shipID string, rows []CargoRow, now time.Time) error {
	if s == nil || s.db == nil || playerID == "" || shipID == "" {
		return nil
	}
	ts := rfc3339(now)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("assets: begin cargo: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM agent_cargo WHERE player_id = ? AND ship_id = ?`, playerID, shipID); err != nil {
		return fmt.Errorf("assets: clear cargo for %s/%s: %w", playerID, shipID, err)
	}
	for _, r := range rows {
		if r.ItemID == "" {
			continue // a nameless line tells us nothing and would collide on the key
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO agent_cargo (player_id, ship_id, item_id, name, quantity, unit_size, captured_at)
			VALUES (?,?,?,?,?,?,?)`,
			playerID, shipID, r.ItemID, r.Name, r.Quantity, r.UnitSize, ts); err != nil {
			return fmt.Errorf("assets: insert cargo %s/%s/%s: %w", playerID, shipID, r.ItemID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("assets: commit cargo: %w", err)
	}

	return nil
}

// LoadCargo returns every cargo line this agent holds, across all its hulls.
func (s *Store) LoadCargo(ctx context.Context, playerID string) ([]CargoRow, error) {
	if s == nil || s.db == nil || playerID == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ship_id, item_id, name, quantity, unit_size
		  FROM agent_cargo WHERE player_id = ?
		 ORDER BY ship_id, item_id`, playerID)
	if err != nil {
		return nil, fmt.Errorf("assets: query cargo for %s: %w", playerID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []CargoRow
	for rows.Next() {
		var r CargoRow
		if err := rows.Scan(&r.ShipID, &r.ItemID, &r.Name, &r.Quantity, &r.UnitSize); err != nil {
			return nil, fmt.Errorf("assets: scan cargo for %s: %w", playerID, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("assets: iterate cargo for %s: %w", playerID, err)
	}

	return out, nil
}
