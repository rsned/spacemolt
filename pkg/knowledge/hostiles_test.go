package knowledge

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// The three repeater classes seen live (2026-11-12) file as three hostile
// types with their hull/shield constants; an unclassified one is skipped;
// nothing lands in the wildlife tables.
func TestCaptureHostilesNearbyRepeaters(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := context.Background()
	reps := []serverapi.RepeaterInfo{
		{RepeaterID: "repeater_a", Name: "Repeater Monolith", ShipClass: "monolith", Hull: 6400, MaxHull: 6400, Shield: 9600, MaxShield: 9600},
		{RepeaterID: "repeater_6a04720605c86ab455ba7ff7eab050ac", Name: "Repeater Pane", ShipClass: "pane", Hull: 400, MaxHull: 400, Shield: 600, MaxShield: 600},
		{RepeaterID: "repeater_ae9cae2eb40b5b9ac5d4baeb4bcc22ea", Name: "Repeater Spire", ShipClass: "spire", Hull: 1600, MaxHull: 1600, MaxShield: 2400, InBattle: true},
		{RepeaterID: "repeater_d", Name: "Repeater ?"},
	}
	n, err := CaptureHostilesNearby(ctx, kb, reps, "gloaming", "the_threshold", "craftsman-1", 2388274)
	if err != nil || n != 3 {
		t.Fatalf("captured %d, err %v; want 3", n, err)
	}
	// A second look adds sightings, not types.
	if _, err = CaptureHostilesNearby(ctx, kb, reps[:1], "gloaming", "the_threshold", "craftsman-1", 2388300); err != nil {
		t.Fatal(err)
	}

	want := map[string][2]int{"repeater_monolith": {6400, 9600}, "repeater_pane": {400, 600}, "repeater_spire": {1600, 2400}}
	rows, err := kb.db.Query(`SELECT hostile_type, kind, max_hull, max_shield FROM hostile_types`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	got := 0
	for rows.Next() {
		var ht, kind string
		var h, s int
		if err = rows.Scan(&ht, &kind, &h, &s); err != nil {
			t.Fatal(err)
		}
		got++
		if w := want[ht]; w != [2]int{h, s} || kind != HostileKindRepeater {
			t.Errorf("%s: kind %q hull/shield %d/%d, want %v", ht, kind, h, s, w)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("hostile_types rows = %d, want 3", got)
	}

	var sightings, inBattle, wildlife int
	if err = kb.db.QueryRow(`SELECT COUNT(*), SUM(in_battle) FROM hostile_sightings`).Scan(&sightings, &inBattle); err != nil {
		t.Fatal(err)
	}
	if err = kb.db.QueryRow(`SELECT COUNT(*) FROM wildlife_species WHERE species LIKE 'repeater%'`).Scan(&wildlife); err != nil {
		t.Fatal(err)
	}
	if sightings != 4 || inBattle != 1 || wildlife != 0 {
		t.Errorf("sightings %d in_battle %d wildlife %d; want 4, 1, 0", sightings, inBattle, wildlife)
	}
}

func TestMigration72_IsReplayable(t *testing.T) {
	kb := newTestSQLiteKB(t)
	for i := range 2 {
		if _, err := kb.db.Exec("DELETE FROM schema_migrations WHERE version >= 72"); err != nil {
			t.Fatal(err)
		}
		if err := runMigrations(kb.db); err != nil {
			t.Fatalf("replay %d: %v", i+1, err)
		}
	}
}

func TestUpsertHostileAttacksReplacesBattleRow(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := context.Background()
	row := HostileAttack{HostileType: "repeater_monolith", BattleID: "b1", WeaponName: "w", DamageType: "em", Shots: 3, Hits: 2, DamageTotal: 305}
	for range 2 {
		if err := kb.UpsertHostileAttacks(ctx, []HostileAttack{row}); err != nil {
			t.Fatal(err)
		}
	}
	var n, shots int
	if err := kb.db.QueryRow(`SELECT COUNT(*), SUM(shots) FROM hostile_attacks`).Scan(&n, &shots); err != nil {
		t.Fatal(err)
	}
	if n != 1 || shots != 3 {
		t.Errorf("rows %d shots %d, want 1, 3", n, shots)
	}
}

// A battle spec fills hostile_types without any sighting, and a re-read
// replaces the individual's loadout rather than adding to it.
func TestRecordHostileSpecs(t *testing.T) {
	kb := newTestSQLiteKB(t)
	ctx := context.Background()
	spec := HostileSpec{
		HostileID: "repeater_m1", HostileType: "repeater_monolith", Kind: HostileKindRepeater, Class: "monolith",
		Name: "Repeater Monolith", MaxHull: 6400, MaxShield: 9600, BattleID: "b1",
		Modules: []HostileModule{{Name: "Heavy Pulse Laser", Category: "weapon", Count: 8}},
	}
	for range 2 {
		if err := kb.RecordHostileSpecs(ctx, []HostileSpec{spec}); err != nil {
			t.Fatal(err)
		}
	}
	var hull, shield int
	if err := kb.db.QueryRow(`SELECT max_hull, max_shield FROM hostile_types WHERE hostile_type='repeater_monolith'`).Scan(&hull, &shield); err != nil {
		t.Fatal(err)
	}
	var rows, count int
	if err := kb.db.QueryRow(`SELECT COUNT(*), SUM(count) FROM hostile_loadouts WHERE hostile_id='repeater_m1'`).Scan(&rows, &count); err != nil {
		t.Fatal(err)
	}
	if hull != 6400 || shield != 9600 || rows != 1 || count != 8 {
		t.Errorf("hull %d shield %d loadout rows %d count %d; want 6400 9600 1 8", hull, shield, rows, count)
	}
}
