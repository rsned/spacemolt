# Haul-fleet earnings-per-jump (overmind status page) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a net-of-fuel "Haul fleet efficiency" panel and a per-worker lifetime stats line to the existing `overmind-status` page, sourced entirely from `haul_results`.

**Architecture:** One new read-only aggregator in `pkg/market` (`HaulEfficiencySince`) feeds two render additions in `pkg/ovstatus` (a fleet panel + a per-worker sub-row). `cmd/tools/overmind-status/main.go` opens `market.db` (nil-safe), computes windowed + lifetime aggregates each request, applies a flat fuel model, and passes an optional `*ovstatus.HaulStats` into `Render`. When the DB is absent the page renders byte-identical to today.

**Tech Stack:** Go 1.24, SQLite (`pkg/market` Collector), `html`/`strings` HTML rendering, standard `testing`.

## Global Constraints

- Go 1.24+; use modern idioms (`b.Loop()` in benchmarks if any — none here).
- **No worker change, no schema change, no fleet redeploy, no historical backfill.** SP1 is read-only over existing `haul_results`.
- All new code must pass `golangci-lint` with zero new findings. The pre-commit hook runs `go build ./...` + race tests + lint and **hard-fails on any unused symbol** — every symbol a task adds must be used within that task's own commit, and `go build ./...` must stay green at every commit (never leave a changed function signature with an un-updated caller).
- Fuel model is a flat estimate, panel-only: `fuelCrPerUnit` default `5`, `fuelPerJump` default `9` (both flags). Per-worker line is lifetime **gross** (no fuel term).
- Ship losses render as the literal `—` (em dash), never a fabricated `0`.
- Rows with `jumps_traveled = 0` are excluded from all aggregates (degenerate; zero divisor).
- Never use `--no-verify`.

---

### Task 1: `market.HaulEfficiencySince` aggregator

**Files:**
- Create: `pkg/market/haul_efficiency.go`
- Test: `pkg/market/haul_efficiency_test.go`

**Interfaces:**
- Consumes: existing `Collector` (`c.db *sql.DB`, `pkg/market/collector.go:38`) and the `haul_results` table (`agent_id`, `realized_profit`, `jumps_traveled`, `sold_at`).
- Produces:
  - `type HaulEfficiencyRow struct { AgentID string; Hauls int; SumProfit float64; SumJumps int64 }`
  - `func (c *Collector) HaulEfficiencySince(ctx context.Context, since time.Time) (perAgent []HaulEfficiencyRow, fleet HaulEfficiencyRow, err error)` — per-agent aggregates with `sold_at >= since AND jumps_traveled > 0`, plus the summed fleet row (`fleet.AgentID == ""`). Zero `since` = all-time.

- [ ] **Step 1: Write the failing test**

Create `pkg/market/haul_efficiency_test.go`:

