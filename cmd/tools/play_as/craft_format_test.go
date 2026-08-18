package main

import (
	"strings"
	"testing"
)

// Variant 1 — single queued job (has job_id, no jobs/results/dry_run).
const craftJobQueuedSample = `{
  "action": "craft",
  "job_id": "job-abc-123",
  "recipe": "shield_cell",
  "mode": "ship",
  "venue": "station-7",
  "runs": 3,
  "effective_time_per_run": 45.5,
  "est_completion_tick": 9000,
  "message": "Job accepted.",
  "escrowed": {
    "fee": 50,
    "labor": 200,
    "inputs": [
      {"name": "Iron Ore", "item_id": "iron_ore", "quantity": 6},
      {"name": "Copper", "item_id": "copper", "quantity": 3}
    ]
  },
  "produces": [
    {"name": "Shield Cell", "quantity": 3}
  ]
}`

// Variant 2 — queue listing (has jobs array).
const craftQueueSample = `{
  "action": "craft_queue",
  "jobs": [
    {
      "job_id": "job-abc-123",
      "recipe": "shield_cell",
      "runs_done": 1,
      "runs_remaining": 2,
      "runs_total": 3,
      "progress": 0.3333,
      "eta_ticks": 50,
      "position": 1,
      "status": "running"
    },
    {
      "job_id": "job-def-456",
      "recipe": "power_cell",
      "runs_done": 0,
      "runs_remaining": 5,
      "runs_total": 5,
      "progress": 0.0,
      "eta_ticks": 120,
      "position": 2,
      "status": "queued"
    }
  ]
}`

// Variant 3 — bulk results (has results array).
const craftBulkSample = `{
  "action": "craft_bulk",
  "results": [
    {"index": 0, "success": true, "job_id": "job-aaa-111", "recipe": "shield_cell", "runs": 2},
    {"index": 1, "success": false, "recipe": "rare_module", "error": "missing inputs", "error_code": "insufficient_materials"}
  ],
  "summary": {
    "total": 2,
    "succeeded": 1,
    "failed": 1
  }
}`

// Variant 4 — dry-run quote (has dry_run: true).
const craftDryRunSample = `{
  "action": "craft",
  "dry_run": true,
  "recipe": "shield_cell",
  "quantity": 5,
  "runs": 5,
  "venue": "station-7",
  "credits_total": 750,
  "have_inputs": true,
  "have_credits": false,
  "effective_time_per_run": 45.5,
  "est_completion_tick": 9500,
  "message": "You lack credits.",
  "cost": {
    "fee": 50,
    "labor": 700,
    "inputs": [
      {"name": "Iron Ore", "quantity": 10},
      {"name": "Copper", "quantity": 5}
    ]
  }
}`

// craftJobQueuedWrapped is the same payload wrapped in an action_result frame.
const craftJobQueuedWrapped = `{"command":"craft","tick":42,"result":` + craftJobQueuedSample + `}`

func TestFormatCraftJobQueued_KeySubstrings(t *testing.T) {
	out := formatCraft([]byte(craftJobQueuedSample))
	for _, want := range []string{
		"job-abc-123",   // job ID
		"shield_cell",   // recipe name
		"station-7",     // venue
		"3 runs",        // run count — brief says "(%d runs, …)"
		"ETA tick 9000", // ETA tick
		"Shield Cell",   // produces entry
		"Iron Ore",      // escrowed input
		"labor 200",     // escrowed credits
		"Job accepted.", // message passthrough
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatCraft(queued) missing %q:\n%s", want, out)
		}
	}
}

func TestFormatCraftJobQueued_UnwrapsActionResult(t *testing.T) {
	out := formatCraft([]byte(craftJobQueuedWrapped))
	if !strings.Contains(out, "job-abc-123") {
		t.Errorf("action_result frame not unwrapped:\n%s", out)
	}
}

func TestFormatCraftQueue_KeySubstrings(t *testing.T) {
	out := formatCraft([]byte(craftQueueSample))
	for _, want := range []string{
		"Crafting queue", // header
		"2 jobs",         // job count
		"job-abc-123",    // first job ID
		"shield_cell",    // first recipe
		"1/3 runs",       // progress fraction
		"33%",            // progress pct (0.3333*100 ≈ 33)
		"running",        // status
		"job-def-456",    // second job ID
		"power_cell",     // second recipe
		"queued",         // second status
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatCraft(queue) missing %q:\n%s", want, out)
		}
	}
}

func TestFormatCraftQueue_Empty(t *testing.T) {
	raw := []byte(`{"jobs":[]}`)
	out := formatCraft(raw)
	if !strings.Contains(out, "empty") {
		t.Errorf("empty queue should say 'empty', got:\n%s", out)
	}
}

