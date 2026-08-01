package assets

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ReplaceHulls swaps in the agent's full owned-ship set. Ships absent from rows
// are deleted — a hull the agent has sold must not linger as phantom capacity.
func (s *Store) ReplaceHulls(ctx context.Context, playerID string, rows []Hull, now time.Time) error {
	ts := rfc3339(now)

	return s.replaceSet(ctx, `DELETE FROM agent_hulls WHERE player_id = ?`, playerID, func(tx *sql.Tx) error {
		for _, h := range rows {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO agent_hulls (player_id, ship_id, class_id, class_name, is_active,
					hull_current, hull_max, hull_raw, fuel_current, fuel_max, fuel_raw,
					cargo_used, location, location_base_id, modules,
					listing_id, listing_price, listing_base_id, captured_at)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				playerID, h.ShipID, h.ClassID, h.ClassName, h.IsActive,
				h.HullCurrent, h.HullMax, h.HullRaw, h.FuelCurrent, h.FuelMax, h.FuelRaw,
				h.CargoUsed, h.Location, h.LocationBaseID, h.Modules,
				h.ListingID, h.ListingPrice, h.ListingBaseID, ts); err != nil {
				return fmt.Errorf("assets: insert hull %s/%s: %w", playerID, h.ShipID, err)
			}
		}

		return nil
	})
}
