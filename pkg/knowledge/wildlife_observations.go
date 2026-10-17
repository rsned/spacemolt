package knowledge

import (
	"context"
	"fmt"
)

// WildlifeObservation is one sighting of one individual creature at one game
// tick.
//
// Stage is NOT a field: it is derived from Name on write, so a caller cannot
// record a stage that disagrees with the name it came from.
type WildlifeObservation struct {
	CreatureID  string
	GameTick    int64
	ObservedUTC string
	Species     string
	Name        string
	Role        string
	Hull        int
	MaxHull     int
	InCombat    bool
	Branded     bool
	SystemID    string
	POIID       string
	AgentID     string
}

// WildlifeStageBucket is one species-and-stage population: the per-stage hull
// distribution a lifecycle writeup renders.
//
// Individuals counts DISTINCT creature_id within the stage, never rows. Stage
// "" is MATURE.
type WildlifeStageBucket struct {
	Species     string
	Stage       string
	DisplayName string // a representative sighted name, e.g. "Young Cloudwhale"
	Individuals int
	HullMin     int
	HullMax     int
	Sightings   int
	FirstSeen   string
	LastSeen    string
}

// WildlifeGrowth is one individual observed at more than one hull: the growth
// the collection exists to find.
type WildlifeGrowth struct {
	CreatureID   string
	Species      string
	FirstStage   string
	LastStage    string
	FirstHull    int
	LastHull     int
	FirstTick    int64
	LastTick     int64
	TicksElapsed int64
	Sightings    int
}

