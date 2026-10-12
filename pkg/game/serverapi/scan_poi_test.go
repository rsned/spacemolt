package serverapi

import (
	"encoding/json"
	"testing"
)

// v0.618 turned scan into the POI sensor: `scan <poi_id>` reports who is at
// another POI in your system, gated on a fitted ship scanner. It is the only
// replacement for get_system_agents, which the same release emptied, so every
// field it returns has to decode — a missing tag silently reads as zero and the
// sensor reports an empty POI instead of a populated one.
func TestScanResponseDecodesPOIScan(t *testing.T) {
	raw := []byte(`{
      "success": true,
      "poi_id": "sol_central",
      "scan_power": 55,
      "message": "Scan complete.",
      "signature_detected": true,
      "contacts": [{"target_id": "p1", "username": "craft_boss", "ship_class": "prospect"}],
      "npcs":    [{"name": "Patrol Cutter"}],
      "pirates": [{"name": "Dross Raider"}],
      "leviathans": [{
        "leviathan_id": "lev1", "species": "molt_leviathan",
        "name": "Juvenile Molt Leviathan", "stage": "juvenile",
        "hull": 400, "max_hull": 900, "aggressive": true, "in_combat": false
      }]
    }`)
	var resp ScanResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.POIID != "sol_central" {
		t.Errorf("POIID = %q, want sol_central", resp.POIID)
	}
	if resp.ScanPower != 55 {
		t.Errorf("ScanPower = %d, want 55", resp.ScanPower)
	}
	if len(resp.Contacts) != 1 || resp.Contacts[0].Username != "craft_boss" {
		t.Errorf("Contacts = %+v, want one craft_boss contact", resp.Contacts)
	}
	if len(resp.NPCs) != 1 || len(resp.Pirates) != 1 {
		t.Errorf("npcs=%d pirates=%d, want 1 and 1", len(resp.NPCs), len(resp.Pirates))
	}
	// A leviathan kills a starter hull in about two ticks, so an aggressive one
	// at the scanned POI is the single most important thing the scan can say.
	if len(resp.Leviathans) != 1 {
		t.Fatalf("Leviathans = %+v, want one", resp.Leviathans)
	}
	lev := resp.Leviathans[0]
	if lev.Species != "molt_leviathan" || !lev.Aggressive || lev.MaxHull != 900 {
		t.Errorf("leviathan = %+v, want aggressive molt_leviathan with max_hull 900", lev)
	}
}

// The faction-intel variant returns the same leviathan list.
func TestFactionScanPOIResponseDecodesLeviathans(t *testing.T) {
	var resp FactionScanPOIResponse
	if err := json.Unmarshal([]byte(`{"poi_id":"p","hops":1,"scan_power":30,"facility_level":1,
	  "message":"ok","leviathans":[{"leviathan_id":"l","species":"s","name":"n","stage":"adult",
	  "hull":1,"max_hull":2,"aggressive":false,"in_combat":true}]}`), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Leviathans) != 1 || resp.Leviathans[0].Stage != "adult" {
		t.Errorf("Leviathans = %+v, want one adult", resp.Leviathans)
	}
}
