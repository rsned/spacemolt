package knowledge

import (
	"context"
	"testing"
	"time"
)

// TestParseCreatureScan uses the six verbatim replies measured on 2026-08-17.
// The species/role/danger fields do not exist on the wire — they are packed into
// the display name — so this parse is the only thing standing between the reply
// and an empty danger column.
func TestParseCreatureScan(t *testing.T) {
	for _, tc := range []struct {
		username  string
		revealed  []string
		hull      int
		wantName  string
		wantRole  string
		wantDang  string
		wantTrait string
		wantRanch bool
	}{
		{"Slag-Tortoise [grazer — harmless prey]", []string{"species", "role", "hull"}, 90,
			"Slag-Tortoise", "grazer", "harmless prey", "harmless prey", false},
		{"Cauldronback [grazer — harmless prey]", []string{"species", "role", "hull"}, 120,
			"Cauldronback", "grazer", "harmless prey", "harmless prey", false},
		{"Inkwyrm [grazer — harmless prey]", []string{"species", "role", "hull"}, 65,
			"Inkwyrm", "grazer", "harmless prey", "harmless prey", false},
		{"Patina-Grazer [grazer — harmless prey, ranchable stock]",
			[]string{"species", "role", "hull", "ranchable"}, 60,
			"Patina-Grazer", "grazer", "harmless prey", "harmless prey, ranchable stock", true},
		{"Belt-Grazer [grazer — harmless prey, ranchable stock]",
			[]string{"species", "role", "hull", "ranchable"}, 60,
			"Belt-Grazer", "grazer", "harmless prey", "harmless prey, ranchable stock", true},
		{"Soot-Grazer [grazer — harmless prey, ranchable stock]",
			[]string{"species", "role", "hull", "ranchable"}, 60,
			"Soot-Grazer", "grazer", "harmless prey", "harmless prey, ranchable stock", true},
	} {
		got := ParseCreatureScan(tc.username, tc.revealed, tc.hull)
		if got.Name != tc.wantName || got.Role != tc.wantRole {
			t.Errorf("%q: name/role = %q/%q, want %q/%q", tc.username, got.Name, got.Role, tc.wantName, tc.wantRole)
		}
		if got.Danger != tc.wantDang || got.Traits != tc.wantTrait {
			t.Errorf("%q: danger/traits = %q/%q, want %q/%q", tc.username, got.Danger, got.Traits, tc.wantDang, tc.wantTrait)
		}
		if got.Ranchable != tc.wantRanch {
			t.Errorf("%q: ranchable = %v, want %v", tc.username, got.Ranchable, tc.wantRanch)
		}
	}
}

// TestParseCreatureScan_HyphenatedNamesSurvive is the trap this parser exists to
// avoid: the separator is an EM DASH, and four of the six species measured have
// a hyphen in their display name. Splitting on "-" would truncate every one.
func TestParseCreatureScan_HyphenatedNamesSurvive(t *testing.T) {
	got := ParseCreatureScan("Slag-Tortoise [grazer — harmless prey]", nil, 90)
	if got.Name != "Slag-Tortoise" {
		t.Fatalf("name = %q, want the full hyphenated name", got.Name)
	}
}

// TestParseCreatureScan_NonCreature covers a scan of something that is not
// wildlife: no bracket, so there is nothing to unpack and nothing is invented.
func TestParseCreatureScan_NonCreature(t *testing.T) {
	got := ParseCreatureScan("Arthur 'Artificer' Artis", []string{"ship_class", "hull"}, 400)
	if got.Name != "Arthur 'Artificer' Artis" {
		t.Errorf("name = %q", got.Name)
	}
	if got.Role != "" || got.Danger != "" || got.Traits != "" {
		t.Errorf("invented role/danger/traits from a non-creature scan: %+v", got)
	}
}

// TestCaptureWildlifeScan_StampsAndClearsTheWorkList checks the scan campaign's
// exit condition: a scanned species must leave GetUnscannedWildlifeSpecies, and
// the traits/ranchable must land alongside the hull already known from get_nearby.
func TestCaptureWildlifeScan_StampsAndClearsTheWorkList(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	// Seen first by get_nearby: hull and habitat, no danger.
	if err := kb.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{{
		Species: "belt_grazer", Name: "Belt-Grazer", Role: "grazer",
		MaxHull: 60, Habitats: []string{"asteroid_belt"},
	}}); err != nil {
		t.Fatal(err)
	}
	todo, err := kb.GetUnscannedWildlifeSpecies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(todo) != 1 {
		t.Fatalf("want belt_grazer on the scan work list, got %v", todo)
	}

	s := ParseCreatureScan("Belt-Grazer [grazer — harmless prey, ranchable stock]",
		[]string{"species", "role", "hull", "ranchable"}, 60)
	if err := CaptureWildlifeScan(ctx, kb, "belt_grazer", s, time.Now()); err != nil {
		t.Fatal(err)
	}

	todo, err = kb.GetUnscannedWildlifeSpecies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(todo) != 0 {
		t.Errorf("work list = %v, want empty after a scan", todo)
	}

	got, err := kb.GetWildlifeSpecies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 species, got %d", len(got))
	}
	g := got[0]
	if g.Danger != "harmless prey" || !g.Ranchable {
		t.Errorf("danger/ranchable = %q/%v, want 'harmless prey'/true", g.Danger, g.Ranchable)
	}
	if g.ScanTraits != "harmless prey, ranchable stock" {
		t.Errorf("ScanTraits = %q", g.ScanTraits)
	}
	// The scan must not have erased what get_nearby established.
	if g.MaxHull != 60 || len(g.Habitats) != 1 || g.Habitats[0] != "asteroid_belt" {
		t.Errorf("scan clobbered earlier observations: hull=%d habitats=%v", g.MaxHull, g.Habitats)
	}
}

// TestCaptureWildlifeScan_NoTraitsLeavesWorkList keeps a scan that revealed
// nothing useful from marking the species done — otherwise a weak scanner would
// permanently retire a species with an empty danger.
func TestCaptureWildlifeScan_NoTraitsLeavesWorkList(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	if err := kb.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{{
		Species: "molt_leviathan", Role: "predator",
	}}); err != nil {
		t.Fatal(err)
	}
	// A bracketless reply: nothing revealed.
	s := ParseCreatureScan("Molt Leviathan", []string{"hull"}, 2200)
	if err := CaptureWildlifeScan(ctx, kb, "molt_leviathan", s, time.Now()); err != nil {
		t.Fatal(err)
	}
	todo, err := kb.GetUnscannedWildlifeSpecies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(todo) != 1 {
		t.Errorf("work list = %v, want molt_leviathan still queued", todo)
	}
}
