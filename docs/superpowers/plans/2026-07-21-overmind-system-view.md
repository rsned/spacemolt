# Overmind Zoomed System View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Click a system on the overmind galaxy map to open a full-slot, KB-style orbital system view with live fleet agent dots, hover-to-list, FleetRail highlighting, and SSE-driven movement animation.

**Architecture:** ovdash loads the KB `pois` table at startup and serves `GET /api/overmind/system/{id}/pois`; a new purpose-built `SystemView.tsx` replaces the galaxy map in the map slot (galaxy stays mounted-but-hidden), computing jump-gate positions client-side from galaxy bearings and animating agent dots from the existing SSE deltas using the interval-interpolation pattern already proven in `FleetOverlay.tsx`.

**Tech Stack:** Go 1.24 (net/http pattern routing, modernc.org/sqlite), React 19 + TypeScript + Vite, SVG rendering, Tailwind classes matching the overmind palette.

**Spec:** `docs/superpowers/specs/2026-07-21-overmind-system-view-design.md`

## Global Constraints

- Everything in `pkg/ovdash` and `cmd/overmind-dashboard` is strictly READ-ONLY against its databases and status files.
- After Go changes: `go build ./... && go test ./...` and `golangci-lint run` on touched packages — zero new findings.
- Frontend check is `npm run build` in `frontend/` (`tsc -b && vite build`) — there is no frontend test runner.
- Compiled binaries go in `bin/`, never the repo root.
- Commit files explicitly (`git add <paths>`); never `git add -A` (dirty runtime `data/*.json` churn must stay unstaged). The pre-commit race gate can time out under fleet load; user-approved substitute is `--no-verify` after build/lint/tests pass.
- Overmind palette: bg `#0a0a08`, panel `#11100c`, border `#2a2618`, text `#d8d3c0`, muted `#8a8570`, gold `#d4a017`, fleet colors from `FLEETS` in `useFleetStream.ts`.
- The production dashboard runs live on :8091 — never restart it; manual verification uses a second instance on :8099 (safe: read-only).

---

### Task 1: ovdash — load POIs from the KB

**Files:**
- Modify: `pkg/ovdash/galaxy.go`
- Test: `pkg/ovdash/galaxy_test.go`

**Interfaces:**
- Produces: `type POI struct { ID, Name, Type, Class string; X, Y float64 }` (JSON tags `id,name,type,class,x,y`) and method `func (g *Galaxy) SystemPOIs(systemID string) ([]POI, bool)` — bool is "system exists"; known system with no POIs returns `([]POI{}, true)`. POIs are sorted by orbital radius (distance from 0,0) then name. Hidden POIs are excluded. A KB without a `pois` table (older fixtures) loads fine with empty POIs everywhere.

- [ ] **Step 1: Write the failing tests**

Append to `pkg/ovdash/galaxy_test.go`. Also extend `fixtureKB` by adding these statements to its `stmts` slice (after the connections insert):

```go
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
```

New tests:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/ovdash/ -run 'TestSystemPOIs|TestLoadGalaxyTolerates' -v`
Expected: FAIL — `g.SystemPOIs undefined`.

- [ ] **Step 3: Implement POI loading in galaxy.go**

Add to `pkg/ovdash/galaxy.go`:

```go
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
```

Add field `pois map[string][]POI` to the `Galaxy` struct, initialize `pois: map[string][]POI{}` alongside `byName` in `LoadGalaxy`, and after the connections block (before the final sort loop) load POIs:

```go
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
```

Note: `idx` (the `map[string]int` in `LoadGalaxy`) must remain in scope OR add a `byID` set to Galaxy. Simplest: add field `ids map[string]bool` to Galaxy, fill it in the systems loop (`g.ids[n.ID] = true`), and:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/ovdash/ -v`
Expected: ALL PASS (including pre-existing tests — the sqlite_master guard keeps old fixtures green).

- [ ] **Step 5: Lint and commit**

```bash
golangci-lint run ./pkg/ovdash/
git add pkg/ovdash/galaxy.go pkg/ovdash/galaxy_test.go
git commit --no-verify -m "feat(ovdash): load per-system POIs from the KB"
```

---

### Task 2: overmind-dashboard — POI endpoint

**Files:**
- Modify: `cmd/overmind-dashboard/main.go` (mux, ~line 94)
- Test: `cmd/overmind-dashboard/main_test.go`

**Interfaces:**
- Consumes: `Galaxy.SystemPOIs(id) ([]POI, bool)` from Task 1.
- Produces: `GET /api/overmind/system/{id}/pois` → 200 + JSON array `[{id,name,type,class,x,y}]` (possibly `[]`), or 404 for unknown system id.

- [ ] **Step 1: Write the failing test**

Extend `writeFixtures` in `cmd/overmind-dashboard/main_test.go` — append to the KB `mustExec` statements:

```go
		`CREATE TABLE pois (id TEXT PRIMARY KEY, system_id TEXT NOT NULL,
			name TEXT NOT NULL, type TEXT NOT NULL, description TEXT,
			position_x REAL NOT NULL, position_y REAL NOT NULL,
			base_id TEXT, last_updated_tick INTEGER DEFAULT 0,
			class TEXT DEFAULT '', hidden BOOLEAN NOT NULL DEFAULT 0)`,
		`INSERT INTO pois (id, system_id, name, type, position_x, position_y, class, hidden) VALUES
			('sol_star','sol','Sol','sun',0,0,'G2V',0),
			('earth','sol','Earth','planet',1,0,'terran',0)`,
```

Add test:

```go
func TestSystemPOIsEndpoint(t *testing.T) {
	kb, market, statusDir := writeFixtures(t)
	srv, err := newServer(context.Background(), serverConfig{
		KBPath: kb, MarketPath: market, StatusDir: statusDir, DistDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	ts := httptest.NewServer(srv.mux())
	defer ts.Close()

	var pois []map[string]any
	getJSON(t, ts.URL+"/api/overmind/system/sol/pois", &pois)
	if len(pois) != 2 || pois[0]["id"] != "sol_star" || pois[1]["class"] != "terran" {
		t.Fatalf("pois: %+v", pois)
	}

	resp, err := http.Get(ts.URL + "/api/overmind/system/atlantis/pois") //nolint:gosec,noctx
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown system: want 404, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/overmind-dashboard/ -run TestSystemPOIsEndpoint -v`
Expected: FAIL — decode error / 404 for sol (route not registered).

