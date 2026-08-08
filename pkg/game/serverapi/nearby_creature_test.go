package serverapi

import (
	"encoding/json"
	"testing"
)

// liveGetNearby is a real get_nearby payload, captured from craftsman-1 at
// commerce_fields on 2026-08-08 and trimmed only by dropping entries — no field
// was renamed, added or invented.
//
// This fixture replaced a fabricated one. The invented version used
// `is_aggressive` and `status`, neither of which the server sends, so the type
// carried two fields that could never populate while missing `in_combat` and
// `role` that it does send. A test built on a guessed payload passes against a
// wrong type, which is why the fixture has to come off the wire.
const liveGetNearby = `{
  "count": 5,
  "creature_count": 3,
  "creatures": [
    {"creature_id":"crt_bb95d6cc79790d9905fcbba9eaa0fe02","hull":45,"in_combat":false,
     "max_hull":45,"name":"Ash-Scarab","role":"scavenger","species":"ash_scarab"},
    {"creature_id":"crt_0ead63b7f4e62f61d92fe5a93cb66986","hull":40,"in_combat":true,
     "max_hull":45,"name":"Ash-Scarab","role":"scavenger","species":"ash_scarab"},
    {"creature_id":"crt_f1cd697dd6e538ce9591b7d2913f4db3","hull":45,"in_combat":false,
     "max_hull":45,"name":"Ash-Scarab","role":"scavenger","species":"ash_scarab"}
  ],
  "nearby": [],
  "pirate_count": 0,
  "pirates": [],
  "poi_id": "commerce_fields"
}`

// Wildlife must decode. A nil Creatures slice is what "the client cannot see
// wildlife" looks like, which is the bug this type exists to fix.
func TestGetNearbyDecodesCreatures(t *testing.T) {
	var got GetNearbyResponse
	if err := json.Unmarshal([]byte(liveGetNearby), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Creatures) != 3 {
		t.Fatalf("Creatures = %d, want 3 (a nil slice means wildlife is invisible)", len(got.Creatures))
	}
	if got.CreatureCount != 3 {
		t.Errorf("CreatureCount = %d, want 3", got.CreatureCount)
	}

	c := got.Creatures[0]
	if c.CreatureID != "crt_bb95d6cc79790d9905fcbba9eaa0fe02" {
		t.Errorf("CreatureID = %q — this is the id hunt takes, so it must survive decoding", c.CreatureID)
	}
	if c.Species != "ash_scarab" {
		t.Errorf("Species = %q, want ash_scarab", c.Species)
	}
	if c.Name != "Ash-Scarab" {
		t.Errorf("Name = %q, want Ash-Scarab", c.Name)
	}
	if c.Role != "scavenger" {
		t.Errorf("Role = %q, want scavenger — role is the real field, not the invented `status`", c.Role)
	}
	if c.Hull != 45 || c.MaxHull != 45 {
		t.Errorf("hull = %d/%d, want 45/45", c.Hull, c.MaxHull)
	}
	if c.InCombat {
		t.Error("first creature is not fighting; InCombat must decode false")
	}

	// The second is mid-fight — a creature already in combat is one to leave
	// alone, so this field has to round-trip.
	if !got.Creatures[1].InCombat {
		t.Error("second creature has in_combat true; InCombat must decode true")
	}
	if got.Creatures[1].Hull != 40 {
		t.Errorf("damaged creature hull = %d, want 40", got.Creatures[1].Hull)
	}
}

// A body with no creatures key must still decode, leaving Creatures nil.
func TestGetNearbyWithoutCreatures(t *testing.T) {
	var got GetNearbyResponse
	if err := json.Unmarshal([]byte(`{"nearby": [], "count": 0}`), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Creatures != nil {
		t.Errorf("Creatures = %v, want nil when the key is absent", got.Creatures)
	}
	if got.CreatureCount != 0 {
		t.Errorf("CreatureCount = %d, want 0", got.CreatureCount)
	}
}
