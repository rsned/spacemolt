package battlereplay

import (
	"encoding/json"
	"os"
	"testing"
)

// leviathanModel loads the replay model for the battle in which a Rainbow
// Leviathan destroyed a haul worker's shard in Goldcrest — the first apex
// creature we have any combat numbers for at all.
func leviathanModel(t *testing.T) *ReplayModel {
	t.Helper()
	raw, err := os.ReadFile("testdata/model_leviathan_13ec2c95.json")
	if err != nil {
		t.Fatalf("read leviathan model: %v", err)
	}
	var m ReplayModel
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode leviathan model: %v", err)
	}
	return &m
}

const leviathanID = "crt_3f99a3b42b8796db63e9248ceb2fc313"

// TestWildlifeAttacks_RainbowLeviathan pins the apex's offence as the wire
// reported it: a natural energy beam landing 130 a tick, twice out of two.
func TestWildlifeAttacks_RainbowLeviathan(t *testing.T) {
	m := leviathanModel(t)

	got := WildlifeAttacks(m, map[string]string{leviathanID: "rainbow_leviathan"})
	if len(got) != 1 {
		t.Fatalf("want 1 attack row, got %d: %+v", len(got), got)
	}
	a := got[0]
	if a.Species != "rainbow_leviathan" {
		t.Errorf("species = %q", a.Species)
	}
	if a.WeaponName != "Rainbow Leviathan (natural)" {
		t.Errorf("weapon_name = %q, want the natural weapon named on the wire", a.WeaponName)
	}
	if a.DamageType != "energy" {
		t.Errorf("damage_type = %q, want energy — this is the field a resist fit is chosen against", a.DamageType)
	}
	if a.ShotKind != "beam" {
		t.Errorf("shot_kind = %q, want beam", a.ShotKind)
	}
	if a.Shots != 2 || a.Hits != 2 {
		t.Errorf("shots/hits = %d/%d, want 2/2", a.Shots, a.Hits)
	}
	if a.DamageMin != 130 || a.DamageMax != 130 {
		t.Errorf("damage range = %v..%v, want 130..130", a.DamageMin, a.DamageMax)
	}
	if a.BattleID != m.BattleID {
		t.Errorf("battle_id = %q, want %q so a re-import replaces rather than doubles", a.BattleID, m.BattleID)
	}
}

// TestWildlifeAttacks_SkipsUnnamedCreatures guards against filing shots under a
// guessed species: a creature the caller cannot name is skipped, because the
// battle log carries only a display name and an empty ship_class.
func TestWildlifeAttacks_SkipsUnnamedCreatures(t *testing.T) {
	m := leviathanModel(t)

	if got := WildlifeAttacks(m, nil); len(got) != 0 {
		t.Errorf("want no rows without a species mapping, got %+v", got)
	}
	if got := WildlifeAttacks(m, map[string]string{"crt_someone_else": "belt_grazer"}); len(got) != 0 {
		t.Errorf("want no rows for an unrelated mapping, got %+v", got)
	}
}

// TestWildlifeAttacks_IgnoresPlayerShots checks the filter is on Kind, not on
// "everything that fired": the player's own shots must never be attributed to
// the creature's profile.
func TestWildlifeAttacks_IgnoresPlayerShots(t *testing.T) {
	m := leviathanModel(t)

	// The player is side 2 and fired nothing that connected here, but the guard
	// is what matters: mapping the PLAYER's id to a species must yield nothing,
	// because the player is not Kind "creature".
	var playerID string
	for _, p := range m.Participants {
		if p.Kind == "player" {
			playerID = p.PlayerID
		}
	}
	if playerID == "" {
		t.Fatal("fixture has no player participant")
	}
	if got := WildlifeAttacks(m, map[string]string{playerID: "not_a_creature"}); len(got) != 0 {
		t.Errorf("player shots were attributed to a species: %+v", got)
	}
}

// TestWildlifeDefences_RainbowLeviathan records the other half of the fit
// decision. 2200 hull with zero shield is what makes a starter hull hopeless
// here, and confirms the docs' claim that creatures carry no shields.
func TestWildlifeDefences_RainbowLeviathan(t *testing.T) {
	m := leviathanModel(t)

	got := WildlifeDefences(m, map[string]string{leviathanID: "rainbow_leviathan"})
	if len(got) != 1 {
		t.Fatalf("want 1 species row, got %d", len(got))
	}
	if got[0].MaxHull != 2200 {
		t.Errorf("max_hull = %d, want 2200", got[0].MaxHull)
	}
	if got[0].MaxShield != 0 {
		t.Errorf("max_shield = %d, want 0: creatures are documented as shieldless", got[0].MaxShield)
	}
	if got[0].Name != "Rainbow Leviathan" {
		t.Errorf("name = %q", got[0].Name)
	}
}
