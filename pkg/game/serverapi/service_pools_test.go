package serverapi

import (
	"encoding/json"
	"testing"
)

// v0.572.0: stations carry finite recruitment / medical / marine-training
// pools that replenish from supplied facilities. Live shape from the
// operator's Haven facilities list, 2026-08-30.
func TestFacilityListResponse_DecodesServicePools(t *testing.T) {
	raw := `{"action":"list","base_id":"grand_exchange_station",
	"service_pools":{
		"marine_training":{"capacity":512,"refill_per_cycle":128,"remaining":512,"supply_item":"protein_rations"},
		"medical":{"capacity":2048,"refill_per_cycle":512,"remaining":2048,"supply_item":"medical_supplies"},
		"personnel":{"capacity":2048,"refill_per_cycle":512,"remaining":2048,"supply_item":"protein_rations","next_cycle_supply_required":4}}}`
	var resp FacilityListResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	sp := resp.ServicePools
	if sp == nil {
		t.Fatal("ServicePools nil")
	}
	if sp.Personnel.Remaining != 2048 || sp.Personnel.SupplyItem != "protein_rations" ||
		sp.Personnel.NextCycleSupplyRequired != 4 ||
		sp.Medical.Capacity != 2048 || sp.Medical.SupplyItem != "medical_supplies" ||
		sp.MarineTraining.RefillPerCycle != 128 {
		t.Errorf("pools = %+v", sp)
	}
}
