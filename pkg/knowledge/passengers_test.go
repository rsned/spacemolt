package knowledge

import (
	"testing"
	"time"
)

func TestPassengersMigrationCreatesTable(t *testing.T) {
	kb := newTestKB(t)
	var count int
	if err := kb.db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='passengers'",
	).Scan(&count); err != nil {
		t.Fatalf("query for passengers: %v", err)
	}
	if count != 1 {
		t.Errorf("passengers table not created (count=%d)", count)
	}
}

func TestRecordPassengersRoundTrip(t *testing.T) {
	kb := newTestKB(t)
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)

	in := SeenPassenger{
		CitizenID:   "ziggy_stardrift",
		Name:        "Ziggy Stardrift",
		Citizenship: "nebula",
		Bio:         "A glam-rock legend.",
		Class:       "first",
		Source:      "list_station_passengers",
		SeenAt:      now,
	}
	if err := kb.RecordPassengers([]SeenPassenger{in}); err != nil {
		t.Fatalf("RecordPassengers: %v", err)
	}

	got, err := kb.GetPassenger("ziggy_stardrift")
	if err != nil {
		t.Fatalf("GetPassenger: %v", err)
	}
	if got == nil {
		t.Fatal("GetPassenger returned nil")
	}
	if got.Name != in.Name || got.Citizenship != in.Citizenship ||
		got.Bio != in.Bio || got.Class != in.Class {
		t.Errorf("round-trip mismatch: got %+v want %+v", *got, in)
	}
}

func TestRecordPassengersEmptyIDDropped(t *testing.T) {
	kb := newTestKB(t)
	if err := kb.RecordPassengers([]SeenPassenger{{Name: "Nobody"}}); err != nil {
		t.Fatalf("RecordPassengers: %v", err)
	}
	var count int
	if err := kb.db.QueryRow("SELECT COUNT(*) FROM passengers").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected empty-id record dropped, found %d rows", count)
	}
}

// A sparse later source (no citizenship/bio) must not wipe data captured from a
// richer earlier source; the name still updates and the sighting count grows.
func TestRecordPassengersCoalesceMerge(t *testing.T) {
	kb := newTestKB(t)
	t0 := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)

	rich := SeenPassenger{
		CitizenID:   "lin_mantari",
		Name:        "Lin Mantari",
		Citizenship: "nebula",
		Bio:         "A clock restorer.",
		Class:       "first",
		Source:      "list_station_passengers",
		SeenAt:      t0,
	}
	if err := kb.RecordPassengers([]SeenPassenger{rich}); err != nil {
		t.Fatalf("RecordPassengers rich: %v", err)
	}

	// Aboard manifest: same passenger, no citizenship/bio, slightly newer name.
	sparse := SeenPassenger{
		CitizenID: "lin_mantari",
		Name:      "Lin Mantari (aboard)",
		Class:     "first",
		Source:    "list_passengers",
		SeenAt:    t0.Add(time.Hour),
	}
	if err := kb.RecordPassengers([]SeenPassenger{sparse}); err != nil {
		t.Fatalf("RecordPassengers sparse: %v", err)
	}

	got, err := kb.GetPassenger("lin_mantari")
	if err != nil {
		t.Fatalf("GetPassenger: %v", err)
	}
	if got.Citizenship != "nebula" {
		t.Errorf("citizenship clobbered: got %q want nebula", got.Citizenship)
	}
	if got.Bio != "A clock restorer." {
		t.Errorf("bio clobbered: got %q", got.Bio)
	}
	if got.Name != "Lin Mantari (aboard)" {
		t.Errorf("name not updated: got %q", got.Name)
	}

	var sightings int
	if err := kb.db.QueryRow(
		"SELECT sighting_count FROM passengers WHERE citizen_id='lin_mantari'",
	).Scan(&sightings); err != nil {
		t.Fatalf("sighting_count: %v", err)
	}
	if sightings != 2 {
		t.Errorf("sighting_count = %d, want 2", sightings)
	}
}

func TestGetPassengerNotFound(t *testing.T) {
	kb := newTestKB(t)
	got, err := kb.GetPassenger("nobody")
	if err != nil {
		t.Fatalf("GetPassenger: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing passenger, got %+v", *got)
	}
}
