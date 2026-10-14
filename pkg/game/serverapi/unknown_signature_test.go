package serverapi

import (
	"encoding/json"
	"testing"
)

// unknown_signature is the server telling us a CLOAKED ship sits at the POI we
// just arrived at, within one sensor level of our scanner. Presence only: an
// area scan (scan with no target_id) resolves it. Omitted when false.
//
// Dropping it silently discards the only warning we get before an ambush, which
// is exactly how a player hunter took ten of our ships in 2026-08. It arrives
// on arrival, on get_nearby, and on the observation feed.
func TestTravelResponseDecodesUnknownSignature(t *testing.T) {
	var resp TravelResponse
	raw := []byte(`{"action":"arrived","poi":"Nashira Belt","poi_id":"nashira_belt","unknown_signature":true}`)
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.UnknownSignature {
		t.Error("TravelResponse.UnknownSignature = false, want true — a cloaked contact at the arrival POI")
	}
}

// Omitted means false, not missing: an arrival with no cloaked contact must not
// read as a warning.
func TestTravelResponseUnknownSignatureOmittedIsFalse(t *testing.T) {
	var resp TravelResponse
	if err := json.Unmarshal([]byte(`{"action":"arrived","poi_id":"x"}`), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.UnknownSignature {
		t.Error("UnknownSignature = true on a payload that omitted it")
	}
}

// The same flag rides get_nearby, so a stationary worker polling its own POI
// sees a cloaked arrival too.
func TestGetNearbyResponseDecodesUnknownSignature(t *testing.T) {
	var resp GetNearbyResponse
	raw := []byte(`{"poi_id":"nashira_belt","count":0,"unknown_signature":true}`)
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.UnknownSignature {
		t.Error("GetNearbyResponse.UnknownSignature = false, want true")
	}
}
