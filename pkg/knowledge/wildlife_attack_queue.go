package knowledge

import (
	"context"
	"fmt"
)

// BattleToCapture is one battle whose log has not been mined for creature
// offence yet, together with what we already know about who was in it.
type BattleToCapture struct {
	BattleID string
	// SpeciesByCreatureID comes from the kills recorded in that battle, which
	// were typed by the get_nearby that named the quarry. It is the trustworthy
	// half of the mapping; the rest is resolved by display name at capture time.
	SpeciesByCreatureID map[string]string
}

// BattlesNeedingAttackCapture lists battles we fought creatures in and never
// read the damage log for.
//
// The queue is derived rather than stored: a kill row carries the battle id, and
// wildlife_attacks carries a row per species per battle, so a battle present in
// the first and absent from the second is exactly one that has not been mined.
// That means no queue table to keep in sync, and re-running the capture is
// idempotent — the battle simply stops appearing.
//
// Battles are returned newest first: a fresh log is the one most likely to still
// be fetchable, and the freshest fights are the ones an operator is asking about.
//
// agentID scopes the queue to battles that agent actually fought. It is not an
// optimisation: the command is scheduled fleet-wide, and an unscoped queue would
// have all 161 workers racing to fetch the same handful of logs every hour —
// idempotent, but 161x the requests for one result. Empty means every battle,
// which is what a one-off operator query wants.
func (kb *SQLiteKB) BattlesNeedingAttackCapture(ctx context.Context, agentID string, limit int) ([]BattleToCapture, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := kb.db.QueryContext(ctx, `
		SELECT k.battle_id, k.creature_id, k.species, MAX(k.game_tick) AS gt
		FROM wildlife_kills k
		WHERE k.battle_id <> ''
		  AND (? = '' OR k.agent_id = ?)
		  AND NOT EXISTS (
		      SELECT 1 FROM wildlife_attacks a WHERE a.battle_id = k.battle_id
		  )
		GROUP BY k.battle_id, k.creature_id, k.species
		ORDER BY gt DESC`, agentID, agentID)
	if err != nil {
		return nil, fmt.Errorf("battles needing attack capture: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []BattleToCapture
	index := map[string]int{}
	for rows.Next() {
		var battleID, creatureID, species string
		var tick int64
		if err := rows.Scan(&battleID, &creatureID, &species, &tick); err != nil {
			return nil, fmt.Errorf("scan battle to capture: %w", err)
		}
		i, ok := index[battleID]
		if !ok {
			if len(out) >= limit {
				// The limit counts BATTLES, not kill rows; a battle already
				// being collected still takes its remaining creatures below.
				continue
			}
			out = append(out, BattleToCapture{
				BattleID:            battleID,
				SpeciesByCreatureID: map[string]string{},
			})
			i = len(out) - 1
			index[battleID] = i
		}
		if creatureID != "" && species != "" {
			out[i].SpeciesByCreatureID[creatureID] = species
		}
	}

	return out, rows.Err()
}
