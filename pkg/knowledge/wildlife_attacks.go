package knowledge

import (
	"context"
	"fmt"
	"time"
)

// WildlifeAttack is what one species was observed shooting with, in one battle.
//
// It is per battle rather than per species so that re-importing a battle log
// replaces its row instead of adding to it. The individual shots are never
// stored here — they live on the server in the battle log — so these counters
// are the observation itself, not a cached rollup of local data. Hit rate is
// therefore computed on read, from Hits over Shots.
type WildlifeAttack struct {
	Species     string
	BattleID    string
	WeaponName  string
	DamageType  string
	ShotKind    string
	Shots       int
	Hits        int
	DamageTotal float64
	DamageMin   float64
	DamageMax   float64
	ObservedUTC string
}

// WildlifeAttackProfile is a species' offence rolled up across every battle it
// has been seen in: what it shoots, how hard, and how often it connects.
//
// This is the half of a fight a resistance fit is chosen against. The other half
// is WildlifeSpecies.MaxShield/MaxHull, which says what damage type to bring.
type WildlifeAttackProfile struct {
	Species    string
	WeaponName string
	DamageType string
	ShotKind   string
	Battles    int
	Shots      int
	Hits       int
	// HitRate is Hits/Shots, 0 when nothing was fired.
	HitRate float64
	// DamagePerHit averages only the shots that connected, which is what a
	// survivability estimate needs: a miss does no damage and would drag the
	// average down toward a number no hit ever deals.
	DamagePerHit float64
	DamageMin    float64
	DamageMax    float64
}

// UpsertWildlifeAttacks records per-battle attack observations for creatures.
func (kb *SQLiteKB) UpsertWildlifeAttacks(ctx context.Context, rows []WildlifeAttack) error {
	if len(rows) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return kb.inTx(ctx, func(tx txer) error {
		for _, r := range rows {
			if r.Species == "" {
				continue
			}
			observed := r.ObservedUTC
			if observed == "" {
				observed = now
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO wildlife_attacks
					(species, battle_id, weapon_name, damage_type, shot_kind,
					 shots, hits, damage_total, damage_min, damage_max, observed_utc)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(species, battle_id, weapon_name, damage_type, shot_kind) DO UPDATE SET
					shots        = excluded.shots,
					hits         = excluded.hits,
					damage_total = excluded.damage_total,
					damage_min   = excluded.damage_min,
					damage_max   = excluded.damage_max,
					observed_utc = excluded.observed_utc
			`, r.Species, r.BattleID, r.WeaponName, r.DamageType, r.ShotKind,
				r.Shots, r.Hits, r.DamageTotal, r.DamageMin, r.DamageMax, observed); err != nil {
				return fmt.Errorf("upsert wildlife attack %s/%s: %w", r.Species, r.WeaponName, err)
			}
		}
		return nil
	})
}

// GetWildlifeAttackProfile rolls the per-battle rows up across battles for one
// species, or for every species when species is empty.
func (kb *SQLiteKB) GetWildlifeAttackProfile(ctx context.Context, species string) ([]WildlifeAttackProfile, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT species, weapon_name, damage_type, shot_kind,
		       COUNT(DISTINCT battle_id), SUM(shots), SUM(hits), SUM(damage_total),
		       MIN(damage_min), MAX(damage_max)
		FROM wildlife_attacks
		WHERE (? = '' OR species = ?)
		GROUP BY species, weapon_name, damage_type, shot_kind
		ORDER BY species, SUM(shots) DESC, weapon_name
	`, species, species)
	if err != nil {
		return nil, fmt.Errorf("query wildlife attack profile: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []WildlifeAttackProfile
	for rows.Next() {
		var p WildlifeAttackProfile
		if err := rows.Scan(&p.Species, &p.WeaponName, &p.DamageType, &p.ShotKind,
			&p.Battles, &p.Shots, &p.Hits, &p.DamagePerHit, &p.DamageMin, &p.DamageMax); err != nil {
			return nil, fmt.Errorf("scan wildlife attack profile: %w", err)
		}
		total := p.DamagePerHit // SUM(damage_total) landed here; convert to a mean below
		if p.Shots > 0 {
			p.HitRate = float64(p.Hits) / float64(p.Shots)
		}
		if p.Hits > 0 {
			p.DamagePerHit = total / float64(p.Hits)
		} else {
			p.DamagePerHit = 0
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
