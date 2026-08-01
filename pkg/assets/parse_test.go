package assets

import "testing"

// TestParseCurrentMax pins the OwnedShip hull/fuel format. These arrive as
// STRINGS ("1020/1020", "150/200"), not numbers. The partial case is the one
// that matters: a hull that exists but is not ready to fly.
func TestParseCurrentMax(t *testing.T) {
	for _, tc := range []struct {
		in       string
		cur, max int
		ok       bool
	}{
		{"1020/1020", 1020, 1020, true},
		{"150/200", 150, 200, true},
		{"0/200", 0, 200, true},
		{"", 0, 0, false},
		{"340", 0, 0, false},
		{"a/b", 0, 0, false},
		{"1/2/3", 0, 0, false},
	} {
		cur, max, ok := ParseCurrentMax(tc.in)
		if cur != tc.cur || max != tc.max || ok != tc.ok {
			t.Errorf("ParseCurrentMax(%q) = (%d,%d,%v), want (%d,%d,%v)",
				tc.in, cur, max, ok, tc.cur, tc.max, tc.ok)
		}
	}
}

// TestHullsFromGoldenPayload decodes a real list_ships body. The raw hull and
// fuel strings are retained alongside the parsed ints so a server-side format
// change surfaces as a mismatch instead of silent zeros.
func TestHullsFromGoldenPayload(t *testing.T) {
	raw := []byte(`{"action":"list_ships","count":2,"active_ship_id":"aaa",
	 "ships":[
	  {"class_id":"survey_vessel","class_name":"Survey Vessel","fuel":"1020/1020",
	   "hull":"340/340","is_active":false,"location":"stored at Grand Exchange Station",
	   "location_base_id":"grand_exchange_station","modules":3,
	   "ship_id":"74aeb79e64d9a12f682a2ee6daad79e4"},
	  {"class_id":"reclaim","class_name":"Reclaim","fuel":"150/200","hull":"180/180",
	   "is_active":true,"location":"stored at Grand Exchange Station",
	   "location_base_id":"grand_exchange_station","modules":2,
	   "ship_id":"67ef4a3e25dc336829d7d3e25736fe61"}]}`)

	hulls, err := HullsFrom(raw)
	if err != nil {
		t.Fatalf("HullsFrom: %v", err)
	}
	if len(hulls) != 2 {
		t.Fatalf("got %d hulls, want 2", len(hulls))
	}

	h := hulls[0]
	if h.ShipID != "74aeb79e64d9a12f682a2ee6daad79e4" || h.ClassID != "survey_vessel" {
		t.Errorf("hull[0] identity = %q/%q", h.ShipID, h.ClassID)
	}
	if h.ClassName != "Survey Vessel" {
		t.Errorf("hull[0] class name = %q, want %q", h.ClassName, "Survey Vessel")
	}
	if h.HullCurrent != 340 || h.HullMax != 340 || h.HullRaw != "340/340" {
		t.Errorf("hull[0] hull = %d/%d raw=%q", h.HullCurrent, h.HullMax, h.HullRaw)
	}
	if h.FuelCurrent != 1020 || h.FuelMax != 1020 || h.FuelRaw != "1020/1020" {
		t.Errorf("hull[0] fuel = %d/%d raw=%q", h.FuelCurrent, h.FuelMax, h.FuelRaw)
	}
	if h.CargoUsed != 0 {
		t.Errorf("hull[0] cargo used = %d, want 0", h.CargoUsed)
	}
	if h.LocationBaseID != "grand_exchange_station" || h.Modules != 3 {
		t.Errorf("hull[0] base=%q modules=%d", h.LocationBaseID, h.Modules)
	}
	if h.IsActive {
		t.Error("hull[0] must not be active")
	}

	h = hulls[1]
	if h.HullCurrent != 180 || h.HullMax != 180 || h.HullRaw != "180/180" {
		t.Errorf("hull[1] hull = %d/%d raw=%q", h.HullCurrent, h.HullMax, h.HullRaw)
	}
	if h.FuelCurrent != 150 || h.FuelMax != 200 {
		t.Errorf("hull[1] fuel = %d/%d, want 150/200", h.FuelCurrent, h.FuelMax)
	}
	if !h.IsActive {
		t.Error("hull[1] must be active")
	}
}

// TestHullsFromEmptyIsNotAnError pins that an absent cache entry yields no
// hulls and no error: capture must degrade to "nothing this pass", never fail.
func TestHullsFromEmptyIsNotAnError(t *testing.T) {
	hulls, err := HullsFrom(nil)
	if err != nil || len(hulls) != 0 {
		t.Fatalf("HullsFrom(nil) = %v, %v; want empty, nil", hulls, err)
	}
}
