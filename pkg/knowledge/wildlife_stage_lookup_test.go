package knowledge

import (
	"context"
	"testing"
)

// TestSpeciesByDisplayName_ResolvesAcrossGrowthStages pins the interaction
// between name canonicalisation and species resolution.
//
// This is a REGRESSION test for a real silent failure. Canonicalising
// wildlife_species.name (so a stage-qualified sighting stops overwriting the
// mature name) moved the display-name index key to "molt leviathan", while a
// battle participant is still called "Juvenile Molt Leviathan". Resolution is
// an exact map hit, so it missed, ResolveCreatureSpecies named no creature, and
// capture_wildlife_attacks filed ZERO rows for a battle whose damage was right
// there in the log -- reporting success the whole way.
//
// Both the index and the lookup go through NormalizeCreatureName, so making it
// stage-insensitive fixes both ends at once, in either direction: a juvenile
// participant resolves against a guide that knows only the adult, and an adult
// participant against a guide that first met a juvenile.
func TestSpeciesByDisplayName_ResolvesAcrossGrowthStages(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	if err := kb.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{
		{Species: "molt_leviathan", Name: "Juvenile Molt Leviathan", Role: "predator"},
	}); err != nil {
		t.Fatal(err)
	}

	// Precondition: the guide stores the CANONICAL name, not the sighted one.
	// Without this the test would pass for the wrong reason the moment
	// canonicalisation were reverted.
	var stored string
	if err := kb.db.QueryRowContext(ctx,
		`SELECT name FROM wildlife_species WHERE species = 'molt_leviathan'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "Molt Leviathan" {
		t.Fatalf("precondition: stored name = %q, want canonical %q", stored, "Molt Leviathan")
	}

	idx, err := kb.SpeciesByDisplayName(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"Juvenile Molt Leviathan", // as a battle participant reports it
		"Molt Leviathan",          // as the guide now holds it
		"Young Molt Leviathan",    // a later stage of the same animal
	} {
		if got := idx[NormalizeCreatureName(name)]; got != "molt_leviathan" {
			t.Errorf("resolve %q = %q, want molt_leviathan", name, got)
		}
	}
}
