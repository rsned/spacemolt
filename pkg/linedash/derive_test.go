package linedash

import (
	"reflect"
	"slices"
	"testing"
)

// deriveRef is two stations: Conjunction Junction (iron refinery, carbon arc
// furnace, atmospheric recycler, Faction Office L2) and Voss Redoubt (a still).
func deriveRef() RefData {
	cj := Station{BaseID: cjBase, POIID: cjPOI, Name: "Conjunction Junction", SystemName: "Cloverfield"}
	voss := Station{BaseID: vossBase, POIID: vossPOI, Name: "Voss Redoubt Station", SystemName: "Alhena"}
	sirius := Station{BaseID: "sirius_observatory_station", POIID: "sirius_observatory", Name: "Sirius Observatory Station", SystemName: "Sirius"}
	return RefData{
		Stations: map[string]Station{
			cjBase: cj, cjPOI: cj, vossBase: voss, vossPOI: voss,
			sirius.BaseID: sirius, sirius.POIID: sirius,
		},
		Recipes: map[string]Recipe{
			"refine_steel":               {ID: "refine_steel", Name: "Refine Steel", CraftingTime: 0.55, Inputs: map[string]int{"iron_ore": 5}, Outputs: map[string]int{"steel_plate": 2}},
			"carbon_arc_circuit_etching": {ID: "carbon_arc_circuit_etching", Name: "Carbon Arc Circuit Etching", CraftingTime: 4.6, Inputs: map[string]int{"steel_plate": 1, "carbon": 2}, Outputs: map[string]int{"circuit_board": 3}},
			"draw_copper_piping":         {ID: "draw_copper_piping", Name: "Draw Copper Piping", CraftingTime: 1.8, Inputs: map[string]int{"copper_ore": 4, "steel_plate": 2}, Outputs: map[string]int{"copper_piping": 4}},
			"grow_sunspindle":            {ID: "grow_sunspindle", Name: "Grow Sunspindle", CraftingTime: 360, Inputs: map[string]int{"purified_water": 300, "liquid_nitrogen": 30}, Outputs: map[string]int{"sunspindle": 200}},
			"distill_moonshine":          {ID: "distill_moonshine", Name: "Distill Moonshine", CraftingTime: 13.5, Inputs: map[string]int{"water_ice": 10, "pharmaceutical_culture": 2}, Outputs: map[string]int{"pirate_moonshine": 6}},
			"crystal_cut":                {ID: "crystal_cut", Name: "Crystal Cut", CraftingTime: 10, Inputs: map[string]int{"iron_ore": 1}, Outputs: map[string]int{"energy_crystal": 1}},
		},
		ItemCategory: map[string]string{
			"steel_plate": "refined", "circuit_board": "component", "copper_piping": "refined",
			"sunspindle": "consumable", "pirate_moonshine": "contraband", "void_dust": "contraband",
		},
		ItemName: map[string]string{
			"steel_plate": "Steel Plate", "circuit_board": "Circuit Board", "copper_piping": "Copper Piping",
			"sunspindle": "Sunspindle", "pirate_moonshine": "Pirate Moonshine", "void_dust": "Void Dust",
		},
		Facilities: map[string][]Facility{
			cjBase: {
				{BaseID: cjBase, Type: "Iron Refinery", Level: 1, RecipeID: "refine_steel"},
				{BaseID: cjBase, Type: "Carbon Arc Furnace", Level: 1, RecipeID: "carbon_arc_circuit_etching"},
				{BaseID: cjBase, Type: "Copper Drawing Mill", Level: 1, RecipeID: "draw_copper_piping"},
				{BaseID: cjBase, Type: "Atmospheric Recycler", Level: 1},
				{BaseID: cjBase, Type: "Faction Office", Level: 2},
			},
			vossBase: {{BaseID: vossBase, Type: "Backwater Still", Level: 1, RecipeID: "distill_moonshine"}},
		},
		Upkeep: map[string]map[string]int{"Atmospheric Recycler": {"sunspindle": 1, "oxygen_gas": 2}},
	}
}