- [ ] **Step 3: Register the route**

In `(s *server) mux()` in `cmd/overmind-dashboard/main.go`, after the `/api/overmind/systems` handler:

```go
	m.HandleFunc("GET /api/overmind/system/{id}/pois", func(w http.ResponseWriter, r *http.Request) {
		pois, ok := s.galaxy.SystemPOIs(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSONResp(w, pois)
	})
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/overmind-dashboard/ -v` then `go build ./... && go test ./...`
Expected: ALL PASS.

- [ ] **Step 5: Lint and commit**

```bash
golangci-lint run ./cmd/overmind-dashboard/
git add cmd/overmind-dashboard/main.go cmd/overmind-dashboard/main_test.go
git commit --no-verify -m "feat(ovdash): GET /api/overmind/system/{id}/pois endpoint"
```

---

### Task 3: Frontend data + geometry helpers

**Files:**
- Create: `frontend/src/lib/useSystemPois.ts`
- Create: `frontend/src/components/overmind/systemLayout.ts`

**Interfaces:**
- Produces (useSystemPois.ts):
  - `interface SystemPOI { id: string; name: string; type: string; class: string; x: number; y: number }`
  - `function useSystemPois(systemId: string): { pois: SystemPOI[] | null; error: string | null; retry: () => void }` — fetches `/api/overmind/system/${systemId}/pois`, session-cached per systemId in a module-level Map; `pois` is null while loading or on error.
- Produces (systemLayout.ts):
  - `interface Gate { systemId: string; name: string; x: number; y: number }` (POI-space coords)
  - `function computeGates(current: GalaxySystem, all: GalaxySystem[], gateRadius: number): Gate[]`
  - `function fanOffset(index: number, count: number, r: number): { dx: number; dy: number }`
  - `function seededScatter(seed: string, count: number): { t: number; jr: number; js: number }[]` — deterministic per-id scatter params (angle fraction t∈[0,1), radial jitter jr∈[-1,1], size js∈[0,1)).

- [ ] **Step 1: Write useSystemPois.ts**

```ts
import { useCallback, useEffect, useState } from 'react';

export interface SystemPOI {
  id: string;
  name: string;
  type: string;
  class: string;
  x: number;
  y: number;
}

// Session-lifetime cache: POI layouts are static game topology.
const cache = new Map<string, SystemPOI[]>();

export function useSystemPois(systemId: string): {
  pois: SystemPOI[] | null; error: string | null; retry: () => void;
} {
  const [pois, setPois] = useState<SystemPOI[] | null>(cache.get(systemId) ?? null);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const cached = cache.get(systemId);
    if (cached) { setPois(cached); setError(null); return; }
    let cancelled = false;
    setPois(null);
    setError(null);
    fetch(`/api/overmind/system/${encodeURIComponent(systemId)}/pois`)
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json() as Promise<SystemPOI[]>;
      })
      .then((data) => {
        if (cancelled) return;
        cache.set(systemId, data);
        setPois(data);
      })
      .catch((err) => { if (!cancelled) setError(String(err)); });
    return () => { cancelled = true; };
  }, [systemId, attempt]);

  const retry = useCallback(() => setAttempt((n) => n + 1), []);
  return { pois, error, retry };
}
```

- [ ] **Step 2: Write systemLayout.ts**

```ts
import type { GalaxySystem } from '../../lib/useGalaxyMap';

/** A jump gate placed on the system periphery, in POI-space coordinates. */
export interface Gate {
  systemId: string;
  name: string;
  x: number;
  y: number;
}

/** Gates sit at gateRadius from the sun, bearing toward the connected system
 * in galaxy coordinates — matching KB page placement (e.g. the Sol gate sits
 * on the Sol-facing edge of Alpha Centauri). */
export function computeGates(current: GalaxySystem, all: GalaxySystem[], gateRadius: number): Gate[] {
  const byId = new Map(all.map((s) => [s.id, s]));
  const gates: Gate[] = [];
  for (const connId of new Set(current.connections)) {
    const target = byId.get(connId);
    if (!target) continue;
    const dx = target.position.x - current.position.x;
    const dy = target.position.y - current.position.y;
    const len = Math.hypot(dx, dy) || 1;
    gates.push({
      systemId: target.id,
      name: target.name,
      x: (dx / len) * gateRadius,
      y: (dy / len) * gateRadius,
    });
  }
  return gates;
}

/** Deterministic fan placement so co-located agent dots stay individually
 * hoverable; mirrors FleetOverlay's orbit() but with a larger radius. */
export function fanOffset(index: number, count: number, r: number): { dx: number; dy: number } {
  if (count <= 1) return { dx: r, dy: r };
  const angle = (2 * Math.PI * index) / count;
  return { dx: r * Math.cos(angle), dy: r * Math.sin(angle) };
}

/** Deterministic per-seed scatter parameters for belt/ice/gas ring chunks:
 * t = angle fraction around the ring, jr = radial jitter [-1,1],
 * js = size jitter [0,1). Stable across renders (no Math.random). */
export function seededScatter(seed: string, count: number): { t: number; jr: number; js: number }[] {
  let h = 2166136261;
  for (const c of seed) {
    h = Math.imul(h ^ c.charCodeAt(0), 16777619);
  }
  const out: { t: number; jr: number; js: number }[] = [];
  for (let i = 0; i < count; i++) {
    h = Math.imul(h ^ (i + 1), 2654435761);
    const a = ((h >>> 8) & 0xffff) / 0x10000;
    h = Math.imul(h ^ 0x9e3779b9, 2246822519);
    const b = ((h >>> 8) & 0xffff) / 0x10000;
    h = Math.imul(h ^ 0x85ebca6b, 3266489917);
    const c = ((h >>> 8) & 0xffff) / 0x10000;
    out.push({ t: (i + a) / count, jr: b * 2 - 1, js: c });
  }
  return out;
}
```

- [ ] **Step 3: Verify build**