// TestFormatCraftQueue_LiveActionResultShape locks in the exact `craft queue`
// frame the live v0.389 server returns: action="queue" (not "craft_queue")
// wrapped in an action_result envelope. formatCraft must unwrap the envelope
// and render the queue — the dispatch bug that bypassed this entirely is what
// this guards against regressing on the formatter side.
func TestFormatCraftQueue_LiveActionResultShape(t *testing.T) {
	raw := []byte(`{"command":"craft","tick":1127360,"result":{"action":"queue","jobs":[{"eta_ticks":11,"external":true,"facility_id":"1041788bcb57d48bade4c683b65bc027","job_id":"be4cd3a20444e358c493116f984b9eaa","mode":"craft","orderer":"self","position":0,"produces":[{"item_id":"trade_cipher","name":"Trade Cipher","quantity":10}],"progress":0.27,"recipe":"Encode Trade Cipher","runs_done":0,"runs_remaining":3,"runs_total":3,"status":"queued","venue":"Haven Cipher Foundry"}]}}`)
	out := formatCraft(raw)
	if out == "" {
		t.Fatal("formatCraft returned empty for the live queue action_result shape")
	}
	for _, want := range []string{
		"Crafting queue",      // header
		"1 job",               // job count
		"Encode Trade Cipher", // recipe
		"0/3 runs",            // progress fraction
		"ETA 11 ticks",        // eta
		"queued",              // status
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatCraft(live queue) missing %q:\n%s", want, out)
		}
	}
}

// TestFormatCraftQueue_GroupsByFacility verifies that jobs spread across
// multiple facility queues are grouped under per-facility headers, each with
// its own position counter, rather than flattened into one ambiguous list
// where #0 appears more than once.
func TestFormatCraftQueue_GroupsByFacility(t *testing.T) {
	raw := []byte(`{"command":"craft","tick":1144309,"result":{"action":"queue","jobs":[` +
		`{"eta_ticks":28,"facility_id":"workshop:abc:grand_exchange_station","job_id":"j1","mode":"craft","position":0,"produces":[{"item_id":"target_painter_ii","name":"Target Painter II","quantity":1}],"progress":0.39,"recipe":"Build Target Painter II","runs_done":0,"runs_total":1,"status":"queued","venue":"Station Workshop"},` +
		`{"eta_ticks":33,"facility_id":"workshop:abc:grand_exchange_station","job_id":"j2","mode":"craft","position":1,"produces":[{"item_id":"autocannon_i","name":"Autocannon I","quantity":1}],"progress":0,"recipe":"Build Autocannon I","runs_done":0,"runs_total":1,"status":"queued","venue":"Station Workshop"},` +
		`{"eta_ticks":9,"facility_id":"f9ef7a19","job_id":"j3","mode":"craft","position":0,"produces":[{"item_id":"steel_plate","name":"Steel Plate","quantity":2}],"progress":0.65,"recipe":"Refine Steel","runs_done":10,"runs_total":100,"status":"active","venue":"Iron Refinery"}]}}`)
	out := formatCraft(raw)
	for _, want := range []string{
		"3 jobs across 2 facilities",                         // grouped header
		"Station Workshop @ grand_exchange_station — 2 jobs", // structured workshop id → station
		"Iron Refinery [f9ef7a19] — 1 job",                   // opaque faction hash, truncated
		"2x Steel Plate",                                     // produces rendered
		"10/100 runs done, current run 65%",                  // active job progress
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatCraft(grouped queue) missing %q:\n%s", want, out)
		}
	}
}

func TestFormatCraftBulk_KeySubstrings(t *testing.T) {
	out := formatCraft([]byte(craftBulkSample))
	for _, want := range []string{
		"Bulk craft",             // header
		"2 total",                // summary total
		"1 ok",                   // succeeded
		"1 failed",               // failed
		"job-aaa-111",            // successful job ID
		"shield_cell",            // successful recipe
		"rare_module",            // failed recipe
		"missing inputs",         // error message
		"insufficient_materials", // error code
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatCraft(bulk) missing %q:\n%s", want, out)
		}
	}
}

func TestFormatCraftDryRun_KeySubstrings(t *testing.T) {
	out := formatCraft([]byte(craftDryRunSample))
	for _, want := range []string{
		"Dry run",           // header
		"shield_cell",       // recipe
		"5",                 // quantity / runs
		"station-7",         // venue
		"ETA tick 9500",     // ETA tick
		"Iron Ore",          // input name
		"750",               // credits_total
		"labor 700",         // cost breakdown
		"fee 50",            // fee
		"You lack credits.", // message
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatCraft(dry_run) missing %q:\n%s", want, out)
		}
	}
}

func TestFormatCraftDryRun_HaveChecks(t *testing.T) {
	out := formatCraft([]byte(craftDryRunSample))
	// have_inputs: true → ✅, have_credits: false → ❌
	if !strings.Contains(out, "✅") {
		t.Errorf("dry run: have_inputs true should show ✅:\n%s", out)
	}
	if !strings.Contains(out, "❌") {
		t.Errorf("dry run: have_credits false should show ❌:\n%s", out)
	}
}

func TestFormatCraft_UnknownVariant_ReturnsEmpty(t *testing.T) {
	out := formatCraft([]byte(`{"action":"craft","some_other_key":42}`))
	if out != "" {
		t.Errorf("unknown variant should return empty, got:\n%s", out)
	}
}
