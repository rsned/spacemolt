package main

import "testing"

// mining_power does not follow the module's type. All twelve gas/ice/rad
// harvesters are type="utility" yet carry mining_power (8/18/35/60 by tier), so
// routing the detail row on type alone dropped every one of them: item_mining
// held 13 of the catalog's 25 powered modules, and item_utilities.harvest_power
// was empty for all twelve.
//
// That under-counts a fit's summed mining power, which is the dangerous
// direction: mining succeeds only while the sum is BELOW a resource's
// supported_power, so a rig reading 0 looks safely under a ceiling it is
// actually over.
func TestConvertModule_HarvestersKeepMiningPower(t *testing.T) {
	m := convertModule(CatalogItemJSON{
		ID: "gas_harvester_ii", Type: "utility", Slot: "utility",
		Special: "gas_harvesting", MiningPower: 18,
	})
	if m.Mining == nil {
		t.Fatal("Mining detail is nil for a utility module carrying mining_power")
	}
	if m.Mining.MiningPower != 18 {
		t.Errorf("MiningPower = %d, want 18", m.Mining.MiningPower)
	}
	if m.Utility == nil {
		t.Error("Utility detail must still be populated; a harvester is both")
	}
}

func TestConvertModule_MiningTypeStillRoutes(t *testing.T) {
	m := convertModule(CatalogItemJSON{
		ID: "mining_laser_iv", Type: "mining", Slot: "utility", MiningPower: 40,
	})
	if m.Mining == nil || m.Mining.MiningPower != 40 {
		t.Fatalf("Mining = %+v, want MiningPower 40", m.Mining)
	}
}

// A module with no mining power must not gain an empty detail row.
func TestConvertModule_NonMiningModulesGetNoMiningRow(t *testing.T) {
	for _, c := range []CatalogItemJSON{
		{ID: "pulse_laser_iii", Type: "weapon", Damage: 28},
		{ID: "shield_booster_iv", Type: "defense"},
		{ID: "cloaking_device_i", Type: "utility"},
	} {
		if m := convertModule(c); m.Mining != nil {
			t.Errorf("%s: Mining = %+v, want nil", c.ID, m.Mining)
		}
	}
}
