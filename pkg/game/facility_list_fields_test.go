package game

import (
	"encoding/json"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// openapi declares FacilityResponse with NO properties at all, so
// TestPassthroughStructsCoverOpenAPISchemas cannot guard the facility `list`
// response — live payloads are the only source of truth for it. Captured
// 2026-07-14 from the running fleet, after the monitor flagged both fields as
// unmodelled.
const liveFacilityListFragment = `{
  "action": "list",
  "base_id": "grand_exchange",
  "station_facilities": [],
  "player_facilities": [],
  "faction_facilities": [],
  "public_facilities": [],
  "player_rent": {"est_rent_per_day":6966,"facilities":3,"grace_cycles":260,
    "note":"Rent at this station only. Use action 'owned' for your bill across all stations.",
    "total_rent_per_cycle":81},
  "life_support": {"demand":62,"maintenance":[{"item_id":"water_ice","name":"Water Ice",
    "quantity_per_cycle":6}],"maintenance_cycle_ticks":100,"plants":1,"supply":106}
}`

func TestFacilityListDecodesRentAndLifeSupport(t *testing.T) {
	t.Parallel()

	var resp serverapi.FacilityListResponse
	if err := json.Unmarshal([]byte(liveFacilityListFragment), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// player_rent is the viewer's own bill at this station, distinct from
	// faction_rent (billed to the treasury).
	if got := resp.PlayerRent["est_rent_per_day"]; got != float64(6966) {
		t.Errorf("PlayerRent[est_rent_per_day] = %v, want 6966", got)
	}
	if got := resp.PlayerRent["total_rent_per_cycle"]; got != float64(81) {
		t.Errorf("PlayerRent[total_rent_per_cycle] = %v, want 81", got)
	}

	if resp.LifeSupport == nil {
		t.Fatal("LifeSupport did not decode")
	}
	// Supply must exceed demand or the station is starving; both must bind, and
	// a mis-tagged field would read as a plausible-looking zero.
	if resp.LifeSupport.Supply != 106 || resp.LifeSupport.Demand != 62 {
		t.Errorf("LifeSupport supply/demand = %d/%d, want 106/62",
			resp.LifeSupport.Supply, resp.LifeSupport.Demand)
	}
	if resp.LifeSupport.Plants != 1 {
		t.Errorf("LifeSupport.Plants = %d, want 1", resp.LifeSupport.Plants)
	}
	if resp.LifeSupport.MaintenanceCycleTick != 100 {
		t.Errorf("LifeSupport.MaintenanceCycleTick = %d, want 100", resp.LifeSupport.MaintenanceCycleTick)
	}
	if len(resp.LifeSupport.Maintenance) != 1 {
		t.Fatalf("LifeSupport.Maintenance has %d entries, want 1", len(resp.LifeSupport.Maintenance))
	}
	m := resp.LifeSupport.Maintenance[0]
	if m.ItemID != "water_ice" || m.QuantityPerCycle != 6 {
		t.Errorf("maintenance[0] = %+v, want water_ice x6", m)
	}
}

// Every key the server sends must be covered by the struct's json tags — an
// uncovered one decodes silently to zero and is what the API monitor flags.
func TestFacilityListCoversLivePayload(t *testing.T) {
	t.Parallel()

	expected, known := expectedFieldsForAction("list")
	if !known {
		t.Fatal(`action "list" has no entry in actionResponseTypes`)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(liveFacilityListFragment), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for key := range payload {
		if !expected[key] {
			t.Errorf("live field %q is not covered by FacilityListResponse", key)
		}
	}
}

// tenant_facilities appears on `list` at the viewer's own faction station for
// members with ManageBases: facilities other players/factions own there and
// the rent each pays us. Captured live at Conjunction Junction 2026-11-22;
// missed_rent_cycles is omitted when the tenant is paid up.
const liveTenantFacilitiesFragment = `{
  "action": "list",
  "base_id": "c976fa7df53c87f9616165d7d411b41a",
  "tenant_facilities": [
    {"facility_id":"21876b5c31b146c36dc9cd7bccb42e0f","faction_id":"5b565d6303a0bea2eaf559129c1686f5",
     "faction_name":"Data Bots","faction_tag":" DB ","missed_rent_cycles":3,"name":"Faction Lockbox",
     "operational":true,"rent_per_cycle":10,"type":"faction_lockbox"},
    {"facility_id":"d4408d03496f710c19ebb4890c20cbae","faction_id":"10f7b3f724fc29a7981dfb1dfdbf02f5",
     "faction_name":"You Sexy Mother Truckers","faction_tag":"YSMT","name":"Faction Lockbox",
     "operational":true,"rent_per_cycle":10,"type":"faction_lockbox"}
  ]
}`

func TestFacilityListDecodesTenantFacilities(t *testing.T) {
	t.Parallel()

	var resp serverapi.FacilityListResponse
	if err := json.Unmarshal([]byte(liveTenantFacilitiesFragment), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.TenantFacilities) != 2 {
		t.Fatalf("TenantFacilities len = %d, want 2", len(resp.TenantFacilities))
	}
	db := resp.TenantFacilities[0]
	if db.FacilityID != "21876b5c31b146c36dc9cd7bccb42e0f" || db.Type != "faction_lockbox" || db.Name != "Faction Lockbox" {
		t.Errorf("tenant[0] id/type/name = %q/%q/%q", db.FacilityID, db.Type, db.Name)
	}
	if db.FactionID != "5b565d6303a0bea2eaf559129c1686f5" || db.FactionName != "Data Bots" || db.FactionTag != " DB " {
		t.Errorf("tenant[0] owner = %q/%q/%q", db.FactionID, db.FactionName, db.FactionTag)
	}
	if db.RentPerCycle != 10 || db.MissedRentCycles != 3 || !db.Operational {
		t.Errorf("tenant[0] rent/missed/operational = %d/%d/%v, want 10/3/true",
			db.RentPerCycle, db.MissedRentCycles, db.Operational)
	}
	if got := resp.TenantFacilities[1].MissedRentCycles; got != 0 {
		t.Errorf("paid-up tenant MissedRentCycles = %d, want 0", got)
	}
}