func deriveInputs() DeriveInputs {
	return DeriveInputs{
		Roster: map[string]RosterEntry{
			"miningbot_0000": {AgentID: "miningbot_0000", Fleet: "crft-mining", Role: "resident", Station: cjPOI},
			"craftsman-3":    {AgentID: "craftsman-3", Fleet: "craft", Role: "craftsman", Station: cjPOI},
			"craftsman-10": {AgentID: "craftsman-10", Fleet: "craft", Role: "craftsman", Station: vossPOI,
				Consign: []string{"pirate_moonshine:trader-9:100"}},
			"craftsman-8":  {AgentID: "craftsman-8", Fleet: "craft", Role: "craftsman", Station: vossBase},
			"trader-10":    {AgentID: "trader-10", Fleet: "crft-mining", Role: "resident", Station: vossPOI},
			"hauler-x":     {AgentID: "hauler-x", Fleet: "haul", Role: "hauler"},
			"gone":         {AgentID: "gone", Fleet: "craft", Station: cjPOI, Removed: true},
			"marketbot_12": {AgentID: "marketbot_12", Fleet: "mb", Station: cjPOI},
		},
		Schedules: map[string][]Task{
			"miningbot_0000": {
				{ID: 1, Frequency: "hourly", Command: "craft refine_steel 500"},
				{ID: 33, Frequency: "hourly", Command: "craft carbon_arc_circuit_etching 1000 prefer_own"},
				{ID: 34, Frequency: "half_hourly", Command: "faction_deposit carbon"},
				{ID: 35, Frequency: "hourly", Command: "craft crystal_cut 10"},
			},
			"craftsman-3":  {{ID: 2, Frequency: "daily", Command: "craft grow_sunspindle 1"}, {ID: 3, Frequency: "hourly", Command: "craft draw_copper_piping 200"}},
			"craftsman-10": {{ID: 5, Frequency: "ten_minutely", Command: "craft distill_moonshine 60"}},
			"craftsman-8":  {{ID: 6, Frequency: "ten_minutely", Command: "craft distill_moonshine 60"}},
			"trader-10": {
				{ID: 10, Frequency: "daily", Command: "buy_directed pharmaceutical_culture 850 sirius_observatory_station 90 self"},
				{ID: 11, Frequency: "daily", Command: "deliver pharmaceutical_culture 850 sirius_observatory_station voss_redoubt craftsman-10"},
			},
			"hauler-x":     {{ID: 1, Frequency: "hourly", Command: "craft refine_steel 500"}},
			"gone":         {{ID: 1, Frequency: "hourly", Command: "craft refine_steel 500"}},
			"marketbot_12": {{ID: 1, Frequency: "hourly", Command: "craft refine_steel 500"}},
		},
		Ref:         deriveRef(),
		Listings:    []Listing{{StationID: cjBase, Item: "void_dust", Agent: "miningbot_0000"}},
		FactionTag:  "CRFT",
		DeferFleets: []string{"mb"},
	}
}

func lineByID(t *testing.T, lines []Line, id string) Line {
	t.Helper()
	for _, l := range lines {
		if l.ID == id {
			return l
		}
	}
	ids := make([]string, 0, len(lines))
	for _, l := range lines {
		ids = append(ids, l.ID)
	}
	t.Fatalf("no line %q; have %v", id, ids)
	return Line{}
}

func nodeByType(t *testing.T, l Line, typ NodeType) Node {
	t.Helper()
	for _, n := range l.Nodes {
		if n.Type == typ {
			return n
		}
	}
	t.Fatalf("line %s has no %s node", l.ID, typ)
	return Node{}
}

func inputsNode(t *testing.T, l Line) Node {
	t.Helper()
	for _, n := range l.Nodes {
		if n.ID == "inputs" {
			return n
		}
	}
	t.Fatalf("line %s has no inputs node", l.ID)
	return Node{}
}