```go
package market

import (
	"context"
	"testing"
	"time"
)

func TestHaulEfficiencySince(t *testing.T) {
	c := newHaulTestCollector(t) // shared helper in haul_results_test.go
	ctx := context.Background()
	rows := []HaulResult{
		{OppID: 1, AgentID: "trader-1", ItemID: "iron", Qty: 10, RealizedProfit: 1000, JumpsTraveled: 4, SoldAt: "2026-07-15T10:00:00Z"},
		{OppID: 2, AgentID: "trader-1", ItemID: "iron", Qty: 10, RealizedProfit: 500, JumpsTraveled: 1, SoldAt: "2026-07-15T11:00:00Z"},
		{OppID: 3, AgentID: "trader-2", ItemID: "gold", Qty: 5, RealizedProfit: 900, JumpsTraveled: 3, SoldAt: "2026-07-15T10:30:00Z"},
		// Old row (before the 07-10 window):
		{OppID: 4, AgentID: "trader-1", ItemID: "iron", Qty: 10, RealizedProfit: 300, JumpsTraveled: 2, SoldAt: "2026-07-01T10:00:00Z"},
		// Zero-jump degenerate row (must be excluded everywhere):
		{OppID: 5, AgentID: "trader-2", ItemID: "gold", Qty: 1, RealizedProfit: 50, JumpsTraveled: 0, SoldAt: "2026-07-15T12:00:00Z"},
	}
	for _, r := range rows {
		if err := c.RecordHaulResult(ctx, r); err != nil {
			t.Fatalf("RecordHaulResult: %v", err)
		}
	}

	since, _ := time.Parse(time.RFC3339, "2026-07-10T00:00:00Z")
	perAgent, fleet, err := c.HaulEfficiencySince(ctx, since)
	if err != nil {
		t.Fatalf("HaulEfficiencySince: %v", err)
	}
	byAgent := map[string]HaulEfficiencyRow{}
	for _, r := range perAgent {
		byAgent[r.AgentID] = r
	}
	if g := byAgent["trader-1"]; g.Hauls != 2 || g.SumProfit != 1500 || g.SumJumps != 5 {
		t.Errorf("trader-1 windowed = %+v, want Hauls2 Profit1500 Jumps5", g)
	}
	if g := byAgent["trader-2"]; g.Hauls != 1 || g.SumProfit != 900 || g.SumJumps != 3 {
		t.Errorf("trader-2 windowed = %+v, want Hauls1 Profit900 Jumps3 (zero-jump excluded)", g)
	}
	if fleet.Hauls != 3 || fleet.SumProfit != 2400 || fleet.SumJumps != 8 {
		t.Errorf("fleet windowed = %+v, want Hauls3 Profit2400 Jumps8", fleet)
	}

	_, fleetAll, err := c.HaulEfficiencySince(ctx, time.Time{})
	if err != nil {
		t.Fatalf("HaulEfficiencySince(all): %v", err)
	}
	if fleetAll.Hauls != 4 || fleetAll.SumProfit != 2700 || fleetAll.SumJumps != 10 {
		t.Errorf("fleet all-time = %+v, want Hauls4 Profit2700 Jumps10", fleetAll)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/market/ -run TestHaulEfficiencySince`
Expected: FAIL — `undefined: HaulEfficiencySince` / `undefined: HaulEfficiencyRow` (compile error).

- [ ] **Step 3: Write minimal implementation**

Create `pkg/market/haul_efficiency.go`:

```go
package market

import (
	"context"
	"fmt"
	"time"
)

// HaulEfficiencyRow is one agent's haul aggregates over a time window.
type HaulEfficiencyRow struct {
	AgentID   string
	Hauls     int
	SumProfit float64 // Σ realized_profit
	SumJumps  int64   // Σ jumps_traveled
}

// HaulEfficiencySince returns per-agent aggregates over haul_results rows with
// sold_at >= since and jumps_traveled > 0, plus the summed fleet row (AgentID
// ""). A zero `since` (time.Time{}) means all-time. Agents with no qualifying
// rows are absent from perAgent. Rows with jumps_traveled = 0 are excluded
// (degenerate and a zero divisor for cr/jump).
func (c *Collector) HaulEfficiencySince(ctx context.Context, since time.Time) (perAgent []HaulEfficiencyRow, fleet HaulEfficiencyRow, err error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT agent_id, COUNT(*), COALESCE(SUM(realized_profit),0), COALESCE(SUM(jumps_traveled),0)
  FROM haul_results
 WHERE sold_at >= ? AND jumps_traveled > 0
 GROUP BY agent_id`, since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, HaulEfficiencyRow{}, fmt.Errorf("query haul efficiency: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var r HaulEfficiencyRow
		if err := rows.Scan(&r.AgentID, &r.Hauls, &r.SumProfit, &r.SumJumps); err != nil {
			return nil, HaulEfficiencyRow{}, fmt.Errorf("scan haul efficiency: %w", err)
		}
		perAgent = append(perAgent, r)
		fleet.Hauls += r.Hauls
		fleet.SumProfit += r.SumProfit
		fleet.SumJumps += r.SumJumps
	}
	if err := rows.Err(); err != nil {
		return nil, HaulEfficiencyRow{}, fmt.Errorf("iterate haul efficiency: %w", err)
	}
	return perAgent, fleet, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/market/ -run TestHaulEfficiencySince`
Expected: PASS.

- [ ] **Step 5: Lint + commit**

Run: `golangci-lint run ./pkg/market/` → expect `0 issues.`

```bash
git add pkg/market/haul_efficiency.go pkg/market/haul_efficiency_test.go
git commit -m "feat(market): HaulEfficiencySince per-agent+fleet cr/jump aggregator"
```

---

### Task 2: ovstatus efficiency types, fuel helper, panel + per-worker line

**Files:**
- Modify: `pkg/ovstatus/ovstatus.go` (add types + `PerJumpMetrics` + `renderEffPanel` + `renderLifetimeLine`; thread `hs *HaulStats` through `Render`→`renderDoc`→`renderSection`→`renderRow`; extend `styleBlock`)
- Modify: `cmd/tools/overmind-status/main.go:93` (update the single `Render` caller to pass `nil` — keeps `go build ./...` green; real data arrives in Task 3)
- Test: `pkg/ovstatus/haul_stats_test.go`

**Interfaces:**
- Consumes: nothing from Task 1 directly (types are independent of `market`).
- Produces (all in package `ovstatus`):
  - `type HaulStats struct { Panel *EffPanel; Lifetime map[string]AgentLifetime }`
  - `type EffPanel struct { WindowLabel string; Hauls int; GrossPerJump, FuelPerJump, NetPerJump float64; Agents []PanelAgent }`
  - `type PanelAgent struct { AgentID string; Hauls int; NetPerJump float64 }`
  - `type AgentLifetime struct { Hauls int; Jumps int64; AvgPerJump float64 }`
  - `func PerJumpMetrics(sumProfit float64, sumJumps int64, fuelPerJump, fuelCrPerUnit float64) (gross, fuelCr, net float64)` — exported so `main` (Task 3) can reuse it.
  - New `Render` signature: `func Render(sources []Source, hs *HaulStats, refresh int, now time.Time) string`.

- [ ] **Step 1: Write the failing test**

Create `pkg/ovstatus/haul_stats_test.go`:

```go
package ovstatus

