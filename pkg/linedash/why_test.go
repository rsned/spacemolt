package linedash

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/outcomes"
)

// whyLine is the moonshine fixture with the task keys Derive would attach.
func whyLine(t *testing.T) Line {
	t.Helper()
	l := fixtureLine(t)
	l.TaskKeys = map[string][]string{
		"trader-10":    {"task:10", "task:11"},
		"craftsman-8":  {"task:6", "behaviour:ensure_home"},
		"craftsman-10": {"task:5", "behaviour:ensure_home"},
	}
	return l
}

func outcome(key, status, reason string, h float64) outcomes.Outcome {
	return outcomes.Outcome{Key: key, Status: status, Reason: reason, At: at(h)}
}

func setOutcomes(in *Inputs, agent string, os ...outcomes.Outcome) {
	if in.Outcomes == nil {
		in.Outcomes = map[string]map[string]outcomes.Outcome{}
	}
	m := map[string]outcomes.Outcome{}
	for _, o := range os {
		m[o.Key] = o
	}
	in.Outcomes[agent] = m
}

const capReason = "buy_directed: price 70 exceeds ceiling 50 (replan)"

func TestWhyNamesLatestFailure(t *testing.T) {
	l := whyLine(t)
	in := baseInputs(l)
	setOutcomes(&in, "trader-10", outcome("task:10", outcomes.StatusFailed, capReason, 1), outcome("task:11", outcomes.StatusOK, "", 2))
	r := Evaluate(l, in)
	if n := nodeOf(t, r, "culture_buy"); n.Why != "trader-10: "+capReason {
		t.Errorf("culture_buy Why = %q", n.Why)
	}
}

// One record per key holds the LATEST outcome, so a success after a failure
// replaces it and the refusal must no longer be shown.
func TestWhyClearsAfterLaterSuccess(t *testing.T) {
	l := whyLine(t)
	in := baseInputs(l)
	setOutcomes(&in, "trader-10", outcome("task:10", outcomes.StatusOK, "", 0.5))
	if n := nodeOf(t, Evaluate(l, in), "culture_buy"); strings.Contains(n.Why, "exceeds ceiling") {
		t.Errorf("Why still names the cleared refusal: %q", n.Why)
	}
}

func TestWhyIgnoresOutcomeForRemovedTask(t *testing.T) {
	l := whyLine(t)
	in := baseInputs(l)
	setOutcomes(&in, "trader-10", outcome("task:99", outcomes.StatusFailed, capReason, 1))
	if n := nodeOf(t, Evaluate(l, in), "culture_buy"); strings.Contains(n.Why, "exceeds ceiling") {
		t.Errorf("Why came from a task that is not on the line: %q", n.Why)
	}
}

func TestWhyCauseUnknownWhenStarvedWithoutRecord(t *testing.T) {
	l := whyLine(t)
	in := baseInputs(l)
	n := nodeOf(t, Evaluate(l, in), "still")
	if n.State != StateStarved || n.Why != "cause unknown: "+string(AlertInputsStarved) {
		t.Errorf("still state %s Why %q", n.State, n.Why)
	}
}

func TestWhySharedTankEmpty(t *testing.T) {
	l := whyLine(t)
	for i := range l.Nodes {
		if l.Nodes[i].ID == "voss_inputs" {
			l.Nodes[i].ItemShares = map[string]int{"water_ice": 3, "pharmaceutical_culture": 1}
			l.Nodes[i].SharedBy = 3
		}
	}
	in := baseInputs(l)
	r := Evaluate(l, in)
	if n := nodeOf(t, r, "still"); n.Why != "water_ice 0 (shared with 3 lines)" {
		t.Errorf("still Why = %q", n.Why)
	}
	if r.Why == "" {
		t.Error("line Why empty while its produce node is starved")
	}
	if in := nodeOf(t, r, "voss_inputs"); in.SharedBy != 3 || in.ItemShares["water_ice"] != 3 {
		t.Errorf("inputs report SharedBy %d shares %v", in.SharedBy, in.ItemShares)
	}
}