// RecordWildlifeObservations stores per-individual sightings, collapsing the
// deliberate double-look by (creature_id, game_tick).
//
// A later sighting REPLACES an earlier one for the same tick rather than being
// ignored, because the second look of a pair is the one taken after the arrival
// race has settled and is therefore the better reading.
func (kb *SQLiteKB) RecordWildlifeObservations(ctx context.Context, rows []WildlifeObservation) error {
	if len(rows) == 0 {
		return nil
	}

	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("record wildlife observations: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, r := range rows {
		if r.CreatureID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO wildlife_observations
				(creature_id, game_tick, observed_utc, species, name, stage, role,
				 hull, max_hull, in_combat, branded, system_id, poi_id, agent_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(creature_id, game_tick) DO UPDATE SET
				observed_utc = excluded.observed_utc,
				species      = CASE WHEN excluded.species <> '' THEN excluded.species ELSE wildlife_observations.species END,
				name         = CASE WHEN excluded.name <> '' THEN excluded.name ELSE wildlife_observations.name END,
				stage        = excluded.stage,
				role         = CASE WHEN excluded.role <> '' THEN excluded.role ELSE wildlife_observations.role END,
				hull         = excluded.hull,
				max_hull     = MAX(excluded.max_hull, wildlife_observations.max_hull),
				in_combat    = excluded.in_combat,
				branded      = excluded.branded,
				system_id    = CASE WHEN excluded.system_id <> '' THEN excluded.system_id ELSE wildlife_observations.system_id END,
				poi_id       = CASE WHEN excluded.poi_id <> '' THEN excluded.poi_id ELSE wildlife_observations.poi_id END,
				agent_id     = CASE WHEN excluded.agent_id <> '' THEN excluded.agent_id ELSE wildlife_observations.agent_id END
		`, r.CreatureID, r.GameTick, r.ObservedUTC, r.Species, r.Name, StageWord(r.Name), r.Role,
			r.Hull, r.MaxHull, boolInt(r.InCombat), boolInt(r.Branded),
			r.SystemID, r.POIID, r.AgentID); err != nil {
			return fmt.Errorf("record wildlife observation %s: %w", r.CreatureID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit wildlife observations: %w", err)
	}

	return nil
}

// WildlifeStageHistogram returns the per-stage hull distribution, ordered by
// species then by hull so the stages come out in growth order WITHOUT a
// hardcoded ladder -- the bands do not overlap within a species, so hull gives
// the order for free. That matters because the ladder differs between species:
// hoarfrost has a hatchling below its juvenile, cloudwhale a calf.
func (kb *SQLiteKB) WildlifeStageHistogram(ctx context.Context) ([]WildlifeStageBucket, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT species,
		       stage,
		       MIN(name)                  AS display_name,
		       COUNT(DISTINCT creature_id) AS individuals,
		       MIN(max_hull)              AS hull_min,
		       MAX(max_hull)              AS hull_max,
		       COUNT(*)                   AS sightings,
		       MIN(observed_utc)          AS first_seen,
		       MAX(observed_utc)          AS last_seen
		FROM wildlife_observations
		WHERE max_hull > 0
		GROUP BY species, stage
		ORDER BY species, hull_min`)
	if err != nil {
		return nil, fmt.Errorf("wildlife stage histogram: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []WildlifeStageBucket
	for rows.Next() {
		var b WildlifeStageBucket
		if err := rows.Scan(&b.Species, &b.Stage, &b.DisplayName, &b.Individuals,
			&b.HullMin, &b.HullMax, &b.Sightings, &b.FirstSeen, &b.LastSeen); err != nil {
			return nil, fmt.Errorf("scan wildlife stage bucket: %w", err)
		}
		out = append(out, b)
	}

	return out, rows.Err()
}

// WildlifeGrowthObserved returns the individuals whose max_hull has actually
// changed between sightings.
//
// This is the question the whole table exists to answer and it is deliberately
// a separate query rather than a column: as of 2026-10-16, ZERO of 1,971
// tracked individuals had been observed growing, so an empty result is the
// expected answer and must not be mistaken for a capture failure.
func (kb *SQLiteKB) WildlifeGrowthObserved(ctx context.Context) ([]WildlifeGrowth, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT creature_id,
		       MIN(species)  AS species,
		       MIN(game_tick) AS first_tick,
		       MAX(game_tick) AS last_tick,
		       COUNT(*)       AS sightings
		FROM wildlife_observations
		WHERE max_hull > 0
		GROUP BY creature_id
		HAVING MIN(max_hull) <> MAX(max_hull)
		ORDER BY MAX(max_hull) - MIN(max_hull) DESC`)
	if err != nil {
		return nil, fmt.Errorf("wildlife growth observed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []WildlifeGrowth
	for rows.Next() {
		var g WildlifeGrowth
		if err := rows.Scan(&g.CreatureID, &g.Species, &g.FirstTick, &g.LastTick, &g.Sightings); err != nil {
			return nil, fmt.Errorf("scan wildlife growth: %w", err)
		}
		g.TicksElapsed = g.LastTick - g.FirstTick
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// The endpoint readings come from the first and last tick specifically,
	// not from MIN/MAX of hull: an animal's stage at its FIRST sighting is
	// what makes a transition legible, and MIN(max_hull) would silently pick
	// the smallest reading even if it arrived last.
	for i := range out {
		if err := kb.db.QueryRowContext(ctx,
			`SELECT max_hull, stage FROM wildlife_observations WHERE creature_id = ? AND game_tick = ?`,
			out[i].CreatureID, out[i].FirstTick).Scan(&out[i].FirstHull, &out[i].FirstStage); err != nil {
			return nil, fmt.Errorf("wildlife growth first reading %s: %w", out[i].CreatureID, err)
		}
		if err := kb.db.QueryRowContext(ctx,
			`SELECT max_hull, stage FROM wildlife_observations WHERE creature_id = ? AND game_tick = ?`,
			out[i].CreatureID, out[i].LastTick).Scan(&out[i].LastHull, &out[i].LastStage); err != nil {
			return nil, fmt.Errorf("wildlife growth last reading %s: %w", out[i].CreatureID, err)
		}
	}

	return out, nil
}
