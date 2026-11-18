package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

var now = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

// fixtureKB is a KB with one charted system (sol), three frontier systems
// east of it (far_a - far_b joined by a lane, far_b holding a Repeater
// Monolith), and a lane from far_a back into charted space.
func fixtureKB(t *testing.T) *knowledge.SQLiteKB {
	t.Helper()
	ctx := context.Background()
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: filepath.Join(t.TempDir(), "kb.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kb.Close() })
	if err := kb.RememberSystem(ctx, knowledge.System{ID: "sol", Name: "Sol"}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberSystem(ctx, knowledge.System{ID: "hub", Name: "Hub", Position: game.Position{X: 50, Y: 0}}); err != nil {
		t.Fatal(err)
	}
	if err := kb.RememberPOI(ctx, knowledge.POI{ID: "hub_station", SystemID: "hub", Name: "Hub Station", Type: "station"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		id   string
		x, y float64
	}{{"far_a", 400, 10}, {"far_b", 420, -10}, {"far_c", 900, 0}, {"north_a", 0, 500}} {
		if _, err := kb.RecordMeasuredSystemPosition(ctx, s.id, strings.ToUpper(s.id), s.x, s.y); err != nil {
			t.Fatal(err)
		}
	}
	for _, l := range [][2]string{{"far_a", "far_b"}, {"far_a", "hub"}, {"hub", "sol"}} {
		if err := kb.RememberConnection(ctx, l[0], l[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := kb.RecordHostileSightings(ctx, []knowledge.HostileSighting{{
		HostileID: "rep1", HostileType: "repeater_monolith", Kind: knowledge.HostileKindRepeater, Class: "monolith",
		Name: "Repeater Monolith", SystemID: "far_b", POIID: "far_b_core", AgentID: "explorer-1",
	}}); err != nil {
		t.Fatal(err)
	}
	return kb
}

func TestGenerateTakesOnlyFrontierSystems(t *testing.T) {
	kb := fixtureKB(t)
	path := filepath.Join(t.TempDir(), "frontier-run.json")
	if err := generate(context.Background(), kb, path, now); err != nil {
		t.Fatal(err)
	}
	r, err := sweep.LoadFrontierRun(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Systems) != 4 || r.Systems["sol"] != nil || r.Systems["hub"] != nil {
		t.Fatalf("ledger holds %d systems, want the 4 frontier ones only", len(r.Systems))
	}
	if r.Systems["far_a"].Cluster != r.Systems["far_b"].Cluster {
		t.Error("far_a and far_b are lane-connected but not one cluster")
	}
	if len(r.Lanes) != 1 {
		t.Errorf("lanes %v, want only far_a-far_b (the lane into charted space is not kept)", r.Lanes)
	}
	if b := r.Systems["far_b"]; b.Status != sweep.FrontierIncomplete || !strings.Contains(b.Reason, "hostile: ") ||
		!strings.Contains(b.Reason, "repeater_monolith") {
		t.Errorf("far_b (Repeater Monolith) generated %q / %q", b.Status, b.Reason)
	}
	if a := r.Systems["far_a"]; a.Status != sweep.FrontierTodo || a.X != 400 || a.Wedge != "E" {
		t.Errorf("far_a generated %+v", a)
	}
}

func TestGenerateRefusesAnActiveRun(t *testing.T) {
	kb := fixtureKB(t)
	path := filepath.Join(t.TempDir(), "frontier-run.json")
	if err := generate(context.Background(), kb, path, now); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := sweep.ClaimCluster(path, "explorer-1", "far_c", now); err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	before, _ := os.ReadFile(path)
	err := generate(context.Background(), kb, path, now)
	if err == nil || !strings.Contains(err.Error(), "archive") {
		t.Fatalf("err %v, want a refusal telling the operator to archive the run", err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("generate changed an active run")
	}
}

func TestStatusCountsByWedge(t *testing.T) {
	kb := fixtureKB(t)
	path := filepath.Join(t.TempDir(), "frontier-run.json")
	if err := generate(context.Background(), kb, path, now); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := status(&out, path); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"4 systems", "3 todo", "1 incomplete", "far_b", "hostile: "} {
		if !strings.Contains(got, want) {
			t.Errorf("status lacks %q:\n%s", want, got)
		}
	}
}

// Reserves go to the wedges farthest from a refuelling station.
func TestAssignGivesReservesTheRemoteWedges(t *testing.T) {
	r := sweep.NewFrontierRun([]sweep.FrontierInput{
		{ID: "near_e", X: 300, Y: 0},                        // E, 250 from the station
		{ID: "far_n", X: 0, Y: 2000},                        // N, ~2000 from the station
		{ID: "mid_w", X: -900, Y: 0},                        // W, 950 from the station
		{ID: "dead_s", X: 0, Y: -5000, Lethal: "leviathan"}, // S has nothing todo
	}, nil, now)
	stations := []sweep.Point{{X: 50, Y: 0}}
	got := assign(r, stations, []string{"explorer-1", "explorer-2", "explorer-5"}, []string{"explorer-5"})

	byWedge := map[string]assignment{}
	for _, a := range got {
		byWedge[a.Wedge] = a
	}
	if len(got) != 8 {
		t.Fatalf("%d assignments, want one row per wedge", len(got))
	}
	if byWedge["N"].Ship != "explorer-5" {
		t.Errorf("the Reserve went to %+v, want the remotest wedge N", got)
	}
	if byWedge["W"].Ship != "explorer-1" || byWedge["E"].Ship != "explorer-2" {
		t.Errorf("W=%q E=%q, want the rest in roster order by remoteness", byWedge["W"].Ship, byWedge["E"].Ship)
	}
	if byWedge["S"].Ship != "" || byWedge["S"].Todo != 0 {
		t.Errorf("S (nothing todo) got %+v, want no ship", byWedge["S"])
	}
	if byWedge["E"].Start != "near_e" || byWedge["N"].Todo != 1 {
		t.Errorf("E start %q, N todo %d", byWedge["E"].Start, byWedge["N"].Todo)
	}
}

// The relic report lists the ones that gave something first, then the
// mission offers that were skipped.
func TestRelicsReport(t *testing.T) {
	kb := fixtureKB(t)
	ctx := context.Background()
	for _, r := range []knowledge.RelicInteraction{
		{POIID: "husk", SystemID: "far_a", AgentID: "explorer-1", Tick: 1, Outcome: knowledge.RelicNothing, Message: "Dust.", ObservedUTC: now},
		{POIID: "vault", SystemID: "far_a", AgentID: "explorer-1", Tick: 2, Outcome: knowledge.RelicItems,
			Message: "You pry it open.", Items: map[string]int{"relic_shard": 2}, ObservedUTC: now},
		{POIID: "gate", SystemID: "far_c", AgentID: "explorer-2", Tick: 3, Outcome: knowledge.RelicMissionSkipped,
			Prompt: "Help us.", MissionJSON: `{"id":"m1"}`, ObservedUTC: now},
		{POIID: "archive", SystemID: "far_c", AgentID: "explorer-2", Tick: 4, Outcome: knowledge.RelicDataCopied,
			Message: "Copied.", DataCopied: true, ObservedUTC: now},
	} {
		if err := kb.RecordRelicInteraction(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := relics(ctx, &out, kb); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	vault, archive, gate := strings.Index(got, "vault"), strings.Index(got, "archive"), strings.Index(got, "gate")
	if vault < 0 || archive < 0 || gate < 0 || gate < vault || gate < archive {
		t.Errorf("want vault and archive listed before gate:\n%s", got)
	}
	for _, want := range []string{"relic_shard x2", "data copied", `{"id":"m1"}`, "2 responded", "1 offered a mission", "4 interactions"} {
		if !strings.Contains(got, want) {
			t.Errorf("report lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "husk") {
		t.Errorf("a relic that gave nothing is listed:\n%s", got)
	}
}
