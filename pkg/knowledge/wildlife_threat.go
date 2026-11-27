package knowledge

import (
	"context"
	"fmt"
)

// WildlifeDamage is a species' recorded offence: damage per shot fired
// (misses included) and how many shots that rests on.
type WildlifeDamage struct {
	PerShot float64
	Shots   int
}

// WildlifeDamagePerShot is, per species, the damage it has dealt per shot
// fired across every recorded battle. A species fires about once a tick, so
// this stands in for its damage per tick.
func (kb *SQLiteKB) WildlifeDamagePerShot(ctx context.Context) (map[string]WildlifeDamage, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT species, SUM(damage_total), SUM(shots)
		FROM wildlife_attacks
		GROUP BY species
		HAVING SUM(shots) > 0
	`)
	if err != nil {
		return nil, fmt.Errorf("query wildlife damage per shot: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]WildlifeDamage{}
	for rows.Next() {
		var species string
		var total, shots float64
		if err := rows.Scan(&species, &total, &shots); err != nil {
			return nil, fmt.Errorf("scan wildlife damage per shot: %w", err)
		}
		out[species] = WildlifeDamage{PerShot: total / shots, Shots: int(shots)}
	}
	return out, rows.Err()
}

// HunterDamagePerTick is the hull agentID destroyed per fight tick over its
// last lastKills recorded kills, and how many kills that covers. Fight ticks
// include closing the range, so the figure runs low rather than high.
func (kb *SQLiteKB) HunterDamagePerTick(ctx context.Context, agentID string, lastKills int) (perTick float64, kills int, err error) {
	var hull, ticks float64
	err = kb.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(max_hull), 0), COALESCE(SUM(duration_ticks), 0), COUNT(*)
		FROM (
			SELECT max_hull, duration_ticks FROM wildlife_kills
			WHERE agent_id = ? AND duration_ticks > 0 AND max_hull > 0
			ORDER BY killed_utc DESC
			LIMIT ?
		)
	`, agentID, lastKills).Scan(&hull, &ticks, &kills)
	if err != nil {
		return 0, 0, fmt.Errorf("query hunter damage per tick: %w", err)
	}
	if ticks <= 0 {
		return 0, 0, nil
	}
	return hull / ticks, kills, nil
}
