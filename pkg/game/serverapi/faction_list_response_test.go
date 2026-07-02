package serverapi

import (
	"encoding/json"
	"testing"
)

func TestDecodeFactionListWithFacilities(t *testing.T) {
	// Real faction_list payload observed 2026-07-02 while docked at a station
	// where the caller's faction owns facilities.
	raw := `{"action":"faction_list","base_id":"grand_exchange_station","faction_facilities":[{"facility_id":"57cc8b5caf0bed5c3830a285443c1219","faction_service":"faction_intel","level":1,"name":"Intel Terminal","rent_per_cycle":45,"status":"active","type":"intel_terminal"},{"capacity":100,"facility_id":"3919f3323b6d8052e995cf57ec55ec15","faction_service":"faction_market","level":1,"name":"Market Runner","rent_per_cycle":10,"status":"active","type":"market_runner"},{"custom_name":"Bob's Iron Smeltery","facility_id":"f9ef7a1940efe4484fefb2e393209a53","faction_service":"","level":1,"name":"Iron Refinery","rent_per_cycle":21,"rental_fee_per_run":2,"status":"active","type":"iron_refinery"},{"capacity":200000,"facility_id":"c21d6929af60f4724237785d12981fd2","faction_service":"faction_storage","level":2,"name":"Faction Warehouse","rent_per_cycle":10,"status":"active","type":"faction_warehouse"}],"faction_id":"e727c0e918d994c72db2978fe5b18edc","faction_storage":{"credits":329427,"item_types":9,"rooms":0},"hint":"Use action 'faction_build' to build new faction facilities."}`
	var r FactionListResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.BaseID != "grand_exchange_station" || r.FactionID != "e727c0e918d994c72db2978fe5b18edc" {
		t.Errorf("base/faction id: got base=%q faction=%q", r.BaseID, r.FactionID)
	}
	if r.Hint == "" {
		t.Error("expected hint to be populated")
	}
	if r.FactionStorage == nil {
		t.Fatal("expected faction_storage to be populated")
	}
	if r.FactionStorage.Credits != 329427 || r.FactionStorage.ItemTypes != 9 || r.FactionStorage.Rooms != 0 {
		t.Errorf("storage summary: got %+v", r.FactionStorage)
	}
	if len(r.FactionFacilities) != 4 {
		t.Fatalf("expected 4 facilities, got %d", len(r.FactionFacilities))
	}
	// faction_storage facility carries capacity + level.
	var warehouse *FactionFacility
	for i := range r.FactionFacilities {
		if r.FactionFacilities[i].Type == "faction_warehouse" {
			warehouse = &r.FactionFacilities[i]
		}
	}
	if warehouse == nil {
		t.Fatal("faction_warehouse facility not decoded")
	}
	if warehouse.Capacity != 200000 || warehouse.Level != 2 || warehouse.FactionService != "faction_storage" || warehouse.Status != "active" {
		t.Errorf("warehouse: got %+v", warehouse)
	}
	// Refinery carries custom_name + rental_fee_per_run and no faction_service.
	var refinery *FactionFacility
	for i := range r.FactionFacilities {
		if r.FactionFacilities[i].Type == "iron_refinery" {
			refinery = &r.FactionFacilities[i]
		}
	}
	if refinery == nil {
		t.Fatal("iron_refinery facility not decoded")
	}
	if refinery.CustomName != "Bob's Iron Smeltery" || refinery.RentalFeePerRun != 2 || refinery.FactionService != "" {
		t.Errorf("refinery: got %+v", refinery)
	}
}

func TestDecodeFactionListPlain(t *testing.T) {
	// The classic paginated listing (no faction context) still decodes; the new
	// fields stay zero/nil.
	raw := `{"action":"faction_list","factions":[{"faction_id":"f1","name":"Alpha"}],"total_count":1}`
	var r FactionListResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Factions) != 1 || r.TotalCount != 1 {
		t.Errorf("plain listing: got %+v", r)
	}
	if r.FactionStorage != nil || len(r.FactionFacilities) != 0 || r.BaseID != "" {
		t.Errorf("expected empty faction-context fields, got storage=%v facilities=%d base=%q",
			r.FactionStorage, len(r.FactionFacilities), r.BaseID)
	}
}
