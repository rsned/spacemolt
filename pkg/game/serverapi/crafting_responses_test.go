package serverapi

import (
	"encoding/json"
	"testing"
)

func TestDecodeCraftJobQueued(t *testing.T) {
	raw := `{"action":"craft","job_id":"j1","recipe":"basic_iron_smelting","mode":"craft","venue":"Station Workshop","venue_type":"workshop","facility_id":"","runs":5,"effective_time_per_run":12.5,"est_completion_tick":1042,"escrowed":{"fee":0,"labor":0,"inputs":[{"item_id":"iron_ore","name":"Iron Ore","quantity":50}]},"message":"queued"}`
	var r CraftJobQueued
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.JobID != "j1" || r.Runs != 5 || r.EffectiveTimePerRun != 12.5 {
		t.Fatalf("bad decode: %+v", r)
	}
	if len(r.Escrowed.Inputs) != 1 || r.Escrowed.Inputs[0].ItemID != "iron_ore" || r.Escrowed.Inputs[0].Quantity != 50 {
		t.Fatalf("bad escrow inputs: %+v", r.Escrowed)
	}
}

func TestDecodeCraftQueueListing(t *testing.T) {
	raw := `{"action":"queue","jobs":[{"job_id":"j1","recipe":"r","mode":"craft","runs_total":10,"runs_done":3,"runs_remaining":7,"progress":0.3,"eta_ticks":40,"position":1,"orderer":"me","status":"running","facility_id":"f1"}]}`
	var r CraftQueueListing
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Jobs) != 1 || r.Jobs[0].RunsRemaining != 7 || r.Jobs[0].Progress != 0.3 {
		t.Fatalf("bad decode: %+v", r)
	}
}

func TestDecodeCraftBulkResponse(t *testing.T) {
	raw := `{"action":"craft","mode":"bulk","results":[{"index":0,"success":true,"job_id":"j1"},{"index":1,"success":false,"error":"no inputs","error_code":"insufficient_inputs"}],"summary":{"total":2,"succeeded":1,"failed":1}}`
	var r CraftBulkResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.Summary.Failed != 1 || len(r.Results) != 2 || r.Results[1].ErrorCode != "insufficient_inputs" {
		t.Fatalf("bad decode: %+v", r)
	}
}

func TestDecodeCraftDryRunResponse(t *testing.T) {
	raw := `{"action":"craft","dry_run":true,"recipe":"r","mode":"craft","quantity":20,"runs":2,"venue":"Workshop","venue_type":"workshop","facility_id":"","cost":{"fee":0,"labor":0,"inputs":[{"item_id":"iron_ore","name":"Iron Ore","quantity":200}]},"credits_total":0,"have_inputs":false,"have_credits":true,"effective_time_per_run":10,"est_completion_tick":1050,"message":"quote"}`
	var r CraftDryRunResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if !r.DryRun || r.Quantity != 20 || r.HaveInputs {
		t.Fatalf("bad decode: %+v", r)
	}
}

func TestDecodeCraftingUpdateEvent(t *testing.T) {
	raw := `{"tick":1043,"jobs":[{"job_id":"j1","recipe":"r","mode":"craft","venue":"Workshop","storage":"station","deposited":[{"item_id":"steel_plate","item_name":"Steel Plate","quantity":1}],"runs_done":1,"runs_remaining":4,"completed":false}]}`
	var r CraftingUpdateEvent
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.Tick != 1043 || len(r.Jobs) != 1 || r.Jobs[0].Deposited[0].ItemName != "Steel Plate" {
		t.Fatalf("bad decode: %+v", r)
	}
	if r.Jobs[0].Completed {
		t.Fatalf("expected not completed: %+v", r.Jobs[0])
	}
}

func TestRecycleResponseAliasesCraftJobQueued(t *testing.T) {
	var r RecycleResponse
	r.JobID = "j9"
	if CraftJobQueued(r).JobID != "j9" {
		t.Fatal("RecycleResponse is not an alias of CraftJobQueued")
	}
}

func TestDecodeFacilityOwnedResponse(t *testing.T) {
	raw := `{"action":"owned","facilities":[{"active":true,"base_id":"grand_exchange_station","base_name":"Grand Exchange Station","facility_id":"38f50d8a118ff2757ba3aaf0f9119672","name":"Signal Relay","rent_per_cycle":10,"system_id":"haven","type":"signal_relay"}],"hint":"Use action 'list' while docked for full per-facility detail at that station.","rent":{"est_rent_per_day":2580,"facilities":3,"note":"Rent is auto-deducted...","total_rent_per_cycle":30}}`
	var r FacilityOwnedResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.Action != "owned" || len(r.Facilities) != 1 || r.Facilities[0].RentPerCycle != 10 || !r.Facilities[0].Active {
		t.Fatalf("bad decode: %+v", r)
	}
	if r.Rent.TotalRentPerCycle != 30 || r.Rent.EstRentPerDay != 2580 || r.Rent.Facilities != 3 {
		t.Fatalf("bad rent: %+v", r.Rent)
	}
}