import (
	"strings"
	"testing"
	"time"
)

func TestPerJumpMetrics(t *testing.T) {
	gross, fuel, net := PerJumpMetrics(24000, 10, 9, 5) // gross 2400, fuel 45, net 2355
	if gross != 2400 || fuel != 45 || net != 2355 {
		t.Fatalf("got gross %v fuel %v net %v, want 2400/45/2355", gross, fuel, net)
	}
	g, f, n := PerJumpMetrics(0, 0, 9, 5)
	if g != 0 || f != 0 || n != 0 {
		t.Fatalf("zero jumps: got %v/%v/%v, want 0/0/0 (no divide)", g, f, n)
	}
}

func TestRenderLifetimeLine(t *testing.T) {
	var b strings.Builder
	renderLifetimeLine(&b, AgentLifetime{Hauls: 281, Jumps: 1405, AvgPerJump: 2391})
	out := b.String()
	for _, want := range []string{"281 hauls", "1,405 jumps", "— losses", "avg 2,391 cr/jump", `colspan="6"`} {
		if !strings.Contains(out, want) {
			t.Errorf("line missing %q; got %s", want, out)
		}
	}
}

func TestRenderEffPanel(t *testing.T) {
	var b strings.Builder
	renderEffPanel(&b, &EffPanel{
		WindowLabel: "48h", Hauls: 1204, GrossPerJump: 2391, FuelPerJump: 45, NetPerJump: 2346,
		Agents: []PanelAgent{{AgentID: "salvager-10", Hauls: 178, NetPerJump: 4050}},
	})
	out := b.String()
	for _, want := range []string{"Haul fleet efficiency", "48h", "NET 2,346 cr/jump", "1,204 hauls", "salvager-10 178h 4,050"} {
		if !strings.Contains(out, want) {
			t.Errorf("panel missing %q; got %s", want, out)
		}
	}
}

func TestRenderEffPanelEmpty(t *testing.T) {
	var b strings.Builder
	renderEffPanel(&b, &EffPanel{WindowLabel: "48h", Hauls: 0})
	if !strings.Contains(b.String(), "No hauls in the last 48h") {
		t.Fatalf("empty panel should say no hauls; got %s", b.String())
	}
}

