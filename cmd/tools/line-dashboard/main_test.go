package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/assets"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/linedash"
	"github.com/rsned/spacemolt/pkg/market"
)

func testServer(dir string) *server {
	return &server{
		opt: options{
			overrides:       filepath.Join(dir, "overmind", "line-overrides.yaml"),
			kbDB:            filepath.Join(dir, "kb.db"),
			facilityCatalog: filepath.Join(dir, "catalog_facilities.json"),
			faction:         "CRFT",
			deferFleets:     "mb",
			overmindDir:     filepath.Join(dir, "overmind"),
			agentsDir:       filepath.Join(dir, "agents"),
			assetsDB:        filepath.Join(dir, "assets.db"),
			marketDB:        filepath.Join(dir, "market.db"),
			refresh:         60,
		},
		logger: log.New(io.Discard, "", 0),
	}
}

// A wrong --overmind-dir must say so on every line and must not turn into a
// confident "no bots on node".
func TestBuildWarnsOnEmptyOvermindDir(t *testing.T) {
	s := testServer(t.TempDir())
	defer s.closeHandles()

	reports, _ := s.build(context.Background())
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}
	r := reports[0]
	for _, want := range []string{"overmind-dir: no fleet yamls found", "overmind-dir: no status files found"} {
		if !slices.Contains(r.StaleSources, want) {
			t.Errorf("StaleSources %v lack %q", r.StaleSources, want)
		}
	}
	for _, n := range r.Nodes {
		if n.ID == "ice_mine" && (n.State != linedash.StateUnknown || slices.Contains(n.Alerts, linedash.AlertNoBotsLive)) {
			t.Errorf("ice_mine state=%q alerts=%v, want unknown without no-bots", n.State, n.Alerts)
		}
	}
}

