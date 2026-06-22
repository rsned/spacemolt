package main

import (
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// realStationPassengers is a verbatim list_station_passengers response from the
// live server (Grand Exchange Station, 9 waiting).
const realStationPassengers = `{"count":9,"station":"Grand Exchange Station","waiting":[{"bio":"A loan shark.","citizen_id":"loan_shark_demme","citizenship":"nebula","class":"first","destination":"mera_sanctum_station","destination_name":"Mera Sanctum Station","name":"Auberon Demme"},{"bio":"A station caterer.","citizen_id":"fatuya_perrari_fossou","citizenship":"nebula","class":"economy","destination":"market_prime_exchange","destination_name":"Market Prime Exchange","name":"Fatuya Perrari-Fossou"},{"bio":"A customs inspector.","citizen_id":"dot_tinley","citizenship":"outerrim","class":"business","destination":"frontier_station","destination_name":"Frontier Station","name":"Dot Tinley"},{"bio":"A clock restorer.","citizen_id":"lin_mantari","citizenship":"nebula","class":"first","destination":"the_levy_customs_station","destination_name":"The Levy Customs Station","name":"Lin Mantari"},{"bio":"A fabricator.","citizen_id":"gus_lucky_tarplock","citizenship":"outerrim","class":"business","destination":"deep_range_outpost","destination_name":"Deep Range Outpost","name":"Gus 'Lucky' Tarplock"},{"bio":"A man with a towel.","citizen_id":"arthur_dint","citizenship":"nebula","class":"economy","destination":"market_prime_exchange","destination_name":"Market Prime Exchange","name":"Arthur Dint"},{"bio":"A corporate executive.","citizen_id":"famis_mantari","citizenship":"nebula","class":"first","destination":"traders_rest_resort_station","destination_name":"Trader's Rest Resort Station","name":"Famis Mantari"},{"bio":"An alphabetical man.","citizen_id":"alphabetical_renn","citizenship":"nebula","class":"economy","destination":"market_prime_exchange","destination_name":"Market Prime Exchange","name":"Quentin Renn"},{"bio":"A fixer.","citizen_id":"fixer_dao","citizenship":"nebula","class":"first","destination":"sable_port_station","destination_name":"Sable Port Station","name":"Lin Dao"}]}`

func TestFormatStationPassengers_RealSample(t *testing.T) {
	out := formatStationPassengers([]byte(realStationPassengers))

	// Header with station name and total.
	if !strings.Contains(out, "Passengers waiting at Grand Exchange Station — 9 total") {
		t.Fatalf("missing header, got:\n%s", out)
	}

	// Destinations grouped, with counts. Headers include the station id (which
	// load_passenger requires) in the form "Name [id]".
	for _, want := range []string{
		"Deep Range Outpost [deep_range_outpost] (1)",
		"Frontier Station [frontier_station] (1)",
		"Market Prime Exchange [market_prime_exchange] (3)",
		"Mera Sanctum Station [mera_sanctum_station] (1)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing destination group %q, got:\n%s", want, out)
		}
	}

	// Destinations are alphabetical: Deep Range < Frontier < Market Prime.
	if i, j := strings.Index(out, "Deep Range Outpost"), strings.Index(out, "Frontier Station"); i > j {
		t.Errorf("destinations not alphabetical: Deep Range at %d, Frontier at %d", i, j)
	}
	if i, j := strings.Index(out, "Frontier Station"), strings.Index(out, "Market Prime Exchange"); i > j {
		t.Errorf("destinations not alphabetical: Frontier at %d, Market Prime at %d", i, j)
	}

	// Within Market Prime Exchange, the 3 economy passengers are name-sorted.
	mpx := out[strings.Index(out, "Market Prime Exchange"):]
	a := strings.Index(mpx, "Arthur Dint")
	f := strings.Index(mpx, "Fatuya Perrari-Fossou")
	q := strings.Index(mpx, "Quentin Renn")
	if a < 0 || a >= f || f >= q {
		t.Errorf("economy passengers not name-sorted (Arthur=%d Fatuya=%d Quentin=%d):\n%s", a, f, q, mpx)
	}

	// Class label and per-passenger detail (citizenship + citizen_id) present.
	if !strings.Contains(out, "economy:") {
		t.Errorf("missing class label, got:\n%s", out)
	}
	if !strings.Contains(out, "[nebula]") || !strings.Contains(out, "[outerrim]") {
		t.Errorf("missing citizenship brackets, got:\n%s", out)
	}
	if !strings.Contains(out, "loan_shark_demme") {
		t.Errorf("missing citizen_id, got:\n%s", out)
	}
}

