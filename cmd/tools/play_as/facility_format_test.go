package main

import (
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// TestFormatFacilityFactionList_ShowsFacilityID guards that the facility_id
// (needed as an argument to other facility commands) appears in the rendered
// faction-facility table. Uses the real server shape.
func TestFormatFacilityFactionList_ShowsFacilityID(t *testing.T) {
	raw := []byte(`{"base_id":"grand_exchange_station","faction_id":"e727c0e918d994c72db2978fe5b18edc","faction_facilities":[` +
		`{"active":true,"category":"faction","facility_id":"57cc8b5caf0bed5c3830a285443c1219","faction_service":"faction_intel","name":"Intel Terminal","type":"intel_terminal"},` +
		`{"active":true,"category":"faction","facility_id":"077a6773b401e4bd4737ab9649726f12","faction_service":"faction_admin","name":"Faction Desk","type":"faction_desk"}` +
		`]}`)
	out := formatFacilityFactionList(raw)
	if !strings.Contains(out, "Facility ID") {
		t.Errorf("header missing 'Facility ID' column:\n%s", out)
	}
	for _, id := range []string{"57cc8b5caf0bed5c3830a285443c1219", "077a6773b401e4bd4737ab9649726f12"} {
		if !strings.Contains(out, id) {
			t.Errorf("output missing facility_id %s:\n%s", id, out)
		}
	}
}

// TestFormatFacilityList_ShowsFacilityID guards the facility_id column in both
// the Personal and Faction sections of a plain `facility list`.
func TestFormatFacilityList_ShowsFacilityID(t *testing.T) {
	raw := []byte(`{"base_id":"grand_exchange_station",` +
		`"player_facilities":[{"active":true,"facility_id":"aaa111bbb222ccc333ddd444eee555ff","name":"Faction Quarters","personal_service":"faction_commons","rent_per_cycle":120,"rent_paid_until_tick":964000,"type":"faction_quarters"}],` +
		`"faction_facilities":[{"active":true,"facility_id":"57cc8b5caf0bed5c3830a285443c1219","faction_service":"faction_intel","name":"Intel Terminal","type":"intel_terminal"}]}`)
	out := formatFacilityList(raw)
	if strings.Count(out, "Facility ID") != 2 {
		t.Errorf("expected a 'Facility ID' column header in both sections:\n%s", out)
	}
	for _, id := range []string{"aaa111bbb222ccc333ddd444eee555ff", "57cc8b5caf0bed5c3830a285443c1219"} {
		if !strings.Contains(out, id) {
			t.Errorf("output missing facility_id %s:\n%s", id, out)
		}
	}
}

// TestFormatFacilityList_StationFacilitiesToggle guards that station-owned
// facilities are hidden by default and only rendered when the
// --show_station_facilities flag (showStationFacilities) is set.
func TestFormatFacilityList_StationFacilitiesToggle(t *testing.T) {
	raw := []byte(`{"base_id":"voss_redoubt_station","player_facilities":[],"faction_facilities":[],"station_facilities":[` +
		`{"active":true,"category":"service","facility_id":"a961e393a55e9f58961577940bf0a6ba","level":2,"maintenance_satisfied":false,"name":"Maintenance Deck","service":"repair","type":"maintenance_deck"},` +
		`{"active":true,"category":"production","facility_id":"77c6d42fa7fcfb7bfad637d26d1a5e7e","idle_reason":"no_inputs","level":1,"maintenance_satisfied":true,"name":"Fuel Reclamation Still","recipe_id":"scavenge_fuel_cells","type":"fuel_reclamation_still","production":{"backlog_ticks":0,"items_per_hour":40,"output_per_run":1,"public":true,"queued_items":0,"queued_runs":0,"recipe":"Scavenge Fuel Cells","rental_fee_per_run":15,"ticks_per_run":9}}]}`)

	// Default: station facilities hidden, and with no player/faction facilities
	// the list reports none.
	showStationFacilities = false
	out := formatFacilityList(raw)
	if strings.Contains(out, "Station ") || strings.Contains(out, "Maintenance Deck") {
		t.Errorf("station facilities should be hidden by default:\n%s", out)
	}
	if !strings.Contains(out, "(no facilities)") {
		t.Errorf("expected '(no facilities)' when only station facilities exist and toggle off:\n%s", out)
	}

	// Toggle on: services and production render under separate headings, with
	// the production facility's idle reason surfaced in its own block.
	showStationFacilities = true
	defer func() { showStationFacilities = false }()
	out = formatFacilityList(raw)
	for _, want := range []string{"Station Services (1):", "Maintenance Deck", "Station Production (1):", "Fuel Reclamation Still", "⚙ Scavenge Fuel Cells"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q with toggle on:\n%s", want, out)
		}
	}
}