func TestRenderNilHaulStatsUnchanged(t *testing.T) {
	got := Render(nil, nil, 300, time.Now())
	if strings.Contains(got, "Haul fleet efficiency") {
		t.Fatal("nil HaulStats must not render the efficiency panel")
	}
	if strings.Contains(got, "effline") {
		t.Fatal("nil HaulStats must not render per-worker lines")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/ovstatus/ -run 'TestPerJumpMetrics|TestRenderLifetimeLine|TestRenderEffPanel|TestRenderNilHaulStatsUnchanged'`
Expected: FAIL — undefined `PerJumpMetrics`, `AgentLifetime`, `renderLifetimeLine`, `renderEffPanel`, `EffPanel`, `PanelAgent`, and `Render` arg-count mismatch (compile error).

- [ ] **Step 3: Write minimal implementation**

In `pkg/ovstatus/ovstatus.go`, add the types + helpers just after the `Source` type (around line 28):

```go
// HaulStats bundles the haul-fleet efficiency data rendered onto the page: an
// optional fleet panel and a per-agent lifetime line map. A nil *HaulStats (or
// nil fields) renders the page exactly as before.
type HaulStats struct {
	Panel    *EffPanel                // nil -> no panel
	Lifetime map[string]AgentLifetime // agent_id -> per-worker line; absent -> no line
}

// EffPanel is the fleet efficiency headline: windowed gross/fuel/net cr/jump and
// a per-agent net/jump ranking.
type EffPanel struct {
	WindowLabel  string
	Hauls        int
	GrossPerJump float64
	FuelPerJump  float64
	NetPerJump   float64
	Agents       []PanelAgent // ranked NetPerJump desc (caller sorts)
}

// PanelAgent is one agent's windowed net cr/jump for the panel ranking.
type PanelAgent struct {
	AgentID    string
	Hauls      int
	NetPerJump float64
}

// AgentLifetime is one worker's lifetime stats line (gross avg, no fuel term).
type AgentLifetime struct {
	Hauls      int
	Jumps      int64
	AvgPerJump float64
}

// PerJumpMetrics returns gross, fuel, and net credits-per-jump for a haul
// aggregate. fuelPerJump is fuel units burned per jump; fuelCrPerUnit is the
// credit price per fuel unit; their product is the (constant) fuel cr/jump.
// When sumJumps <= 0 all three are 0 (no hauls, no divide).
func PerJumpMetrics(sumProfit float64, sumJumps int64, fuelPerJump, fuelCrPerUnit float64) (gross, fuelCr, net float64) {
	if sumJumps <= 0 {
		return 0, 0, 0
	}
	gross = sumProfit / float64(sumJumps)
	fuelCr = fuelPerJump * fuelCrPerUnit
	net = gross - fuelCr
	return gross, fuelCr, net
}
```

Change `Render` (line 50) to thread `hs`:

```go
func Render(sources []Source, hs *HaulStats, refresh int, now time.Time) string {
```

and its final line (73) to:

```go
	return renderDoc(sections, hs, refresh, now)
```

Change `renderDoc` signature (line 76) to `func renderDoc(sections []section, hs *HaulStats, refresh int, now time.Time) string`, and after the TOC `</nav>` write (line 103) insert the panel + pass `hs` into `renderSection`:

```go
	b.WriteString("</nav>\n")

	if hs != nil && hs.Panel != nil {
		renderEffPanel(&b, hs.Panel)
	}

	for _, sec := range sections {
		renderSection(&b, sec, hs, now)
	}
```

Change `renderSection` signature (line 113) to `func renderSection(b *strings.Builder, sec section, hs *HaulStats, now time.Time)`, and its worker loop (line 147-149) to:

```go
	for _, w := range sec.Workers {
		renderRow(b, w, hs, now)
	}
```

Change `renderRow` signature (line 153) to `func renderRow(b *strings.Builder, w balances.LiveRecord, hs *HaulStats, now time.Time)`, and after its closing `b.WriteString("</tr>\n")` (line 166) add:

```go
	if hs != nil {
		if lt, ok := hs.Lifetime[w.AgentID]; ok {
			renderLifetimeLine(b, lt)
		}
	}
```

Add the two render helpers (place them just after `renderRow`):

```go
// renderEffPanel renders the fleet efficiency headline above the per-overmind
// sections.
func renderEffPanel(b *strings.Builder, p *EffPanel) {
	b.WriteString("<section class=\"effpanel\">\n")
	fmt.Fprintf(b, "<h2>Haul fleet efficiency <span class=\"subtle\">(%s)</span></h2>\n", html.EscapeString(p.WindowLabel))
	if p.Hauls == 0 {
		fmt.Fprintf(b, "<p class=\"subtle\">No hauls in the last %s.</p>\n</section>\n", html.EscapeString(p.WindowLabel))
		return
	}
	fmt.Fprintf(b, "<p class=\"effhead\">gross %s − fuel %s = <strong>NET %s cr/jump</strong> · %s hauls</p>\n",
		formatCredits(p.GrossPerJump), formatCredits(p.FuelPerJump), formatCredits(p.NetPerJump), formatCredits(float64(p.Hauls)))
	if len(p.Agents) > 0 {
		b.WriteString("<p class=\"subtle\">")
		for i, a := range p.Agents {
			if i > 0 {
				b.WriteString(" · ")
			}
			fmt.Fprintf(b, "%s %dh %s", html.EscapeString(a.AgentID), a.Hauls, formatCredits(a.NetPerJump))
		}
		b.WriteString("</p>\n")
	}
	b.WriteString("</section>\n")
}

// renderLifetimeLine emits a sub-row spanning all six columns with a worker's
// lifetime throughput. Ship losses show as an em dash until a death counter
// exists (SP-loss).
func renderLifetimeLine(b *strings.Builder, lt AgentLifetime) {
	fmt.Fprintf(b, "<tr class=\"effline\"><td colspan=\"6\" class=\"subtle\">%s hauls · %s jumps · — losses · avg %s cr/jump</td></tr>\n",
		formatCredits(float64(lt.Hauls)), formatCredits(float64(lt.Jumps)), formatCredits(lt.AvgPerJump))
}
```

Extend `styleBlock` — after the `.subtle { opacity: 0.65; ... }` line (around line 345) add:

```css
.effpanel { margin: 0.5rem 0 1rem; padding: 0.5rem 0.75rem; border: 1px solid rgba(127,127,127,0.3); border-radius: 6px; }
.effhead { font-size: 1rem; margin: 0.2rem 0; }
tr.effline td { padding-top: 0; border-top: 0; }
```

Update the caller in `cmd/tools/overmind-status/main.go:93` to pass `nil` for now:

```go
		if _, err := w.Write([]byte(ovstatus.Render(sources, nil, rf, time.Now()))); err != nil {
```

- [ ] **Step 4: Run tests + build to verify they pass**

Run: `go test ./pkg/ovstatus/` → PASS.
Run: `go build ./...` → succeeds (main.go caller updated).

- [ ] **Step 5: Lint + commit**

Run: `golangci-lint run ./pkg/ovstatus/ ./cmd/tools/overmind-status/` → `0 issues.`

```bash
git add pkg/ovstatus/ovstatus.go pkg/ovstatus/haul_stats_test.go cmd/tools/overmind-status/main.go
git commit -m "feat(ovstatus): efficiency panel + per-worker lifetime line rendering"
```

---

### Task 3: Wire real haul data into the overmind-status page

**Files:**
- Modify: `cmd/tools/overmind-status/main.go` (flags, open `market.db`, per-request aggregate, build `HaulStats`, pass to `Render`)
- Test: `cmd/tools/overmind-status/main_test.go`

**Interfaces:**
- Consumes: `market.HaulEfficiencySince` + `market.HaulEfficiencyRow` (Task 1); `ovstatus.HaulStats`, `EffPanel`, `PanelAgent`, `AgentLifetime`, `PerJumpMetrics`, and the new `Render(sources, hs, refresh, now)` signature (Task 2); `market.Open`/`market.Config` (`pkg/market/collector.go`).
- Produces: `func buildHaulStats(windowedAgents []market.HaulEfficiencyRow, windowedFleet market.HaulEfficiencyRow, lifetimeAgents []market.HaulEfficiencyRow, windowLabel string, fuelPerJump, fuelCrPerUnit float64) *ovstatus.HaulStats` and `func windowLabel(d time.Duration) string`.

- [ ] **Step 1: Write the failing test**

Create `cmd/tools/overmind-status/main_test.go`:

```go
package main

import (
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/market"
)

func TestBuildHaulStats(t *testing.T) {
	windowed := []market.HaulEfficiencyRow{
		{AgentID: "a", Hauls: 2, SumProfit: 1000, SumJumps: 10}, // net 100-45 = 55
		{AgentID: "b", Hauls: 1, SumProfit: 2000, SumJumps: 10}, // net 200-45 = 155
	}
	fleet := market.HaulEfficiencyRow{Hauls: 3, SumProfit: 3000, SumJumps: 20} // gross 150, fuel 45, net 105
	lifetime := []market.HaulEfficiencyRow{
		{AgentID: "a", Hauls: 9, SumProfit: 9000, SumJumps: 90}, // avg 100
	}
	hs := buildHaulStats(windowed, fleet, lifetime, "48h", 9, 5)

	if hs.Panel.GrossPerJump != 150 || hs.Panel.FuelPerJump != 45 || hs.Panel.NetPerJump != 105 || hs.Panel.Hauls != 3 {
		t.Fatalf("panel = %+v, want gross150 fuel45 net105 hauls3", hs.Panel)
	}
	if hs.Panel.WindowLabel != "48h" {
		t.Errorf("window label = %q, want 48h", hs.Panel.WindowLabel)
	}
	if len(hs.Panel.Agents) != 2 || hs.Panel.Agents[0].AgentID != "b" || hs.Panel.Agents[1].AgentID != "a" {
		t.Fatalf("ranking = %+v, want b(155) then a(55)", hs.Panel.Agents)
	}
	if lt := hs.Lifetime["a"]; lt.Hauls != 9 || lt.Jumps != 90 || lt.AvgPerJump != 100 {
		t.Fatalf("lifetime a = %+v, want 9/90/100", lt)
	}
}

func TestWindowLabel(t *testing.T) {
	if got := windowLabel(48 * time.Hour); got != "48h" {
		t.Errorf("48h -> %q", got)
	}
	if got := windowLabel(90 * time.Minute); got != "1h30m0s" {
		t.Errorf("90m -> %q (want the Duration.String fallback)", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/overmind-status/`
Expected: FAIL — `undefined: buildHaulStats` / `undefined: windowLabel`.

- [ ] **Step 3: Write minimal implementation**

In `cmd/tools/overmind-status/main.go`, add imports `"context"`, `"sort"`, and `"github.com/rsned/spacemolt/pkg/market"` to the import block. Add these two functions above `main`:

```go
// windowLabel renders a duration as a compact "48h" when it is a whole number
// of hours, else the standard Duration string.
func windowLabel(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return d.String()
}

// buildHaulStats maps windowed + lifetime haul aggregates into the ovstatus
// bundle: the windowed fleet + per-agent rows become the net-of-fuel panel
// (ranked by net cr/jump desc), and the lifetime rows become per-worker gross
// stat lines.
func buildHaulStats(windowedAgents []market.HaulEfficiencyRow, windowedFleet market.HaulEfficiencyRow,
	lifetimeAgents []market.HaulEfficiencyRow, label string, fuelPerJump, fuelCrPerUnit float64) *ovstatus.HaulStats {
	gross, fuelCr, net := ovstatus.PerJumpMetrics(windowedFleet.SumProfit, windowedFleet.SumJumps, fuelPerJump, fuelCrPerUnit)
	panel := &ovstatus.EffPanel{
		WindowLabel: label, Hauls: windowedFleet.Hauls,
		GrossPerJump: gross, FuelPerJump: fuelCr, NetPerJump: net,
	}
	for _, a := range windowedAgents {
		_, _, an := ovstatus.PerJumpMetrics(a.SumProfit, a.SumJumps, fuelPerJump, fuelCrPerUnit)
		panel.Agents = append(panel.Agents, ovstatus.PanelAgent{AgentID: a.AgentID, Hauls: a.Hauls, NetPerJump: an})
	}
	sort.Slice(panel.Agents, func(i, j int) bool { return panel.Agents[i].NetPerJump > panel.Agents[j].NetPerJump })

	lifetime := make(map[string]ovstatus.AgentLifetime, len(lifetimeAgents))
	for _, a := range lifetimeAgents {
		avg := 0.0
		if a.SumJumps > 0 {
			avg = a.SumProfit / float64(a.SumJumps)
		}
		lifetime[a.AgentID] = ovstatus.AgentLifetime{Hauls: a.Hauls, Jumps: a.SumJumps, AvgPerJump: avg}
	}
	return &ovstatus.HaulStats{Panel: panel, Lifetime: lifetime}
}
```

Add `"fmt"` to imports if not present (it is needed by `windowLabel`).

Add the flags in `main` (after the `refresh` flag, line 68):

```go
	marketDBPath := flag.String("market-db-path", "data/market.db", "Path to market.db for the haul-efficiency panel ('' disables it)")
	effWindow := flag.Duration("eff-window", 48*time.Hour, "Efficiency panel window")
	fuelPerJump := flag.Float64("fuel-per-jump", 9, "Estimated fuel units burned per jump (panel fuel term)")
	fuelCrPerUnit := flag.Float64("fuel-cr-per-unit", 5, "Estimated credits per fuel unit (panel fuel term)")
```

After `logger := ...` (line 76), open the DB once (nil-safe):

```go
	var col *market.Collector
	if *marketDBPath != "" {
		c, err := market.Open(market.Config{DBPath: *marketDBPath, WAL: true})
		if err != nil {
			logger.Printf("efficiency panel disabled: open %s: %v", *marketDBPath, err)
		} else {
			col = c
			defer col.Close() //nolint:errcheck
		}
	}
```

Replace the handler body's `Render` call (line 93) so it computes `hs` when `col` is available:

```go
		var hs *ovstatus.HaulStats
		if col != nil {
			now := time.Now()
			wAgents, wFleet, err1 := col.HaulEfficiencySince(r.Context(), now.Add(-*effWindow))
			lAgents, _, err2 := col.HaulEfficiencySince(r.Context(), time.Time{})
			if err1 != nil || err2 != nil {
				logger.Printf("efficiency query: %v / %v", err1, err2)
			} else {
				hs = buildHaulStats(wAgents, wFleet, lAgents, windowLabel(*effWindow), *fuelPerJump, *fuelCrPerUnit)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write([]byte(ovstatus.Render(sources, hs, rf, time.Now()))); err != nil {
			logger.Printf("write response: %v", err)
		}
```

(Remove the old `w.Header()...Render(sources, nil, ...)` lines this replaces.)

- [ ] **Step 4: Run tests + build to verify they pass**

Run: `go test ./cmd/tools/overmind-status/` → PASS.
Run: `go build ./...` → succeeds.

- [ ] **Step 5: Manual smoke (real DB)**

Run: `go run ./cmd/tools/overmind-status --addr :8099 --refresh 0` then `curl -s localhost:8099/ | grep -E 'Haul fleet efficiency|effline' | head`
Expected: the panel header line and at least one `effline` per-worker row appear (the live `data/market.db` has 6,490 hauls). Stop the process afterward.

- [ ] **Step 6: Lint + commit**

Run: `golangci-lint run ./cmd/tools/overmind-status/` → `0 issues.`

```bash
git add cmd/tools/overmind-status/main.go cmd/tools/overmind-status/main_test.go
git commit -m "feat(overmind-status): net-of-fuel efficiency panel + per-worker stats line"
```

---

## Self-Review

**1. Spec coverage:**
- Fleet net-of-fuel panel → Task 2 (`renderEffPanel`) + Task 3 (`buildHaulStats` fuel math). ✓
- Per-worker lifetime gross stats line → Task 2 (`renderLifetimeLine`) + Task 3 (lifetime map). ✓
- One aggregator over `haul_results`, windowed + all-time, `jumps_traveled=0` excluded → Task 1. ✓
- Flat fuel constants via flags (`--fuel-per-jump 9`, `--fuel-cr-per-unit 5`); `--market-db-path`, `--eff-window` → Task 3. ✓
- Graceful nil when DB absent / query error → Task 3 (`col == nil`, err branch) + Task 2 (`Render(nil)` unchanged, tested). ✓
- Ship losses render `—` → Task 2 `renderLifetimeLine`. ✓
- Losses/task-line/shuttles/sweetspot are non-goals → not implemented. ✓
- No schema/worker change, read-only → confirmed (only new file is a `SELECT`). ✓

**2. Placeholder scan:** No TBD/TODO/"handle errors"/"similar to" — every code step is complete. ✓

**3. Type consistency:** `HaulEfficiencyRow{AgentID,Hauls,SumProfit,SumJumps}` used identically in Tasks 1 & 3. `PerJumpMetrics(sumProfit float64, sumJumps int64, fuelPerJump, fuelCrPerUnit float64)` defined in Task 2, called with matching arg order/types in Task 3. `HaulStats/EffPanel/PanelAgent/AgentLifetime` field names match across Tasks 2 & 3. New `Render(sources, hs, refresh, now)` signature: caller updated to `nil` in Task 2, to real `hs` in Task 3 — build green at each. ✓