func TestNotConformingAllFailed(t *testing.T) {
	l := whyLine(t)
	in := baseInputs(l)
	// What the worker really writes: every craft refused, while the always-ok
	// idle line and a successful trip home keep succeeding every pass.
	setOutcomes(&in, "craftsman-8",
		outcome("task:6", outcomes.StatusFailed, "cannot_craft: Not enough materials", 0.2),
		outcome("idle:ensure_home", outcomes.StatusOK, "", 0.05),
		outcome("behaviour:ensure_home", outcomes.StatusOK, "", 0.05))
	setOutcomes(&in, "craftsman-10", outcome("task:5", outcomes.StatusOK, "", 0.2))
	n := nodeOf(t, Evaluate(l, in), "still")
	if !hasAlert(n, AlertNotConforming) {
		t.Errorf("still alerts %v, want %q for craftsman-8", n.Alerts, AlertNotConforming)
	}
}

func TestNotConformingAwayFromPinnedStation(t *testing.T) {
	l := whyLine(t)
	in := baseInputs(l)
	re := in.Roster["craftsman-10"]
	re.Station = "voss_redoubt"
	in.Roster["craftsman-10"] = re
	lv := in.Live["craftsman-10"]
	lv.POI = "sol_central" // docked somewhere else
	in.Live["craftsman-10"] = lv
	if n := nodeOf(t, Evaluate(l, in), "still"); !hasAlert(n, AlertNotConforming) {
		t.Errorf("still alerts %v, want %q (craftsman-10 docked away from home)", n.Alerts, AlertNotConforming)
	}
	in.SameStation = func(a, b string) bool { return true }
	if n := nodeOf(t, Evaluate(l, in), "still"); hasAlert(n, AlertNotConforming) {
		t.Error("flagged although SameStation says it is home")
	}
}

func TestQueueDetailAndStaleness(t *testing.T) {
	l := whyLine(t)
	in := baseInputs(l)
	in.Queues = map[string]CraftQueue{"craftsman-10": {CapturedAt: at(0.1), Jobs: []QueuedJob{
		{Recipe: "distill_moonshine", RunsRemaining: 4, ETATicks: 72},
		{Recipe: "something_else", RunsRemaining: 9, ETATicks: 900},
	}}}
	n := nodeOf(t, Evaluate(l, in), "still")
	if !slices.Contains(n.Detail, "queue: 4 runs, ETA 72 ticks") {
		t.Errorf("still detail %v", n.Detail)
	}
	in.Queues["craftsman-10"] = CraftQueue{CapturedAt: at(3), Jobs: in.Queues["craftsman-10"].Jobs}
	if n := nodeOf(t, Evaluate(l, in), "still"); !n.Stale {
		t.Error("a 3h-old queue capture did not grey the still")
	}
}

func TestLineReportCarriesDerivedFields(t *testing.T) {
	l := whyLine(t)
	l.StationID, l.StationName, l.SystemName = vossBase, "Voss Redoubt Station", "Alhena"
	l.Category, l.Kind, l.Purpose = "Contraband", KindFacility, PurposeForSale
	l.DependedOn = Deps{Sales: 1}
	for i := range l.Nodes {
		if l.Nodes[i].ID == "tc_sell" {
			l.Nodes[i].Note = noteNoSupply
		}
	}
	r := Evaluate(l, baseInputs(l))
	if r.StationName != "Voss Redoubt Station" || r.SystemName != "Alhena" || r.StationID != vossBase || r.Category != "Contraband" ||
		r.Kind != KindFacility || r.Purpose != PurposeForSale || r.DependedOn != (Deps{Sales: 1}) || r.Agents != len(AgentsOf(l)) {
		t.Errorf("report = %+v", r)
	}
	if n := nodeOf(t, r, "tc_sell"); !slices.Contains(n.Detail, noteNoSupply) {
		t.Errorf("tc_sell detail %v, want the note", n.Detail)
	}
}