// TestFormatFacilityForSale_Empty renders the no-listings case for
// `facility browse_for_sale` rather than falling back to a stale facility list.
func TestFormatFacilityForSale_Empty(t *testing.T) {
	raw := []byte(`{"action":"browse_for_sale","base_id":"grand_exchange_station","base_name":"Grand Exchange Station","count":0,"listings":[]}`)
	out := formatFacility(raw)
	if !strings.Contains(out, "No facilities listed for sale at Grand Exchange Station") {
		t.Errorf("empty browse_for_sale should report none for sale, got:\n%s", out)
	}
	if strings.Contains(out, "Personal:") || strings.Contains(out, "Faction:") {
		t.Errorf("must not render a facility list for browse_for_sale:\n%s", out)
	}
}

// TestFormatFacilityForSale_Listings renders listings generically, surfacing the
// facility_id (needed by buy_listing/cancel_listing) first.
func TestFormatFacilityForSale_Listings(t *testing.T) {
	raw := []byte(`{"action":"browse_for_sale","base_name":"Grand Exchange Station","count":1,"listings":[` +
		`{"facility_id":"57cc8b5caf0bed5c3830a285443c1219","type":"workbench","name":"Workbench","price":5000,"seller":"craftsman-1"}]}`)
	out := formatFacility(raw)
	for _, want := range []string{
		"Facilities for sale at Grand Exchange Station (1)",
		"facility_id: 57cc8b5caf0bed5c3830a285443c1219",
		"name: Workbench",
		"seller: craftsman-1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestFormatFacilityJobList_RendersQueue guards that facility job_list routes to
// formatCraftQueue and produces the expected job table. The server returns the
// same {action, jobs:[...]} shape as craft action=queue.
func TestFormatFacilityJobList_RendersQueue(t *testing.T) {
	raw := []byte(`{"action":"job_list","jobs":[` +
		`{"job_id":"job-abc-123","recipe":"shield_cell","runs_done":1,"runs_remaining":2,"runs_total":3,"progress":0.3333,"eta_ticks":50,"position":1,"status":"running"},` +
		`{"job_id":"job-def-456","recipe":"power_cell","runs_done":0,"runs_remaining":5,"runs_total":5,"progress":0.0,"eta_ticks":120,"position":2,"status":"queued"}` +
		`]}`)
	out := formatFacility(raw)
	for _, want := range []string{
		"Crafting queue",
		"2 jobs",
		"job-abc-123",
		"shield_cell",
		"1/3",
		"running",
		"job-def-456",
		"power_cell",
		"queued",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("facility job_list: missing %q in:\n%s", want, out)
		}
	}
}

// TestFormatFacilityJobList_Empty guards the empty-queue case for facility job_list.
func TestFormatFacilityJobList_Empty(t *testing.T) {
	raw := []byte(`{"action":"job_list","jobs":[]}`)
	out := formatFacility(raw)
	if !strings.Contains(out, "empty") {
		t.Errorf("facility job_list empty queue should say 'empty', got:\n%s", out)
	}
}

// TestFormatFacilityActionMessage_SetOutputPrice guards that a set_output_price
// response with a server message is rendered with action and message text.
func TestFormatFacilityActionMessage_SetOutputPrice(t *testing.T) {
	raw := []byte(`{"action":"set_output_price","facility_id":"fac-111","message":"Output price updated."}`)
	out := formatFacilityActionMessage(raw)
	for _, want := range []string{
		"facility set_output_price",
		"Output price updated.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatFacilityActionMessage(set_output_price): missing %q in:\n%s", want, out)
		}
	}
}

// TestFormatFacilityActionMessage_JobCancelWithID guards that a job_cancel
// response surfaces both the message and the job_id.
func TestFormatFacilityActionMessage_JobCancelWithID(t *testing.T) {
	raw := []byte(`{"action":"job_cancel","facility_id":"fac-111","job_id":"job-abc-123","message":"Job cancelled."}`)
	out := formatFacilityActionMessage(raw)
	for _, want := range []string{
		"facility job_cancel",
		"Job cancelled.",
		"job job-abc-123",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatFacilityActionMessage(job_cancel): missing %q in:\n%s", want, out)
		}
	}
}

