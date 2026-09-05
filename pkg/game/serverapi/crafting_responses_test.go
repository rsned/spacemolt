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
	// Verbatim live `craft queue` payload (2026-07-24): carries venue + a
	// produces[] entry whose quantity is the per-run output (4 copper_piping ×
	// 225 runs), plus the top-level kind/total_jobs discriminators added
	// server-side. Guards the CraftQueueListing/CraftJobEntry shape against drift.
	raw := `{"action":"queue","kind":"queue","total_jobs":1,"jobs":[{"eta_ticks":144,"facility_id":"workshop:abc:grand_exchange_station","job_id":"340c12","mode":"craft","orderer":"self","position":0,"produces":[{"item_id":"copper_piping","name":"Copper Piping","quantity":4}],"progress":0.2066,"recipe":"Draw Copper Piping","runs_done":39,"runs_remaining":186,"runs_total":225,"status":"active","venue":"Station Workshop"}]}`
	var r CraftQueueListing
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.Action != "queue" || len(r.Jobs) != 1 {
		t.Fatalf("bad decode: %+v", r)
	}
	if r.Kind != "queue" || r.TotalJobs != 1 {
		t.Fatalf("kind/total_jobs mismatch: %+v", r)
	}
	j := r.Jobs[0]
	if j.RunsTotal != 225 || j.RunsDone != 39 || j.RunsRemaining != 186 {
		t.Fatalf("runs mismatch: %+v", j)
	}
	if j.Venue != "Station Workshop" || j.Status != "active" || j.ETATicks != 144 {
		t.Fatalf("field mismatch: %+v", j)
	}
	if len(j.Produces) != 1 || j.Produces[0].ItemID != "copper_piping" || j.Produces[0].Quantity != 4 {
		t.Fatalf("produces mismatch: %+v", j.Produces)
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

// TestCraftQueueListing_FacilityScope decodes a real job_list reply from a
// facility query. The top-level facility_id/venue were missing from the struct
// until 2026-09-05, so a listing scoped to one facility decoded as if it were
// unscoped -- and an empty one could not be told apart from "no such facility".
// The job-level base_id/base_name/deliver_to were missing too: a facility job
// keeps running while its owner is away, so the station it runs at is not
// implied by the caller's position the way hand-crafting's is.
func TestCraftQueueListing_FacilityScope(t *testing.T) {
	const raw = `{"action":"job_list","facility_id":"b659c3602da933e414c9fa91a072968a",
	"jobs":[{"base_id":"grand_exchange_station","base_name":"Grand Exchange Station",
	"deliver_to":"storage","eta_ticks":8,"facility_id":"b659c3602da933e414c9fa91a072968a",
	"job_id":"0c6507080966edae17615cc903670c8b","mode":"craft","orderer":"self","position":0,
	"produces":[{"item_id":"nanoplastic_composite","name":"Nanoplastic Composite","quantity":3}],
	"progress":0.878306878306878,"recipe":"Extrude Nanoplastic","runs_done":6,
	"runs_remaining":28,"runs_total":34,"status":"active","venue":"Polymer Extruder"}],
	"total_jobs":1,"venue":"Polymer Extruder"}`

	var got CraftQueueListing
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.FacilityID != "b659c3602da933e414c9fa91a072968a" {
		t.Errorf("FacilityID = %q, want the queried facility", got.FacilityID)
	}
	if got.Venue != "Polymer Extruder" {
		t.Errorf("Venue = %q, want Polymer Extruder", got.Venue)
	}
	if len(got.Jobs) != 1 {
		t.Fatalf("Jobs = %d, want 1", len(got.Jobs))
	}
	j := got.Jobs[0]
	if j.BaseID != "grand_exchange_station" || j.BaseName != "Grand Exchange Station" {
		t.Errorf("job base = %q/%q, want grand_exchange_station", j.BaseID, j.BaseName)
	}
	if j.DeliverTo != "storage" {
		t.Errorf("DeliverTo = %q, want storage", j.DeliverTo)
	}
	// The counters the worker polls to decide a job is done.
	if j.RunsTotal != 34 || j.RunsDone != 6 || j.RunsRemaining != 28 {
		t.Errorf("runs = %d/%d/%d, want 34/6/28", j.RunsTotal, j.RunsDone, j.RunsRemaining)
	}
	if j.Status != "active" || j.Venue != "Polymer Extruder" {
		t.Errorf("status/venue = %q/%q", j.Status, j.Venue)
	}
}
