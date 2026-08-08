package serverapi

import (
	"encoding/json"
	"testing"
)

// A get_nearby body carrying a creatures list must decode into Creatures.
// Wildlife is invisible to the client without this, so hunting is impossible.
func TestGetNearbyDecodesCreatures(t *testing.T) {
	const body = `{
	  "action": "get_nearby",
	  "nearby": [],
	  "count": 0,
	  "poi_id": "sol_asteroid_belt",
	  "creatures": [
	    {"creature_id": "c1", "species": "belt_grazer", "name": "Belt-Grazer",
	     "hull": 40, "max_hull": 40, "is_aggressive": false, "status": "grazing"},
	    {"creature_id": "c2", "species": "slag_tortoise", "name": "Slag-Tortoise",
	     "hull": 90, "max_hull": 120, "shield": 10, "max_shield": 20,
	     "is_aggressive": false, "status": "idle"}
	  ]
	}`

	var got GetNearbyResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Creatures) != 2 {
		t.Fatalf("Creatures = %d, want 2 (a nil slice means wildlife is invisible)", len(got.Creatures))
	}
	c := got.Creatures[0]
	if c.CreatureID != "c1" {
		t.Errorf("CreatureID = %q, want c1 — this is the id hunt takes", c.CreatureID)
	}
	if c.Species != "belt_grazer" {
		t.Errorf("Species = %q, want belt_grazer", c.Species)
	}
	if c.Hull != 40 || c.MaxHull != 40 {
		t.Errorf("hull = %d/%d, want 40/40", c.Hull, c.MaxHull)
	}
	if c.Name != "Belt-Grazer" {
		t.Errorf("Name = %q, want Belt-Grazer", c.Name)
	}
	if c.Status != "grazing" {
		t.Errorf("Status = %q, want grazing", c.Status)
	}
	if c.IsAggressive {
		t.Error("belt-grazers are passive; IsAggressive must decode false")
	}
	if got.Creatures[1].MaxShield != 20 {
		t.Errorf("MaxShield = %d, want 20", got.Creatures[1].MaxShield)
	}
}

// An older body with no creatures key must still decode, leaving Creatures nil.
func TestGetNearbyWithoutCreatures(t *testing.T) {
	var got GetNearbyResponse
	if err := json.Unmarshal([]byte(`{"nearby": [], "count": 0}`), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Creatures != nil {
		t.Errorf("Creatures = %v, want nil when the key is absent", got.Creatures)
	}
}
