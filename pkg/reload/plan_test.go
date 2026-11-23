package reload

import (
	"context"

	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestBuildLoadoutSplitsAmmoFedFromAmmoFree(t *testing.T) {
	modules := []serverapi.ShipModule{
		{ID: "m1", TypeID: "autocannon_ii", Type: "weapon", AmmoType: "autocannon", MagazineSize: 20},
		{ID: "m2", TypeID: "pulse_laser_i", Type: "weapon"},
		{ID: "m3", TypeID: "shield_i", Type: "defense"},
	}
	cargo := []serverapi.CargoItem{{ItemID: "ac_rounds", Quantity: 4}}
	lookup := func(id string) (string, string) {
		if id == "ac_rounds" {
			return "AC rounds", "autocannon"
		}
		return "", ""
	}

	lo := BuildLoadout(modules, cargo, lookup)
	if len(lo.Plan) != 1 || lo.Plan[0].WeaponID != "m1" {
		t.Fatalf("Plan = %+v, want the autocannon only", lo.Plan)
	}
	if len(lo.Plan[0].Candidates) != 1 || lo.Plan[0].Candidates[0].ItemID != "ac_rounds" {
		t.Errorf("Candidates = %+v, want ac_rounds", lo.Plan[0].Candidates)
	}
	if len(lo.Free) != 1 || lo.Free[0] != (FreeWeapon{ModuleID: "m2", TypeID: "pulse_laser_i"}) {
		t.Errorf("Free = %+v, want the pulse laser only", lo.Free)
	}
	if got := BuildPlan(modules, cargo, lookup); len(got) != 1 {
		t.Errorf("BuildPlan len = %d, want 1", len(got))
	}
}

type catalogKB struct {
	knowledge.Base
	items map[string]*knowledge.CatalogItem
}

func (k catalogKB) GetItem(_ context.Context, id string) (*knowledge.CatalogItem, error) {
	return k.items[id], nil
}

// A spare weapon module in the hold is not ammunition for the fitted weapon
// of its kind: only ammo items are reload candidates.
func TestSpareWeaponInCargoIsNotAmmo(t *testing.T) {
	kb := catalogKB{items: map[string]*knowledge.CatalogItem{
		"railgun_i":         {Name: "Railgun I", Module: &knowledge.ItemModule{Weapon: &knowledge.ItemWeapon{AmmoType: "railgun"}}},
		"ferrous_slug_case": {Name: "Ferrous Slug Case", Ammo: &knowledge.ItemAmmo{AmmoType: "railgun"}},
	}}
	ctx := context.Background()
	modules := []serverapi.ShipModule{{ID: "m1", Name: "Railgun I", TypeID: "railgun_i", Type: "weapon", MagazineSize: 5}}
	build := func(cargo ...serverapi.CargoItem) []Entry {
		return BuildLoadoutWith(modules, cargo, KBLookup(ctx, kb), KBWeaponLookup(ctx, kb)).Plan
	}

	plan := build(serverapi.CargoItem{ItemID: "railgun_i", Quantity: 1})
	if len(plan) != 1 || plan[0].AmmoType != "railgun" {
		t.Fatalf("plan = %+v, want the railgun resolved to its ammo type from the catalog", plan)
	}
	if len(plan[0].Candidates) != 0 {
		t.Errorf("candidates = %+v, want none: a spare railgun is not ammunition", plan[0].Candidates)
	}
	plan = build(serverapi.CargoItem{ItemID: "railgun_i", Quantity: 1}, serverapi.CargoItem{ItemID: "ferrous_slug_case", Quantity: 2})
	if len(plan[0].Candidates) != 1 || plan[0].Candidates[0].ItemID != "ferrous_slug_case" {
		t.Errorf("candidates = %+v, want only ferrous_slug_case", plan[0].Candidates)
	}
}
