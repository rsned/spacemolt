package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SeenPlayer is a single player observation. Mirrors the shape of
// game.ObservedPlayer but lives in pkg/knowledge so this package does not
// import pkg/game. Callers adapt one to the other.
type SeenPlayer struct {
	PlayerID       string
	Username       string
	ShipClass      string
	FactionID      string
	FactionTag     string
	ClanTag        string
	PrimaryColor   string
	SecondaryColor string
	StatusMessage  string
	Anonymous      bool
	InCombat       bool

	SystemID string // "" => identity-only, no sightings row
	POIID    string // "" => system-scope sighting (stored as empty string for PK uniqueness)
	Source   string // "get_nearby" | "get_system_agents" | "battle_alert" | ...
	SeenAt   time.Time

	Tick       int64  // game tick of the observation; 0 = unknown
	ObserverID string // player id of the agent that made the observation
}

// RecordSightings inserts/updates rows in seen_players, seen_player_ships,
// and seen_player_sightings for each observation. All writes share a single
// transaction. Records with an empty PlayerID are silently dropped.
func (kb *SQLiteKB) RecordSightings(obs []SeenPlayer) error {
	if len(obs) == 0 {
		return nil
	}

	tx, err := kb.db.Begin()
	if err != nil {
		return fmt.Errorf("knowledge: begin sightings tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, o := range obs {
		if o.PlayerID == "" {
			continue
		}
		seenStr := o.SeenAt.UTC().Format(time.RFC3339)
		anon := boolToInt(o.Anonymous)

		if _, err := tx.Exec(`
INSERT INTO seen_players
	(player_id, username, faction_id, faction_tag, clan_tag,
	 primary_color, secondary_color, status_message, anonymous,
	 first_seen_utc, last_seen_utc, sighting_count)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
ON CONFLICT(player_id) DO UPDATE SET
	username        = excluded.username,
	faction_id      = COALESCE(NULLIF(excluded.faction_id, ''), faction_id),
	faction_tag     = COALESCE(NULLIF(excluded.faction_tag, ''), faction_tag),
	clan_tag        = COALESCE(NULLIF(excluded.clan_tag, ''), clan_tag),
	primary_color   = COALESCE(NULLIF(excluded.primary_color, ''), primary_color),
	secondary_color = COALESCE(NULLIF(excluded.secondary_color, ''), secondary_color),
	status_message  = COALESCE(NULLIF(excluded.status_message, ''), status_message),
	anonymous       = excluded.anonymous,
	last_seen_utc   = excluded.last_seen_utc,
	sighting_count  = sighting_count + 1`,
			o.PlayerID, o.Username, o.FactionID, o.FactionTag, o.ClanTag,
			o.PrimaryColor, o.SecondaryColor, o.StatusMessage, anon,
			seenStr, seenStr,
		); err != nil {
			return fmt.Errorf("knowledge: upsert seen_players: %w", err)
		}

		if o.ShipClass != "" {
			if _, err := tx.Exec(`
INSERT INTO seen_player_ships
	(player_id, ship_class, first_seen_utc, last_seen_utc, sighting_count)
VALUES (?, ?, ?, ?, 1)
ON CONFLICT(player_id, ship_class) DO UPDATE SET
	last_seen_utc  = excluded.last_seen_utc,
	sighting_count = sighting_count + 1`,
				o.PlayerID, o.ShipClass, seenStr, seenStr,
			); err != nil {
				return fmt.Errorf("knowledge: upsert seen_player_ships: %w", err)
			}
		}

		if o.SystemID != "" {
			bucket := o.SeenAt.UTC().Truncate(time.Hour).Format(time.RFC3339)
			if _, err := tx.Exec(`
INSERT INTO seen_player_sightings
	(player_id, system_id, poi_id, bucket_hour_utc, ship_class, source,
	 in_combat, first_seen_utc, last_seen_utc, observation_count)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
ON CONFLICT(player_id, system_id, poi_id, bucket_hour_utc) DO UPDATE SET
	last_seen_utc     = excluded.last_seen_utc,
	in_combat         = excluded.in_combat,
	observation_count = observation_count + 1`,
				o.PlayerID, o.SystemID, o.POIID, bucket, o.ShipClass, o.Source,
				boolToInt(o.InCombat), seenStr, seenStr,
			); err != nil {
				return fmt.Errorf("knowledge: upsert seen_player_sightings: %w", err)
			}
			// Append-only timeline row: one per observation, never merged,
			// so "who was in this system at this tick, and who saw them" is
			// answerable after the fact.
			// The same observer seeing the same player in the same system at
			// the same tick is one observation however many calls reported it;
			// a POI-level read (get_nearby) upgrades a system-wide one with its
			// poi_id, and a later system-wide read never erases a known POI.
			if _, err := tx.Exec(`
INSERT INTO seen_player_events
	(player_id, observer_id, system_id, poi_id, ship_class, source,
	 in_combat, tick, seen_at_utc)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(observer_id, player_id, system_id, tick) WHERE tick > 0 DO UPDATE SET
	poi_id      = CASE WHEN excluded.poi_id <> '' THEN excluded.poi_id ELSE poi_id END,
	source      = CASE WHEN excluded.poi_id <> '' OR poi_id = '' THEN excluded.source ELSE source END,
	ship_class  = COALESCE(NULLIF(excluded.ship_class, ''), ship_class),
	in_combat   = excluded.in_combat,
	seen_at_utc = excluded.seen_at_utc`,
				o.PlayerID, o.ObserverID, o.SystemID, o.POIID, o.ShipClass, o.Source,
				boolToInt(o.InCombat), o.Tick, seenStr,
			); err != nil {
				return fmt.Errorf("knowledge: insert seen_player_events: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("knowledge: commit sightings: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// GetSeenPlayer returns the stored row for a player_id, or (nil, nil)
// if no such row exists.
func (kb *SQLiteKB) GetSeenPlayer(playerID string) (*SeenPlayer, error) {
	if playerID == "" {
		return nil, nil
	}
	var (
		out     SeenPlayer
		factID  sql.NullString
		factTag sql.NullString
		clan    sql.NullString
		pcol    sql.NullString
		scol    sql.NullString
		status  sql.NullString
		anonInt int
		last    string
	)
	err := kb.db.QueryRow(`
SELECT player_id, username, faction_id, faction_tag, clan_tag,
       primary_color, secondary_color, status_message, anonymous,
       last_seen_utc
FROM seen_players WHERE player_id = ?`, playerID,
	).Scan(
		&out.PlayerID, &out.Username, &factID, &factTag, &clan,
		&pcol, &scol, &status, &anonInt, &last,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("knowledge: get seen_player %s: %w", playerID, err)
	}
	out.FactionID = factID.String
	out.FactionTag = factTag.String
	out.ClanTag = clan.String
	out.PrimaryColor = pcol.String
	out.SecondaryColor = scol.String
	out.StatusMessage = status.String
	out.Anonymous = anonInt != 0
	if t, perr := time.Parse(time.RFC3339, last); perr == nil {
		out.SeenAt = t
	}
	return &out, nil
}

// SeenFaction is a distinct faction observed across seen_players, enriched with
// whether it has already been captured into the factions table.
type SeenFaction struct {
	FactionID   string
	FactionTag  string
	PlayerCount int       // distinct players seen flying this faction
	Sightings   int       // total sighting_count across those players
	LastSeen    time.Time // most recent sighting of any of its members
	Seeded      bool      // a row exists in the factions table
	Name        string    // faction name, if seeded
}

// ListSeenFactions returns the distinct non-empty faction_ids observed across
// seen_players, ordered by player count desc. Each row is left-joined against
// the factions table so callers can see which still need backfilling.
func (kb *SQLiteKB) ListSeenFactions(ctx context.Context) ([]SeenFaction, error) {
	rows, err := kb.db.QueryContext(ctx, `
SELECT sp.faction_id,
       MAX(sp.faction_tag),
       COUNT(*),
       COALESCE(SUM(sp.sighting_count), 0),
       MAX(sp.last_seen_utc),
       f.name,
       f.captured_utc
FROM seen_players sp
LEFT JOIN factions f ON f.faction_id = sp.faction_id
WHERE sp.faction_id IS NOT NULL AND sp.faction_id <> ''
GROUP BY sp.faction_id
ORDER BY COUNT(*) DESC, sp.faction_id`)
	if err != nil {
		return nil, fmt.Errorf("knowledge: list seen factions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []SeenFaction
	for rows.Next() {
		var (
			sf       SeenFaction
			tag      sql.NullString
			lastSeen sql.NullString
			name     sql.NullString
			captured sql.NullString
		)
		if err := rows.Scan(&sf.FactionID, &tag, &sf.PlayerCount, &sf.Sightings, &lastSeen, &name, &captured); err != nil {
			return nil, fmt.Errorf("knowledge: scan seen faction: %w", err)
		}
		sf.FactionTag = tag.String
		sf.LastSeen = parseUTC(lastSeen.String)
		sf.Seeded = captured.Valid
		sf.Name = name.String
		out = append(out, sf)
	}
	return out, rows.Err()
}
