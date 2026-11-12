package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// HostileKindRepeater is the hostile kind for v0.687 Frontier Repeaters.
const HostileKindRepeater = "repeater"

// HostileSighting is one hostile seen at a POI at a moment, with its type
// constants carried along so a sighting alone can populate hostile_types.
type HostileSighting struct {
	HostileID   string
	HostileType string // e.g. "repeater_monolith"
	Kind        string // e.g. "repeater"
	Class       string // e.g. "monolith"
	Name        string
	Hull        int
	MaxHull     int
	Shield      int
	MaxShield   int
	InBattle    bool
	SystemID    string
	POIID       string
	GameTick    int64
	AgentID     string
}

// HostileRecorder stores hostile sightings. SQLiteKB implements it.
type HostileRecorder interface {
	RecordHostileSightings(ctx context.Context, rows []HostileSighting) error
}

// RepeaterHostileType is the hostile_type key for a repeater ship_class
// ("pane" -> "repeater_pane"); empty when the class is missing, so an
// unclassified repeater is skipped rather than filed under a phantom type.
func RepeaterHostileType(shipClass string) string {
	c := strings.ToLower(strings.TrimSpace(shipClass))
	if c == "" {
		return ""
	}
	return HostileKindRepeater + "_" + c
}

// HostilesFromNearby turns get_nearby's repeaters into sightings.
func HostilesFromNearby(repeaters []serverapi.RepeaterInfo, systemID, poiID, agentID string, tick int64) []HostileSighting {
	out := make([]HostileSighting, 0, len(repeaters))
	for _, r := range repeaters {
		ht := RepeaterHostileType(r.ShipClass)
		if ht == "" || r.RepeaterID == "" {
			continue
		}
		out = append(out, HostileSighting{
			HostileID: r.RepeaterID, HostileType: ht,
			Kind: HostileKindRepeater, Class: strings.ToLower(r.ShipClass), Name: r.Name,
			Hull: r.Hull, MaxHull: r.MaxHull, Shield: r.Shield, MaxShield: r.MaxShield,
			InBattle: r.InBattle, SystemID: systemID, POIID: poiID, GameTick: tick, AgentID: agentID,
		})
	}
	return out
}

// CaptureHostilesNearby records the repeaters in a get_nearby reply. A KB
// that cannot store hostiles (nil, in-memory, mock) is not an error.
func CaptureHostilesNearby(ctx context.Context, kb Base, repeaters []serverapi.RepeaterInfo, systemID, poiID, agentID string, tick int64) (int, error) {
	rec, ok := kb.(HostileRecorder)
	if kb == nil || !ok {
		return 0, nil
	}
	rows := HostilesFromNearby(repeaters, systemID, poiID, agentID, tick)
	if len(rows) == 0 {
		return 0, nil
	}
	if err := rec.RecordHostileSightings(ctx, rows); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// RecordHostileSightings upserts each row's hostile type (max hull/shield
// only grow; name/kind/class keep the latest non-empty value) and appends a
// sighting row.
func (kb *SQLiteKB) RecordHostileSightings(ctx context.Context, rows []HostileSighting) error {
	if len(rows) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("record hostiles: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, r := range rows {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO hostile_types (hostile_type, kind, class, name, max_hull, max_shield, first_seen_utc, last_seen_utc)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(hostile_type) DO UPDATE SET
				kind = CASE WHEN excluded.kind <> '' THEN excluded.kind ELSE kind END,
				class = CASE WHEN excluded.class <> '' THEN excluded.class ELSE class END,
				name = CASE WHEN excluded.name <> '' THEN excluded.name ELSE name END,
				max_hull = MAX(max_hull, excluded.max_hull),
				max_shield = MAX(max_shield, excluded.max_shield),
				last_seen_utc = excluded.last_seen_utc`,
			r.HostileType, r.Kind, r.Class, r.Name, r.MaxHull, r.MaxShield, now, now); err != nil {
			return fmt.Errorf("record hostile type %s: %w", r.HostileType, err)
		}
		inBattle := 0
		if r.InBattle {
			inBattle = 1
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO hostile_sightings (hostile_id, hostile_type, system_id, poi_id, hull, shield, in_battle, game_tick, observed_utc, agent_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.HostileID, r.HostileType, r.SystemID, r.POIID, r.Hull, r.Shield, inBattle, r.GameTick, now, r.AgentID); err != nil {
			return fmt.Errorf("record hostile sighting %s: %w", r.HostileID, err)
		}
	}
	return tx.Commit()
}

// HostileAttack is what one hostile type was observed shooting with in one
// battle; the hostile_attacks twin of WildlifeAttack.
type HostileAttack struct {
	HostileType string
	BattleID    string
	WeaponName  string
	DamageType  string
	ShotKind    string
	Shots       int
	Hits        int
	DamageTotal float64
	DamageMin   float64
	DamageMax   float64
}

// HostileAttackRecorder stores hostile offence. SQLiteKB implements it.
type HostileAttackRecorder interface {
	UpsertHostileAttacks(ctx context.Context, rows []HostileAttack) error
}

// UpsertHostileAttacks records per-battle attack rows; re-importing a battle
// replaces its rows.
func (kb *SQLiteKB) UpsertHostileAttacks(ctx context.Context, rows []HostileAttack) error {
	if len(rows) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return kb.inTx(ctx, func(tx txer) error {
		for _, r := range rows {
			if r.HostileType == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO hostile_attacks
					(hostile_type, battle_id, weapon_name, damage_type, shot_kind,
					 shots, hits, damage_total, damage_min, damage_max, observed_utc)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(hostile_type, battle_id, weapon_name, damage_type, shot_kind) DO UPDATE SET
					shots = excluded.shots, hits = excluded.hits,
					damage_total = excluded.damage_total, damage_min = excluded.damage_min,
					damage_max = excluded.damage_max, observed_utc = excluded.observed_utc
			`, r.HostileType, r.BattleID, r.WeaponName, r.DamageType, r.ShotKind,
				r.Shots, r.Hits, r.DamageTotal, r.DamageMin, r.DamageMax, now); err != nil {
				return fmt.Errorf("upsert hostile attack %s/%s: %w", r.HostileType, r.WeaponName, err)
			}
		}
		return nil
	})
}
