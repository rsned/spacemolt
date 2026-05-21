package knowledge

import (
	"context"
	"database/sql"
	"fmt"
)

// ListFactionIDs returns all faction IDs that have a header row.
func (kb *SQLiteKB) ListFactionIDs(ctx context.Context) ([]string, error) {
	rows, err := kb.db.QueryContext(ctx, `SELECT faction_id FROM factions ORDER BY tag`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// LoadFactionView assembles the full current state for a faction. Returns
// (nil, nil) if no faction header row exists.
func (kb *SQLiteKB) LoadFactionView(ctx context.Context, factionID string) (*FactionView, error) {
	v := &FactionView{}
	row := kb.db.QueryRowContext(ctx, `
		SELECT faction_id, name, tag, leader_id, leader_username, treasury, member_count,
			owned_bases, description, charter, emblem, primary_color, secondary_color,
			founded_utc, intel_systems, intel_trade, captured_utc
		FROM factions WHERE faction_id=?`, factionID)
	var capturedUTC string
	r := &v.Faction
	if err := row.Scan(&r.FactionID, &r.Name, &r.Tag, &r.LeaderID, &r.LeaderUsername,
		&r.Treasury, &r.MemberCount, &r.OwnedBases, &r.Description, &r.Charter, &r.Emblem,
		&r.PrimaryColor, &r.SecondaryColor, &r.FoundedUTC, &r.IntelSystems, &r.IntelTrade, &capturedUTC); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load faction: %w", err)
	}
	r.CapturedAt = parseUTC(capturedUTC)

	var err error
	if v.Members, err = kb.loadMembers(ctx, factionID); err != nil {
		return nil, err
	}
	if v.Relations, err = kb.loadRelations(ctx, factionID); err != nil {
		return nil, err
	}
	if v.Bases, err = kb.loadBases(ctx, factionID); err != nil {
		return nil, err
	}
	if v.Facilities, err = kb.loadFactionFacilities(ctx, factionID); err != nil {
		return nil, err
	}
	if v.Storage, err = kb.loadStorage(ctx, factionID); err != nil {
		return nil, err
	}
	if v.Orders, err = kb.loadOrders(ctx, factionID); err != nil {
		return nil, err
	}
	if v.Missions, err = kb.loadMissions(ctx, factionID); err != nil {
		return nil, err
	}
	if v.Rooms, err = kb.loadRooms(ctx, factionID); err != nil {
		return nil, err
	}
	return v, nil
}

func (kb *SQLiteKB) loadMembers(ctx context.Context, fid string) ([]FactionMember, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT player_id, username, role, joined_utc, last_seen_utc, is_online, captured_utc
		FROM faction_members WHERE faction_id=? ORDER BY role, username`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionMember
	for rows.Next() {
		m := FactionMember{FactionID: fid}
		var online int
		var cap string
		if err := rows.Scan(&m.PlayerID, &m.Username, &m.Role, &m.JoinedUTC, &m.LastSeenUTC, &online, &cap); err != nil {
			return nil, err
		}
		m.IsOnline = online != 0
		m.CapturedAt = parseUTC(cap)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadRelations(ctx context.Context, fid string) ([]FactionRelation, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT target_faction_id, target_name, target_tag, kind, reason, terms, our_kills, their_kills, started_utc, captured_utc
		FROM faction_relations WHERE faction_id=? ORDER BY kind, target_tag`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionRelation
	for rows.Next() {
		rel := FactionRelation{FactionID: fid}
		var cap string
		if err := rows.Scan(&rel.TargetFactionID, &rel.TargetName, &rel.TargetTag, &rel.Kind, &rel.Reason, &rel.Terms, &rel.OurKills, &rel.TheirKills, &rel.StartedUTC, &cap); err != nil {
			return nil, err
		}
		rel.CapturedAt = parseUTC(cap)
		out = append(out, rel)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadBases(ctx context.Context, fid string) ([]FactionBaseRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT base_id, base_name, system_id, system_name, poi_id, services_json, captured_utc
		FROM faction_bases WHERE faction_id=? ORDER BY base_name`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionBaseRow
	for rows.Next() {
		b := FactionBaseRow{FactionID: fid}
		var cap string
		if err := rows.Scan(&b.BaseID, &b.BaseName, &b.SystemID, &b.SystemName, &b.POIID, &b.ServicesJSON, &cap); err != nil {
			return nil, err
		}
		b.CapturedAt = parseUTC(cap)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadFactionFacilities(ctx context.Context, fid string) ([]FactionFacilityRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT base_id, facility_id, facility_type, category, level, status, recipe_id, details_json, captured_utc
		FROM faction_facilities WHERE faction_id=? ORDER BY base_id, facility_type`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionFacilityRow
	for rows.Next() {
		f := FactionFacilityRow{FactionID: fid}
		var cap string
		if err := rows.Scan(&f.BaseID, &f.FacilityID, &f.FacilityType, &f.Category, &f.Level, &f.Status, &f.RecipeID, &f.DetailsJSON, &cap); err != nil {
			return nil, err
		}
		f.CapturedAt = parseUTC(cap)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadStorage(ctx context.Context, fid string) ([]FactionStorageRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT base_id, credits, item_count, captured_utc
		FROM faction_storage WHERE faction_id=? ORDER BY base_id`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionStorageRow
	for rows.Next() {
		s := FactionStorageRow{FactionID: fid}
		var cap string
		if err := rows.Scan(&s.BaseID, &s.Credits, &s.ItemCount, &cap); err != nil {
			return nil, err
		}
		s.CapturedAt = parseUTC(cap)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		items, err := kb.loadStorageItems(ctx, fid, out[i].BaseID)
		if err != nil {
			return nil, err
		}
		out[i].Items = items
	}
	return out, nil
}

func (kb *SQLiteKB) loadStorageItems(ctx context.Context, fid, baseID string) ([]FactionStorageItem, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT item_id, name, quantity, size FROM faction_storage_items
		WHERE faction_id=? AND base_id=? ORDER BY name`, fid, baseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionStorageItem
	for rows.Next() {
		var it FactionStorageItem
		if err := rows.Scan(&it.ItemID, &it.Name, &it.Quantity, &it.Size); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadOrders(ctx context.Context, fid string) ([]FactionOrderRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT base_id, order_id, side, item_id, item_name, price_each, quantity, captured_utc
		FROM faction_orders WHERE faction_id=? ORDER BY base_id, side, item_name`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionOrderRow
	for rows.Next() {
		o := FactionOrderRow{FactionID: fid}
		var cap string
		if err := rows.Scan(&o.BaseID, &o.OrderID, &o.Side, &o.ItemID, &o.ItemName, &o.PriceEach, &o.Quantity, &cap); err != nil {
			return nil, err
		}
		o.CapturedAt = parseUTC(cap)
		out = append(out, o)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadMissions(ctx context.Context, fid string) ([]FactionMissionRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT base_id, mission_id, title, type, description, giver_name, rewards_json, objectives_json, assigned_player_id, expiration_utc, captured_utc
		FROM faction_missions WHERE faction_id=? ORDER BY base_id, title`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionMissionRow
	for rows.Next() {
		m := FactionMissionRow{FactionID: fid}
		var cap string
		if err := rows.Scan(&m.BaseID, &m.MissionID, &m.Title, &m.Type, &m.Description, &m.GiverName, &m.RewardsJSON, &m.ObjectivesJSON, &m.AssignedPlayerID, &m.ExpirationUTC, &cap); err != nil {
			return nil, err
		}
		m.CapturedAt = parseUTC(cap)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (kb *SQLiteKB) loadRooms(ctx context.Context, fid string) ([]FactionRoomRow, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT base_id, room_id, name, access, description, captured_utc
		FROM faction_rooms WHERE faction_id=? ORDER BY base_id, name`, fid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FactionRoomRow
	for rows.Next() {
		rm := FactionRoomRow{FactionID: fid}
		var cap string
		if err := rows.Scan(&rm.BaseID, &rm.RoomID, &rm.Name, &rm.Access, &rm.Description, &cap); err != nil {
			return nil, err
		}
		rm.CapturedAt = parseUTC(cap)
		out = append(out, rm)
	}
	return out, rows.Err()
}
