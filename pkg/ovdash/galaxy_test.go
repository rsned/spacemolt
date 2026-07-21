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
		`INSERT INTO connections VALUES ('sol','nova_terra',12.5)`,
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
	// Connections are bidirectional on the node view even though the table
	// stores one row per lane.
	if len(sol.Connections) != 1 || sol.Connections[0] != "nova_terra" {
		t.Fatalf("sol connections wrong: %v", sol.Connections)
	}
	if nt := byID["nova_terra"]; len(nt.Connections) != 1 || nt.Connections[0] != "sol" {
		t.Fatalf("reverse connection missing: %v", nt.Connections)
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
