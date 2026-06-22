package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// FactionTag returns the tag recorded for a faction and ok=true, or
// ("", false, nil) when the faction is unknown or has no tag stored. Used for
// cheap id→tag display lookups without loading the full faction view.
func (kb *SQLiteKB) FactionTag(ctx context.Context, factionID string) (string, bool, error) {
	var tag sql.NullString
	err := kb.db.QueryRowContext(ctx, `SELECT tag FROM factions WHERE faction_id = ?`, factionID).Scan(&tag)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("faction tag: %w", err)
	}
	if !tag.Valid || tag.String == "" {
		return "", false, nil
	}
	return tag.String, true, nil
}

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

// factionListSeedSentinelUTC marks a faction header seeded from faction_list
// data only — there has been no full faction_info capture yet. It is set far in
// the past on purpose so the faction backfiller still treats the row as stale
// and will enrich it with full detail when the faction is next observed, rather
// than being fooled into thinking a fresh capture exists.
const factionListSeedSentinelUTC = "1970-01-01T00:00:00Z"

// UpsertFactionListEntry inserts a faction header from the lightweight fields a
// faction_list response carries. When the faction already exists it refreshes
// only those columns, leaving the faction_info-sourced columns (treasury,
// leader_id, description, charter, emblem, founded_utc, intel_*) and the
// captured_utc full-capture timestamp untouched — so a cheap header refresh
// never clobbers richer data. Reports whether a new row was inserted.
func (kb *SQLiteKB) UpsertFactionListEntry(ctx context.Context, e FactionListEntry) (bool, error) {
	var existed bool
	if err := kb.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM factions WHERE faction_id = ?)`, e.FactionID).Scan(&existed); err != nil {
		return false, fmt.Errorf("faction exists check: %w", err)
	}
	// On INSERT the columns faction_list doesn't carry get empty/zero defaults
	// (matching StoreFaction, which never writes NULL — LoadFactionView scans
	// them as plain strings). On CONFLICT only the faction_list columns are
	// updated, so an existing full capture's rich columns and captured_utc are
	// left intact.
	_, err := kb.db.ExecContext(ctx, `
		INSERT INTO factions (faction_id, name, tag, leader_id, leader_username, treasury,
			member_count, owned_bases, description, charter, emblem, primary_color,
			secondary_color, founded_utc, intel_systems, intel_trade, captured_utc)
		VALUES (?,?,?,'',?,0,?,?,'','','',?,?,'',0,0,?)
		ON CONFLICT(faction_id) DO UPDATE SET
			name=excluded.name, tag=excluded.tag, leader_username=excluded.leader_username,
			member_count=excluded.member_count, owned_bases=excluded.owned_bases,
			primary_color=excluded.primary_color, secondary_color=excluded.secondary_color`,
		e.FactionID, e.Name, e.Tag, e.LeaderUsername, e.MemberCount,
		e.OwnedBases, e.PrimaryColor, e.SecondaryColor, factionListSeedSentinelUTC)
	if err != nil {
		return false, fmt.Errorf("upsert faction list entry: %w", err)
	}
	return !existed, nil
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

// ReplaceFactionFuelBunkers replaces the whole galaxy-wide fuel-bunker set for a
// faction (the faction_info summary is delivered as one list across all bases,
// not per-base, so the replace is faction-scoped).
func (kb *SQLiteKB) ReplaceFactionFuelBunkers(ctx context.Context, factionID string, bunkers []FactionFuelBunkerRow) error {
	return kb.inTx(ctx, func(tx txer) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM faction_fuel_bunkers WHERE faction_id=?`, factionID); err != nil {
			return err
		}
		for _, b := range bunkers {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_fuel_bunkers (faction_id, base_id, base_name, fuel_reserve, fuel_capacity, captured_utc)
				VALUES (?,?,?,?,?,?)`,
				factionID, b.BaseID, b.BaseName, b.FuelReserve, b.FuelCapacity, utc(b.CapturedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}
