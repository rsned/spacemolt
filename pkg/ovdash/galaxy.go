// Package ovdash contains the read-only data plumbing behind
// cmd/overmind-dashboard: galaxy topology, live fleet snapshots, SSE deltas,
// and earnings accounting. Nothing in this package writes anywhere.
package ovdash

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// sqliteDriver is the database/sql driver name used across this package.
// Must match the driver imported above (verify against pkg/market's import).
const sqliteDriver = "sqlite"

// SystemNode is one galaxy system in the shape the frontend's useGalaxyMap
// hook already consumes (GalaxySystem in frontend/src/lib/useGalaxyMap.ts).
type SystemNode struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	X           float64  `json:"-"`
	Y           float64  `json:"-"`
	Empire      string   `json:"empire"`
	Police      int      `json:"police_level"`
	Stronghold  bool     `json:"is_stronghold"`
	LastVisited int64    `json:"last_visited_tick"`
	Connections []string `json:"connections"`
}

// MarshalJSON emits the nested {"position":{"x","y"}} object the frontend
// expects while keeping flat float fields internally.
func (n SystemNode) MarshalJSON() ([]byte, error) {
	type alias SystemNode // no methods: avoids recursion
	return json.Marshal(struct {
		alias
		Position struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		} `json:"position"`
	}{alias: alias(n), Position: struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}{n.X, n.Y}})
}

// POI is one point of interest inside a system, in the shape the frontend's
// SystemView consumes. Coordinates are the KB's system-local units.
type POI struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Type  string  `json:"type"`
	Class string  `json:"class"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
}

// Galaxy is the immutable topology loaded once at startup.
type Galaxy struct {
	Systems []SystemNode
	byName  map[string]string // lower(name) -> id
	ids     map[string]bool    // system id -> exists
	pois    map[string][]POI   // system id -> POIs
}

// LoadGalaxy reads systems and connections from the knowledge DB (read-only).
func LoadGalaxy(ctx context.Context, dbPath string) (*Galaxy, error) {
	db, err := sql.Open(sqliteDriver, "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open kb: %w", err)
	}
	defer db.Close() //nolint:errcheck

	rows, err := db.QueryContext(ctx, `SELECT id, name, position_x, position_y,
		police_level, empire, is_stronghold, last_visited_tick FROM systems`)
	if err != nil {
		return nil, fmt.Errorf("query systems: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	g := &Galaxy{
		byName: map[string]string{},
		ids:    map[string]bool{},
		pois:   map[string][]POI{},
	}
	idx := map[string]int{}
	for rows.Next() {
		var n SystemNode
		if err := rows.Scan(&n.ID, &n.Name, &n.X, &n.Y, &n.Police, &n.Empire,
			&n.Stronghold, &n.LastVisited); err != nil {
			return nil, fmt.Errorf("scan system: %w", err)
		}
		idx[n.ID] = len(g.Systems)
		g.byName[strings.ToLower(n.Name)] = n.ID
		g.ids[n.ID] = true
		g.Systems = append(g.Systems, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	crows, err := db.QueryContext(ctx, `SELECT from_system, to_system FROM connections`)
	if err != nil {
		return nil, fmt.Errorf("query connections: %w", err)
	}
	defer crows.Close() //nolint:errcheck
	// The KB stores connections symmetrically (both (a,b) and (b,a) rows for
	// every lane); dedupe on the normalized pair so each lane is expanded
	// into both endpoints' Connections exactly once.
	seenPairs := map[[2]string]bool{}
	for crows.Next() {
		var from, to string
		if err := crows.Scan(&from, &to); err != nil {
			return nil, fmt.Errorf("scan connection: %w", err)
		}
		pair := [2]string{from, to}
		if pair[0] > pair[1] {
			pair[0], pair[1] = pair[1], pair[0]
		}
		if seenPairs[pair] {
			continue
		}
		seenPairs[pair] = true
		if i, ok := idx[from]; ok {
			g.Systems[i].Connections = append(g.Systems[i].Connections, to)
		}
		if i, ok := idx[to]; ok {
			g.Systems[i].Connections = append(g.Systems[i].Connections, from)
		}
	}
	if err := crows.Err(); err != nil {
		return nil, err
	}

	// POIs power the zoomed per-system view. Older KB snapshots (and test
	// fixtures) predate the pois table; treat its absence as "no POIs".
	var havePOIs int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='pois'`,
	).Scan(&havePOIs); err != nil {
		return nil, fmt.Errorf("check pois table: %w", err)
	}
	if havePOIs > 0 {
		prows, err := db.QueryContext(ctx, `SELECT id, system_id, name, type,
			COALESCE(class,''), position_x, position_y FROM pois WHERE hidden = 0`)
		if err != nil {
			return nil, fmt.Errorf("query pois: %w", err)
		}
		defer prows.Close() //nolint:errcheck
		for prows.Next() {
			var p POI
			var systemID string
			if err := prows.Scan(&p.ID, &systemID, &p.Name, &p.Type, &p.Class,
				&p.X, &p.Y); err != nil {
				return nil, fmt.Errorf("scan poi: %w", err)
			}
			g.pois[systemID] = append(g.pois[systemID], p)
		}
		if err := prows.Err(); err != nil {
			return nil, err
		}
		for _, list := range g.pois {
			sort.Slice(list, func(i, j int) bool {
				ri := list[i].X*list[i].X + list[i].Y*list[i].Y
				rj := list[j].X*list[j].X + list[j].Y*list[j].Y
				if ri != rj {
					return ri < rj
				}
				return list[i].Name < list[j].Name
			})
		}
	}

	for i := range g.Systems {
		sort.Strings(g.Systems[i].Connections)
	}
	return g, nil
}

// ResolveName maps a display name ("Nova Terra") to a system id, case-insensitively.
func (g *Galaxy) ResolveName(name string) (string, bool) {
	id, ok := g.byName[strings.ToLower(name)]
	return id, ok
}

// SystemPOIs returns the POIs for a system id (sorted by orbital radius,
// hidden excluded) and whether the system exists at all. A known system with
// no POIs yields an empty non-nil slice so it JSON-encodes as [].
func (g *Galaxy) SystemPOIs(systemID string) ([]POI, bool) {
	if !g.ids[systemID] {
		return nil, false
	}
	if list, ok := g.pois[systemID]; ok {
		return list, true
	}
	return []POI{}, true
}
