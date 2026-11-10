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