func TestFormatStationPassengers_SystemAndFare(t *testing.T) {
	raw := `{"count":3,"station":"Grand Exchange Station","waiting":[` +
		`{"citizen_id":"mabani_perranda","name":"Mabani Perranda","citizenship":"nebula","class":"first","destination":"central_nexus","destination_name":"Central Nexus","destination_system":"Nexus Prime","estimated_fare":17085},` +
		`{"citizen_id":"embezzler_pratt","name":"Wendell Pratt","citizenship":"nebula","class":"business","destination":"sable_port_station","destination_name":"Sable Port Station","destination_system":"Barnard 44","estimated_fare":9750},` +
		`{"citizen_id":"kedan_fossari","name":"Kedan Fossari","citizenship":"nebula","class":"economy","destination":"market_prime_exchange","destination_name":"Market Prime Exchange","destination_system":"Market Prime","estimated_fare":297}]}`
	out := formatStationPassengers([]byte(raw))

	// Destination system appended to the group header after "Name [id]".
	if !strings.Contains(out, "Central Nexus [central_nexus] · Nexus Prime") {
		t.Errorf("missing destination system in header, got:\n%s", out)
	}
	// Estimated fares rendered with a ~ marker, right-aligned to a shared width
	// (max digits = 5, so 297 pads to "  297").
	for _, want := range []string{"~17085 cr", "~ 9750 cr", "~  297 cr"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing estimated fare %q, got:\n%s", want, out)
		}
	}
}

func TestDestSystemJumps(t *testing.T) {
	// origin - hub - market   and   origin - sable (chain)
	systems := []knowledge.System{
		{ID: "origin_sys", Name: "Origin Prime"},
		{ID: "hub_sys", Name: "Hub Central"},
		{ID: "market_sys", Name: "Market Prime"},
		{ID: "sable_sys", Name: "Barnard 44"},
		{ID: "island_sys", Name: "Lost Reach"}, // present but unconnected
	}
	conns := []knowledge.Connection{
		{FromSystem: "origin_sys", ToSystem: "hub_sys"},
		{FromSystem: "hub_sys", ToSystem: "market_sys"},
		{FromSystem: "origin_sys", ToSystem: "sable_sys"},
	}
	dests := []string{
		"Market Prime", // 2 jumps (by name)
		"Barnard 44",   // 1 jump
		"Origin Prime", // 0 jumps (same system)
		"Lost Reach",   // resolvable but unreachable -> omitted
		"Nowhere",      // unresolvable -> omitted
	}

	got := destSystemJumps("origin_sys", systems, conns, dests)

	want := map[string]int{"Market Prime": 2, "Barnard 44": 1, "Origin Prime": 0}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for name, j := range want {
		if got[name] != j {
			t.Errorf("jumps to %q = %d, want %d", name, got[name], j)
		}
	}
	for _, omitted := range []string{"Lost Reach", "Nowhere"} {
		if _, ok := got[omitted]; ok {
			t.Errorf("%q should be omitted (unreachable/unresolvable), got %d", omitted, got[omitted])
		}
	}
}

func TestRenderPax_JumpsAndPerHop(t *testing.T) {
	// Two destinations: one 3 jumps away, one same-system. Per-jump = fare/jumps.
	items := []paxItem{
		{Name: "Far Traveler", Class: "first", DestName: "Deep Station", DestID: "deep", DestSystem: "Far Reach", EstFare: 9000, Jumps: 3},
		{Name: "Local Hop", Class: "economy", DestName: "Home Dock", DestID: "home", DestSystem: "Origin Prime", EstFare: 300, Jumps: 0},
	}
	out := renderPaxByDestination(items, paxClassOrderStation, paxFareEstimate)

	// Header carries the jump distance for the multi-jump destination and the
	// same-system marker for the local one.
	if !strings.Contains(out, "Deep Station [deep] · Far Reach · 3 jumps") {
		t.Errorf("missing jump distance in header, got:\n%s", out)
	}
	if !strings.Contains(out, "Home Dock [home] · Origin Prime · same system") {
		t.Errorf("missing same-system marker, got:\n%s", out)
	}
	// Per-jump fare appears for the multi-jump passenger (9000/3 = 3000) but not
	// for the same-system one (no jumps to divide by).
	if !strings.Contains(out, "~3000/jump") {
		t.Errorf("missing per-jump fare, got:\n%s", out)
	}
	local := out[strings.Index(out, "Local Hop"):]
	if strings.Contains(local, "/jump") {
		t.Errorf("same-system passenger should have no per-jump fare, got:\n%s", local)
	}
}