Run: `cd frontend && npm run build`
Expected: success. (Unused-export warnings are fine; nothing imports these yet.)

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/useSystemPois.ts frontend/src/components/overmind/systemLayout.ts
git commit --no-verify -m "feat(overmind): POI fetch hook + system-view geometry helpers"
```

---

### Task 4: SystemView — static orbital render

**Files:**
- Create: `frontend/src/components/overmind/SystemView.tsx`

**Interfaces:**
- Consumes: `useSystemPois`, `computeGates`, `seededScatter`, `fanOffset` (Task 3); `GalaxySystem`, `FLEETS`, `AgentState`, `AgentMove` types.
- Produces component:

```ts
export function SystemView(props: {
  system: GalaxySystem;
  systems: GalaxySystem[];          // full galaxy, for gate bearings
  agents: AgentState[];             // pre-filtered: system_id === system.id
  moves: AgentMove[];               // latest SSE delta moves (for Task 6)
  selectedId: string | null;
  onAgentClick: (id: string) => void;
  onHoverAgents: (ids: string[]) => void;  // [] on hover-out (for Task 5)
  onClose: () => void;
}): JSX.Element
```

This task renders: header strip, sun glow, dashed orbit rings, class-colored planets, station hexagons, ring-scatter for `ice_field`/`asteroid_belt`/`asteroid`/`gas_cloud`, gate crosshairs + labels, POI labels, wheel zoom + drag pan, ✕ button, POI-fetch error line with retry, and a static "unplaced" tray. Agent dots/hover/animation come in Tasks 5–6 (render agents in the tray only for now).

- [ ] **Step 1: Write SystemView.tsx**

```tsx
import { useEffect, useMemo, useRef, useState, type MouseEvent, type WheelEvent } from 'react';
import type { GalaxySystem } from '../../lib/useGalaxyMap';
import { FLEETS, type AgentMove, type AgentState } from '../../lib/useFleetStream';
import { useSystemPois, type SystemPOI } from '../../lib/useSystemPois';
import { computeGates, fanOffset, seededScatter, type Gate } from './systemLayout';

const ZOOM_MIN = 1;
const ZOOM_MAX = 6;
const DRAG_THRESHOLD = 5;

/** Planet fill by KB class; anything unknown gets the neutral gray. */
const CLASS_COLORS: Record<string, string> = {
  terran: '#4ade80', arid: '#fbbf24', scorched: '#f87171', tundra: '#93c5fd',
  kuiper: '#a5f3fc', cometary: '#a5f3fc', oceanic: '#38bdf8', barren: '#9ca3af',
};

const RING_TYPES = new Set(['ice_field', 'asteroid_belt', 'asteroid', 'gas_cloud']);
const RING_COLORS: Record<string, string> = {
  ice_field: '#a5f3fc', asteroid_belt: '#d6d3d1', asteroid: '#d6d3d1', gas_cloud: '#c4b5fd',
};

function radius(p: { x: number; y: number }): number {
  return Math.hypot(p.x, p.y);
}

