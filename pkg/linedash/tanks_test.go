package linedash

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
)

func seedTanks(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "assets.db")
	store, err := assets.Open(assets.Config{DBPath: path})
	if err != nil {
		t.Fatalf("assets.Open: %v", err)
	}
	stmts := []string{
		`INSERT INTO agents(player_id, agent_id, username) VALUES ('p10', 'craftsman-10', 'Craftsman Ten')`,
		`INSERT INTO faction_profile(faction_id, tag) VALUES ('f1', 'CRFT')`,
		`INSERT INTO agent_storage_items(player_id, base_id, item_id, quantity, captured_at) VALUES ('p10', 'voss', 'water_ice', 120, '2026-10-24T10:00:00Z')`,
		`INSERT INTO faction_storage_items(faction_id, base_id, item_id, quantity, captured_at) VALUES ('f1', 'voss', 'water_ice', 1455, '2026-10-24T11:30:15.5Z')`,
	}
	for _, s := range stmts {
		if _, err := store.DB().Exec(s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	return path
}

func openSeeded(t *testing.T) *Tanks {
	t.Helper()
	tk, err := OpenTanks(context.Background(), seedTanks(t))
	if err != nil {
		t.Fatalf("OpenTanks: %v", err)
	}
	t.Cleanup(func() { _ = tk.Close() })

	return tk
}

func TestLevelsAgentAndFactionHolders(t *testing.T) {
	tk := openSeeded(t)
	n := Node{
		Items: []string{"water_ice"},
		Holders: []Holder{
			{Agent: "craftsman-10", Station: "voss"},
			{Faction: "CRFT", Station: "voss"},
		},
	}
	levels, err := tk.Levels(context.Background(), n)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 2 {
		t.Fatalf("got %d levels, want 2", len(levels))
	}
	if levels[0].Holder.Agent != "craftsman-10" || levels[0].Units != 120 {
		t.Errorf("level 0 = %+v", levels[0])
	}
	if levels[1].Holder.Faction != "CRFT" || levels[1].Units != 1455 {
		t.Errorf("level 1 = %+v", levels[1])
	}
	if got := SumUnits(levels, "water_ice"); got != 1575 {
		t.Errorf("SumUnits = %v, want 1575", got)
	}
	want := time.Date(2026, 10, 24, 11, 30, 15, 500_000_000, time.UTC)
	if got := Freshest(levels); !got.Equal(want) {
		t.Errorf("Freshest = %v, want %v", got, want)
	}
}

func TestLevelsMissingRowIsZeroWithZeroTime(t *testing.T) {
	tk := openSeeded(t)
	n := Node{
		Items: []string{"water_ice", "moonshine"},
		Holders: []Holder{
			{Agent: "craftsman-10", Station: "voss"},
			{Agent: "ghost", Station: "voss"},
		},
	}
	levels, err := tk.Levels(context.Background(), n)
	if err != nil {
		t.Fatal(err)
	}
	// craftsman-10 gets both items in item order; ghost is unknown to
	// assets.db, so its levels are zero and never captured.
	if len(levels) != 4 {
		t.Fatalf("got %d levels, want 4: %+v", len(levels), levels)
	}
	if levels[1].Item != "moonshine" || levels[1].Units != 0 || !levels[1].CapturedAt.IsZero() {
		t.Errorf("missing row level = %+v", levels[1])
	}
	if levels[0].CapturedAt.IsZero() {
		t.Errorf("present row has zero time")
	}
	for _, lv := range levels[2:] {
		if lv.Holder.Agent != "ghost" || lv.Units != 0 || !lv.CapturedAt.IsZero() {
			t.Errorf("unknown holder level = %+v, want zero units and zero time", lv)
		}
	}
	if !Freshest(nil).IsZero() {
		t.Errorf("Freshest(nil) not zero")
	}
}

// An empty tank of a holder whose storage snapshot exists carries the
// snapshot's header time: captured, and empty, is not never-captured.
func TestLevelsEmptyTankTakesHeaderTime(t *testing.T) {
	path := seedTanks(t)
	store, err := assets.Open(assets.Config{DBPath: path})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`INSERT INTO agent_storage(player_id, base_id, captured_at) VALUES ('p10', 'voss', '2026-10-24T10:00:00Z')`,
		`INSERT INTO agent_storage(player_id, base_id, captured_at) VALUES ('p10', 'other', '2026-10-24T10:00:00Z')`,
		`INSERT INTO faction_storage(faction_id, base_id, captured_at) VALUES ('f1', 'voss', '2026-10-24T11:30:15.5Z')`,
	} {
		if _, err = store.DB().Exec(s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	tk, err := OpenTanks(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tk.Close() })

	n := Node{
		Items: []string{"moonshine"},
		Holders: []Holder{
			{Agent: "craftsman-10", Station: "voss"},
			{Agent: "craftsman-10", Station: "nowhere"}, // snapshot exists, no row at this base
			{Faction: "CRFT", Station: "voss"},
		},
	}
	levels, err := tk.Levels(context.Background(), n)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 3 {
		t.Fatalf("got %d levels, want 3", len(levels))
	}
	agentAt := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	factionAt := time.Date(2026, 10, 24, 11, 30, 15, 500_000_000, time.UTC)
	for i, want := range []time.Time{agentAt, agentAt, factionAt} {
		if levels[i].Units != 0 || !levels[i].CapturedAt.Equal(want) {
			t.Errorf("level %d = %+v, want 0 units captured %v", i, levels[i], want)
		}
	}
}

func TestLevelsUnknownFactionTagErrors(t *testing.T) {
	tk := openSeeded(t)
	n := Node{Items: []string{"water_ice"}, Holders: []Holder{{Faction: "NOPE", Station: "voss"}}}
	if _, err := tk.Levels(context.Background(), n); err == nil {
		t.Fatal("want error for unknown faction tag")
	}
}

func TestOpenTanksMissingFileErrors(t *testing.T) {
	if _, err := OpenTanks(context.Background(), filepath.Join(t.TempDir(), "none.db")); err == nil {
		t.Fatal("want error for missing db")
	}
}
