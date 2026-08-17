package knowledge

import (
	"context"
	"fmt"
	"time"
)

// WildlifeCoverage is one LOOK for wildlife — a get_nearby at a POI or a
// survey_system census — recorded whether or not anything was found.
//
// It is deliberately separate from WildlifeSighting. A sighting is evidence
// that a species was present, so a place holding nothing produces no sighting
// and leaves no trace. That makes "checked, empty" identical in the database to
// "never visited", and the two demand opposite conclusions: the first bounds a
// population at zero, the second says nothing at all.
//
// CreaturesSeen == 0 is therefore the most valuable row in this table, not a
// degenerate one.
type WildlifeCoverage struct {
	SystemID string
	POIID    string
	// POIType is the habitat kind (asteroid_belt, sun, ...) so a zero can be
	// read against where it was taken: no creatures at a sun says much less
	// about a system's population than no creatures at its richest belt.
	POIType string
	// Source is WildlifeSourceNearby or WildlifeSourceSurvey. A survey carries
	// no POI id — its census is system-wide.
	Source        string
	SpeciesSeen   int
	CreaturesSeen int
	GameTick      int64
	ObservedUTC   string
	AgentID       string
}

// RecordWildlifeCoverage appends coverage rows. Rows with neither a system nor
// a POI are skipped: an observation that cannot say where it was taken bounds
// nothing.
func (kb *SQLiteKB) RecordWildlifeCoverage(ctx context.Context, rows []WildlifeCoverage) error {
	if len(rows) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)

	return kb.inTx(ctx, func(tx txer) error {
		for _, r := range rows {
			if r.SystemID == "" && r.POIID == "" {
				continue
			}
			observed := r.ObservedUTC
			if observed == "" {
				observed = now
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO wildlife_surveys
					(system_id, poi_id, poi_type, source, species_seen,
					 creatures_seen, game_tick, observed_utc, agent_id)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, r.SystemID, r.POIID, r.POIType, r.Source, r.SpeciesSeen,
				r.CreaturesSeen, r.GameTick, observed, r.AgentID); err != nil {
				return fmt.Errorf("record wildlife coverage %s/%s: %w", r.SystemID, r.POIID, err)
			}
		}

		return nil
	})
}

// GetWildlifeCoverage returns coverage rows for a place, newest first. An empty
// systemID returns every system; an empty poiID returns every POI in the system
// named. limit <= 0 defaults to 100.
func (kb *SQLiteKB) GetWildlifeCoverage(ctx context.Context, systemID, poiID string, limit int) ([]WildlifeCoverage, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT system_id, poi_id, poi_type, source, species_seen,
	             creatures_seen, game_tick, observed_utc, agent_id
	      FROM wildlife_surveys WHERE 1=1`
	args := []any{}
	if systemID != "" {
		q += " AND system_id = ?"
		args = append(args, systemID)
	}
	if poiID != "" {
		q += " AND poi_id = ?"
		args = append(args, poiID)
	}
	q += " ORDER BY game_tick DESC, id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := kb.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("get wildlife coverage: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []WildlifeCoverage
	for rows.Next() {
		var c WildlifeCoverage
		if err := rows.Scan(&c.SystemID, &c.POIID, &c.POIType, &c.Source,
			&c.SpeciesSeen, &c.CreaturesSeen, &c.GameTick, &c.ObservedUTC,
			&c.AgentID); err != nil {
			return nil, fmt.Errorf("scan wildlife coverage: %w", err)
		}
		out = append(out, c)
	}

	return out, rows.Err()
}

// WildlifeLook is the latest observation of one POI: how many creatures were
// standing there and when anyone last bothered to look.
type WildlifeLook struct {
	POIID         string
	POIType       string
	CreaturesSeen int
	SpeciesSeen   int
	GameTick      int64
	ObservedUTC   string
}

// LatestWildlifeLooks returns the most recent look at each POI in a system.
//
// This is the query the coverage table exists for: a POI absent from the result
// has never been checked, and a POI present with CreaturesSeen == 0 has been
// checked and was empty. Nothing else in the KB can tell those apart.
func (kb *SQLiteKB) LatestWildlifeLooks(ctx context.Context, systemID string) ([]WildlifeLook, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT s.poi_id, s.poi_type, s.creatures_seen, s.species_seen,
		       s.game_tick, s.observed_utc
		FROM wildlife_surveys s
		JOIN (
			SELECT poi_id, MAX(game_tick) AS gt
			FROM wildlife_surveys
			WHERE system_id = ? AND poi_id <> ''
			GROUP BY poi_id
		) l ON l.poi_id = s.poi_id AND l.gt = s.game_tick
		WHERE s.system_id = ? AND s.poi_id <> ''
		GROUP BY s.poi_id
		ORDER BY s.creatures_seen DESC, s.poi_id`, systemID, systemID)
	if err != nil {
		return nil, fmt.Errorf("latest wildlife looks %s: %w", systemID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []WildlifeLook
	for rows.Next() {
		var w WildlifeLook
		if err := rows.Scan(&w.POIID, &w.POIType, &w.CreaturesSeen,
			&w.SpeciesSeen, &w.GameTick, &w.ObservedUTC); err != nil {
			return nil, fmt.Errorf("scan wildlife look: %w", err)
		}
		out = append(out, w)
	}

	return out, rows.Err()
}