// TestFormatFacilityActionMessage_NoMessage guards that an empty message field
// causes formatFacilityActionMessage to return "" (triggering JSON fallback).
func TestFormatFacilityActionMessage_NoMessage(t *testing.T) {
	raw := []byte(`{"action":"job_add","facility_id":"fac-111","job_id":"job-new-999"}`)
	out := formatFacilityActionMessage(raw)
	if out != "" {
		t.Errorf("formatFacilityActionMessage with no message should return empty, got:\n%s", out)
	}
}

// TestFormatFacilityList_ShowsProductionDetails guards that production-category
// station facilities surface their busyness and rental cost when
// --show_station_facilities is active.
func TestFormatFacilityList_ShowsProductionDetails(t *testing.T) {
	showStationFacilities = true
	defer func() { showStationFacilities = false }()
	// ticks_per_run arrives fractional from the live server (e.g. 13.2), so the
	// production struct must decode it as a float — an int field here makes
	// json.Unmarshal fail and the whole `facility list` fall back to raw JSON.
	raw := []byte(`{"base_id":"grand_exchange_station","station_facilities":[{"active":true,"category":"production","description":"Pressurized containment lab...","facility_id":"42eb7b38","level":1,"maintenance_satisfied":true,"name":"Argon Cell Lab","recipe_id":"synthesize_argon_power_cell","type":"argon_cell_lab","production":{"backlog_ticks":0,"items_per_hour":22,"output_per_run":2,"public":true,"queued_items":0,"queued_runs":0,"recipe":"Synthesize Argon Power Cell","rental_fee_per_run":225,"ticks_per_run":13.2}}]}`)
	out := formatFacilityList(raw)
	// Recipe shows in the Type column with the ⚙ prefix; ticks/run renders to
	// two decimals ("13.20") under the two-line "Cycle / tick/run" header.
	for _, want := range []string{"Station Production (1):", "tick/run", "Argon Cell Lab", "⚙ Synthesize Argon Power Cell", "225", "22", "13.20"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatFacilityList output missing %q\n%s", want, out)
		}
	}
}

// TestFormatFacility_MutationActionsRoute guards that the formatFacility
// dispatcher routes mutation sub-actions to formatFacilityActionMessage.
func TestFormatFacility_MutationActionsRoute(t *testing.T) {
	for _, action := range []string{"job_cancel", "job_reorder", "set_output_price", "set_access", "upgrade"} {
		raw := []byte(`{"action":"` + action + `","message":"Done."}`)
		out := formatFacility(raw)
		if !strings.Contains(out, "facility "+action) {
			t.Errorf("formatFacility(%s): missing action label in:\n%s", action, out)
		}
		if !strings.Contains(out, "Done.") {
			t.Errorf("formatFacility(%s): missing message in:\n%s", action, out)
		}
	}
}

// TestCraftingUpdateLines guards the human-readable rendering of a
// crafting_update push event (deposit + remaining runs).
func TestCraftingUpdateLines(t *testing.T) {
	ev := serverapi.CraftingUpdateEvent{
		Tick: 1133055,
		Jobs: []serverapi.CraftingUpdateJob{{
			JobID:         "efecc6bf",
			Recipe:        "Draw Copper Piping",
			Storage:       "station",
			RunsDone:      1,
			RunsRemaining: 22,
			Deposited:     []serverapi.CraftingUpdateDeposit{{ItemID: "copper_piping", ItemName: "Copper Piping", Quantity: 4}},
		}},
	}
	lines := craftingUpdateLines(ev)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %v", len(lines), lines)
	}
	want := "Crafted 4 copper_piping to storage. 22 runs remaining"
	if lines[0] != want {
		t.Errorf("line = %q, want %q", lines[0], want)
	}

	// Completed job reads "Complete" instead of a remaining count.
	ev.Jobs[0].Completed = true
	ev.Jobs[0].RunsRemaining = 0
	lines = craftingUpdateLines(ev)
	if len(lines) != 1 || !strings.Contains(lines[0], "Complete") {
		t.Errorf("completed job line = %v, want it to contain Complete", lines)
	}
}

