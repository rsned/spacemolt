package serverapi

import (
	"encoding/json"
	"testing"
)

// Live get_location reply recorded from craftsman-1 mid-drift on 2026-11-13.
func TestLocationResponseDriftSample(t *testing.T) {
	raw := `{"location":{"connections":[],"docked_at":null,"in_transit":true,"nearby_repeaters":[],"poi_id":"","system_id":"","transit_bearing":226.5,"transit_ticks_elapsed":297,"transit_type":"pathfinder","transit_x":-4790.231354544311,"transit_y":-6130.078720788663},"message":"Current location"}`
	var r LocationResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	l := r.Location
	if !l.Drifting() {
		t.Fatal("Drifting() = false, want true")
	}
	if l.TransitX != -4790.231354544311 || l.TransitY != -6130.078720788663 {
		t.Errorf("transit position = (%v, %v)", l.TransitX, l.TransitY)
	}
	if l.TransitBearing != 226.5 {
		t.Errorf("TransitBearing = %v, want 226.5", l.TransitBearing)
	}
	if l.TransitTicksElapsed != 297 {
		t.Errorf("TransitTicksElapsed = %d, want 297", l.TransitTicksElapsed)
	}
	if l.DockedAt != nil {
		t.Errorf("DockedAt = %q, want nil", *l.DockedAt)
	}
}

func TestLocationResponseInSystem(t *testing.T) {
	raw := `{"location":{"system_id":"frontier","poi_id":"icecap_drift","connections":["horizon"],"docked_at":null,"nearby_repeaters":[{"id":"r1"}]}}`
	var r LocationResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	l := r.Location
	if l.Drifting() {
		t.Error("Drifting() = true in a system")
	}
	if l.SystemID != "frontier" || l.POIID != "icecap_drift" {
		t.Errorf("system/poi = %q/%q", l.SystemID, l.POIID)
	}
	if len(l.NearbyRepeaters) != 1 {
		t.Errorf("NearbyRepeaters = %d, want 1", len(l.NearbyRepeaters))
	}
}
