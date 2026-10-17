package knowledge

import (
	"context"
	"testing"
)

// TestRecordWildlifeObservations_DedupesTheDoubleLook pins the primary key.
//
// The explore loop looks twice per POI on purpose, to beat the arrival race, so
// raw sightings double every creature. Both looks land in the SAME game tick,
// so (creature_id, game_tick) collapses them while leaving a genuine
// re-sighting at a later tick as its own row. Getting this wrong reports a
// population at twice its real size.
func TestRecordWildlifeObservations_DedupesTheDoubleLook(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	obs := []WildlifeObservation{
		{CreatureID: "lev_a", GameTick: 100, Species: "molt_leviathan", Name: "Juvenile Molt Leviathan", Hull: 569, MaxHull: 569},
		{CreatureID: "lev_a", GameTick: 100, Species: "molt_leviathan", Name: "Juvenile Molt Leviathan", Hull: 569, MaxHull: 569},
		{CreatureID: "lev_a", GameTick: 232, Species: "molt_leviathan", Name: "Juvenile Molt Leviathan", Hull: 364, MaxHull: 569, InCombat: true},
	}
	if err := kb.RecordWildlifeObservations(ctx, obs); err != nil {
		t.Fatal(err)
	}

	var rows int
	if err := kb.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM wildlife_observations WHERE creature_id = 'lev_a'`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Errorf("rows = %d, want 2 (double-look collapsed, later tick kept)", rows)
	}

	// The mid-combat reading must survive intact: it is the only evidence we
	// have that damage does not persist out of combat.
	var hull, inCombat int
	if err := kb.db.QueryRowContext(ctx,
		`SELECT hull, in_combat FROM wildlife_observations WHERE creature_id='lev_a' AND game_tick=232`).
		Scan(&hull, &inCombat); err != nil {
		t.Fatal(err)
	}
	if hull != 364 || inCombat != 1 {
		t.Errorf("tick 232: hull=%d in_combat=%d, want 364/1", hull, inCombat)
	}
}

// TestRecordWildlifeObservations_DerivesStage checks the stage column is filled
// from the display name at write time, including "" for a mature animal.
func TestRecordWildlifeObservations_DerivesStage(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	if err := kb.RecordWildlifeObservations(ctx, []WildlifeObservation{
		{CreatureID: "lev_h", GameTick: 1, Species: "hoarfrost_leviathan", Name: "Hoarfrost Leviathan Hatchling", MaxHull: 312},
		{CreatureID: "lev_j", GameTick: 1, Species: "hoarfrost_leviathan", Name: "Juvenile Hoarfrost Leviathan", MaxHull: 743},
		{CreatureID: "crt_c", GameTick: 1, Species: "cloudwhale", Name: "Cloudwhale", MaxHull: 3373},
	}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ id, wantStage string }{
		{"lev_h", "hatchling"},
		{"lev_j", "juvenile"},
		{"crt_c", ""}, // mature; "" is the adult bucket, not unknown
	} {
		var stage string
		if err := kb.db.QueryRowContext(ctx,
			`SELECT stage FROM wildlife_observations WHERE creature_id = ?`, tc.id).Scan(&stage); err != nil {
			t.Fatal(err)
		}
		if stage != tc.wantStage {
			t.Errorf("%s: stage = %q, want %q", tc.id, stage, tc.wantStage)
		}
	}
}

// TestWildlifeStageHistogram is the whole point of the table: a per-stage hull
// distribution counting INDIVIDUALS, not rows.
//
// One animal observed at two stages must count in BOTH -- it genuinely was a
// hatchling and later a juvenile, and a lifecycle histogram that drops the
// earlier reading drains its lower bands as animals mature.
func TestWildlifeStageHistogram(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	if err := kb.RecordWildlifeObservations(ctx, []WildlifeObservation{
		// One animal, growing: hatchling at tick 1, juvenile at tick 900.
		{CreatureID: "lev_grow", GameTick: 1, Species: "hoarfrost_leviathan", Name: "Hoarfrost Leviathan Hatchling", MaxHull: 312},
		{CreatureID: "lev_grow", GameTick: 900, Species: "hoarfrost_leviathan", Name: "Juvenile Hoarfrost Leviathan", MaxHull: 700},
		// Two more juveniles, each seen twice in the same tick (double-look).
		{CreatureID: "lev_j1", GameTick: 900, Species: "hoarfrost_leviathan", Name: "Juvenile Hoarfrost Leviathan", MaxHull: 743},
		{CreatureID: "lev_j1", GameTick: 900, Species: "hoarfrost_leviathan", Name: "Juvenile Hoarfrost Leviathan", MaxHull: 743},
		{CreatureID: "lev_j2", GameTick: 900, Species: "hoarfrost_leviathan", Name: "Juvenile Hoarfrost Leviathan", MaxHull: 769},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := kb.WildlifeStageHistogram(ctx)
	if err != nil {
		t.Fatal(err)
	}

	byStage := map[string]WildlifeStageBucket{}
	for _, b := range got {
		if b.Species == "hoarfrost_leviathan" {
			byStage[b.Stage] = b
		}
	}

	h, ok := byStage["hatchling"]
	if !ok {
		t.Fatal("hatchling bucket missing: a matured animal must still count at the stage it was seen")
	}
	if h.Individuals != 1 || h.HullMin != 312 || h.HullMax != 312 {
		t.Errorf("hatchling = n%d %d-%d, want n1 312-312", h.Individuals, h.HullMin, h.HullMax)
	}

	j := byStage["juvenile"]
	if j.Individuals != 3 {
		t.Errorf("juvenile individuals = %d, want 3 (double-look collapsed)", j.Individuals)
	}
	if j.HullMin != 700 || j.HullMax != 769 {
		t.Errorf("juvenile hull = %d-%d, want 700-769", j.HullMin, j.HullMax)
	}
}

// TestWildlifeGrowthObserved finds individuals whose hull has actually changed
// -- the question the collector exists to answer, and which as of 2026-10-16 no
// creature in 1,971 tracked individuals had yet answered yes to.
func TestWildlifeGrowthObserved(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	if err := kb.RecordWildlifeObservations(ctx, []WildlifeObservation{
		{CreatureID: "lev_grow", GameTick: 1, Species: "hoarfrost_leviathan", Name: "Hoarfrost Leviathan Hatchling", MaxHull: 312},
		{CreatureID: "lev_grow", GameTick: 900, Species: "hoarfrost_leviathan", Name: "Juvenile Hoarfrost Leviathan", MaxHull: 700},
		{CreatureID: "lev_flat", GameTick: 1, Species: "molt_leviathan", Name: "Juvenile Molt Leviathan", MaxHull: 569},
		{CreatureID: "lev_flat", GameTick: 900, Species: "molt_leviathan", Name: "Juvenile Molt Leviathan", MaxHull: 569},
	}); err != nil {
		t.Fatal(err)
	}

	grew, err := kb.WildlifeGrowthObserved(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(grew) != 1 {
		t.Fatalf("got %d growing individuals, want 1: %+v", len(grew), grew)
	}
	g := grew[0]
	if g.CreatureID != "lev_grow" || g.FirstHull != 312 || g.LastHull != 700 {
		t.Errorf("got %+v, want lev_grow 312 -> 700", g)
	}
	if g.FirstStage != "hatchling" || g.LastStage != "juvenile" {
		t.Errorf("stages = %q -> %q, want hatchling -> juvenile", g.FirstStage, g.LastStage)
	}
	if g.TicksElapsed != 899 {
		t.Errorf("ticks = %d, want 899", g.TicksElapsed)
	}
}