// TestPartitionFlagsKV guards that bare key=value tokens (e.g. action=queue)
// fold into flags while plain identifiers stay positional, so `craft
// action=queue` is recognized as the queue listing rather than a recipe id.
func TestPartitionFlagsKV(t *testing.T) {
	pos, flags := partitionFlagsKV([]string{"action=queue"})
	if len(pos) != 0 {
		t.Errorf("action=queue should not be positional, got %v", pos)
	}
	if flags["action"] != "queue" {
		t.Errorf("flags[action] = %q, want queue", flags["action"])
	}

	// Plain recipe id stays positional; --flags and key=value coexist.
	pos, flags = partitionFlagsKV([]string{"refine_steel", "5", "--deliver_to=faction", "preset=fast"})
	if len(pos) != 2 || pos[0] != "refine_steel" || pos[1] != "5" {
		t.Errorf("positional = %v, want [refine_steel 5]", pos)
	}
	if flags["deliver_to"] != "faction" || flags["preset"] != "fast" {
		t.Errorf("flags = %v, want deliver_to=faction preset=fast", flags)
	}
}

// TestFacilityPositionalKeys guards that bare positional arguments map to the
// correct payload keys per action — in particular that `facility set_access
// public` sends access=public rather than facility_type=public.
func TestFacilityPositionalKeys(t *testing.T) {
	cases := map[string][]string{
		"set_access":       {"access"},
		"set_output_price": {"item_id", "price"},
		"buy_listing":      {"listing_id"},
		"cancel_listing":   {"listing_id"},
		"build":            {"facility_type"},
		"types":            {"facility_type"},
		"unknown_action":   {"facility_type"},
	}
	for action, want := range cases {
		got := facilityPositionalKeys(action)
		if len(got) != len(want) {
			t.Errorf("facilityPositionalKeys(%q) = %v, want %v", action, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("facilityPositionalKeys(%q)[%d] = %q, want %q", action, i, got[i], want[i])
			}
		}
	}
}

// TestBuildFacilityPayload guards the `facility ...` arg parser, in particular
// that the --flag=value form is split correctly (regression: --deliver_to=faction
// was folding "=faction" into the key and swallowing the next token), that the
// --flag value form coexists with it, that numeric fields are coerced to ints,
// and that --show_station_facilities is a client-side toggle kept out of the
// payload.
func TestBuildFacilityPayload(t *testing.T) {
	// The reported command: mix of --flag value and --flag=value plus a numeric.
	parts := []string{
		"facility", "job_add",
		"--recipe_id", "manufacture_fuel_basic",
		"--facility_id", "fb24fd71ecb5590893fa64e8ba9bf3ed",
		"--deliver_to=faction",
		"--quantity", "1000",
	}
	payload, showStation, err := buildFacilityPayload(parts)
	if err != nil {
		t.Fatalf("buildFacilityPayload error: %v", err)
	}
	if showStation {
		t.Errorf("showStation = true, want false")
	}
	want := map[string]any{
		"action":      "job_add",
		"recipe_id":   "manufacture_fuel_basic",
		"facility_id": "fb24fd71ecb5590893fa64e8ba9bf3ed",
		"deliver_to":  "faction",
		"quantity":    1000, // coerced from "1000"
	}
	if len(payload) != len(want) {
		t.Fatalf("payload = %v, want %v", payload, want)
	}
	for k, v := range want {
		if payload[k] != v {
			t.Errorf("payload[%q] = %#v, want %#v", k, payload[k], v)
		}
	}

	// set_access public -> access=public (positional mapping), not facility_type.
	payload, _, err = buildFacilityPayload([]string{"facility", "set_access", "public"})
	if err != nil {
		t.Fatalf("set_access error: %v", err)
	}
	if payload["access"] != "public" || payload["facility_type"] != nil {
		t.Errorf("set_access payload = %v, want access=public", payload)
	}

	// --show_station_facilities is a toggle, not a payload field; bare key=value
	// (action=list) resolves the action.
	payload, showStation, err = buildFacilityPayload([]string{"facility", "action=list", "--show_station_facilities"})
	if err != nil {
		t.Fatalf("show_station error: %v", err)
	}
	if !showStation {
		t.Errorf("showStation = false, want true")
	}
	if _, present := payload["show_station_facilities"]; present {
		t.Errorf("show_station_facilities leaked into payload: %v", payload)
	}
	if payload["action"] != "list" {
		t.Errorf("payload[action] = %v, want list", payload["action"])
	}

	// Missing action is an error.
	if _, _, err := buildFacilityPayload([]string{"facility", "--recipe_id", "x"}); err == nil {
		t.Errorf("expected error for missing action, got nil")
	}
}