func TestLoadCraftQueue(t *testing.T) {
	dir := t.TempDir()
	if q, err := LoadCraftQueue(dir); err != nil || !q.CapturedAt.IsZero() {
		t.Fatalf("missing file = %+v, %v", q, err)
	}
	body := `{"captured_at":"2026-11-01T18:00:00Z","jobs":[{"recipe":"grow_sunspindle","runs_remaining":1,"eta_ticks":72,"position":2,"facility_id":"","base_id":"b","status":"queued"}]}`
	if err := os.WriteFile(filepath.Join(dir, "craft-queue.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	q, err := LoadCraftQueue(dir)
	if err != nil || len(q.Jobs) != 1 || q.Jobs[0].ETATicks != 72 || q.Jobs[0].BaseID != "b" || q.CapturedAt.IsZero() {
		t.Errorf("LoadCraftQueue = %+v, %v", q, err)
	}
}

// An agent that cannot fly home is non-conforming, and the route refusal is
// the why.
func TestNotConformingRouteRefusal(t *testing.T) {
	l := whyLine(t)
	in := baseInputs(l)
	setOutcomes(&in, "craftsman-10", outcome("task:5", outcomes.StatusOK, "", 0.3),
		outcome("behaviour:ensure_home", outcomes.StatusFailed, "insufficient fuel for route: route to alhena needs ~638 fuel, 550 available", 0.1))
	n := nodeOf(t, Evaluate(l, in), "still")
	if !hasAlert(n, AlertNotConforming) {
		t.Errorf("still alerts %v, want %q", n.Alerts, AlertNotConforming)
	}
	if !strings.Contains(n.Why, "needs ~638 fuel") {
		t.Errorf("still Why = %q, want the route refusal", n.Why)
	}
}

// Schedule ids are reused (max+1) once the top task is retired: an outcome
// recorded for the old task under the same id must not explain the new one.
func TestCurrentOutcomesDropsReusedTaskID(t *testing.T) {
	got := CurrentOutcomes(map[string]outcomes.Outcome{
		"task:7":                {Key: "task:7", Command: "craft refine_steel 500", Status: outcomes.StatusFailed},
		"task:8":                {Key: "task:8", Command: "craft grow_sunspindle 1", Status: outcomes.StatusFailed},
		"task:9":                {Key: "task:9", Command: "capture_storage", Status: outcomes.StatusOK},
		"behaviour:ensure_home": {Key: "behaviour:ensure_home", Status: outcomes.StatusOK},
	}, []Task{{ID: 7, Command: "capture_craft_queue"}, {ID: 8, Command: "craft grow_sunspindle 1"}})
	if _, ok := got["task:7"]; ok {
		t.Error("task:7 kept although id 7 now runs a different command")
	}
	if _, ok := got["task:9"]; ok {
		t.Error("task:9 kept although no task 9 exists")
	}
	if _, ok := got["task:8"]; !ok {
		t.Error("task:8 dropped although its command still matches")
	}
	if _, ok := got["behaviour:ensure_home"]; !ok {
		t.Error("non-task keys must pass through")
	}
}

// Live: the queue listing's "recipe" carries the recipe's display NAME
// ("Manufacture Fuel Basic"), not its id. Derive records the name too.
func TestQueueMatchesRecipeDisplayName(t *testing.T) {
	l := whyLine(t)
	for i := range l.Nodes {
		if l.Nodes[i].ID == "still" {
			l.Nodes[i].RecipeName = "Distill Moonshine"
		}
	}
	in := baseInputs(l)
	in.Queues = map[string]CraftQueue{"craftsman-10": {CapturedAt: at(0.1), Jobs: []QueuedJob{
		{Recipe: "Distill Moonshine", RunsRemaining: 20, ETATicks: 11},
	}}}
	if n := nodeOf(t, Evaluate(l, in), "still"); !slices.Contains(n.Detail, "queue: 20 runs, ETA 11 ticks") {
		t.Errorf("still detail %v", n.Detail)
	}
}

// A hauler's job on a line is to be away from its pin: only agents whose line
// work happens at their station (produce, sell) are flagged for being docked
// elsewhere.
func TestNotConformingAwayIgnoresHaulers(t *testing.T) {
	l := whyLine(t)
	in := baseInputs(l)
	re := in.Roster["trader-10"] // culture buyer + hauler on this line
	re.Station = "voss_redoubt"
	in.Roster["trader-10"] = re
	lv := in.Live["trader-10"]
	lv.POI = "sirius_observatory_station"
	in.Live["trader-10"] = lv
	if n := nodeOf(t, Evaluate(l, in), "culture_buy"); hasAlert(n, AlertNotConforming) {
		t.Errorf("hauler away on its delivery flagged as not doing its assignment: %v", n.Alerts)
	}
}

// A node's why comes from its own work: the still's craft task, not a
// delivery the same agent runs for another leg.
func TestWhyUsesTheNodesOwnKeys(t *testing.T) {
	l := whyLine(t)
	l.TaskKeys["craftsman-10"] = []string{"task:5", "task:40", "behaviour:ensure_home"}
	for i := range l.Nodes {
		if l.Nodes[i].ID == "still" {
			l.Nodes[i].Keys = []string{"task:5"}
		}
	}
	in := baseInputs(l)
	setOutcomes(&in, "craftsman-10",
		outcome("task:5", outcomes.StatusFailed, "cannot_craft: Not enough materials", 0.5),
		outcome("task:40", outcomes.StatusFailed, "deliver: nothing delivered of 10 x — source has none", 0.1))
	if n := nodeOf(t, Evaluate(l, in), "still"); !strings.Contains(n.Why, "cannot_craft") {
		t.Errorf("still Why = %q, want its own craft refusal", n.Why)
	}
}

// A line with nothing downstream of its output (a stockpile for builds) is
// meant to fill its output tank: no "output not drawn".
func TestOutputNotDrawnOnlyWithSomethingDownstream(t *testing.T) {
	l := Line{ID: "b:nodes", Name: "Control Node", OutputItem: "control_node", CapacityPerDay: 100,
		Nodes: []Node{
			{ID: "inputs", Type: NodeStore, Items: []string{"copper_wiring"}, Holders: []Holder{{Faction: "CRFT", Station: "b"}}},
			{ID: "produce", Type: NodeProduce, Items: []string{"control_node"}, Agents: []string{"m"}, Station: "b", Recipe: "r",
				UnitsPerRun: 1, BatchRuns: 1, InputsPerRun: map[string]int{"copper_wiring": 1}},
			{ID: "output", Type: NodeStore, Items: []string{"control_node"}, Holders: []Holder{{Faction: "CRFT", Station: "b"}}},
		},
		Edges: []Edge{{From: "inputs", To: "produce", Kind: EdgeCraft}, {From: "produce", To: "output", Kind: EdgeCraft}},
	}
	in := baseInputs(l)
	in.Tanks["inputs"] = []TankLevel{{Holder: Holder{Faction: "CRFT", Station: "b"}, Item: "copper_wiring", Units: 500, CapturedAt: evalNow}}
	in.Tanks["output"] = []TankLevel{{Holder: Holder{Faction: "CRFT", Station: "b"}, Item: "control_node", Units: 1963, CapturedAt: evalNow}}
	in.Events = []Event{{AgentID: "m", Type: EventCrafted, At: at(0.2), Item: "control_node", Runs: 1, Recipe: "r"}}
	r := Evaluate(l, in)
	if n := nodeOf(t, r, "produce"); hasAlert(n, AlertOutputNotDrawn) || n.State == StateChoked {
		t.Errorf("stockpile line flagged: state %s alerts %v", n.State, n.Alerts)
	}
	if n := nodeOf(t, r, "output"); n.State == StateChoked {
		t.Errorf("a full stockpile output tank reads choked")
	}
}

func TestDeriveNodeKeys(t *testing.T) {
	l := lineByID(t, derived(t), vossBase+":distill_moonshine")
	for _, n := range l.Nodes {
		switch n.Type {
		case NodeProduce:
			if !slices.Equal(n.Keys, []string{"task:5", "task:6"}) {
				t.Errorf("still keys = %v, want the two craft tasks", n.Keys)
			}
		case NodePurchase:
			if !slices.Equal(n.Keys, []string{"task:11", "task:10"}) {
				t.Errorf("purchase keys = %v, want deliver + buy", n.Keys)
			}
		}
	}
}

// A job logs crafting.completed only when the whole job ends, so a busy
// facility can go an hour without one. Runs still queued for the recipe in a
// fresh capture mean the line is working, not stalled.
func TestQueuedRunsMeanNotStalled(t *testing.T) {
	l := whyLine(t)
	in := baseInputs(l)
	stillInputsFull(&in)
	if n := nodeOf(t, Evaluate(l, in), "still"); n.State != StateChoked {
		t.Fatalf("precondition: still with full inputs and no craft should be choked, got %s", n.State)
	}
	in.Queues = map[string]CraftQueue{"craftsman-10": {CapturedAt: at(0.1), Jobs: []QueuedJob{{Recipe: "distill_moonshine", RunsRemaining: 263, ETATicks: 122}}}}
	n := nodeOf(t, Evaluate(l, in), "still")
	if n.State != StateOptimal {
		t.Errorf("still with 263 runs queued = %s, want optimal", n.State)
	}
	for _, d := range n.Detail {
		if strings.HasPrefix(d, "inputs present, no craft") {
			t.Errorf("stale stall detail kept: %v", n.Detail)
		}
	}
}

// An empty output tank with nothing downstream (fuel into the station tank, a
// stockpile drawn by builds) is being used, not starved. With a consign or
// haul out of it (moonshine) it still starves those legs:
// TestEvalStoreEmptyStarved.
func TestEmptyOutputTankIsNotStarved(t *testing.T) {
	l := Line{ID: "b:fuel", Name: "Fuel", OutputItem: "fuel_reserve", CapacityPerDay: 100,
		Nodes: []Node{
			{ID: "inputs", Type: NodeStore, Items: []string{"liquid_hydrogen"}, Holders: []Holder{{Faction: "CRFT", Station: "b"}}},
			{ID: "produce", Type: NodeProduce, Items: []string{"fuel_reserve"}, Agents: []string{"m"}, Station: "b", Recipe: "r",
				UnitsPerRun: 1, BatchRuns: 1, InputsPerRun: map[string]int{"liquid_hydrogen": 1}},
			{ID: "output", Type: NodeStore, Items: []string{"fuel_reserve"}, Holders: []Holder{{Faction: "CRFT", Station: "b"}}},
		},
		Edges: []Edge{{From: "inputs", To: "produce", Kind: EdgeCraft}, {From: "produce", To: "output", Kind: EdgeCraft}},
	}
	in := baseInputs(l)
	if n := nodeOf(t, Evaluate(l, in), "output"); n.State == StateStarved {
		t.Errorf("empty output tank with nothing downstream reads starved")
	}
}

// The line's why prefers a node that knows its cause over one that doesn't.
func TestLineWhyPrefersAKnownCause(t *testing.T) {
	l := whyLine(t)
	in := baseInputs(l) // culture_buy starved (no purchase), still starved (empty inputs)
	setOutcomes(&in, "craftsman-10", outcome("task:5", outcomes.StatusFailed, "cannot_craft: Not enough materials", 0.2))
	for i := range l.Nodes {
		if l.Nodes[i].ID == "still" {
			l.Nodes[i].Keys = []string{"task:5"}
		}
	}
	r := Evaluate(l, in)
	if !strings.Contains(r.Why, "cannot_craft") {
		t.Errorf("line Why = %q, want the still's known cause over culture_buy's unknown one", r.Why)
	}
}
