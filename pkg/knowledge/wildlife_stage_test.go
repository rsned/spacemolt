package knowledge

import "testing"

// Creature names carry a growth stage, which is BOTH a protection marker and
// the reason wildlife_species.name was flapping: the row holds one name per
// species, so "Young Cloudwhale" overwrote "Cloudwhale" and back again
// depending on which individual was seen last.
//
// Observed live: "Young Cloudwhale" (grazer, 1287-1767 hull) and "Juvenile
// Hoarfrost Leviathan" (predator, 669-769). The operator supplied the full
// vocabulary: hatchling, young, calf, juvenile.
func TestCanonicalSpeciesNameStripsTheGrowthStage(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Young Cloudwhale", "Cloudwhale"},
		{"Juvenile Hoarfrost Leviathan", "Hoarfrost Leviathan"},
		{"Hatchling Veil-Ray", "Veil-Ray"},
		{"JUVENILE Rime-Grazer", "Rime-Grazer"}, // case must not matter
		{"juvenile rime-grazer", "rime-grazer"}, // nor lowercase input
		{"Cloudwhale Calf", "Cloudwhale"},       // stage can trail the name
		{"Cloudwhale", "Cloudwhale"},            // mature name is untouched
		{"Pilot-Whale", "Pilot-Whale"},          // a name that merely CONTAINS no stage
		{"", ""},
	} {
		if got := CanonicalSpeciesName(tc.in); got != tc.want {
			t.Errorf("CanonicalSpeciesName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A stage word must only match as a whole word. "Younghull" and "Calfax" are
// not juveniles, and stripping a substring would mangle real species names.
func TestCanonicalSpeciesNameMatchesWholeWordsOnly(t *testing.T) {
	for _, in := range []string{"Youngling Thing", "Calfax Drifter", "Juvenilia"} {
		if got := CanonicalSpeciesName(in); got != in {
			t.Errorf("CanonicalSpeciesName(%q) = %q, want it unchanged", in, got)
		}
	}
}

// The same vocabulary flags a creature as a protected juvenile, which is what
// it is FOR: these must not be hunted.
func TestIsProtectedJuvenile(t *testing.T) {
	for _, in := range []string{
		"Young Cloudwhale", "Juvenile Hoarfrost Leviathan",
		"Hatchling Veil-Ray", "Cloudwhale Calf", "juvenile rime-grazer",
	} {
		if !IsProtectedJuvenile(in) {
			t.Errorf("IsProtectedJuvenile(%q) = false, want true", in)
		}
	}
	for _, in := range []string{
		"Cloudwhale", "Hoarfrost Leviathan", "Belt-Grazer",
		"Crusher-Mantis", "Youngling Thing", "", "Rainbow Leviathan",
	} {
		if IsProtectedJuvenile(in) {
			t.Errorf("IsProtectedJuvenile(%q) = true, want false", in)
		}
	}
}

// The field guide keeps ONE row per species, so a stage-qualified name has to
// be canonicalised on the way in. Live on 2026-10-14 the cloudwhale row read
// "Young Cloudwhale" -- the only stage-qualified name among 49 species -- and
// would have flipped to "Cloudwhale" and back as different individuals were
// sighted, because name is last-write-wins.
func TestUpsertWildlifeSpeciesCanonicalisesStageNames(t *testing.T) {
	kb := newStageTestKB(t)
	ctx := t.Context()

	// Seen as a juvenile first, exactly as the live data arrived.
	if err := kb.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{
		{Species: "cloudwhale", Name: "Young Cloudwhale", Role: "grazer", MaxHull: 1433},
	}); err != nil {
		t.Fatalf("upsert juvenile: %v", err)
	}
	if got := speciesName(t, kb, "cloudwhale"); got != "Cloudwhale" {
		t.Errorf("name = %q, want the canonical \"Cloudwhale\"", got)
	}

	// A mature sighting must not change the name, and must still ratchet hull.
	if err := kb.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{
		{Species: "cloudwhale", Name: "Cloudwhale", Role: "grazer", MaxHull: 2400},
	}); err != nil {
		t.Fatalf("upsert mature: %v", err)
	}
	if got := speciesName(t, kb, "cloudwhale"); got != "Cloudwhale" {
		t.Errorf("name = %q after a mature sighting, want \"Cloudwhale\"", got)
	}

	// And a juvenile seen AFTER the adult must not drag the name back.
	if err := kb.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{
		{Species: "cloudwhale", Name: "Young Cloudwhale", Role: "grazer", MaxHull: 1300},
	}); err != nil {
		t.Fatalf("upsert juvenile again: %v", err)
	}
	if got := speciesName(t, kb, "cloudwhale"); got != "Cloudwhale" {
		t.Errorf("name = %q; a later juvenile sighting re-introduced the stage", got)
	}
}

func newStageTestKB(t *testing.T) *SQLiteKB {
	t.Helper()
	kb, err := NewSQLiteKB(Config{DBPath: t.TempDir() + "/kb.db"})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })

	return kb
}

func speciesName(t *testing.T, kb *SQLiteKB, species string) string {
	t.Helper()
	var name string
	if err := kb.db.QueryRowContext(t.Context(),
		`SELECT name FROM wildlife_species WHERE species = ?`, species).Scan(&name); err != nil {
		t.Fatalf("read name: %v", err)
	}

	return name
}
