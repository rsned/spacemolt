package assets

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// UpsertFactionProfile writes the faction's scalars. One row per faction,
// refreshed by whichever member captured this cycle.
func (s *Store) UpsertFactionProfile(ctx context.Context, p FactionProfile, now time.Time) error {
	if s == nil || s.db == nil || p.FactionID == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO faction_profile
			(faction_id, name, tag, leader_id, treasury, member_count, owned_bases, captured_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(faction_id) DO UPDATE SET
			name=excluded.name, tag=excluded.tag, leader_id=excluded.leader_id,
			treasury=excluded.treasury, member_count=excluded.member_count,
			owned_bases=excluded.owned_bases, captured_at=excluded.captured_at`,
		p.FactionID, p.Name, p.Tag, p.LeaderID, p.Treasury, p.MemberCount, p.OwnedBases,
		rfc3339(now)); err != nil {
		return fmt.Errorf("assets: upsert faction profile %s: %w", p.FactionID, err)
	}

	return nil
}

// ReplaceFactionStorage swaps in the faction's full storage picture at both
// grains, exactly as ReplaceStorage does for an agent.
func (s *Store) ReplaceFactionStorage(ctx context.Context, factionID string, bases []FactionStorageBase, now time.Time) error {
	ts := rfc3339(now)

	return s.replaceSet(ctx, `DELETE FROM faction_storage WHERE faction_id = ?`, factionID, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM faction_storage_items WHERE faction_id = ?`, factionID); err != nil {
			return fmt.Errorf("assets: clear faction storage items for %s: %w", factionID, err)
		}
		for _, b := range bases {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_storage
					(faction_id, base_id, credits, fuel_reserve, fuel_capacity, captured_at)
				VALUES (?,?,?,?,?,?)`,
				factionID, b.BaseID, b.Credits, b.FuelReserve, b.FuelCapacity, ts); err != nil {
				return fmt.Errorf("assets: insert faction storage %s/%s: %w", factionID, b.BaseID, err)
			}
			for _, it := range b.Items {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO faction_storage_items
						(faction_id, base_id, item_id, name, quantity, size, captured_at)
					VALUES (?,?,?,?,?,?,?)`,
					factionID, b.BaseID, it.ItemID, it.Name, it.Quantity, it.Size, ts); err != nil {
					return fmt.Errorf("assets: insert faction storage item %s/%s/%s: %w",
						factionID, b.BaseID, it.ItemID, err)
				}
			}
		}

		return nil
	})
}

// LoadFactionStorage returns the stored faction holdings, resolving base ids
// through r. CaptureFaction uses it (nil resolver) to carry failed bases forward.
func (s *Store) LoadFactionStorage(ctx context.Context, factionID string, r BaseResolver) ([]FactionStorageBase, time.Time, error) {
	if s == nil || s.db == nil || factionID == "" {
		return nil, time.Time{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT base_id, credits, fuel_reserve, fuel_capacity, captured_at
		  FROM faction_storage WHERE faction_id = ? ORDER BY base_id`, factionID)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("assets: load faction storage %s: %w", factionID, err)
	}
	defer func() { _ = rows.Close() }()

	var (
		out    []FactionStorageBase
		at     time.Time
		rawIDs []string
	)
	for rows.Next() {
		var (
			b  FactionStorageBase
			ts string
		)
		if err := rows.Scan(&b.BaseID, &b.Credits, &b.FuelReserve, &b.FuelCapacity, &ts); err != nil {
			return nil, time.Time{}, fmt.Errorf("assets: scan faction storage %s: %w", factionID, err)
		}
		at = parseCapturedAt(ts)
		rawIDs = append(rawIDs, b.BaseID)
		b.BaseID = r.resolve(b.BaseID)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("assets: iterate faction storage %s: %w", factionID, err)
	}

	for i := range out {
		items, err := s.loadFactionStorageItems(ctx, factionID, rawIDs[i])
		if err != nil {
			return nil, time.Time{}, err
		}
		out[i].Items = items
	}

	return out, at, nil
}

func (s *Store) loadFactionStorageItems(ctx context.Context, factionID, baseID string) ([]StorageItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT item_id, name, quantity, size FROM faction_storage_items
		 WHERE faction_id = ? AND base_id = ? ORDER BY item_id`, factionID, baseID)
	if err != nil {
		return nil, fmt.Errorf("assets: load faction storage items %s/%s: %w", factionID, baseID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []StorageItem
	for rows.Next() {
		var it StorageItem
		if err := rows.Scan(&it.ItemID, &it.Name, &it.Quantity, &it.Size); err != nil {
			return nil, fmt.Errorf("assets: scan faction storage item %s/%s: %w", factionID, baseID, err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("assets: iterate faction storage items %s/%s: %w", factionID, baseID, err)
	}

	return out, nil
}
