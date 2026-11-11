package serverapi

import (
	"encoding/json"
	"testing"
)

// v0.689.0 hazard gear: modules carry hazard_resistance (per hazard type),
// hazard_mining_reduction and hazard_scan_filter.
func TestShipModuleDecodesHazardGear(t *testing.T) {
	raw := `{"id":"m1","type_id":"shear_bracing_ii","name":"Shear Bracing II","type":"defense",
		"hazard_resistance":{"tidal_shear":45,"debris":45},
		"hazard_mining_reduction":50,"hazard_scan_filter":75}`
	var m ShipModule
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if m.HazardResistance[HazardTidalShear] != 45 || m.HazardResistance[HazardDebris] != 45 {
		t.Errorf("HazardResistance = %v", m.HazardResistance)
	}
	if m.HazardMiningReduction != 50 || m.HazardScanFilter != 75 {
		t.Errorf("mining reduction %d, scan filter %d", m.HazardMiningReduction, m.HazardScanFilter)
	}
}
