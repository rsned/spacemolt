package main

import (
	"context"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// ammoCatalog is the test stand-in for the knowledge-base lookup. The pairings
// are the real ones from data/game-api/latest/catalog_items.json, where a
// weapon's ammo_type equals an ammo item's effect.subtype.
func ammoCatalog(itemID string) (name, ammoType string) {
	switch itemID {
	case "standard_guided_missiles":
		return "Standard Guided Missiles", "missile"
	case "armor_buster_missiles":
		return "Armor-Buster Missiles", "missile"
	case "ferrous_slug_case":
		return "Ferrous Slug Case", "railgun"
	case "standard_rounds_box":
		return "Standard Rounds Box", "autocannon"
	case "iron_ore":
		return "Iron Ore", "" // not ammo
	}
	return "", ""
}

func weaponMod(id, typeID, name, ammoType string, cur, mag int, loaded string) serverapi.ShipModule {
	return serverapi.ShipModule{
		ID: id, TypeID: typeID, Name: name, Type: "weapon",
		AmmoType: ammoType, CurrentAmmo: cur, MagazineSize: mag, LoadedAmmoID: loaded,
	}
}

func TestBuildReloadPlan(t *testing.T) {
	modules := []serverapi.ShipModule{
		weaponMod("w1", "missile_launcher_ii", "Missile Launcher II", "missile", 0, 12, "standard_guided_missiles"),
		weaponMod("w2", "railgun_i", "Railgun I", "railgun", 3, 20, ""),
		// An energy weapon takes no ammo and must not appear.
		{ID: "w3", TypeID: "pulse_laser_i", Name: "Pulse Laser I", Type: "weapon"},
		// A non-weapon module must not appear.
		{ID: "m1", TypeID: "advanced_drone_bay", Name: "Advanced Drone Bay", Type: "utility"},
	}
	cargo := []serverapi.CargoItem{
		{ItemID: "iron_ore", Quantity: 200},
		{ItemID: "armor_buster_missiles", Quantity: 4},
		{ItemID: "standard_guided_missiles", Quantity: 42},
	}

	plan := buildReloadPlan(modules, cargo, ammoCatalog)

	if len(plan) != 2 {
		t.Fatalf("want 2 ammo-using weapons, got %d", len(plan))
	}

	w1 := plan[0]
	if w1.WeaponID != "w1" || w1.AmmoType != "missile" {
		t.Fatalf("first entry: got %+v", w1)
	}
	if !w1.Empty() {
		t.Error("0/12 must read as empty")
	}
	if len(w1.Candidates) != 2 {
		t.Fatalf("want both missile types as candidates, got %d", len(w1.Candidates))
	}
	// The already-loaded type comes first: reloading a partial magazine with a
	// different ammo discards the remaining rounds (ReloadResponse.rounds_discarded).
	if w1.Candidates[0].ItemID != "standard_guided_missiles" {
		t.Errorf("loaded ammo must rank first, got %q", w1.Candidates[0].ItemID)
	}
	if w1.Candidates[0].Quantity != 42 || w1.Candidates[0].Name != "Standard Guided Missiles" {
		t.Errorf("candidate detail wrong: %+v", w1.Candidates[0])
	}

	w2 := plan[1]
	if w2.WeaponID != "w2" || w2.AmmoType != "railgun" {
		t.Fatalf("second entry: got %+v", w2)
	}
	if w2.Empty() {
		t.Error("3/20 is low, not empty")
	}
	if len(w2.Candidates) != 0 {
		t.Errorf("no railgun ammo in cargo, got %d candidates", len(w2.Candidates))
	}
}

// Without a live sample confirming the server populates modules[].ammo_type,
// the catalog must be able to supply it from the module's type_id.
func TestBuildReloadPlanFallsBackToCatalogAmmoType(t *testing.T) {
	byType := func(itemID string) (string, string) {
		if itemID == "missile_launcher_i" {
			return "Missile Launcher I", "missile"
		}
		return ammoCatalog(itemID)
	}
	modules := []serverapi.ShipModule{
		weaponMod("w1", "missile_launcher_i", "Missile Launcher I", "", 0, 8, ""),
	}
	cargo := []serverapi.CargoItem{{ItemID: "standard_guided_missiles", Quantity: 10}}

	plan := buildReloadPlan(modules, cargo, byType)
	if len(plan) != 1 {
		t.Fatalf("want 1 entry, got %d", len(plan))
	}
	if plan[0].AmmoType != "missile" {
		t.Errorf("want ammo type from catalog, got %q", plan[0].AmmoType)
	}
	if len(plan[0].Candidates) != 1 {
		t.Errorf("want 1 candidate, got %d", len(plan[0].Candidates))
	}
}

// Candidates with equal standing sort by quantity then id, so the rendered
// command is stable between calls.
func TestBuildReloadPlanOrdersCandidatesDeterministically(t *testing.T) {
	modules := []serverapi.ShipModule{weaponMod("w1", "missile_launcher_ii", "ML II", "missile", 0, 12, "")}
	cargo := []serverapi.CargoItem{
		{ItemID: "standard_guided_missiles", Quantity: 4},
		{ItemID: "armor_buster_missiles", Quantity: 9},
	}
	plan := buildReloadPlan(modules, cargo, ammoCatalog)
	got := []string{plan[0].Candidates[0].ItemID, plan[0].Candidates[1].ItemID}
	want := []string{"armor_buster_missiles", "standard_guided_missiles"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidate %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestFormatReloadPlan(t *testing.T) {
	plan := []reloadPlanEntry{
		{
			WeaponID: "3f2a1b", WeaponName: "Missile Launcher II", WeaponTypeID: "missile_launcher_ii",
			AmmoType: "missile", CurrentAmmo: 0, MagazineSize: 12,
			Candidates: []reloadAmmoCandidate{{ItemID: "standard_guided_missiles", Name: "Standard Guided Missiles", Quantity: 42}},
		},
		{
			WeaponID: "91bdcc", WeaponName: "Railgun I", WeaponTypeID: "railgun_i",
			AmmoType: "railgun", CurrentAmmo: 20, MagazineSize: 20,
		},
	}
	got := formatReloadPlan(plan)
	want := []string{
		"Fitted weapons that take ammo (one reload per module, one tick each):",
		"  Missile Launcher II (missile_launcher_ii) — 0/12 missile  EMPTY",
		"    reload 3f2a1b standard_guided_missiles   (42 Standard Guided Missiles in cargo)",
		"  Railgun I (railgun_i) — 20/20 railgun  full",
		"    no railgun ammo in cargo",
		"Run `reload all` to reload every empty weapon in sequence.",
	}
	if len(got) != len(want) {
		t.Fatalf("want %d lines, got %d:\n%s", len(want), len(got), strings.Join(got, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n want %q\n  got %q", i, want[i], got[i])
		}
	}
}

func TestFormatReloadPlanNoAmmoWeapons(t *testing.T) {
	got := formatReloadPlan(nil)
	want := []string{"No fitted weapon takes ammo (energy weapons need no reload)."}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("want %q, got %q", want[0], strings.Join(got, "\n"))
	}
}

// `reload all` acts only on weapons that are actually empty and have ammo to
// load; a full magazine is skipped and a dry one with no cargo cannot be fixed.
func TestReloadAllTargets(t *testing.T) {
	plan := []reloadPlanEntry{
		{WeaponID: "empty-with-ammo", CurrentAmmo: 0, MagazineSize: 12,
			Candidates: []reloadAmmoCandidate{{ItemID: "standard_guided_missiles", Quantity: 42}}},
		{WeaponID: "full", CurrentAmmo: 20, MagazineSize: 20,
			Candidates: []reloadAmmoCandidate{{ItemID: "ferrous_slug_case", Quantity: 5}}},
		{WeaponID: "empty-no-ammo", CurrentAmmo: 0, MagazineSize: 8},
		{WeaponID: "partial-with-ammo", CurrentAmmo: 3, MagazineSize: 20,
			Candidates: []reloadAmmoCandidate{{ItemID: "ferrous_slug_case", Quantity: 5}}},
	}
	got := reloadAllTargets(plan)
	if len(got) != 1 {
		t.Fatalf("want 1 target, got %d: %+v", len(got), got)
	}
	if got[0].WeaponID != "empty-with-ammo" || got[0].AmmoItemID != "standard_guided_missiles" {
		t.Errorf("wrong target: %+v", got[0])
	}
}

// The lookup must read both halves of the pairing out of the same catalog:
// an ammo item's own ammo type, and a weapon module's required ammo type.
func TestKBAmmoLookup(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	ctx := context.Background()
	mag := 12
	if err := kb.StoreItems(ctx, []knowledge.CatalogItem{
		{
			ID: "standard_guided_missiles", Name: "Standard Guided Missiles", Category: "ammo",
			Ammo: &knowledge.ItemAmmo{AmmoType: "missile"},
		},
		{
			ID: "missile_launcher_ii", Name: "Missile Launcher II",
			Module: &knowledge.ItemModule{
				Type: "weapon", TypeID: "missile_launcher_ii",
				Weapon: &knowledge.ItemWeapon{AmmoType: "missile", MagazineSize: &mag},
			},
		},
		{ID: "iron_ore", Name: "Iron Ore", Category: "ore"},
	}); err != nil {
		t.Fatalf("StoreItems: %v", err)
	}

	lookup := kbAmmoLookup(ctx, kb)

	name, at := lookup("standard_guided_missiles")
	if name != "Standard Guided Missiles" || at != "missile" {
		t.Errorf("ammo item: got %q/%q", name, at)
	}
	name, at = lookup("missile_launcher_ii")
	if name != "Missile Launcher II" || at != "missile" {
		t.Errorf("weapon module: got %q/%q", name, at)
	}
	if _, at = lookup("iron_ore"); at != "" {
		t.Errorf("ore is not ammo, got %q", at)
	}
	if _, at = lookup("no_such_item"); at != "" {
		t.Errorf("unknown item, got %q", at)
	}
}

// With no knowledge base there is no catalog; the lookup must degrade to
// "nothing is ammo" rather than panic.
func TestKBAmmoLookupNilBase(t *testing.T) {
	lookup := kbAmmoLookup(context.Background(), nil)
	if name, at := lookup("standard_guided_missiles"); name != "" || at != "" {
		t.Errorf("want empty, got %q/%q", name, at)
	}
}
