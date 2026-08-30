package knowledge

import (
	"context"
	"testing"
	"time"
)

// v0.571.0: a creature scan carries the species' lore in `description`, and
// scanning is the only way to read it — "the entry is not stored for you".
const patinaLore = "Green-crusted and unhurried, it combs copper dust from the rubble with its filter-plates spread wide, laying the metal it strains down over its own shell until the plating weathers to verdigris."

func TestParseCreatureScanCarriesDescription(t *testing.T) {
	got := ParseCreatureScan("Patina-Grazer [grazer — harmless prey, ranchable stock]",
		[]string{"species", "role", "hull", "ranchable", "description"}, 60, patinaLore)
	if got.Description != patinaLore {
		t.Errorf("Description = %q, want the lore", got.Description)
	}
	if got.Name != "Patina-Grazer" || !got.Ranchable || got.Hull != 60 {
		t.Errorf("parse regressed: %+v", got)
	}
}

func TestCaptureWildlifeScanPersistsLoreAndKeepsItLater(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)

	s := ParseCreatureScan("Patina-Grazer [grazer — harmless prey, ranchable stock]",
		[]string{"species", "role", "hull", "ranchable", "description"}, 60, patinaLore)
	if err := CaptureWildlifeScan(ctx, kb, "patina_grazer", s, now); err != nil {
		t.Fatal(err)
	}
	var desc string
	if err := kb.db.QueryRow(`SELECT description FROM wildlife_species WHERE species='patina_grazer'`).Scan(&desc); err != nil {
		t.Fatalf("read description: %v", err)
	}
	if desc != patinaLore {
		t.Errorf("stored description = %q", desc)
	}

	// An older client, or a scan whose revealed_info omitted description, must
	// not erase the lore we already recorded.
	again := ParseCreatureScan("Patina-Grazer [grazer — harmless prey]", []string{"species", "role", "hull"}, 60, "")
	if err := CaptureWildlifeScan(ctx, kb, "patina_grazer", again, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := kb.db.QueryRow(`SELECT description FROM wildlife_species WHERE species='patina_grazer'`).Scan(&desc); err != nil {
		t.Fatal(err)
	}
	if desc != patinaLore {
		t.Errorf("description erased by a later lore-less scan: %q", desc)
	}
}