func TestFormatStationPassengers_Empty(t *testing.T) {
	out := formatStationPassengers([]byte(`{"station":"Frontier Station","count":0,"waiting":[]}`))
	if !strings.Contains(out, "No passengers waiting at Frontier Station") {
		t.Errorf("expected empty-station message, got:\n%s", out)
	}
}

func TestFormatStationPassengers_ErrorFrame(t *testing.T) {
	for _, raw := range []string{`{"error":"not_docked","message":"You must be docked."}`, `{}`} {
		if out := formatStationPassengers([]byte(raw)); out != "" {
			t.Errorf("expected empty string for %q, got:\n%s", raw, out)
		}
	}
}

func TestFormatListPassengers_GroupedWithBerthsAndFare(t *testing.T) {
	raw := `{"count":2,"economy_berths":"1/12","business_berths":"0/6","first_berths":"1/3","passengers":[` +
		`{"citizen_id":"c1","name":"Zed Aulin","citizenship":"solarian","class":"economy","destination_name":"Aldebaran Gate","fare":420,"ticks_remaining":180},` +
		`{"citizen_id":"c2","name":"Mara Vex","citizenship":"crimson","class":"first","destination_name":"Aldebaran Gate","fare":3100,"ticks_remaining":90}]}`
	out := formatListPassengers([]byte(raw))

	if !strings.Contains(out, "Passengers aboard — 2 | Berths: eco 1/12, biz 0/6, first 1/3") {
		t.Fatalf("missing berth header, got:\n%s", out)
	}
	if !strings.Contains(out, "Aldebaran Gate (2)") {
		t.Errorf("missing destination group, got:\n%s", out)
	}
	// Aboard manifest orders first class before economy.
	if i, j := strings.Index(out, "first:"), strings.Index(out, "economy:"); i < 0 || i >= j {
		t.Errorf("class order wrong (first=%d economy=%d):\n%s", i, j, out)
	}
	// Fare/ticks shown, right-aligned to shared widths (fareW=4, ticksW=3).
	if !strings.Contains(out, " 420 cr  180t") || !strings.Contains(out, "3100 cr   90t") {
		t.Errorf("missing aligned fare/ticks, got:\n%s", out)
	}
}

func TestFormatListPassengers_EmptyButValid(t *testing.T) {
	out := formatListPassengers([]byte(`{"count":0,"economy_berths":"0/12","business_berths":"0/6","first_berths":"0/3","passengers":[]}`))
	if !strings.Contains(out, "Passengers aboard — 0") || !strings.Contains(out, "(none aboard)") {
		t.Errorf("expected empty-manifest render, got:\n%s", out)
	}
}

func TestFormatListPassengers_ErrorFrame(t *testing.T) {
	if out := formatListPassengers([]byte(`{"error":"no_ship","message":"No active ship."}`)); out != "" {
		t.Errorf("expected empty string for error frame, got:\n%s", out)
	}
}

func TestFormatLoadPassenger(t *testing.T) {
	raw := `{"message":"3 boarded.","count":2,"total_fare":1260,"loaded":[` +
		`{"name":"Zed Aulin","class":"economy","destination_name":"Aldebaran Gate","fare":420,"ticks_remaining":180},` +
		`{"name":"Mara Vex","class":"first","destination_name":"Bellatrix Hub","fare":840,"ticks_remaining":160}]}`
	out := formatLoadPassenger([]byte(raw))
	for _, want := range []string{
		"Loaded 2 passenger(s) — 1260 cr total",
		"Zed Aulin [economy] → Aldebaran Gate, 420 cr (180t)",
		"Mara Vex [first] → Bellatrix Hub, 840 cr (160t)",
		"3 boarded.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q, got:\n%s", want, out)
		}
	}
}

func TestFormatUnloadPassenger_Delivered(t *testing.T) {
	out := formatUnloadPassenger([]byte(`{"name":"Zed Aulin","delivered":true,"fare_paid":420,"message":"Arrived."}`))
	if !strings.Contains(out, "Delivered Zed Aulin — 420 cr") {
		t.Errorf("expected delivered line, got:\n%s", out)
	}
}

func TestFormatUnloadPassenger_Stranded(t *testing.T) {
	out := formatUnloadPassenger([]byte(`{"name":"Mara Vex","delivered":false,"fare_paid":0,"message":"Wrong station."}`))
	if !strings.Contains(out, "Mara Vex disembarked — stranded (no fare)") {
		t.Errorf("expected stranded line, got:\n%s", out)
	}
}

func TestFormatUnloadPassenger_ErrorFrame(t *testing.T) {
	if out := formatUnloadPassenger([]byte(`{}`)); out != "" {
		t.Errorf("expected empty string for empty object, got:\n%s", out)
	}
}
