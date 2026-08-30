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
		username   string
		revealed   []string
		hull       int
		wantName   string
		wantThreat string
		wantDang   string
		wantTrait  string
		wantRanch  bool
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
		got := ParseCreatureScan(tc.username, tc.revealed, tc.hull, "")
		if got.Name != tc.wantName || got.ThreatClass != tc.wantThreat {
			t.Errorf("%q: name/threat = %q/%q, want %q/%q", tc.username, got.Name, got.ThreatClass, tc.wantName, tc.wantThreat)
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
	got := ParseCreatureScan("Slag-Tortoise [grazer — harmless prey]", nil, 90, "")
	if got.Name != "Slag-Tortoise" {
		t.Fatalf("name = %q, want the full hyphenated name", got.Name)
	}
}

// TestParseCreatureScan_NonCreature covers a scan of something that is not
// wildlife: no bracket, so there is nothing to unpack and nothing is invented.
func TestParseCreatureScan_NonCreature(t *testing.T) {
	got := ParseCreatureScan("Arthur 'Artificer' Artis", []string{"ship_class", "hull"}, 400, "")
	if got.Name != "Arthur 'Artificer' Artis" {
		t.Errorf("name = %q", got.Name)
	}
	if got.ThreatClass != "" || got.Danger != "" || got.Traits != "" {
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
		[]string{"species", "role", "hull", "ranchable"}, 60, "")
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
	s := ParseCreatureScan("Molt Leviathan", []string{"hull"}, 2200, "")
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

// TestParseCreatureScan_ThreatClassIsNotTheRole pins the distinction that the
// bracket hides, using the three scans taken live on 2026-08-17.
//
// Carrion-Moth is the case that proves it: survey_system reports role
// "scavenger", but its scan bracket says "grazer". The bracket is a two-valued
// prey/predator flag, and treating it as the taxonomy refiles scavengers as
// grazers. The predators are the other half — the bracket SHOUTS "PREDATOR",
// which no lowercase role comparison matches.
func TestParseCreatureScan_ThreatClassIsNotTheRole(t *testing.T) {
	for _, tc := range []struct {
		username   string
		hull       int
		wantName   string
		wantThreat string
		wantDanger string
	}{
		{"Coronid [grazer — harmless prey]", 100, "Coronid", "grazer", "harmless prey"},
		{"Carrion-Moth [grazer — harmless prey]", 40, "Carrion-Moth", "grazer", "harmless prey"},
		{"Rainbow Leviathan [PREDATOR — hunts ships]", 2200, "Rainbow Leviathan", "PREDATOR", "hunts ships"},
	} {
		got := ParseCreatureScan(tc.username, []string{"species", "role", "hull"}, tc.hull, "")
		if got.Name != tc.wantName || got.ThreatClass != tc.wantThreat || got.Danger != tc.wantDanger {
			t.Errorf("%q: %+v", tc.username, got)
		}
	}
}

// TestCaptureWildlifeScan_DoesNotClobberTheCensusRole is the regression. Before
// this fix the scan wrote its bracket into role, and the live KB held
// carrion_moth as a "grazer" and rainbow_leviathan as "PREDATOR".
func TestCaptureWildlifeScan_DoesNotClobberTheCensusRole(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	// The census establishes the taxonomy first, as it does in the field.
	if err := kb.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{
		{Species: "carrion_moth", Name: "Carrion-Moth", Role: "scavenger"},
		{Species: "rainbow_leviathan", Name: "Rainbow Leviathan", Role: "predator"},
	}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	for _, s := range []struct {
		species  string
		username string
		hull     int
	}{
		{"carrion_moth", "Carrion-Moth [grazer — harmless prey]", 40},
		{"rainbow_leviathan", "Rainbow Leviathan [PREDATOR — hunts ships]", 2200},
	} {
		if err := CaptureWildlifeScan(ctx, kb,
			s.species, ParseCreatureScan(s.username, []string{"species", "role", "hull"}, s.hull, ""), now); err != nil {
			t.Fatal(err)
		}
	}

	got := map[string]WildlifeSpecies{}
	all, err := kb.GetWildlifeSpecies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range all {
		got[s.Species] = s
	}

	if r := got["carrion_moth"].Role; r != "scavenger" {
		t.Errorf("carrion_moth role = %q, want scavenger — the scan bracket refiled a scavenger as a grazer", r)
	}
	if r := got["rainbow_leviathan"].Role; r != "predator" {
		t.Errorf("rainbow_leviathan role = %q, want lowercase predator — WHERE role='predator' must match it", r)
	}
	// The scan is still the only source of these, and must have landed.
	if h := got["rainbow_leviathan"].MaxHull; h != 2200 {
		t.Errorf("rainbow_leviathan max_hull = %d, want 2200 from the scan", h)
	}
	if d := got["rainbow_leviathan"].Danger; d != "hunts ships" {
		t.Errorf("rainbow_leviathan danger = %q", d)
	}
	if d := got["carrion_moth"].Danger; d != "harmless prey" {
		t.Errorf("carrion_moth danger = %q", d)
	}
}
