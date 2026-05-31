package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// FactionCapturedAt returns the captured_utc of a stored faction header and
// whether a row exists. A missing faction yields (zero, false, nil) — not an
// error — so callers can treat "unknown" and "stale" uniformly.
func (kb *SQLiteKB) FactionCapturedAt(ctx context.Context, factionID string) (time.Time, bool, error) {
	var capturedAt string
	err := kb.db.QueryRowContext(ctx,
		`SELECT captured_utc FROM factions WHERE faction_id = ?`, factionID).Scan(&capturedAt)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("faction captured_at: %w", err)
	}
	return parseUTC(capturedAt), true, nil
}

// StoreFaction upserts the faction header row.
func (kb *SQLiteKB) StoreFaction(ctx context.Context, r FactionRecord) error {
	_, err := kb.db.ExecContext(ctx, `
		INSERT INTO factions (faction_id, name, tag, leader_id, leader_username,
			treasury, member_count, owned_bases, description, charter, emblem,
			primary_color, secondary_color, founded_utc, intel_systems, intel_trade, captured_utc)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(faction_id) DO UPDATE SET
			name=excluded.name, tag=excluded.tag, leader_id=excluded.leader_id,
			leader_username=excluded.leader_username, treasury=excluded.treasury,
			member_count=excluded.member_count, owned_bases=excluded.owned_bases,
			description=excluded.description, charter=excluded.charter, emblem=excluded.emblem,
			primary_color=excluded.primary_color, secondary_color=excluded.secondary_color,
			founded_utc=excluded.founded_utc, intel_systems=excluded.intel_systems,
			intel_trade=excluded.intel_trade, captured_utc=excluded.captured_utc`,
		r.FactionID, r.Name, r.Tag, r.LeaderID, r.LeaderUsername, r.Treasury,
		r.MemberCount, r.OwnedBases, r.Description, r.Charter, r.Emblem,
		r.PrimaryColor, r.SecondaryColor, r.FoundedUTC, r.IntelSystems, r.IntelTrade, utc(r.CapturedAt))
	if err != nil {
		return fmt.Errorf("store faction: %w", err)
	}
	return nil
}

