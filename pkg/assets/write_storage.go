package assets

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ReplaceStorage swaps in the agent's full storage picture across every base,
// at both grains: an item that vanished from a base is deleted, and a base that
// vanished entirely is deleted too.
//
// Passing an empty slice legitimately clears everything. Unlike hulls, zero is
// reachable for storage -- an agent really can sell out. The guard against
// "empty because the calls failed" lives in CaptureStorage, which only ever
// hands this function a set it actually managed to observe.
func (s *Store) ReplaceStorage(ctx context.Context, playerID string, bases []StorageBase, now time.Time) error {
	return s.ReplaceStorageEvery(ctx, playerID, bases, now, 0)
}

// StorageHistoryDaily is the history spacing for agents whose storage moves
// slowly (everyone but the drone-hosting marketbots): one snapshot a day.
const StorageHistoryDaily = 23 * time.Hour

// StorageHistoryEvery is the minimum spacing of storage history snapshots for
// an agent. Marketbots host the station drones, whose mining rates
// cmd/tools/drone-rates measures, so every capture is kept; other agents fill
// storage slowly and keep one a day.
func StorageHistoryEvery(agentID string) time.Duration {
	if strings.HasPrefix(agentID, "marketbot") {
		return 0
	}

	return StorageHistoryDaily
}

// ReplaceStorageEvery is ReplaceStorage that appends to storage history only
// when the player's last history snapshot is at least historyEvery old (0 =
// every capture). Current storage is replaced either way.
func (s *Store) ReplaceStorageEvery(ctx context.Context, playerID string, bases []StorageBase, now time.Time, historyEvery time.Duration) error {
	ts := rfc3339(now)
	keepHistory := true
	if historyEvery > 0 {
		var last sql.NullString
		if err := s.db.QueryRowContext(ctx,
			`SELECT MAX(captured_at) FROM storage_history_snapshots WHERE player_id = ?`, playerID).Scan(&last); err != nil {
			return fmt.Errorf("assets: last storage snapshot %s: %w", playerID, err)
		}
		keepHistory = !last.Valid || now.Sub(parseCapturedAt(last.String)) >= historyEvery
	}

	// One transaction covers both tables: a crash between them would leave
	// orphaned item rows pointing at a base row that no longer exists.
	return s.replaceSet(ctx, `DELETE FROM agent_storage WHERE player_id = ?`, playerID, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM agent_storage_items WHERE player_id = ?`, playerID); err != nil {
			return fmt.Errorf("assets: clear storage items for %s: %w", playerID, err)
		}
		// History is append-only (storage_rates.go reads it); the snapshot row
		// marks this pass so an absent item reads as 0, not unknown.
		if keepHistory {
			if _, err := tx.ExecContext(ctx,
				`INSERT OR REPLACE INTO storage_history_snapshots (player_id, captured_at) VALUES (?,?)`,
				playerID, ts); err != nil {
				return fmt.Errorf("assets: insert storage snapshot %s: %w", playerID, err)
			}
		}
		for _, b := range bases {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO agent_storage (player_id, base_id, credits, captured_at) VALUES (?,?,?,?)`,
				playerID, b.BaseID, b.Credits, ts); err != nil {
				return fmt.Errorf("assets: insert storage %s/%s: %w", playerID, b.BaseID, err)
			}
			for _, it := range b.Items {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO agent_storage_items
						(player_id, base_id, item_id, name, quantity, size, captured_at)
					VALUES (?,?,?,?,?,?,?)`,
					playerID, b.BaseID, it.ItemID, it.Name, it.Quantity, it.Size, ts); err != nil {
					return fmt.Errorf("assets: insert storage item %s/%s/%s: %w",
						playerID, b.BaseID, it.ItemID, err)
				}
				if !keepHistory {
					continue
				}
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO storage_history (player_id, base_id, item_id, quantity, captured_at)
					VALUES (?,?,?,?,?)
					ON CONFLICT (player_id, base_id, item_id, captured_at)
					DO UPDATE SET quantity = excluded.quantity`,
					playerID, b.BaseID, it.ItemID, it.Quantity, ts); err != nil {
					return fmt.Errorf("assets: insert storage history %s/%s/%s: %w",
						playerID, b.BaseID, it.ItemID, err)
				}
			}
		}

		return nil
	})
}

// LoadStorage returns the stored holdings, resolving base ids through r.
// CaptureStorage uses it (with a nil resolver) to carry forward bases whose
// individual queries failed.
func (s *Store) LoadStorage(ctx context.Context, playerID string, r BaseResolver) ([]StorageBase, time.Time, error) {
	if s == nil || s.db == nil || playerID == "" {
		return nil, time.Time{}, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT base_id, credits, captured_at FROM agent_storage WHERE player_id = ? ORDER BY base_id`,
		playerID)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("assets: load storage %s: %w", playerID, err)
	}
	defer func() { _ = rows.Close() }()

	var (
		out    []StorageBase
		at     time.Time
		rawIDs []string
	)
	for rows.Next() {
		var (
			b  StorageBase
			ts string
		)
		if err := rows.Scan(&b.BaseID, &b.Credits, &ts); err != nil {
			return nil, time.Time{}, fmt.Errorf("assets: scan storage %s: %w", playerID, err)
		}
		at = parseCapturedAt(ts)
		rawIDs = append(rawIDs, b.BaseID)
		b.BaseID = r.resolve(b.BaseID)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("assets: iterate storage %s: %w", playerID, err)
	}

	// Items are fetched with the UNRESOLVED id: that is what the column holds.
	for i := range out {
		items, err := s.loadStorageItems(ctx, playerID, rawIDs[i])
		if err != nil {
			return nil, time.Time{}, err
		}
		out[i].Items = items
	}

	return out, at, nil
}

func (s *Store) loadStorageItems(ctx context.Context, playerID, baseID string) ([]StorageItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT item_id, name, quantity, size FROM agent_storage_items
		 WHERE player_id = ? AND base_id = ? ORDER BY item_id`, playerID, baseID)
	if err != nil {
		return nil, fmt.Errorf("assets: load storage items %s/%s: %w", playerID, baseID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []StorageItem
	for rows.Next() {
		var it StorageItem
		if err := rows.Scan(&it.ItemID, &it.Name, &it.Quantity, &it.Size); err != nil {
			return nil, fmt.Errorf("assets: scan storage item %s/%s: %w", playerID, baseID, err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("assets: iterate storage items %s/%s: %w", playerID, baseID, err)
	}

	return out, nil
}
