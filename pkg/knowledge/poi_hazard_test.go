package knowledge

import (
	"context"
	"testing"
)

func readHazard(t *testing.T, kb *SQLiteKB, poiID string) (string, int, int64) {
	t.Helper()
	var typ string
	var sev int
	var tick int64
	if err := kb.db.QueryRow(`SELECT hazard_type, hazard_severity, hazard_seen_tick FROM pois WHERE id=?`, poiID).
		Scan(&typ, &sev, &tick); err != nil {
		t.Fatalf("read hazard: %v", err)
	}
	return typ, sev, tick
}

// A read carrying a hazard records it; a later read that omits the field (most
// paths do) must not clear it -- absence is not evidence of safety.
func TestRememberPOI_HazardSetNeverClearedByOmission(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	if err := kb.RememberPOI(ctx, POI{ID: "the_maw", SystemID: "alzirr", Name: "The Maw", Type: "sun",
		HazardType: "radiation", HazardSeverity: 4, LastUpdatedTick: 100}); err != nil {
		t.Fatal(err)
	}
	if typ, sev, tick := readHazard(t, kb, "the_maw"); typ != "radiation" || sev != 4 || tick != 100 {
		t.Fatalf("after hazard read: got %q/%d/%d, want radiation/4/100", typ, sev, tick)
	}

	if err := kb.RememberPOI(ctx, POI{ID: "the_maw", SystemID: "alzirr", Name: "The Maw", Type: "sun",
		LastUpdatedTick: 200}); err != nil {
		t.Fatal(err)
	}
	if typ, sev, tick := readHazard(t, kb, "the_maw"); typ != "radiation" || sev != 4 || tick != 100 {
		t.Fatalf("after hazard-less read: got %q/%d/%d, want radiation/4/100 kept", typ, sev, tick)
	}

	if err := kb.RememberPOI(ctx, POI{ID: "the_maw", SystemID: "alzirr", Name: "The Maw", Type: "sun",
		HazardType: "radiation", HazardSeverity: 3, LastUpdatedTick: 300}); err != nil {
		t.Fatal(err)
	}
	if typ, sev, tick := readHazard(t, kb, "the_maw"); typ != "radiation" || sev != 3 || tick != 300 {
		t.Fatalf("after newer hazard read: got %q/%d/%d, want radiation/3/300", typ, sev, tick)
	}

	if err := kb.RememberPOI(ctx, POI{ID: "belt", SystemID: "alzirr", Name: "Belt", Type: "asteroid_belt",
		LastUpdatedTick: 300}); err != nil {
		t.Fatal(err)
	}
	if typ, sev, _ := readHazard(t, kb, "belt"); typ != "" || sev != 0 {
		t.Fatalf("hazard-free POI: got %q/%d, want empty", typ, sev)
	}
}

// Faction intel carries a POI's hazard but not its hidden flag or a full read:
// recording it from intel touches only the hazard columns of a known POI, and
// adds an unknown POI so the hazard is still on file.
func TestRecordPOIHazardTouchesOnlyHazard(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	if err := kb.RememberPOI(ctx, POI{ID: "cinderback", SystemID: "old_clock", Name: "Cinderback", Type: "planet",
		Hidden: true, LastUpdatedTick: 500}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RecordPOIHazard(ctx, POI{ID: "cinderback", SystemID: "old_clock", Name: "Cinderback", Type: "planet",
		HazardType: "radiation", HazardSeverity: 2, LastUpdatedTick: 900}); err != nil {
		t.Fatal(err)
	}
	if typ, sev, tick := readHazard(t, kb, "cinderback"); typ != "radiation" || sev != 2 || tick != 900 {
		t.Fatalf("got %q/%d/%d, want radiation/2/900", typ, sev, tick)
	}
	var hidden bool
	var last int64
	if err := kb.db.QueryRow(`SELECT hidden, last_updated_tick FROM pois WHERE id='cinderback'`).Scan(&hidden, &last); err != nil {
		t.Fatal(err)
	}
	if !hidden || last != 500 {
		t.Fatalf("hidden %v last %d, want the POI's own read kept (true, 500)", hidden, last)
	}

	if err := kb.RecordPOIHazard(ctx, POI{ID: "stolen_mass", SystemID: "eventide_core", Name: "Stolen Mass", Type: "planet",
		HazardType: "tidal_shear", HazardSeverity: 4, LastUpdatedTick: 900}); err != nil {
		t.Fatal(err)
	}
	if typ, sev, _ := readHazard(t, kb, "stolen_mass"); typ != "tidal_shear" || sev != 4 {
		t.Fatalf("unknown POI: got %q/%d, want tidal_shear/4", typ, sev)
	}
	pois, err := kb.GetPOIs(ctx, "eventide_core")
	if err != nil || len(pois) != 1 || pois[0].HazardSeverity != 4 {
		t.Fatalf("GetPOIs(eventide_core) = %+v, %v; want the recorded POI with its hazard", pois, err)
	}
}

func TestMemoryRecordPOIHazard(t *testing.T) {
	kb := NewMemoryKB()
	ctx := context.Background()
	if err := kb.RememberPOI(ctx, POI{ID: "p", SystemID: "s", Name: "P", Hidden: true}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RecordPOIHazard(ctx, POI{ID: "p", SystemID: "s", HazardType: "debris", HazardSeverity: 1}); err != nil {
		t.Fatal(err)
	}
	pois, _ := kb.GetPOIs(ctx, "s")
	if len(pois) != 1 || pois[0].HazardType != "debris" || pois[0].HazardSeverity != 1 || !pois[0].Hidden || pois[0].Name != "P" {
		t.Fatalf("got %+v, want hazard set and the rest kept", pois)
	}
}
