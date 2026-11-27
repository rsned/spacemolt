package knowledge

import (
	"context"
	"testing"
)

func TestWildlifeThreatFigures(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	if err := kb.UpsertWildlifeAttacks(ctx, []WildlifeAttack{
		{Species: "cloudwhale", BattleID: "b1", WeaponName: "Cloudwhale (natural)", Shots: 8, Hits: 6, DamageTotal: 480},
		{Species: "pilot_whale", BattleID: "b2", WeaponName: "bite", Shots: 5, Hits: 4, DamageTotal: 48},
		{Species: "pilot_whale", BattleID: "b3", WeaponName: "bite", Shots: 5, Hits: 0, DamageTotal: 0},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := kb.WildlifeDamagePerShot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got["cloudwhale"] != (WildlifeDamage{PerShot: 60, Shots: 8}) || got["pilot_whale"] != (WildlifeDamage{PerShot: 4.8, Shots: 10}) {
		t.Errorf("damage per shot = %+v", got)
	}

	for i, k := range []WildlifeKill{
		{CreatureID: "c1", Species: "pilot_whale", MaxHull: 220, DurationTicks: 10, AgentID: "pirate-5", KilledUTC: "2026-11-27T01:00:00Z"},
		{CreatureID: "c2", Species: "chlorophage", MaxHull: 55, DurationTicks: 5, AgentID: "pirate-5", KilledUTC: "2026-11-27T02:00:00Z"},
		{CreatureID: "c3", Species: "chlorophage", MaxHull: 55, DurationTicks: 1, AgentID: "pirate-9", KilledUTC: "2026-11-27T03:00:00Z"},
	} {
		k.GameTick = int64(100 + i)
		if err = kb.RecordWildlifeKill(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	per, kills, err := kb.HunterDamagePerTick(ctx, "pirate-5", 50)
	if err != nil || kills != 2 || per != 275.0/15 {
		t.Errorf("pirate-5: %v over %d kill(s), err %v; want %v over 2", per, kills, err, 275.0/15)
	}
	// Only the latest kill.
	if per, kills, _ := kb.HunterDamagePerTick(ctx, "pirate-5", 1); kills != 1 || per != 11 {
		t.Errorf("latest kill only: %v over %d", per, kills)
	}
	if per, kills, _ := kb.HunterDamagePerTick(ctx, "nobody", 50); kills != 0 || per != 0 {
		t.Errorf("no kills: %v over %d", per, kills)
	}
}
