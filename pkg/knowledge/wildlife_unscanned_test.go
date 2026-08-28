package knowledge

import (
	"context"
	"slices"
	"testing"
	"time"
)

// A species is "scanned" once a scan has stamped danger_scanned_utc. Until
// then its danger bracket and hull are unknown, which is exactly what an
// operator needs told before deciding whether a manual scan is safe -- a
// Leviathan does 130 energy/tick and kills a starter hull in two.
func TestUnscannedSpecies_ReportsOnlyThoseWithoutAScan(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	if err := kb.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{
		{Species: "belt_grazer", Name: "Belt Grazer"},
		{Species: "hollow_pilgrim", Name: "Hollow Pilgrim"},
		{Species: "rainbow_leviathan", Name: "Rainbow Leviathan",
			Danger: "apex predator", DangerScannedUTC: time.Now().UTC().Format(time.RFC3339)},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := UnscannedSpecies(ctx, kb, []string{"belt_grazer", "hollow_pilgrim", "rainbow_leviathan"})
	if err != nil {
		t.Fatalf("UnscannedSpecies: %v", err)
	}
	slices.Sort(got)
	want := []string{"belt_grazer", "hollow_pilgrim"}
	if !slices.Equal(got, want) {
		t.Errorf("UnscannedSpecies = %v, want %v", got, want)
	}
}

// Only the species actually asked about may come back: the caller is reporting
// what is at THIS poi, not everything unscanned in the galaxy.
func TestUnscannedSpecies_IsScopedToTheQuery(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	if err := kb.UpsertWildlifeSpecies(ctx, []WildlifeSpecies{
		{Species: "belt_grazer"}, {Species: "pressblister"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := UnscannedSpecies(ctx, kb, []string{"belt_grazer"})
	if err != nil {
		t.Fatalf("UnscannedSpecies: %v", err)
	}
	if !slices.Equal(got, []string{"belt_grazer"}) {
		t.Errorf("got %v, want only belt_grazer", got)
	}
}

func TestUnscannedSpecies_EmptyInputsAreSafe(t *testing.T) {
	ctx := context.Background()
	if got, err := UnscannedSpecies(ctx, newTestKB(t), nil); err != nil || len(got) != 0 {
		t.Errorf("nil species = (%v, %v), want (empty, nil)", got, err)
	}
	if got, err := UnscannedSpecies(ctx, nil, []string{"belt_grazer"}); err != nil || len(got) != 0 {
		t.Errorf("nil kb = (%v, %v), want (empty, nil)", got, err)
	}
}