func derived(t *testing.T) []Line {
	t.Helper()
	lines := Derive(deriveInputs())
	for _, l := range lines {
		if err := l.validate(); err != nil {
			t.Errorf("derived line fails validation: %v", err)
		}
	}
	return lines
}

func TestDeriveFacilityVsHand(t *testing.T) {
	lines := derived(t)
	if k := lineByID(t, lines, cjBase+":refine_steel").Kind; k != "facility" {
		t.Errorf("refine_steel kind = %q, want facility", k)
	}
	if k := lineByID(t, lines, cjBase+":grow_sunspindle").Kind; k != "hand" {
		t.Errorf("grow_sunspindle kind = %q, want hand", k)
	}
}

func TestDeriveUpkeepPurposeAndCategory(t *testing.T) {
	l := lineByID(t, derived(t), cjBase+":grow_sunspindle")
	if l.Purpose != "upkeep" || l.Category != "Station upkeep" {
		t.Errorf("sunspindle purpose %q category %q", l.Purpose, l.Category)
	}
	if l.StationID != cjBase || l.StationName != "Conjunction Junction" || l.SystemName != "Cloverfield" {
		t.Errorf("station fields = %q %q %q", l.StationID, l.StationName, l.SystemName)
	}
}

func TestDeriveSharedInputCounted(t *testing.T) {
	lines := derived(t)
	for _, id := range []string{cjBase + ":carbon_arc_circuit_etching", cjBase + ":draw_copper_piping"} {
		in := inputsNode(t, lineByID(t, lines, id))
		if in.ItemShares["steel_plate"] != 2 || in.SharedBy != 2 {
			t.Errorf("%s inputs: shares %v SharedBy %d, want steel_plate 2", id, in.ItemShares, in.SharedBy)
		}
	}
	if in := inputsNode(t, lineByID(t, lines, cjBase+":grow_sunspindle")); in.SharedBy != 1 {
		t.Errorf("sunspindle inputs SharedBy = %d, want 1 (dedicated)", in.SharedBy)
	}
}

func TestDeriveDependedOn(t *testing.T) {
	l := lineByID(t, derived(t), cjBase+":refine_steel")
	if l.DependedOn != (Deps{Production: 2}) {
		t.Errorf("refine_steel DependedOn = %+v, want 2 production", l.DependedOn)
	}
	if l.Purpose != "feeds" || l.Category != "Refined" {
		t.Errorf("refine_steel purpose %q category %q", l.Purpose, l.Category)
	}
}

func TestDeriveDeliverAddsHaulFromPurchase(t *testing.T) {
	l := lineByID(t, derived(t), vossBase+":distill_moonshine")
	buy := nodeByType(t, l, NodePurchase)
	if buy.MaxPrice != 90 || buy.Station != "sirius_observatory_station" || !slices.Equal(buy.Agents, []string{"trader-10"}) {
		t.Errorf("purchase node = %+v", buy)
	}
	var haul *Edge
	for i, e := range l.Edges {
		if e.Kind == EdgeHaul && e.From == buy.ID && e.To == "inputs" {
			haul = &l.Edges[i]
		}
	}
	if haul == nil || haul.Item != "pharmaceutical_culture" || !slices.Equal(haul.Agents, []string{"trader-10"}) {
		t.Errorf("haul edge = %+v", haul)
	}
	if keys := l.TaskKeys["trader-10"]; !slices.Contains(keys, "task:10") || !slices.Contains(keys, "task:11") {
		t.Errorf("trader-10 task keys = %v", keys)
	}
	if prod := nodeByType(t, l, NodeProduce); !slices.Equal(prod.Agents, []string{"craftsman-10", "craftsman-8"}) {
		t.Errorf("still agents = %v", prod.Agents)
	}
	if l.Purpose != "for_sale" || l.Category != "Contraband" {
		t.Errorf("moonshine purpose %q category %q (consigned to a hauler = for sale)", l.Purpose, l.Category)
	}
}