// ReplaceFactionMembers replaces all members for a faction.
func (kb *SQLiteKB) ReplaceFactionMembers(ctx context.Context, factionID string, members []FactionMember) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM faction_members WHERE faction_id=?`, factionID); err != nil {
			return err
		}
		for _, m := range members {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_members (faction_id, player_id, username, role, joined_utc, last_seen_utc, is_online, captured_utc)
				VALUES (?,?,?,?,?,?,?,?)`,
				factionID, m.PlayerID, m.Username, m.Role, m.JoinedUTC, m.LastSeenUTC, boolToInt(m.IsOnline), utc(m.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceFactionRelations replaces all relations for a faction.
func (kb *SQLiteKB) ReplaceFactionRelations(ctx context.Context, factionID string, rels []FactionRelation) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM faction_relations WHERE faction_id=?`, factionID); err != nil {
			return err
		}
		for _, r := range rels {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_relations (faction_id, target_faction_id, target_name, target_tag, kind, reason, terms, our_kills, their_kills, started_utc, captured_utc)
				VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
				factionID, r.TargetFactionID, r.TargetName, r.TargetTag, r.Kind, r.Reason, r.Terms, r.OurKills, r.TheirKills, r.StartedUTC, utc(r.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// StoreFactionBase upserts a single owned base.
func (kb *SQLiteKB) StoreFactionBase(ctx context.Context, b FactionBaseRow) error {
	_, err := kb.db.ExecContext(ctx, `
		INSERT INTO faction_bases (faction_id, base_id, base_name, system_id, system_name, poi_id, services_json, captured_utc)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(faction_id, base_id) DO UPDATE SET
			base_name=excluded.base_name, system_id=excluded.system_id, system_name=excluded.system_name,
			poi_id=excluded.poi_id, services_json=excluded.services_json, captured_utc=excluded.captured_utc`,
		b.FactionID, b.BaseID, b.BaseName, b.SystemID, b.SystemName, b.POIID, b.ServicesJSON, utc(b.CapturedAt))
	if err != nil {
		return fmt.Errorf("store faction base: %w", err)
	}
	return nil
}

// ReplaceFactionFacilities replaces facilities at one base.
func (kb *SQLiteKB) ReplaceFactionFacilities(ctx context.Context, factionID, baseID string, fs []FactionFacilityRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM faction_facilities WHERE faction_id=? AND base_id=?`, factionID, baseID); err != nil {
			return err
		}
		for _, f := range fs {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_facilities (faction_id, base_id, facility_id, facility_type, category, level, status, recipe_id, details_json, captured_utc)
				VALUES (?,?,?,?,?,?,?,?,?,?)`,
				factionID, baseID, f.FacilityID, f.FacilityType, f.Category, f.Level, f.Status, f.RecipeID, f.DetailsJSON, utc(f.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceFactionStorage replaces storage header + items at one base.
func (kb *SQLiteKB) ReplaceFactionStorage(ctx context.Context, s FactionStorageRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM faction_storage_items WHERE faction_id=? AND base_id=?`, s.FactionID, s.BaseID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO faction_storage (faction_id, base_id, credits, item_count, captured_utc)
			VALUES (?,?,?,?,?)
			ON CONFLICT(faction_id, base_id) DO UPDATE SET
				credits=excluded.credits, item_count=excluded.item_count, captured_utc=excluded.captured_utc`,
			s.FactionID, s.BaseID, s.Credits, s.ItemCount, utc(s.CapturedAt)); err != nil {
			return err
		}
		for _, it := range s.Items {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_storage_items (faction_id, base_id, item_id, name, quantity, size, captured_utc)
				VALUES (?,?,?,?,?,?,?)`,
				s.FactionID, s.BaseID, it.ItemID, it.Name, it.Quantity, it.Size, utc(s.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceFactionOrders replaces orders at one base.
func (kb *SQLiteKB) ReplaceFactionOrders(ctx context.Context, factionID, baseID string, orders []FactionOrderRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM faction_orders WHERE faction_id=? AND base_id=?`, factionID, baseID); err != nil {
			return err
		}
		for _, o := range orders {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_orders (faction_id, base_id, order_id, side, item_id, item_name, price_each, quantity, captured_utc)
				VALUES (?,?,?,?,?,?,?,?,?)`,
				factionID, baseID, o.OrderID, o.Side, o.ItemID, o.ItemName, o.PriceEach, o.Quantity, utc(o.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceFactionMissions replaces missions at one base.
func (kb *SQLiteKB) ReplaceFactionMissions(ctx context.Context, factionID, baseID string, ms []FactionMissionRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM faction_missions WHERE faction_id=? AND base_id=?`, factionID, baseID); err != nil {
			return err
		}
		for _, m := range ms {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_missions (faction_id, base_id, mission_id, title, type, description, giver_name, rewards_json, objectives_json, assigned_player_id, expiration_utc, captured_utc)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
				factionID, baseID, m.MissionID, m.Title, m.Type, m.Description, m.GiverName, m.RewardsJSON, m.ObjectivesJSON, m.AssignedPlayerID, m.ExpirationUTC, utc(m.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceFactionRooms replaces rooms at one base.
func (kb *SQLiteKB) ReplaceFactionRooms(ctx context.Context, factionID, baseID string, rooms []FactionRoomRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM faction_rooms WHERE faction_id=? AND base_id=?`, factionID, baseID); err != nil {
			return err
		}
		for _, r := range rooms {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_rooms (faction_id, base_id, room_id, name, access, description, captured_utc)
				VALUES (?,?,?,?,?,?,?)`,
				factionID, baseID, r.RoomID, r.Name, r.Access, r.Description, utc(r.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}
