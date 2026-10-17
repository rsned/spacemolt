package knowledge

import (
	"context"
	"testing"
	"time"
)

// A creature's DESCRIPTION varies by growth stage, exactly as its name does --
// "A pale hatchling that trails a thin mist of frost..." for a Hoarfrost
// Leviathan Hatchling against whatever an adult reads. wildlife_species holds
// one row per species and the upsert is last-write-wins on a non-empty
// description, so a hatchling scan OVERWRITES the adult's lore and flips back
// on the next scan of a different stage.
//
// Observed live 2026-10-17, after name canonicalisation made it worse rather
// than better: every staged species carried a JUVENILE description under the
// canonical ADULT name.
func TestCaptureWildlifeScan_StoresLorePerStage(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	now := time.Now()

	hatchling := "A pale hatchling that trails a thin mist of frost. It is too small to " +
		"take a frost-lurker alone and lives on scraps, and it bolts from any ship."
	adult := "A full-grown Hoarfrost Leviathan, wreathed in its own weather."

	if err := CaptureWildlifeScan(ctx, kb, "hoarfrost_leviathan", CreatureScan{
		Name: "Hoarfrost Leviathan Hatchling", Traits: "does not attack ships first",
		Danger: "does not attack ships first", Description: hatchling, Hull: 312,
	}, now); err != nil {
		t.Fatal(err)
	}
	if err := CaptureWildlifeScan(ctx, kb, "hoarfrost_leviathan", CreatureScan{
		Name: "Hoarfrost Leviathan", Traits: "does not attack ships first",
		Danger: "does not attack ships first", Description: adult, Hull: 1750,
	}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	lore, err := kb.WildlifeStageLore(ctx, "hoarfrost_leviathan")
	if err != nil {
		t.Fatal(err)
	}
	byStage := map[string]string{}
	for _, l := range lore {
		byStage[l.Stage] = l.Description
	}
	if got := byStage["hatchling"]; got != hatchling {
		t.Errorf("hatchling lore = %q", got)
	}
	if got := byStage[""]; got != adult {
		t.Errorf("mature lore = %q, want the adult text under stage \"\"", got)
	}

	// And the species row must settle on the MATURE lore rather than whichever
	// stage was scanned last -- the row is named for the adult.
	var speciesDesc string
	if err := kb.db.QueryRowContext(ctx,
		`SELECT description FROM wildlife_species WHERE species='hoarfrost_leviathan'`).Scan(&speciesDesc); err != nil {
		t.Fatal(err)
	}
	if speciesDesc != adult {
		t.Errorf("wildlife_species.description = %q, want the adult text", speciesDesc)
	}
}

// A juvenile scan must NOT clobber an adult description already on file, even
// when it arrives afterwards. This is the regression the flap produced.
func TestCaptureWildlifeScan_JuvenileDoesNotClobberAdultLore(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	now := time.Now()

	adult := "A full-grown Molt Leviathan, shell plated like a foundry."
	if err := CaptureWildlifeScan(ctx, kb, "molt_leviathan", CreatureScan{
		Name: "Molt Leviathan", Traits: "predator", Danger: "attacks", Description: adult,
	}, now); err != nil {
		t.Fatal(err)
	}
	if err := CaptureWildlifeScan(ctx, kb, "molt_leviathan", CreatureScan{
		Name: "Juvenile Molt Leviathan", Traits: "predator", Danger: "attacks",
		Description: "A young Molt Leviathan with its first hard shell.",
	}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	var got string
	if err := kb.db.QueryRowContext(ctx,
		`SELECT description FROM wildlife_species WHERE species='molt_leviathan'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != adult {
		t.Errorf("adult lore was clobbered by a juvenile scan: %q", got)
	}
}

// A species known ONLY from a juvenile still records that stage's lore, so the
// text is never lost even though the species row has no adult description yet.
func TestCaptureWildlifeScan_JuvenileOnlySpeciesKeepsItsLore(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	juv := "A young Cloudwhale that has started to strain neon gas on its own."
	if err := CaptureWildlifeScan(ctx, kb, "cloudwhale", CreatureScan{
		Name: "Young Cloudwhale", Traits: "grazer", Danger: "does not attack ships first",
		Description: juv,
	}, time.Now()); err != nil {
		t.Fatal(err)
	}

	lore, err := kb.WildlifeStageLore(ctx, "cloudwhale")
	if err != nil {
		t.Fatal(err)
	}
	if len(lore) != 1 || lore[0].Stage != "young" || lore[0].Description != juv {
		t.Fatalf("got %+v, want one 'young' entry with the juvenile text", lore)
	}
	if lore[0].DisplayName != "Young Cloudwhale" {
		t.Errorf("display name = %q, want the stage-qualified name as sighted", lore[0].DisplayName)
	}
}
