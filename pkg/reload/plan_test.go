package reload

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
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
