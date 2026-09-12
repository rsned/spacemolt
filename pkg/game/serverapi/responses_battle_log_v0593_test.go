package serverapi

import (
	"encoding/json"
	"testing"
)

// v0.593.0 moved the to-hit roll from the volley to the individual weapon.
// Each weapons[] entry now carries its own hit_chance / hit_roll /
// hit_success; the attack's hit_success means "at least one weapon hit", its
// hit_chance is the ship-level chance every gun starts from, the volley-level
// hit_roll is gone, and pre_hit_damage was renamed landed_damage.
//
// Every exported fixture in kb/data/battles predates this, so the structs have
// to read BOTH vintages — hence the pointer fields, which distinguish "key
// absent" from "present and zero" (zero is a real value on a miss).

func TestDecodeV0593PerWeaponHitRolls(t *testing.T) {
	raw := []byte(`{
	  "attacker_id": "a1", "target_id": "t1",
	  "hit_chance": 0.65, "hit_success": true,
	  "damage_type": "kinetic", "zone_distance": 2,
	  "raw_damage": 120, "landed_damage": 84,
	  "shield_damage": 50, "hull_damage": 34, "final_damage": 84,
	  "weapons": [
	    {"name": "Autocannon II", "base_damage": 18, "damage": 18,
	     "hit_chance": 0.65, "hit_roll": 0.41, "hit_success": true,
	     "crit_chance": 0.03, "crit_roll": 0.9, "crit_fired": false},
	    {"name": "Autocannon II", "base_damage": 18, "damage": 0,
	     "hit_chance": 0.65, "hit_roll": 0.88, "hit_success": false,
	     "crit_chance": 0.03, "crit_roll": 0.5, "crit_fired": false},
	    {"name": "Railgun I", "base_damage": 45, "damage": 45,
	     "hit_chance": 0.50, "hit_roll": 0.12, "hit_success": true,
	     "crit_chance": 0.03, "crit_roll": 0.7, "crit_fired": false}
	  ]
	}`)
	var a AttackLogEntry
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	landed, ok := a.Landed()
	if !ok || landed != 84 {
		t.Errorf("Landed() = %d, %v; want 84, true", landed, ok)
	}
	if a.HitRoll != nil {
		t.Errorf("HitRoll = %v; the volley-level roll is gone in v0.593.0", *a.HitRoll)
	}
	hit, total := a.WeaponsHit()
	if hit != 2 || total != 3 {
		t.Errorf("WeaponsHit() = %d/%d; want 2/3 (the compact view's \"hit 2/3\")", hit, total)
	}
	// Per-weapon accuracy is per gun now: the railgun rolls its own 0.50
	// against the autocannons' 0.65, so a single ship-level number no longer
	// describes the volley.
	if a.Weapons[2].HitChance != 0.50 {
		t.Errorf("weapon[2].hit_chance = %v; want 0.50", a.Weapons[2].HitChance)
	}
	if !a.Weapons[0].HitSuccess || a.Weapons[1].HitSuccess {
		t.Errorf("per-weapon hit_success not decoded: %+v", a.Weapons)
	}
}

// The pre-0.593 fixtures are the entire measured corpus (the Haven battle is
// 17,146 shots); they must keep decoding, and pre_hit_damage must keep
// answering Landed().
func TestDecodePreV0593LogStillWorks(t *testing.T) {
	raw := []byte(`{
	  "attacker_id": "a1", "target_id": "t1",
	  "hit_chance": 0.9, "hit_roll": 0.32, "hit_success": true,
	  "damage_type": "void", "zone_distance": 0,
	  "raw_damage": 110, "pre_hit_damage": 97,
	  "shield_damage": 0, "hull_damage": 97, "final_damage": 97,
	  "weapons": [{"name": "Void Laser", "base_damage": 65, "damage": 97,
	    "crit_chance": 0.2, "crit_roll": 0.05, "crit_fired": true}]
	}`)
	var a AttackLogEntry
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	landed, ok := a.Landed()
	if !ok || landed != 97 {
		t.Errorf("Landed() = %d, %v; want 97, true from pre_hit_damage", landed, ok)
	}
	if a.HitRoll == nil || *a.HitRoll != 0.32 {
		t.Errorf("legacy volley-level hit_roll not decoded: %+v", a.HitRoll)
	}
	// An old log has no per-weapon hit fields at all. Reporting 1/1 there
	// would invent a measurement, so WeaponsHit must say "unknown".
	if hit, total := a.WeaponsHit(); hit != 0 || total != 0 {
		t.Errorf("WeaponsHit() on a pre-0.593 log = %d/%d; want 0/0 (not measurable)", hit, total)
	}
}

// A miss lands zero damage, and zero is a real reading — it must not be
// confused with the field being absent.
func TestLandedDamageZeroIsNotAbsent(t *testing.T) {
	var miss AttackLogEntry
	if err := json.Unmarshal([]byte(`{"hit_success":false,"landed_damage":0,"weapons":[
	  {"name":"Pulse Laser I","hit_chance":0.35,"hit_roll":0.77,"hit_success":false}]}`), &miss); err != nil {
		t.Fatal(err)
	}
	landed, ok := miss.Landed()
	if !ok || landed != 0 {
		t.Errorf("Landed() = %d, %v; want 0, true — a miss really landed zero", landed, ok)
	}
	if hit, total := miss.WeaponsHit(); hit != 0 || total != 1 {
		t.Errorf("WeaponsHit() = %d/%d; want 0/1", hit, total)
	}

	var neither AttackLogEntry
	if err := json.Unmarshal([]byte(`{"hit_success":true,"raw_damage":10}`), &neither); err != nil {
		t.Fatal(err)
	}
	if _, ok := neither.Landed(); ok {
		t.Errorf("Landed() reported ok with neither key present; callers must be able to tell")
	}
}
