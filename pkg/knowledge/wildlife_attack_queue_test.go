package knowledge

import (
	"context"
	"testing"
)

func recordKill(t *testing.T, kb *SQLiteKB, battleID, creatureID, species string, tick int64) {
	t.Helper()
	if err := kb.RecordWildlifeKill(context.Background(), WildlifeKill{
		CreatureID: creatureID,
		GameTick:   tick,
		Species:    species,
		SystemID:   "goldcrest",
		BattleID:   battleID,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestBattlesNeedingAttackCapture_DerivesTheQueue covers the whole rule: a
// battle we killed something in, whose damage log has not been read, is work;
// once its attack rows exist it disappears without anything marking it done.
func TestBattlesNeedingAttackCapture_DerivesTheQueue(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	recordKill(t, kb, "battle_a", "crt_1", "belt_grazer", 100)
	recordKill(t, kb, "battle_a", "crt_2", "glitterback_crab", 101)
	recordKill(t, kb, "battle_b", "crt_3", "slag_tortoise", 200)

	got, err := kb.BattlesNeedingAttackCapture(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("queue = %d battles, want 2: %+v", len(got), got)
	}
	// Newest first: battle_b's kill is at the later tick.
	if got[0].BattleID != "battle_b" {
		t.Errorf("queue order = %s first, want the freshest battle", got[0].BattleID)
	}
	var a BattleToCapture
	for _, b := range got {
		if b.BattleID == "battle_a" {
			a = b
		}
	}
	if len(a.SpeciesByCreatureID) != 2 {
		t.Errorf("battle_a species map = %v, want both creatures", a.SpeciesByCreatureID)
	}
	if a.SpeciesByCreatureID["crt_1"] != "belt_grazer" {
		t.Errorf("crt_1 = %q", a.SpeciesByCreatureID["crt_1"])
	}

	// Reading one battle's log removes it, with no queue table to update.
	if err := kb.UpsertWildlifeAttacks(ctx, []WildlifeAttack{{
		Species: "slag_tortoise", BattleID: "battle_b",
		WeaponName: "natural", DamageType: "kinetic", ShotKind: "beam",
		Shots: 1, Hits: 1, DamageTotal: 12, DamageMin: 12, DamageMax: 12,
	}}); err != nil {
		t.Fatal(err)
	}
	got, err = kb.BattlesNeedingAttackCapture(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].BattleID != "battle_a" {
		t.Fatalf("after capture queue = %+v, want only battle_a", got)
	}
}

// TestBattlesNeedingAttackCapture_SkipsKillsWithNoBattle: a creature that died
// without a battle push has no id, so its log can never be fetched. Queuing it
// would put an unfixable entry at the head of every future pass.
func TestBattlesNeedingAttackCapture_SkipsKillsWithNoBattle(t *testing.T) {
	kb := newTestKB(t)

	recordKill(t, kb, "", "crt_9", "belt_grazer", 100)

	got, err := kb.BattlesNeedingAttackCapture(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("queue = %+v, want empty — a kill with no battle id is unfetchable", got)
	}
}

// TestBattlesNeedingAttackCapture_LimitCountsBattlesNotKills guards the budget:
// the caller's limit bounds how many logs get fetched, and a battle with many
// kills must not consume the whole allowance by itself.
func TestBattlesNeedingAttackCapture_LimitCountsBattlesNotKills(t *testing.T) {
	kb := newTestKB(t)

	for i, c := range []string{"crt_1", "crt_2", "crt_3", "crt_4"} {
		recordKill(t, kb, "battle_big", c, "belt_grazer", int64(100+i))
	}
	recordKill(t, kb, "battle_small", "crt_5", "slag_tortoise", 50)

	got, err := kb.BattlesNeedingAttackCapture(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("queue = %d battles, want exactly the limit: %+v", len(got), got)
	}
	if got[0].BattleID != "battle_big" {
		t.Fatalf("kept %s, want the freshest battle", got[0].BattleID)
	}
	if len(got[0].SpeciesByCreatureID) != 4 {
		t.Errorf("species map = %v, want all 4 creatures of the battle that was kept",
			got[0].SpeciesByCreatureID)
	}
}

// TestSpeciesByDisplayName_ResolvesOnlyKnownSpecies pins the safety property:
// the index maps names the guide already holds, and nothing else.
func TestSpeciesByDisplayName_ResolvesOnlyKnownSpecies(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	if err := kb.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{
		{Species: "rainbow_leviathan", Name: "Rainbow Leviathan", Role: "predator"},
		{Species: "glitterback_crab", Name: "Glitterback Crab", Role: "grazer"},
	}); err != nil {
		t.Fatal(err)
	}

	idx, err := kb.SpeciesByDisplayName(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if idx[NormalizeCreatureName("Rainbow Leviathan")] != "rainbow_leviathan" {
		t.Errorf("index = %v", idx)
	}
	// Case and stray whitespace are display artefacts, not identity.
	if idx[NormalizeCreatureName("  RAINBOW LEVIATHAN ")] != "rainbow_leviathan" {
		t.Errorf("case-folded lookup missed: %v", idx)
	}
	if _, ok := idx["crusher-mantis"]; ok {
		t.Error("a species the guide has never seen resolved anyway")
	}
}