func TestDeriveSalesLineForUnproducedListing(t *testing.T) {
	l := lineByID(t, derived(t), cjBase+":sell:void_dust")
	if l.Category != "Market/Sales" || l.Kind != "sales" || l.Purpose != "for_sale" || l.OutputItem != "void_dust" {
		t.Errorf("sales line = %+v", l)
	}
	if sell := nodeByType(t, l, NodeSell); sell.Note != "supply not on record" || sell.Station != cjBase {
		t.Errorf("sell node = %+v", sell)
	}
}

func TestDeriveSkipsUnpinnedRemovedAndDeferred(t *testing.T) {
	for _, l := range derived(t) {
		for _, a := range AgentsOf(l) {
			if a == "hauler-x" || a == "gone" || a == "marketbot_12" {
				t.Errorf("line %s carries %s", l.ID, a)
			}
		}
	}
}

func TestDeriveMergesAliasedStation(t *testing.T) {
	// craftsman-10 is pinned by POI id, craftsman-8 by base id: one line.
	n := 0
	for _, l := range derived(t) {
		if l.OutputItem == "pirate_moonshine" {
			n++
			if l.StationID != vossBase {
				t.Errorf("moonshine line station %q", l.StationID)
			}
		}
	}
	if n != 1 {
		t.Errorf("%d moonshine lines, want 1", n)
	}
}

func TestDeriveDropsForbiddenItems(t *testing.T) {
	for _, l := range derived(t) {
		if l.OutputItem == "energy_crystal" {
			t.Errorf("forbidden line %s derived", l.ID)
		}
	}
}

func TestDeriveStableOrder(t *testing.T) {
	a, b := Derive(deriveInputs()), Derive(deriveInputs())
	if !reflect.DeepEqual(a, b) {
		t.Error("two derivations of the same inputs differ")
	}
	for i := 1; i < len(a); i++ {
		p, q := a[i-1], a[i]
		if p.StationName > q.StationName || (p.StationName == q.StationName && p.Category == q.Category && p.Name > q.Name) {
			t.Errorf("order: %s before %s", p.ID, q.ID)
		}
	}
}

func TestDeriveCapacityUsesFacilitySpeed(t *testing.T) {
	// refine_steel: 0.55 ticks hand; L1 with a Faction Office (tier 2):
	// 0.55 / (5 * 1.30) ticks/run -> 8640 / that * 2 units.
	l := lineByID(t, derived(t), cjBase+":refine_steel")
	ticks := 0.55 / (5 * 1.30)
	want := int(8640 / ticks * 2)
	if l.CapacityPerDay != want {
		t.Errorf("capacity = %d, want %d", l.CapacityPerDay, want)
	}
	// grow_sunspindle by hand: 8640/360 runs * 200 units.
	if c := lineByID(t, derived(t), cjBase+":grow_sunspindle").CapacityPerDay; c != 4800 {
		t.Errorf("sunspindle capacity = %d, want 4800", c)
	}
}

// A faction sell order has no pinned agent: one "market" sell node held in
// faction storage, never an agent named "".
func TestDeriveListingWithoutAgentIsFactionOrder(t *testing.T) {
	in := deriveInputs()
	in.Listings = append(in.Listings, Listing{StationID: cjBase, Item: "steel_plate"})
	l := lineByID(t, Derive(in), cjBase+":refine_steel")
	var sell *Node
	for i := range l.Nodes {
		if l.Nodes[i].Type == NodeSell {
			sell = &l.Nodes[i]
		}
	}
	if sell == nil || sell.ID != "sell:market" || len(sell.Agents) != 0 || len(sell.Holders) != 1 || sell.Holders[0].Faction != "CRFT" {
		t.Errorf("sell node = %+v", sell)
	}
	if err := l.validate(); err != nil {
		t.Error(err)
	}
}