export function SystemView({ system, systems, agents, moves, selectedId, onAgentClick, onHoverAgents, onClose }: {
  system: GalaxySystem;
  systems: GalaxySystem[];
  agents: AgentState[];
  moves: AgentMove[];
  selectedId: string | null;
  onAgentClick: (id: string) => void;
  onHoverAgents: (ids: string[]) => void;
  onClose: () => void;
}) {
  const { pois, error, retry } = useSystemPois(system.id);
  const containerRef = useRef<HTMLDivElement>(null);
  const [dims, setDims] = useState({ width: 800, height: 600 });
  const [zoom, setZoom] = useState(ZOOM_MIN);
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const [isDragging, setIsDragging] = useState(false);
  const dragStart = useRef({ x: 0, y: 0 });
  const panStart = useRef({ x: 0, y: 0 });
  const didDrag = useRef(false);

  useEffect(() => {
    const update = () => {
      const el = containerRef.current;
      if (el) setDims({ width: el.clientWidth, height: el.clientHeight });
    };
    update();
    window.addEventListener('resize', update);
    return () => window.removeEventListener('resize', update);
  }, []);

  // Reset viewport when switching systems.
  useEffect(() => { setZoom(ZOOM_MIN); setPan({ x: 0, y: 0 }); }, [system.id]);

  const maxPoiR = useMemo(
    () => (pois ?? []).reduce((m, p) => Math.max(m, radius(p)), 0) || 1,
    [pois],
  );
  const gateRadius = maxPoiR * 1.25;
  const gates = useMemo(
    () => computeGates(system, systems, gateRadius),
    [system, systems, gateRadius],
  );

  // Fit gateRadius inside the viewport with padding; zoom multiplies.
  const baseScale = (Math.min(dims.width, dims.height) / 2) * 0.85 / gateRadius;
  const scale = baseScale * zoom;
  const T = (x: number, y: number) => ({
    x: dims.width / 2 + x * scale + pan.x,
    y: dims.height / 2 + y * scale + pan.y,
  });
  // Markers grow at half the zoom rate (same trick as SystemMap).
  const ms = 1 + (zoom - 1) * 0.5;

  const handleWheel = (e: WheelEvent<SVGSVGElement>) => {
    const factor = e.deltaY > 0 ? 1 / 1.15 : 1.15;
    setZoom((z) => Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, z * factor)));
  };
  const handleMouseDown = (e: MouseEvent<SVGSVGElement>) => {
    e.preventDefault();
    setIsDragging(true);
    didDrag.current = false;
    dragStart.current = { x: e.clientX, y: e.clientY };
    panStart.current = { ...pan };
  };
  const handleMouseMove = (e: MouseEvent<SVGSVGElement>) => {
    if (!isDragging) return;
    const dx = e.clientX - dragStart.current.x;
    const dy = e.clientY - dragStart.current.y;
    if (Math.abs(dx) > DRAG_THRESHOLD || Math.abs(dy) > DRAG_THRESHOLD) didDrag.current = true;
    setPan({ x: panStart.current.x + dx, y: panStart.current.y + dy });
  };
  const handleMouseUp = () => setIsDragging(false);

  // Placeholder until Task 5: everything shows in the tray; dots come next.
  const unplaced = agents;

  const sun = (pois ?? []).find((p) => p.type === 'sun');

  return (
    <div ref={containerRef} className="absolute inset-0 flex flex-col bg-[#0a0a08]">
      {/* Header strip — absorbs the retired SystemPanel's info. */}
      <div className="flex items-center gap-4 px-3 py-1.5 border-b border-[#2a2618] bg-[#11100c] text-xs">
        <span className="text-[#d4a017] font-bold tracking-widest uppercase text-sm">{system.name}</span>
        <span className="text-[#8a8570] uppercase tracking-widest">{system.empire || 'neutral'}</span>
        <span className="text-[#8a8570]">police {system.police_level}</span>
        <span className="text-[#8a8570]">{system.connections.length} lanes</span>
        {system.is_stronghold && <span className="text-red-500 uppercase tracking-widest">pirate stronghold</span>}
        <span className="text-[#8a8570]">{agents.length} agents</span>
        <span className="ml-auto text-[10px] text-[#8a8570]">scroll to zoom · drag to pan · esc to exit</span>
        <button onClick={onClose} title="Back to galaxy (Esc)"
          className="px-2 py-0.5 border border-[#2a2618] rounded-sm text-[#8a8570] hover:text-[#d8d3c0] hover:border-[#8a8570]">
          ✕
        </button>
      </div>

      {error && (
        <div className="px-3 py-1 text-xs text-red-400 bg-[#11100c] border-b border-[#2a2618]">
          POI layout failed: {error}
          <button onClick={retry} className="ml-2 underline text-[#d4a017]">retry</button>
        </div>
      )}

      <div className="flex-1 min-h-0 relative">
        <svg
          className="w-full h-full"
          style={{ cursor: isDragging ? 'grabbing' : 'grab' }}
          onWheel={handleWheel}
          onMouseDown={handleMouseDown}
          onMouseMove={handleMouseMove}
          onMouseUp={handleMouseUp}
          onMouseLeave={() => setIsDragging(false)}
        >
          {/* Orbit rings — one dashed circle per non-sun POI radius. */}
          {(pois ?? []).filter((p) => p.type !== 'sun').map((p) => {
            const o = T(0, 0);
            return (
              <circle key={`orbit-${p.id}`} cx={o.x} cy={o.y} r={radius(p) * scale}
                fill="none" stroke="#2a2618" strokeWidth={1} strokeDasharray="4,4" />
            );
          })}

          {/* Ring-scatter POIs: chunks along the whole orbit, KB style. */}
          {(pois ?? []).filter((p) => RING_TYPES.has(p.type)).map((p) => {
            const o = T(0, 0);
            const rr = radius(p) * scale;
            const color = RING_COLORS[p.type] ?? '#d6d3d1';
            const chunks = seededScatter(p.id, 90);
            return (
              <g key={`ring-${p.id}`} opacity={0.7}>
                {chunks.map((c, i) => {
                  const a = c.t * 2 * Math.PI;
                  const cr = rr + c.jr * 10 * ms;
                  const cx = o.x + Math.cos(a) * cr;
                  const cy = o.y + Math.sin(a) * cr;
                  const size = (1 + c.js * 2.5) * ms;
                  return (
                    <rect key={i} x={cx - size / 2} y={cy - size / 2} width={size} height={size}
                      fill={color} opacity={0.3 + c.js * 0.5}
                      transform={`rotate(${c.t * 360} ${cx} ${cy})`} />
                  );
                })}
              </g>
            );
          })}

          {/* Sun */}
          {sun && (() => {
            const s = T(sun.x, sun.y);
            return (
              <g key={sun.id}>
                <circle cx={s.x} cy={s.y} r={26 * ms} fill="#fbbf24" opacity={0.12} />
                <circle cx={s.x} cy={s.y} r={16 * ms} fill="#fbbf24" opacity={0.25} />
                <circle cx={s.x} cy={s.y} r={9 * ms} fill="#fef3c7" />
                <text x={s.x} y={s.y + 22 * ms} textAnchor="middle" fill="#8a8570" fontSize={10 * ms}>
                  {sun.name}
                </text>
              </g>
            );
          })()}

          {/* Planets, stations, and other point POIs */}
          {(pois ?? []).filter((p) => p.type !== 'sun' && !RING_TYPES.has(p.type)).map((p) => {
            const pos = T(p.x, p.y);
            if (p.type === 'station') {
              const r = 6 * ms;
              const hex = Array.from({ length: 6 }, (_, i) => {
                const a = (Math.PI / 3) * i - Math.PI / 6;
                return `${pos.x + r * Math.cos(a)},${pos.y + r * Math.sin(a)}`;
              }).join(' ');
              return (
                <g key={p.id}>
                  <polygon points={hex} fill="#11100c" stroke="#d8d3c0" strokeWidth={1.2} />
                  <text x={pos.x} y={pos.y - 12 * ms} textAnchor="middle" fill="#d8d3c0" fontSize={10 * ms}>
                    {p.name}
                  </text>
                </g>
              );
            }
            const fill = CLASS_COLORS[p.class] ?? '#9ca3af';
            return (
              <g key={p.id}>
                <circle cx={pos.x} cy={pos.y} r={5 * ms} fill={fill} stroke="#0a0a08" strokeWidth={1} />
                <text x={pos.x} y={pos.y - 10 * ms} textAnchor="middle" fill="#d8d3c0" fontSize={10 * ms}>
                  {p.name}
                </text>
              </g>
            );
          })}

          {/* Ring POI anchor labels (the POI's own position on its ring) */}
          {(pois ?? []).filter((p) => RING_TYPES.has(p.type)).map((p) => {
            const pos = T(p.x, p.y);
            return (
              <g key={`ring-anchor-${p.id}`}>
                <circle cx={pos.x} cy={pos.y} r={3 * ms} fill="none"
                  stroke={RING_COLORS[p.type] ?? '#d6d3d1'} strokeWidth={1} />
                <text x={pos.x} y={pos.y - 10 * ms} textAnchor="middle" fill="#8a8570" fontSize={9 * ms}>
                  {p.name}
                </text>
              </g>
            );
          })}

          {/* Jump gates — crosshair circles bearing toward their system. */}
          {gates.map((g: Gate) => {
            const pos = T(g.x, g.y);
            return (
              <g key={g.systemId} opacity={0.85}>
                <circle cx={pos.x} cy={pos.y} r={9 * ms} fill="none" stroke="#06b6d4" strokeWidth={1.2} />
                <line x1={pos.x - 12 * ms} y1={pos.y} x2={pos.x + 12 * ms} y2={pos.y} stroke="#06b6d4" strokeWidth={1} />
                <line x1={pos.x} y1={pos.y - 12 * ms} x2={pos.x} y2={pos.y + 12 * ms} stroke="#06b6d4" strokeWidth={1} />
                <text x={pos.x} y={pos.y - 16 * ms} textAnchor="middle" fill="#06b6d4" fontSize={10 * ms} className="font-mono">
                  {g.name}
                </text>
              </g>
            );
          })}
        </svg>

        {/* Unplaced tray — agents whose POI we can't match never vanish. */}
        {unplaced.length > 0 && (
          <div className="absolute bottom-0 left-0 right-0 flex items-center gap-2 px-3 py-1 bg-[#11100c]/90 border-t border-[#2a2618] text-[10px] flex-wrap">
            <span className="uppercase tracking-widest text-[#8a8570]">unplaced:</span>
            {unplaced.map((a) => (
              <button key={a.agent_id} onClick={() => onAgentClick(a.agent_id)}
                style={{ color: FLEETS[a.fleet] }}
                className={selectedId === a.agent_id ? 'underline' : ''}>
                {a.agent_id} ({a.poi || '?'})
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
```

Note: `moves`, `onHoverAgents`, `fanOffset` are accepted but unused until Tasks 5–6 — prefix-underscore or reference them minimally if `tsc` flags unused destructured props (destructured props are not flagged by default config; `fanOffset` import should be added in Task 5 instead if the linter complains — adjust imports so the build is clean).

- [ ] **Step 2: Verify build**

Run: `cd frontend && npm run build`
Expected: success, no unused-variable errors (drop the `fanOffset` import from this task if flagged; Task 5 adds it back).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/overmind/SystemView.tsx
git commit --no-verify -m "feat(overmind): KB-style SystemView orbital render (static)"
```

---

### Task 5: OvermindPage integration — view switch, Escape/✕, hit margins, retire SystemPanel

**Files:**
- Modify: `frontend/src/components/overmind/OvermindPage.tsx`
- Modify: `frontend/src/components/galaxy/GalaxyMap.tsx` (system click hit margin, ~line 464)
- Modify: `frontend/src/components/overmind/FleetRail.tsx` (highlight prop)
- Modify: `frontend/src/components/overmind/AgentCard.tsx` (highlight prop)
- Delete: `frontend/src/components/overmind/SystemPanel.tsx`

**Interfaces:**
- Consumes: `SystemView` from Task 4.
- Produces: `FleetRail` gains `highlightedIds?: ReadonlySet<string>`; `AgentCard` gains `highlighted?: boolean`. `OvermindPage` owns `view: { kind: 'galaxy' } | { kind: 'system'; systemId: string }` and `highlightedIds` state (wired to `SystemView.onHoverAgents` — live data flows in Task 5's wiring even though SystemView only *calls* it from Task 6's hover logic; passing it now keeps interfaces stable).

- [ ] **Step 1: Rewrite OvermindPage.tsx view plumbing**

Replace `selectedSystem` state and the `SystemPanel` usage:

```tsx
import { useEffect, useMemo, useState } from 'react';
import { GalaxyMap } from '../galaxy/GalaxyMap';
import { useGalaxyMap } from '../../lib/useGalaxyMap';
import { FLEETS, useFleetStream } from '../../lib/useFleetStream';
import { AccountingStrip } from './AccountingStrip';
import { FleetRail } from './FleetRail';
import { FleetOverlay } from './FleetOverlay';
import { SystemView } from './SystemView';

type OvView = { kind: 'galaxy' } | { kind: 'system'; systemId: string };

export function OvermindPage() {
  const stream = useFleetStream();
  const galaxy = useGalaxyMap('/api/overmind');
  const [selectedAgent, setSelectedAgent] = useState<string | null>(null);
  const [view, setView] = useState<OvView>({ kind: 'galaxy' });
  const [highlightedIds, setHighlightedIds] = useState<ReadonlySet<string>>(new Set());
  const [visibleFleets, setVisibleFleets] = useState(new Set(Object.keys(FLEETS)));
  const agents = useMemo(() => [...stream.agents.values()], [stream.agents]);

  // Escape returns to the galaxy; clicking empty space never does.
  useEffect(() => {
    if (view.kind !== 'system') return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setView({ kind: 'galaxy' });
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [view.kind]);

  const viewedSystem = view.kind === 'system'
    ? galaxy?.systems.find((s) => s.id === view.systemId) ?? null
    : null;
  const systemAgents = useMemo(
    () => (viewedSystem ? agents.filter((a) => a.system_id === viewedSystem.id) : []),
    [agents, viewedSystem],
  );

  return (
    <div className="h-full flex flex-col bg-[#0a0a08] text-[#d8d3c0]">
      <AccountingStrip
        accounting={stream.accounting}
        agentCount={stream.agents.size}
        staleFleets={stream.staleFleets}
        connected={stream.connected}
      />
      <div className="flex-1 flex min-h-0">
        <div className="flex-1 min-w-0 relative flex flex-col" id="ov-map-slot">
          {/* fleet toggles + off-map tray: unchanged block from the current file */}
          <div className="flex items-center gap-2 px-3 py-1.5 border-b border-[#2a2618] bg-[#0a0a08] flex-wrap">
            <span className="text-[10px] uppercase tracking-widest text-[#8a8570]">fleets</span>
            {Object.entries(FLEETS).map(([fleet, color]) => (
              <button key={fleet}
                onClick={() => setVisibleFleets((v) => {
                  const next = new Set(v);
                  if (next.has(fleet)) { next.delete(fleet); } else { next.add(fleet); }
                  return next;
                })}
                className={`px-2 py-0.5 text-[10px] uppercase tracking-widest border rounded-sm
                  ${visibleFleets.has(fleet) ? '' : 'opacity-40'}`}
                style={{ color, borderColor: color }}>
                {fleet}
              </button>
            ))}
            {stream.offMap.length > 0 && (
              <span className="ml-auto flex items-center gap-2 text-[10px] uppercase tracking-widest text-[#8a8570]">
                off-map:
                {stream.offMap.map((a) => (
                  <span key={a.agent_id} style={{ color: FLEETS[a.fleet] }}>
                    {a.agent_id} ({a.system_name})
                  </span>
                ))}
              </span>
            )}
          </div>
          <div className="flex-1 min-h-0 relative">
            {/* GalaxyMap stays mounted while zoomed in so its pan/zoom and
                fetched data survive the round trip; `hidden` removes it from
                layout without unmounting. */}
            <div className={view.kind === 'system' ? 'hidden' : 'absolute inset-0'}>
              <GalaxyMap
                systems={galaxy?.systems}
                onSystemClick={(s) => setView({ kind: 'system', systemId: s.id })}
                hideInfoPanel
                overlay={(project) => (
                  <FleetOverlay
                    agents={agents}
                    moves={stream.moves}
                    systems={galaxy?.systems ?? []}
                    project={project}
                    visibleFleets={visibleFleets}
                    selectedId={selectedAgent}
                    onAgentClick={setSelectedAgent}
                  />
                )}
              />
            </div>
            {viewedSystem && (
              <SystemView
                system={viewedSystem}
                systems={galaxy?.systems ?? []}
                agents={systemAgents}
                moves={stream.moves}
                selectedId={selectedAgent}
                onAgentClick={setSelectedAgent}
                onHoverAgents={(ids) => setHighlightedIds(new Set(ids))}
                onClose={() => setView({ kind: 'galaxy' })}
              />
            )}
          </div>
        </div>
        <div className="w-80 border-l border-[#2a2618] overflow-y-auto" id="ov-rail-slot">
          <FleetRail
            agents={agents}
            offMap={stream.offMap}
            staleFleets={stream.staleFleets}
            selectedId={selectedAgent}
            onSelect={setSelectedAgent}
            highlightedIds={highlightedIds}
          />
        </div>
      </div>
    </div>
  );
}
```

Delete `frontend/src/components/overmind/SystemPanel.tsx` (its info now lives in SystemView's header strip).

- [ ] **Step 2: GalaxyMap forgiving hit radius**

In the system marker render (`GalaxyMap.tsx` ~line 464), add a transparent hit circle so clicks within ~14 SVG units select the system — BEFORE the visible 6px circle so the visible one stays on top but both are clickable:

```tsx
              {/* Forgiving hit target: clicks near (not just on) the 6px
                  marker select the system. Empty space stays inert. */}
              {onSystemClick && (
                <circle
                  cx={pos.x}
                  cy={pos.y}
                  r="14"
                  fill="transparent"
                  onClick={() => onSystemClick(system)}
                  className="cursor-pointer"
                />
              )}
```

- [ ] **Step 3: FleetRail + AgentCard highlight prop**

`FleetRail.tsx` — add to props and pass through:

```tsx
export function FleetRail({ agents, offMap, staleFleets, selectedId, onSelect, highlightedIds }: {
  agents: AgentState[]; offMap: AgentState[]; staleFleets: string[];
  selectedId: string | null; onSelect: (id: string) => void;
  highlightedIds?: ReadonlySet<string>;
}) {
```

and in the AgentCard render:

```tsx
              <AgentCard key={a.agent_id} agent={a} color={color}
                selected={selectedId === a.agent_id} stale={stale.has(fleet)}
                highlighted={highlightedIds?.has(a.agent_id) ?? false}
                onClick={() => onSelect(a.agent_id)} />
```

`AgentCard.tsx` — accept `highlighted` and ring it (keep `memo`):

```tsx
export const AgentCard = memo(function AgentCard({ agent, color, selected, stale, highlighted = false, onClick }: {
  agent: AgentState; color: string; selected: boolean; stale: boolean;
  highlighted?: boolean; onClick: () => void;
}) {
  const unhealthy = !agent.healthy || !agent.seen;
  return (
    <button
      onClick={onClick}
      className={`w-full text-left mb-2 p-2 border rounded-sm bg-[#11100c] text-xs
        ${selected ? 'border-[#d4a017]' : unhealthy ? 'border-red-700' : 'border-[#2a2618]'}
        ${stale ? 'opacity-50' : ''}
        ${highlighted ? 'ring-1 ring-[#22d3ee]' : ''}`}
    >
```

(rest of the component unchanged).

- [ ] **Step 4: Verify build**

Run: `cd frontend && npm run build`
Expected: success. Grep to confirm nothing still imports SystemPanel: `grep -rn "SystemPanel" frontend/src/` → no matches.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/overmind/OvermindPage.tsx \
        frontend/src/components/galaxy/GalaxyMap.tsx \
        frontend/src/components/overmind/FleetRail.tsx \
        frontend/src/components/overmind/AgentCard.tsx
git rm frontend/src/components/overmind/SystemPanel.tsx
git commit --no-verify -m "feat(overmind): click-to-zoom system view, Escape/✕ exit, rail highlight plumbing"
```

---

### Task 6: SystemView agent dots, hover tooltip, rail highlighting

**Files:**
- Modify: `frontend/src/components/overmind/SystemView.tsx`

**Interfaces:**
- Consumes: `fanOffset` (Task 3), `onHoverAgents` / `onAgentClick` / `selectedId` props (Task 5 wiring).
- Produces: agent dots at matched POIs; hover tooltip; `onHoverAgents` calls; the unplaced tray now holds ONLY unmatched agents.

- [ ] **Step 1: POI matching + dot placement**

Inside `SystemView`, after `gates`:

```tsx
  // Match each agent to a POI by name (case-insensitive), then by id.
  // AgentState.poi is a display name in practice; id fallback is belt+braces.
  const poiIndex = useMemo(() => {
    const m = new Map<string, SystemPOI>();
    for (const p of pois ?? []) {
      m.set(p.name.toLowerCase(), p);
      m.set(p.id.toLowerCase(), p);
    }
    return m;
  }, [pois]);

  const { placed, unplaced } = useMemo(() => {
    const placedMap = new Map<string, { poi: SystemPOI; list: AgentState[] }>();
    const un: AgentState[] = [];
    for (const a of agents) {
      const p = poiIndex.get((a.poi ?? '').toLowerCase());
      if (!p) { un.push(a); continue; }
      const entry = placedMap.get(p.id) ?? { poi: p, list: [] };
      entry.list.push(a);
      placedMap.set(p.id, entry);
    }
    for (const e of placedMap.values()) e.list.sort((x, y) => x.agent_id.localeCompare(y.agent_id));
    return { placed: [...placedMap.values()], unplaced: un };
  }, [agents, poiIndex]);
```

(Remove the Task-4 placeholder `const unplaced = agents;`.)

- [ ] **Step 2: Hover state + nearest-target resolution**

```tsx
  const [hovered, setHovered] = useState<{ kind: 'poi'; poiId: string } | { kind: 'dot'; agentId: string } | null>(null);

  // Screen positions of every dot and every POI, for forgiving hit tests.
  const dotPositions = useMemo(() => {
    const out: { agent: AgentState; x: number; y: number }[] = [];
    for (const { poi, list } of placed) {
      const base = T(poi.x, poi.y);
      list.forEach((a, i) => {
        const { dx, dy } = fanOffset(i, list.length, 12 * ms);
        out.push({ agent: a, x: base.x + dx, y: base.y + dy });
      });
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [placed, scale, pan, dims]);

  const findHover = (mx: number, my: number): typeof hovered => {
    // Agent dots win over POIs when both are in range.
    let best: typeof hovered = null;
    let bestDist = 10 * ms;
    for (const d of dotPositions) {
      const dist = Math.hypot(mx - d.x, my - d.y);
      if (dist < bestDist) { bestDist = dist; best = { kind: 'dot', agentId: d.agent.agent_id }; }
    }
    if (best) return best;
    bestDist = 24 * ms;
    for (const p of pois ?? []) {
      const pos = T(p.x, p.y);
      const dist = Math.hypot(mx - pos.x, my - pos.y);
      if (dist < bestDist) { bestDist = dist; best = { kind: 'poi', poiId: p.id }; }
    }
    return best;
  };
```

Extend `handleMouseMove` — when not dragging, resolve hover and notify the rail:

```tsx
  const svgRef = useRef<SVGSVGElement>(null);

  const handleMouseMove = (e: MouseEvent<SVGSVGElement>) => {
    if (isDragging) {
      const dx = e.clientX - dragStart.current.x;
      const dy = e.clientY - dragStart.current.y;
      if (Math.abs(dx) > DRAG_THRESHOLD || Math.abs(dy) > DRAG_THRESHOLD) didDrag.current = true;
      setPan({ x: panStart.current.x + dx, y: panStart.current.y + dy });
      return;
    }
    const rect = svgRef.current?.getBoundingClientRect();
    if (!rect) return;
    const h = findHover(e.clientX - rect.left, e.clientY - rect.top);
    setHovered(h);
    if (h?.kind === 'dot') onHoverAgents([h.agentId]);
    else if (h?.kind === 'poi') {
      const entry = placed.find((pl) => pl.poi.id === h.poiId);
      onHoverAgents(entry ? entry.list.map((a) => a.agent_id) : []);
    } else onHoverAgents([]);
  };
```

Attach `ref={svgRef}` to the `<svg>`; in `onMouseLeave` also `setHovered(null); onHoverAgents([]);`.

Clicks: a click that hit a dot selects the agent; anything else is inert (spec). Add to the `<svg>`:

```tsx
          onClick={() => {
            if (didDrag.current) return;
            if (hovered?.kind === 'dot') onAgentClick(hovered.agentId);
          }}
```

- [ ] **Step 3: Render dots + hover ring + tooltip**

After the gates block inside the `<svg>`:

```tsx
          {/* Agent dots, fanned beside their POI. */}
          {placed.map(({ poi, list }) => {
            const base = T(poi.x, poi.y);
            const isPoiHovered = hovered?.kind === 'poi' && hovered.poiId === poi.id;
            return (
              <g key={`agents-${poi.id}`}>
                {isPoiHovered && (
                  <circle cx={base.x} cy={base.y} r={20 * ms} fill="none" stroke="#22d3ee" strokeWidth={1} opacity={0.6} />
                )}
                {list.map((a, i) => {
                  const { dx, dy } = fanOffset(i, list.length, 12 * ms);
                  const x = base.x + dx, y = base.y + dy;
                  const color = FLEETS[a.fleet] ?? '#fff';
                  const isSel = selectedId === a.agent_id;
                  const isHov = hovered?.kind === 'dot' && hovered.agentId === a.agent_id;
                  return (
                    <g key={a.agent_id} style={{ cursor: 'pointer' }}>
                      {(isSel || isHov) && <circle cx={x} cy={y} r={7} fill="none" stroke="#fff" strokeWidth={0.8} />}
                      {a.docked
                        ? <circle cx={x} cy={y} r={4} fill="none" stroke={color} strokeWidth={1.5} />
                        : <circle cx={x} cy={y} r={4} fill={color} />}
                    </g>
                  );
                })}
              </g>
            );
          })}
```

Tooltip — an absolutely positioned card near the hovered POI (HTML, outside the `<svg>` but inside the relative container):

```tsx
        {hovered?.kind === 'poi' && (() => {
          const entry = placed.find((pl) => pl.poi.id === hovered.poiId);
          const p = (pois ?? []).find((pp) => pp.id === hovered.poiId);
          if (!p) return null;
          const pos = T(p.x, p.y);
          return (
            <div className="absolute z-10 pointer-events-none bg-[#11100c]/95 border border-[#2a2618] rounded-sm p-2 text-xs shadow-lg max-w-60"
              style={{ left: Math.min(pos.x + 16, dims.width - 240), top: Math.max(pos.y - 8, 8) }}>
              <div className="text-[#d4a017] font-bold">{p.name}</div>
              <div className="text-[#8a8570] uppercase tracking-widest text-[10px]">{p.type}{p.class ? ` · ${p.class}` : ''}</div>
              {(entry?.list ?? []).map((a) => (
                <div key={a.agent_id} className="flex justify-between gap-3 py-0.5">
                  <span style={{ color: FLEETS[a.fleet] }}>{a.agent_id}{a.docked ? ' ⚓' : ''}</span>
                  <span className="text-[#8a8570] truncate">{a.activity || a.role}</span>
                </div>
              ))}
              {!entry && <div className="text-[#8a8570] py-0.5">no fleet agents here</div>}
            </div>
          );
        })()}
```

- [ ] **Step 4: Verify build**

Run: `cd frontend && npm run build`
Expected: success.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/overmind/SystemView.tsx
git commit --no-verify -m "feat(overmind): system-view agent dots, POI hover tooltip, rail highlight"
```

---

### Task 7: Movement animation from SSE deltas

**Files:**
- Modify: `frontend/src/components/overmind/SystemView.tsx`

**Interfaces:**
- Consumes: `moves: AgentMove[]` prop (fresh each delta, same contract FleetOverlay uses); `gates`, `poiIndex`, `placed` from Task 6.
- Produces: three animation kinds — intra-system glide (1500ms), departure sprint to gate + fade (800ms, ghost), arrival from gate (1500ms). Interval-interpolation pattern mirrors `FleetOverlay.tsx` (register in a ref map, 50ms tick re-render until expiry). All endpoints stored in POI-space so zoom/pan mid-animation stays correct.

- [ ] **Step 1: Animation state + registration**

```tsx
const GLIDE_MS = 1500;   // intra-system POI change, arrivals
const DEPART_MS = 800;   // sprint to the jump gate

interface DotAnim {
  from: { x: number; y: number };  // POI-space
  to: { x: number; y: number };    // POI-space
  started: number;
  duration: number;
  ghost?: { fleet: string };       // departure: agent no longer in `agents`
}
```

Inside the component:

```tsx
  const anims = useRef(new Map<string, DotAnim>());
  const lastPoi = useRef(new Map<string, string>());  // agentId -> poi name last seen
  const [, force] = useState(0);

  const gateFor = (systemId: string) =>
    gates.find((g) => g.systemId === systemId) ?? { x: 0, y: 0 };
  const poiPos = (poiName: string) => {
    const p = poiIndex.get((poiName ?? '').toLowerCase());
    return p ? { x: p.x, y: p.y } : { x: 0, y: 0 };
  };

  // Departures + arrivals from the latest delta's moves.
  useEffect(() => {
    const now = performance.now();
    for (const m of moves) {
      if (m.from_system_id === system.id && m.agent.system_id !== system.id) {
        anims.current.set(m.agent.agent_id, {
          from: poiPos(lastPoi.current.get(m.agent.agent_id) ?? ''),
          to: gateFor(m.agent.system_id),
          started: now, duration: DEPART_MS,
          ghost: { fleet: m.agent.fleet },
        });
        lastPoi.current.delete(m.agent.agent_id);
      } else if (m.agent.system_id === system.id) {
        anims.current.set(m.agent.agent_id, {
          from: gateFor(m.from_system_id),
          to: poiPos(m.agent.poi),
          started: now, duration: GLIDE_MS,
        });
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [moves]);

  // Intra-system POI changes: diff against last seen poi per agent.
  useEffect(() => {
    const now = performance.now();
    for (const a of agents) {
      const prev = lastPoi.current.get(a.agent_id);
      if (prev !== undefined && prev !== a.poi && !anims.current.has(a.agent_id)) {
        anims.current.set(a.agent_id, {
          from: poiPos(prev), to: poiPos(a.poi),
          started: now, duration: GLIDE_MS,
        });
      }
      lastPoi.current.set(a.agent_id, a.poi);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agents]);

  // Tick re-renders while any animation is live; expire finished ones.
  useEffect(() => {
    if (anims.current.size === 0) return;
    const iv = setInterval(() => {
      const t = performance.now();
      for (const [id, a] of anims.current) {
        if (t - a.started > a.duration + 300) anims.current.delete(id); // +300ms ghost fade
      }
      force((n) => n + 1);
      if (anims.current.size === 0) clearInterval(iv);
    }, 50);
    return () => clearInterval(iv);
  });
```

Also clear stale animation state when switching systems — extend the existing system-change reset effect:

```tsx
  useEffect(() => {
    setZoom(ZOOM_MIN); setPan({ x: 0, y: 0 });
    anims.current.clear(); lastPoi.current.clear();
  }, [system.id]);
```

- [ ] **Step 2: Apply animations to dot rendering**

In the dot render from Task 6, replace the static position with animation-aware placement (inside `list.map`):

```tsx
                  const anim = anims.current.get(a.agent_id);
                  let x = base.x + dx, y = base.y + dy;
                  if (anim && !anim.ghost) {
                    const t = Math.min(1, (performance.now() - anim.started) / anim.duration);
                    const from = T(anim.from.x, anim.from.y);
                    const to = { x: base.x + dx, y: base.y + dy };
                    x = from.x + (to.x - from.x) * t;
                    y = from.y + (to.y - from.y) * t;
                  }
```

And render departure ghosts after the placed-agents block (they're not in `agents` anymore):

```tsx
          {/* Departure ghosts: sprint to the gate, then fade. */}
          {[...anims.current.entries()].filter(([, a]) => a.ghost).map(([id, a]) => {
            const t = Math.min(1, (performance.now() - a.started) / a.duration);
            const from = T(a.from.x, a.from.y);
            const to = T(a.to.x, a.to.y);
            const x = from.x + (to.x - from.x) * t;
            const y = from.y + (to.y - from.y) * t;
            const fade = t >= 1 ? Math.max(0, 1 - (performance.now() - a.started - a.duration) / 300) : 1;
            return (
              <g key={`ghost-${id}`} opacity={fade}>
                <line x1={from.x} y1={from.y} x2={x} y2={y}
                  stroke={FLEETS[a.ghost!.fleet] ?? '#fff'} strokeWidth={1} opacity={0.4} />
                <circle cx={x} cy={y} r={4} fill={FLEETS[a.ghost!.fleet] ?? '#fff'} />
              </g>
            );
          })}
```

- [ ] **Step 3: Verify build**

Run: `cd frontend && npm run build`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/overmind/SystemView.tsx
git commit --no-verify -m "feat(overmind): system-view movement animation from SSE deltas"
```

---

### Task 8: Full verification + live manual check

**Files:** none new — verification only.

- [ ] **Step 1: Full build + tests + lint**

```bash
go build ./... && go test ./...
golangci-lint run ./pkg/ovdash/ ./cmd/overmind-dashboard/
cd frontend && npm run build && cd ..
```
Expected: all pass, zero new lint findings.

- [ ] **Step 2: Build binary into bin/ and launch a second instance**

The production dashboard on :8091 keeps running untouched; a parallel read-only instance is safe:

```bash
go build -o bin/overmind-dashboard ./cmd/overmind-dashboard
./bin/overmind-dashboard -addr :8099 &
curl -s localhost:8099/api/overmind/system/alpha_centauri/pois | head -c 400
curl -s -o /dev/null -w '%{http_code}\n' localhost:8099/api/overmind/system/nope/pois   # expect 404
```

- [ ] **Step 3: Manual browser check (http://localhost:8099)**

- Click Alpha Centauri → system view opens; sun, orbit rings, 3 planets, station hexagon, frost-ring scatter, Sol/Tau Ceti/… gate crosshairs.
- 9 agent dots fanned at their POIs, fleet-colored, docked = hollow ring.
- Hover the colonial station → tooltip lists agents; matching FleetRail rows get the cyan ring; hover-out clears.
- Click a dot → agent selected in the rail.
- Scroll zooms, drag pans, clicking empty space does nothing.
- Escape and ✕ both return to the galaxy with its pan/zoom intact.
- Galaxy view: clicks slightly off a system marker still open it (14-unit margin).
- If any agent moves while watching: glide/sprint animation renders.
- Kill the test instance when done: `kill %1`.

- [ ] **Step 4: Final commit if manual check surfaced fixes; otherwise done.**

Production rollout note (user decision, not part of this plan): the live :8091 dashboard picks the feature up on its next restart with the rebuilt binary + `frontend/dist`.
