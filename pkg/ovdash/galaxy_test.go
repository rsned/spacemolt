package ovdash

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
)

// fixtureKB builds a minimal knowledge DB with two connected systems and one
// unconnected one, exercising id/name resolution and lane assembly.
func fixtureKB(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kb.db")
	db, err := sql.Open(sqliteDriver, p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck
	stmts := []string{
		`CREATE TABLE systems (id TEXT PRIMARY KEY, name TEXT NOT NULL,
			position_x REAL NOT NULL, position_y REAL NOT NULL,
			police_level INTEGER DEFAULT 0, empire TEXT DEFAULT '',
			is_stronghold BOOLEAN DEFAULT 0, last_visited_tick INTEGER DEFAULT 0)`,
		`CREATE TABLE connections (from_system TEXT, to_system TEXT, distance REAL)`,
		`INSERT INTO systems VALUES
			('sol','Sol',0,0,10,'solarian',0,100),
			('nova_terra','Nova Terra',50,-30,8,'solarian',0,90),
			('krynn','Krynn',900,900,0,'crimson',1,0)`,
		// The live KB stores both directions of every lane (verified
		// symmetric in production); the fixture mirrors that so the loader's
		// dedupe is actually exercised.
		`INSERT INTO connections VALUES ('sol','nova_terra',12.5), ('nova_terra','sol',12.5)`,
		`CREATE TABLE pois (id TEXT PRIMARY KEY, system_id TEXT NOT NULL,
			name TEXT NOT NULL, type TEXT NOT NULL, description TEXT,
			position_x REAL NOT NULL, position_y REAL NOT NULL,
			base_id TEXT, last_updated_tick INTEGER DEFAULT 0,
			class TEXT DEFAULT '', hidden BOOLEAN NOT NULL DEFAULT 0)`,
		`INSERT INTO pois (id, system_id, name, type, position_x, position_y, class, hidden) VALUES
			('sol_star','sol','Sol','sun',0,0,'G2V',0),
			('earth','sol','Earth','planet',1,0,'terran',0),
			('mars','sol','Mars','planet',2,-0.3,'arid',0),
			('sol_hideout','sol','Hidden Cache','anomaly',5,5,'',1)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestLoadGalaxyBuildsSystemsAndConnections(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatalf("LoadGalaxy: %v", err)
	}
	if len(g.Systems) != 3 {
		t.Fatalf("want 3 systems, got %d", len(g.Systems))
	}
	byID := map[string]SystemNode{}
	for _, s := range g.Systems {
		byID[s.ID] = s
	}
	sol := byID["sol"]
	if sol.Name != "Sol" || sol.X != 0 || sol.Police != 10 || sol.Empire != "solarian" {
		t.Fatalf("sol fields wrong: %+v", sol)
	}
	// The fixture stores both directions of the sol<->nova_terra lane (like
	// the live KB); each must appear exactly once per side, not doubled.
	if len(sol.Connections) != 1 || sol.Connections[0] != "nova_terra" {
		t.Fatalf("sol connections wrong (want exactly one, deduped): %v", sol.Connections)
	}
	if nt := byID["nova_terra"]; len(nt.Connections) != 1 || nt.Connections[0] != "sol" {
		t.Fatalf("nova_terra connections wrong (want exactly one, deduped): %v", nt.Connections)
	}
	if k := byID["krynn"]; !k.Stronghold || len(k.Connections) != 0 {
		t.Fatalf("krynn fields wrong: %+v", k)
	}
}

func TestResolveNameIsCaseInsensitive(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Nova Terra", "nova terra", "NOVA TERRA"} {
		if id, ok := g.ResolveName(name); !ok || id != "nova_terra" {
			t.Fatalf("ResolveName(%q) = %q, %v", name, id, ok)
		}
	}
	if _, ok := g.ResolveName("Atlantis"); ok {
		t.Fatal("unknown name must not resolve")
	}
}

// Workers do not all report the same thing in their status file's `system`
// field: most send the display name ("Nova Terra"), but some send the system
// ID ("nova_terra"). Live on 2026-08-11, assist-frontier and craftsman-9/10
// were all docked at mobile_capital and reported `deep_range`, so they
// resolved against no name, fell into OffMap, and vanished from the dashboard
// map even though Deep Range is an ordinary system with a station.
//
// An id is unambiguous -- ids and display names cannot collide, since a name
// carries a space where the id carries an underscore -- so accepting both is
// safe and strictly widens what the map can place.
func TestResolveNameAcceptsASystemIDToo(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"nova_terra", "NOVA_TERRA"} {
		if id, ok := g.ResolveName(in); !ok || id != "nova_terra" {
			t.Errorf("ResolveName(%q) = %q, %v; want nova_terra, true", in, id, ok)
		}
	}
	// Still no false positives: an unknown id must stay unresolved so a
	// genuinely off-map agent is still reported as such.
	if _, ok := g.ResolveName("deep_range"); ok {
		t.Error("an id absent from this galaxy must not resolve")
	}
}

func TestSystemNodeJSONShapeMatchesUseGalaxyMap(t *testing.T) {
	n := SystemNode{ID: "sol", Name: "Sol", X: 1, Y: 2, Empire: "solarian",
		Police: 10, Stronghold: true, LastVisited: 7, Connections: []string{"a"}}
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	// The frontend hook (frontend/src/lib/useGalaxyMap.ts GalaxySystem)
	// requires exactly these keys.
	for _, k := range []string{"id", "name", "position", "police_level",
		"last_visited_tick", "empire", "is_stronghold", "connections"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("JSON missing key %q: %s", k, b)
		}
	}
	pos, ok := m["position"].(map[string]any)
	if !ok || pos["x"] != 1.0 || pos["y"] != 2.0 {
		t.Fatalf("position shape wrong: %v", m["position"])
	}
}

func TestSystemPOIsSortedAndFiltered(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatalf("LoadGalaxy: %v", err)
	}
	pois, ok := g.SystemPOIs("sol")
	if !ok {
		t.Fatal("sol should exist")
	}
	// Hidden POI excluded; sorted by orbital radius (sun first).
	if len(pois) != 3 {
		t.Fatalf("want 3 pois, got %d: %+v", len(pois), pois)
	}
	if pois[0].ID != "sol_star" || pois[1].ID != "earth" || pois[2].ID != "mars" {
		t.Fatalf("wrong order: %+v", pois)
	}
	if pois[1].Class != "terran" || pois[1].X != 1 || pois[1].Type != "planet" {
		t.Fatalf("earth fields wrong: %+v", pois[1])
	}
}

func TestSystemPOIsKnownSystemWithoutPOIs(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatalf("LoadGalaxy: %v", err)
	}
	pois, ok := g.SystemPOIs("krynn") // exists in fixture, has no pois rows
	if !ok || pois == nil || len(pois) != 0 {
		t.Fatalf("want empty non-nil slice + ok, got %v %v", pois, ok)
	}
}

func TestSystemPOIsUnknownSystem(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKB(t))
	if err != nil {
		t.Fatalf("LoadGalaxy: %v", err)
	}
	if _, ok := g.SystemPOIs("atlantis"); ok {
		t.Fatal("unknown system must return ok=false")
	}
}

// fixtureKBNoPOIs mirrors the pre-POI schema (and the cmd/overmind-dashboard
// fixture): LoadGalaxy must tolerate a KB without a pois table.
func fixtureKBNoPOIs(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kb-nopois.db")
	db, err := sql.Open(sqliteDriver, p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck
	stmts := []string{
		`CREATE TABLE systems (id TEXT PRIMARY KEY, name TEXT NOT NULL,
			position_x REAL NOT NULL, position_y REAL NOT NULL,
			police_level INTEGER DEFAULT 0, empire TEXT DEFAULT '',
			is_stronghold BOOLEAN DEFAULT 0, last_visited_tick INTEGER DEFAULT 0)`,
		`CREATE TABLE connections (from_system TEXT, to_system TEXT, distance REAL)`,
		`INSERT INTO systems VALUES ('sol','Sol',0,0,10,'solarian',0,1)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestLoadGalaxyToleratesMissingPOIsTable(t *testing.T) {
	g, err := LoadGalaxy(context.Background(), fixtureKBNoPOIs(t))
	if err != nil {
		t.Fatalf("LoadGalaxy without pois table: %v", err)
	}
	pois, ok := g.SystemPOIs("sol")
	if !ok || len(pois) != 0 {
		t.Fatalf("want ok+empty, got %v %v", pois, ok)
	}
}