// A client that goes away mid-build must not fail the build's queries and
// poison the cached report for every viewer.
func TestCurrentIgnoresCancelledRequest(t *testing.T) {
	dir := t.TempDir()
	s := testServer(dir)
	defer s.closeHandles()
	a, err := assets.Open(assets.Config{DBPath: s.opt.assetsDB})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	m, err := market.Open(market.Config{DBPath: s.opt.marketDB, WAL: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, r := range s.current(ctx, false) {
		for _, src := range r.StaleSources {
			if strings.Contains(src, "canceled") {
				t.Errorf("StaleSources entry %q from a cancelled request context", src)
			}
		}
	}
}

// TestCurrentForceRebuilds pins the manual refresh: within the cache window a
// plain call returns the cached build, a forced call rebuilds.
func TestCurrentForceRebuilds(t *testing.T) {
	dir := t.TempDir()
	s := testServer(dir)
	defer s.closeHandles()
	ctx := context.Background()

	s.current(ctx, false)
	s.mu.RLock()
	first := s.builtAt
	s.mu.RUnlock()
	if first.IsZero() {
		t.Fatal("first call did not build")
	}

	s.current(ctx, false)
	s.mu.RLock()
	cached := s.builtAt
	s.mu.RUnlock()
	if !cached.Equal(first) {
		t.Fatalf("unforced call rebuilt inside the cache window: %v -> %v", first, cached)
	}

	s.current(ctx, true)
	s.mu.RLock()
	forcedAt := s.builtAt
	s.mu.RUnlock()
	if !forcedAt.After(first) {
		t.Fatalf("forced call did not rebuild: builtAt still %v", forcedAt)
	}
}

func TestForcedQuery(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"/line/moonshine", false},
		{"/line/moonshine?refresh=1", true},
		{"/api/lines.json?refresh", true},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.url, nil)
		if got := forced(r); got != tc.want {
			t.Errorf("forced(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

const (
	cjBase = "c976fa7df53c87f9616165d7d411b41a"
	cjPOI  = "862e07fa76571d0a76cab3cbaf4046e7"
)

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustExec(t *testing.T, exec func(string, ...any) error, stmts ...string) {
	t.Helper()
	for _, q := range stmts {
		if err := exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
}

// seedDerivation lays out a fleet yaml, a schedule, a knowledge DB, a facility
// catalog, assets.db (faction profile) and an empty market.db: one pinned
// crafter growing sunspindle by hand at Conjunction Junction.
func seedDerivation(t *testing.T, s *server, dir string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, "overmind", "craft-fleet.yaml"),
		"workers:\n  - { agent_id: craftsman-3, role: craftsman, station: "+cjPOI+" }\n")
	mustWrite(t, filepath.Join(dir, "agents", "craftsman-3", "schedule.json"),
		`[{"id":2,"frequency":"daily","command":"craft grow_sunspindle 1"},{"id":3,"frequency":"hourly","command":"capture_storage"}]`)
	mustWrite(t, s.opt.facilityCatalog, `{"items":[]}`)

	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: s.opt.kbDB})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, func(q string, a ...any) error { _, xerr := kb.DB().Exec(q, a...); return xerr },
		`INSERT INTO systems(id, name, position_x, position_y) VALUES ('cloverfield', 'Cloverfield', 0, 0)`,
		`INSERT INTO pois(id, system_id, name, type, position_x, position_y, base_id) VALUES ('`+cjPOI+`', 'cloverfield', 'Conjunction Junction', 'station', 0, 0, '`+cjBase+`')`,
		`INSERT INTO bases(id, poi_id, name, condition, satisfaction_pct) VALUES ('`+cjBase+`', '`+cjPOI+`', 'Conjunction Junction', 'thriving', 100)`,
		`INSERT INTO recipes(id, name, category, crafting_time) VALUES ('grow_sunspindle', 'Grow Sunspindle', 'Hydroponics', 360)`,
		`INSERT INTO recipe_inputs(recipe_id, item_id, quantity) VALUES ('grow_sunspindle', 'purified_water', 300)`,
		`INSERT INTO recipe_outputs(recipe_id, item_id, quantity) VALUES ('grow_sunspindle', 'sunspindle', 200)`,
	)
	if err = kb.Close(); err != nil {
		t.Fatal(err)
	}

	a, err := assets.Open(assets.Config{DBPath: s.opt.assetsDB})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, func(q string, args ...any) error { _, xerr := a.DB().Exec(q, args...); return xerr },
		`INSERT INTO faction_profile(faction_id, tag) VALUES ('f-crft', 'CRFT')`)
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	m, err := market.Open(market.Config{DBPath: s.opt.marketDB, WAL: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestServerBuildsFromDerivation(t *testing.T) {
	dir := t.TempDir()
	s := testServer(dir)
	defer s.closeHandles()
	seedDerivation(t, s, dir)

	reports := s.current(context.Background(), true)
	var ids []string
	for _, r := range reports {
		ids = append(ids, r.ID)
	}
	if !slices.Contains(ids, cjBase+":grow_sunspindle") {
		t.Fatalf("derived line ids %v lack %s:grow_sunspindle", ids, cjBase)
	}
	s.mu.RLock()
	warnings := s.warnings
	s.mu.RUnlock()
	if !slices.Contains(warnings, "craftsman-3: needs hourly capture_action_log") {
		t.Errorf("warnings %v lack the cadence warning", warnings)
	}

	rec := httptest.NewRecorder()
	s.handleBoard(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, want := range []string{"Conjunction Junction", "sunspindle", "needs hourly capture_action_log"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("board lacks %q", want)
		}
	}
}

// Marketbot asks belong to the deferred drone lines: a listing at a station
// whose only pinned agents are in a deferred fleet is dropped; a station with
// none of our pinned agents is a faction order and is kept.
func TestAttributeListingsDropsDeferredFleetStations(t *testing.T) {
	ref := linedash.RefData{Stations: map[string]linedash.Station{
		"cj": {BaseID: "cj"}, "rest": {BaseID: "rest"}, "voss": {BaseID: "voss"},
	}}
	roster := map[string]linedash.RosterEntry{
		"marketbot_traders_rest": {AgentID: "marketbot_traders_rest", Fleet: "mb", Station: "rest", Shopkeep: []string{"x:1"}},
		"marketbot_alhena":       {AgentID: "marketbot_alhena", Fleet: "mb", Station: "voss"},
		"craftsman-10":           {AgentID: "craftsman-10", Fleet: "craft", Station: "voss"},
	}
	got := attributeListings([]linedash.Listing{
		{StationID: "rest", Item: "quantum_fragments"},
		{StationID: "cj", Item: "void_dust"},
		{StationID: "voss", Item: "pirate_moonshine"},
	}, roster, ref, []string{"mb"})
	if len(got) != 2 || got[0].StationID != "cj" || got[0].Agent != "" || got[1].StationID != "voss" || got[1].Agent != "craftsman-10" {
		t.Errorf("attributeListings = %+v", got)
	}
}

// An ask is credited to the agent that posted it; a poster in a deferred
// fleet (hauler leftovers) takes the ask off the board.
func TestAttributeListingsCreditsPoster(t *testing.T) {
	ref := linedash.RefData{Stations: map[string]linedash.Station{"gx": {BaseID: "gx"}, "tc": {BaseID: "tc"}}}
	roster := map[string]linedash.RosterEntry{
		"assist-haven": {AgentID: "assist-haven", Fleet: "assist", Station: "gx"},
		"salvager-7":   {AgentID: "salvager-7", Fleet: "haul"},
		"trader-9":     {AgentID: "trader-9", Fleet: "crft-mining"},
	}
	poster := func(station, item string) string {
		return map[string]string{"gx|coolant_fluid": "salvager-7", "tc|pirate_moonshine": "trader-9"}[station+"|"+item]
	}
	got := attributeListingsBy([]linedash.Listing{
		{StationID: "gx", Item: "coolant_fluid"},
		{StationID: "tc", Item: "pirate_moonshine"},
	}, roster, ref, []string{"mb", "haul"}, poster)
	if len(got) != 1 || got[0].StationID != "tc" || got[0].Agent != "trader-9" {
		t.Errorf("attributeListingsBy = %+v, want only tc credited to trader-9", got)
	}
}
