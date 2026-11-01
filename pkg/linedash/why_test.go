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
		"craftsman-8":  {"task:6", "idle:ensure_home"},
		"craftsman-10": {"task:5", "idle:ensure_home"},
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
	setOutcomes(&in, "craftsman-8",
		outcome("task:6", outcomes.StatusFailed, "cannot_craft: Not enough materials", 0.2),
		outcome("idle:ensure_home", outcomes.StatusFailed, "ensure_home: insufficient fuel", 0.1))
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
