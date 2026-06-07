# Ship Fitting Calculator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an offline ship-fitting calculator that answers "how many of module X fit on ship Y" (forward) and "which ships fit N of module X" (reverse), accounting for slots, CPU, power, capacity-adding modules, and the Engineering skill.

**Architecture:** A reusable `pkg/fitting` package holds all data structs and fit math, reading catalog data from the JSON snapshots under `data/game-api/latest`. A thin `cmd/tools/fit` CLI exposes two subcommands (`check`, `ships`) on top of the package. No game connection, no database.

**Tech Stack:** Go 1.25 (module `github.com/rsned/spacemolt`), stdlib only (`encoding/json`, `flag`, `math`). Tests use the standard `testing` package with table-driven tests and a small JSON fixture under `pkg/fitting/testdata`.

---

## Background for the engineer (zero-context primer)

- **Catalog data** lives in three JSON files in `data/game-api/latest/` (a symlink to a dated snapshot). Each file is a JSON object with a top-level `"items"` array; ignore the other keys (`message`, `page`, etc.).
  - `catalog_ships.json` — ship hulls. Relevant fields per entry: `id`, `name`, `cpu_capacity`, `power_capacity`, `weapon_slots`, `defense_slots`, `utility_slots`, `tier`, `default_modules` (array of strings).
  - `catalog_items.json` — mixed items. A **module** is any entry that has a `"slot"` field (values: `"weapon"`, `"defense"`, `"utility"`). Relevant fields: `id`, `name`, `type` (`"weapon"|"defense"|"mining"|"utility"`), `slot`, `cpu_usage`, `power_usage`, `cpu_bonus` (optional, adds CPU capacity), `power_bonus` (optional, adds power capacity), `required_skills` (optional `map[string]int`). Entries without `slot` (ore, components, ammo, etc.) are NOT modules and must be skipped.
  - `catalog_skills.json` — skills. We only need `engineering`, whose `bonus_per_level` map has `cpuEfficiency` and `powerEfficiency` (both currently `1`, meaning 1 percentage-point of usage reduction per level).
- **Fit rules (from the approved spec):**
  - A module occupies one slot of its `slot` type. A hull has `weapon_slots` / `defense_slots` / `utility_slots`. (Note `type:"mining"` modules have `slot:"utility"`.)
  - Effective usage with Engineering level L: `ceil(base_usage * (1 - 0.01 * L * effPerLevel))`, floored at 0. **Round UP** (this matches the server: base 25 reduced to 23.9 → 24).
  - Capacity-adding modules: each fitted module adds its `cpu_bonus` to CPU capacity and `power_bonus` to power capacity, at base value (NOT skill-reduced). A reactor both consumes usage and adds bonus.
  - **Bare hull**: ignore `default_modules` in the math — the player refits from scratch.
  - `required_skills` are surfaced as informational warnings only; they never block a fit.
  - No other per-ship or per-module caps exist.

## File Structure

- Create `pkg/fitting/types.go` — data structs (`Ship`, `Module`, `Engineering`, `ModuleCount`, `FitResult`) and small helpers (`slotCapacity`, `effUsage`, `DefaultEngineering`).
- Create `pkg/fitting/types_test.go` — tests for `effUsage` and `slotCapacity`.
- Create `pkg/fitting/fit.go` — `MaxFit`, `CheckFit`, `ShipsThatFit`.
- Create `pkg/fitting/fit_test.go` — table tests for the three fit functions using in-code structs.
- Create `pkg/fitting/catalog.go` — `Catalog` type, `LoadCatalog`, lookups, `Engineering(level)`.
- Create `pkg/fitting/catalog_test.go` — tests `LoadCatalog` against fixtures.
- Create `pkg/fitting/testdata/catalog_ships.json`, `catalog_items.json`, `catalog_skills.json` — minimal fixtures.
- Create `cmd/tools/fit/main.go` — CLI wiring (flag parsing, subcommand dispatch).
- Create `cmd/tools/fit/format.go` — pure output-formatting functions (testable).
- Create `cmd/tools/fit/format_test.go` — a test for the formatter.

---

## Task 1: Package types and core helpers

**Files:**
- Create: `pkg/fitting/types.go`
- Test: `pkg/fitting/types_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/fitting/types_test.go`:

```go
package fitting

import "testing"

func TestEffUsage(t *testing.T) {
	tests := []struct {
		name         string
		base         int
		level        int
		effPerLevel  float64
		want         int
	}{
		{"level zero no reduction", 25, 0, 1, 25},
		{"ceil rounds up", 25, 4, 1, 24},   // 25*0.96 = 24.0 -> 24
		{"ceil rounds fractional up", 13, 4, 1, 13}, // 13*0.96 = 12.48 -> 13
		{"high level large reduction", 100, 50, 1, 50}, // 100*0.5 = 50 -> 50
		{"never below zero", 10, 100, 1, 0}, // 10*0 = 0
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effUsage(tt.base, tt.level, tt.effPerLevel); got != tt.want {
				t.Errorf("effUsage(%d,%d,%g) = %d, want %d", tt.base, tt.level, tt.effPerLevel, got, tt.want)
			}
		})
	}
}

func TestSlotCapacity(t *testing.T) {
	s := Ship{WeaponSlots: 1, DefenseSlots: 2, UtilitySlots: 3}
	cases := map[string]int{"weapon": 1, "defense": 2, "utility": 3, "bogus": 0}
	for slot, want := range cases {
		if got := slotCapacity(s, slot); got != want {
			t.Errorf("slotCapacity(%q) = %d, want %d", slot, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/fitting/...`
Expected: FAIL — `undefined: effUsage`, `undefined: Ship`, `undefined: slotCapacity`.

- [ ] **Step 3: Write minimal implementation**

Create `pkg/fitting/types.go`:

```go
// Package fitting computes ship-module fitting against the game's catalog data.
// It answers two questions offline: how many of a module fit on a hull, and
// which hulls can fit a given count of a module. Slots, CPU, power, capacity-
// adding modules, and the Engineering skill are all accounted for.
package fitting

import "math"

// Ship is the fitting-relevant subset of a catalog hull.
type Ship struct {
	ID             string
	Name           string
	CPUCapacity    int
	PowerCapacity  int
	WeaponSlots    int
	DefenseSlots   int
	UtilitySlots   int
	Tier           int
	DefaultModules []string // parsed but ignored by the fit math (bare-hull assumption)
}

// Module is the fitting-relevant subset of a catalog module.
type Module struct {
	ID             string
	Name           string
	Type           string         // "weapon" | "defense" | "mining" | "utility"
	Slot           string         // "weapon" | "defense" | "utility"
	CPUUsage       int
	PowerUsage     int
	CPUBonus       int            // adds to CPU capacity (e.g. tow rig)
	PowerBonus     int            // adds to power capacity (e.g. reactor)
	RequiredSkills map[string]int // informational only
}

// Engineering carries the assumed Engineering skill level and the data-driven
// per-level efficiency values read from the skills catalog (both 1 by default).
type Engineering struct {
	Level            int
	CPUEffPerLevel   float64
	PowerEffPerLevel float64
}

// DefaultEngineering returns an Engineering at the given level using the default
// 1%-per-level efficiency for both CPU and power. Used when no catalog skill
// data overrides it.
func DefaultEngineering(level int) Engineering {
	return Engineering{Level: level, CPUEffPerLevel: 1, PowerEffPerLevel: 1}
}

// ModuleCount pairs a module with how many copies are being fitted.
type ModuleCount struct {
	Module Module
	Count  int
}

// FitResult reports the outcome of a fit calculation.
type FitResult struct {
	Fits              bool
	MaxCount          int    // populated by MaxFit: how many copies fit
	SlotType          string // module slot type ("" or "mixed" for multi-slot loadouts)
	SlotsUsed         int
	SlotsAvail        int
	CPUUsed           int
	CPUAvail          int
	PowerUsed         int
	PowerAvail        int
	BindingConstraint string // "weapon slots" | "defense slots" | "utility slots" | "CPU" | "power" | ""
	SkillWarnings     []string
}

// effUsage applies the Engineering reduction to a base usage value and rounds up.
// effPerLevel is the per-level efficiency (percentage points) from the skill data.
func effUsage(base, level int, effPerLevel float64) int {
	reduced := float64(base) * (1 - 0.01*float64(level)*effPerLevel)
	if reduced < 0 {
		reduced = 0
	}
	return int(math.Ceil(reduced))
}

// slotCapacity returns the number of slots of the given type on the ship.
func slotCapacity(s Ship, slot string) int {
	switch slot {
	case "weapon":
		return s.WeaponSlots
	case "defense":
		return s.DefenseSlots
	case "utility":
		return s.UtilitySlots
	default:
		return 0
	}
}

// slotLabel returns the human label for a slot type used in BindingConstraint.
func slotLabel(slot string) string {
	switch slot {
	case "weapon":
		return "weapon slots"
	case "defense":
		return "defense slots"
	case "utility":
		return "utility slots"
	default:
		return slot + " slots"
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/fitting/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/fitting/types.go pkg/fitting/types_test.go
git commit -m "feat(fitting): add core types and usage/slot helpers"
```

---

## Task 2: MaxFit (forward query)

**Files:**
- Create: `pkg/fitting/fit.go`
- Test: `pkg/fitting/fit_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/fitting/fit_test.go`:

```go
package fitting

import "testing"

// cobble is a small hull: 12 CPU, 24 power, 1 weapon / 1 defense / 2 utility slots.
func cobble() Ship {
	return Ship{ID: "cobble", Name: "Cobble", CPUCapacity: 12, PowerCapacity: 24,
		WeaponSlots: 1, DefenseSlots: 1, UtilitySlots: 2, Tier: 0,
		DefaultModules: []string{"mining_laser_i"}}
}

// droneBay: utility module, 12 CPU / 15 power, no bonuses.
func droneBay() Module {
	return Module{ID: "advanced_drone_bay", Name: "Advanced Drone Bay",
		Type: "utility", Slot: "utility", CPUUsage: 12, PowerUsage: 15}
}

// reactor: utility module, consumes 14 CPU / 8 power, adds 30 power capacity.
func reactor() Module {
	return Module{ID: "nuclear_reactor_ii", Name: "Nuclear Reactor II",
		Type: "utility", Slot: "utility", CPUUsage: 14, PowerUsage: 8, PowerBonus: 30,
		RequiredSkills: map[string]int{"mining": 8}}
}

func TestMaxFit_CPULimited(t *testing.T) {
	// Real cobble (CPU 12) + drone bay (CPU 12): 1 fits; the 2nd needs CPU 24 > 12.
	// CPU is checked before power, so CPU is the binding constraint.
	got := MaxFit(cobble(), droneBay(), DefaultEngineering(0))
	if got.MaxCount != 1 {
		t.Fatalf("MaxCount = %d, want 1", got.MaxCount)
	}
	if got.BindingConstraint != "CPU" {
		t.Errorf("BindingConstraint = %q, want %q", got.BindingConstraint, "CPU")
	}
	if !got.Fits {
		t.Errorf("Fits = false, want true")
	}
}

func TestMaxFit_PowerLimited(t *testing.T) {
	// Hull with ample CPU but tight power, so power is the clean binding limit.
	hull := Ship{ID: "ph", Name: "PowerHull", CPUCapacity: 100, PowerCapacity: 24, UtilitySlots: 2}
	mod := Module{ID: "ph_mod", Name: "Mod", Type: "utility", Slot: "utility", CPUUsage: 1, PowerUsage: 15}
	got := MaxFit(hull, mod, DefaultEngineering(0))
	if got.MaxCount != 1 {
		t.Fatalf("MaxCount = %d, want 1", got.MaxCount)
	}
	if got.BindingConstraint != "power" {
		t.Errorf("BindingConstraint = %q, want %q", got.BindingConstraint, "power")
	}
}

func TestMaxFit_SlotLimited(t *testing.T) {
	// A tiny no-cost module limited purely by the 2 utility slots.
	cheap := Module{ID: "x", Name: "X", Type: "utility", Slot: "utility", CPUUsage: 0, PowerUsage: 0}
	got := MaxFit(cobble(), cheap, DefaultEngineering(0))
	if got.MaxCount != 2 {
		t.Fatalf("MaxCount = %d, want 2", got.MaxCount)
	}
	if got.BindingConstraint != "utility slots" {
		t.Errorf("BindingConstraint = %q, want %q", got.BindingConstraint, "utility slots")
	}
}

func TestMaxFit_EngineeringRaisesCount(t *testing.T) {
	// Engineering 20 -> drone bay power 15*0.8=12, cpu 12*0.8=9.6->10.
	// 2 bays: power 24 (==24 ok), cpu 20 > 12 -> CPU blocks 2nd. Still 1.
	// Use a power-only-limited module to show eng raising the count cleanly:
	powerHog := Module{ID: "p", Name: "P", Type: "utility", Slot: "utility", PowerUsage: 15}
	// At eng 0: 24/15 -> 1 fits. At eng 20: 15*0.8=12 -> 24/12 = 2 fit (2 slots).
	if got := MaxFit(cobble(), powerHog, DefaultEngineering(0)); got.MaxCount != 1 {
		t.Fatalf("eng0 MaxCount = %d, want 1", got.MaxCount)
	}
	if got := MaxFit(cobble(), powerHog, DefaultEngineering(20)); got.MaxCount != 2 {
		t.Fatalf("eng20 MaxCount = %d, want 2", got.MaxCount)
	}
}

func TestMaxFit_ReactorAddsBudget(t *testing.T) {
	// Reactor: per copy +30 power capacity, costs 8 power / 14 CPU.
	// CPU is the limiter: cap 12, each costs 14 -> 0 fit. Verify CPU binding.
	got := MaxFit(cobble(), reactor(), DefaultEngineering(0))
	if got.MaxCount != 0 {
		t.Fatalf("MaxCount = %d, want 0 (CPU-limited)", got.MaxCount)
	}
	if got.BindingConstraint != "CPU" {
		t.Errorf("BindingConstraint = %q, want CPU", got.BindingConstraint)
	}
	if got.Fits {
		t.Errorf("Fits = true, want false")
	}
}

func TestMaxFit_ReactorBudgetIterative(t *testing.T) {
	// A high-power hull where a self-powering reactor fits multiple times and
	// its power_bonus matters. Hull: 100 CPU, 10 power, 4 utility slots.
	hull := Ship{ID: "h", CPUCapacity: 100, PowerCapacity: 10, UtilitySlots: 4}
	// Each reactor: cpu 10, power 5, power_bonus +20. Net power per copy: +15.
	r := Module{ID: "r", Type: "utility", Slot: "utility", CPUUsage: 10, PowerUsage: 5, PowerBonus: 20}
	// copy1: power used 5 <= 10+20=30 ok, cpu 10<=100. copy2: power 10 <= 50 ok, cpu 20.
	// copy3: power 15 <= 70 ok. copy4: power 20 <= 90 ok, cpu 40. -> 4 fit (slot-limited).
	got := MaxFit(hull, r, DefaultEngineering(0))
	if got.MaxCount != 4 {
		t.Fatalf("MaxCount = %d, want 4", got.MaxCount)
	}
	if got.BindingConstraint != "utility slots" {
		t.Errorf("BindingConstraint = %q, want utility slots", got.BindingConstraint)
	}
}

func TestMaxFit_SkillWarnings(t *testing.T) {
	got := MaxFit(cobble(), reactor(), DefaultEngineering(0))
	if len(got.SkillWarnings) != 1 || got.SkillWarnings[0] != "requires mining 8" {
		t.Errorf("SkillWarnings = %v, want [requires mining 8]", got.SkillWarnings)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/fitting/...`
Expected: FAIL — `undefined: MaxFit`.

- [ ] **Step 3: Write minimal implementation**

Create `pkg/fitting/fit.go`:

```go
package fitting

import (
	"fmt"
	"sort"
)

// skillWarnings renders a module's required_skills as informational strings,
// sorted by skill name for deterministic output.
func skillWarnings(m Module) []string {
	if len(m.RequiredSkills) == 0 {
		return nil
	}
	names := make([]string, 0, len(m.RequiredSkills))
	for k := range m.RequiredSkills {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, fmt.Sprintf("requires %s %d", n, m.RequiredSkills[n]))
	}
	return out
}

// MaxFit reports how many copies of module m fit on ship s at the given
// Engineering level. It adds copies one at a time so that capacity-adding
// modules (cpu_bonus / power_bonus) correctly raise the budget as they are
// fitted. Iteration is bounded by the relevant slot count.
func MaxFit(s Ship, m Module, eng Engineering) FitResult {
	slotCap := slotCapacity(s, m.Slot)
	effCPU := effUsage(m.CPUUsage, eng.Level, eng.CPUEffPerLevel)
	effPower := effUsage(m.PowerUsage, eng.Level, eng.PowerEffPerLevel)

	count := 0
	binding := ""
	for {
		next := count + 1
		if next > slotCap {
			binding = slotLabel(m.Slot)
			break
		}
		cpuUsed := next * effCPU
		powerUsed := next * effPower
		cpuCap := s.CPUCapacity + next*m.CPUBonus
		powerCap := s.PowerCapacity + next*m.PowerBonus
		if cpuUsed > cpuCap {
			binding = "CPU"
			break
		}
		if powerUsed > powerCap {
			binding = "power"
			break
		}
		count = next
	}

	return FitResult{
		Fits:              count >= 1,
		MaxCount:          count,
		SlotType:          m.Slot,
		SlotsUsed:         count,
		SlotsAvail:        slotCap,
		CPUUsed:           count * effCPU,
		CPUAvail:          s.CPUCapacity + count*m.CPUBonus,
		PowerUsed:         count * effPower,
		PowerAvail:        s.PowerCapacity + count*m.PowerBonus,
		BindingConstraint: binding,
		SkillWarnings:     skillWarnings(m),
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/fitting/...`
Expected: PASS (all `TestMaxFit_*`).

- [ ] **Step 5: Commit**

```bash
git add pkg/fitting/fit.go pkg/fitting/fit_test.go
git commit -m "feat(fitting): add MaxFit forward query"
```

---

## Task 3: CheckFit (arbitrary loadout)

**Files:**
- Modify: `pkg/fitting/fit.go`
- Modify: `pkg/fitting/fit_test.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/fitting/fit_test.go`:

```go
func TestCheckFit_Passes(t *testing.T) {
	// 1 drone bay on the cobble: utility 1/2, cpu 12/12, power 15/24 -> fits.
	got := CheckFit(cobble(), []ModuleCount{{Module: droneBay(), Count: 1}}, DefaultEngineering(0))
	if !got.Fits {
		t.Fatalf("Fits = false, want true (binding=%q)", got.BindingConstraint)
	}
	if got.CPUUsed != 12 || got.PowerUsed != 15 {
		t.Errorf("CPUUsed/PowerUsed = %d/%d, want 12/15", got.CPUUsed, got.PowerUsed)
	}
}

func TestCheckFit_FailsOnPower(t *testing.T) {
	// 2 drone bays: utility 2/2 ok, power 30 > 24 -> fails on power.
	got := CheckFit(cobble(), []ModuleCount{{Module: droneBay(), Count: 2}}, DefaultEngineering(0))
	if got.Fits {
		t.Fatalf("Fits = true, want false")
	}
	if got.BindingConstraint != "power" && got.BindingConstraint != "CPU" {
		t.Errorf("BindingConstraint = %q, want power or CPU", got.BindingConstraint)
	}
}

func TestCheckFit_FailsOnSlots(t *testing.T) {
	cheap := Module{ID: "x", Type: "utility", Slot: "utility"}
	got := CheckFit(cobble(), []ModuleCount{{Module: cheap, Count: 3}}, DefaultEngineering(0))
	if got.Fits {
		t.Fatalf("Fits = true, want false")
	}
	if got.BindingConstraint != "utility slots" {
		t.Errorf("BindingConstraint = %q, want utility slots", got.BindingConstraint)
	}
}

func TestCheckFit_MixedSlots(t *testing.T) {
	// Zero-cost weapon so the test exercises the mixed-slot path, not CPU/power.
	weapon := Module{ID: "w", Type: "weapon", Slot: "weapon", CPUUsage: 0, PowerUsage: 0}
	got := CheckFit(cobble(), []ModuleCount{
		{Module: droneBay(), Count: 1},
		{Module: weapon, Count: 1},
	}, DefaultEngineering(0))
	if !got.Fits {
		t.Fatalf("Fits = false, want true (binding=%q)", got.BindingConstraint)
	}
	if got.SlotType != "mixed" {
		t.Errorf("SlotType = %q, want mixed", got.SlotType)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/fitting/...`
Expected: FAIL — `undefined: CheckFit`.

- [ ] **Step 3: Write minimal implementation**

Append to `pkg/fitting/fit.go`:

```go
// CheckFit reports whether an arbitrary loadout fits on ship s at the given
// Engineering level. It checks slot counts per type plus aggregate CPU and
// power, accounting for capacity-adding modules. The first violated constraint
// is reported in BindingConstraint.
func CheckFit(s Ship, loadout []ModuleCount, eng Engineering) FitResult {
	slotsUsed := map[string]int{}
	cpuUsed, powerUsed := 0, 0
	cpuCap, powerCap := s.CPUCapacity, s.PowerCapacity
	var warnings []string
	seenWarning := map[string]bool{}

	for _, mc := range loadout {
		slotsUsed[mc.Module.Slot] += mc.Count
		cpuUsed += mc.Count * effUsage(mc.Module.CPUUsage, eng.Level, eng.CPUEffPerLevel)
		powerUsed += mc.Count * effUsage(mc.Module.PowerUsage, eng.Level, eng.PowerEffPerLevel)
		cpuCap += mc.Count * mc.Module.CPUBonus
		powerCap += mc.Count * mc.Module.PowerBonus
		for _, w := range skillWarnings(mc.Module) {
			if !seenWarning[w] {
				seenWarning[w] = true
				warnings = append(warnings, w)
			}
		}
	}

	binding := ""
	fits := true
	// Slots first (deterministic order: weapon, defense, utility).
	for _, slot := range []string{"weapon", "defense", "utility"} {
		if slotsUsed[slot] > slotCapacity(s, slot) {
			binding = slotLabel(slot)
			fits = false
			break
		}
	}
	if fits && cpuUsed > cpuCap {
		binding = "CPU"
		fits = false
	}
	if fits && powerUsed > powerCap {
		binding = "power"
		fits = false
	}

	slotType := "mixed"
	totalSlots := 0
	if len(slotsUsed) == 1 {
		for k := range slotsUsed {
			slotType = k
			totalSlots = slotsUsed[k]
		}
	}

	res := FitResult{
		Fits:              fits,
		SlotType:          slotType,
		CPUUsed:           cpuUsed,
		CPUAvail:          cpuCap,
		PowerUsed:         powerUsed,
		PowerAvail:        powerCap,
		BindingConstraint: binding,
		SkillWarnings:     warnings,
	}
	if slotType != "mixed" {
		res.SlotsUsed = totalSlots
		res.SlotsAvail = slotCapacity(s, slotType)
	}
	return res
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/fitting/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/fitting/fit.go pkg/fitting/fit_test.go
git commit -m "feat(fitting): add CheckFit for arbitrary loadouts"
```

---

## Task 4: ShipsThatFit (reverse query)

**Files:**
- Modify: `pkg/fitting/fit.go`
- Modify: `pkg/fitting/fit_test.go`

- [ ] **Step 1: Write the failing test**

Append to `pkg/fitting/fit_test.go`:

```go
func TestShipsThatFit(t *testing.T) {
	small := cobble() // 2 utility slots, power 24
	big := Ship{ID: "big", Name: "Big", CPUCapacity: 200, PowerCapacity: 200,
		UtilitySlots: 6, Tier: 2}
	tiny := Ship{ID: "tiny", Name: "Tiny", CPUCapacity: 5, PowerCapacity: 5,
		UtilitySlots: 1, Tier: 1}
	ships := []Ship{big, small, tiny}

	// Want >= 2 drone bays. small: power 24 -> only 1 fits (fails). big: 6 slots,
	// power 200/15 -> 6 fits (passes). tiny: 1 slot -> 1 (fails).
	got := ShipsThatFit(ships, droneBay(), 2, DefaultEngineering(0))
	if len(got) != 1 {
		t.Fatalf("got %d ships, want 1: %+v", len(got), got)
	}
	if got[0].Ship.ID != "big" {
		t.Errorf("ship = %q, want big", got[0].Ship.ID)
	}
	if got[0].Result.MaxCount != 6 {
		t.Errorf("MaxCount = %d, want 6", got[0].Result.MaxCount)
	}
}

func TestShipsThatFit_SortedByTierThenName(t *testing.T) {
	cheap := Module{ID: "x", Type: "utility", Slot: "utility"} // no cost, slot-limited
	a := Ship{ID: "a", Name: "Apex", UtilitySlots: 5, Tier: 3}
	b := Ship{ID: "b", Name: "Borg", UtilitySlots: 5, Tier: 1}
	c := Ship{ID: "c", Name: "Cstar", UtilitySlots: 5, Tier: 1}
	got := ShipsThatFit([]Ship{a, b, c}, cheap, 1, DefaultEngineering(0))
	order := []string{got[0].Ship.ID, got[1].Ship.ID, got[2].Ship.ID}
	want := []string{"b", "c", "a"} // tier 1 (Borg, Cstar) before tier 3 (Apex)
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/fitting/...`
Expected: FAIL — `undefined: ShipsThatFit` and `ShipFit` (the result struct used as `got[0].Ship` / `got[0].Result`).

- [ ] **Step 3: Write minimal implementation**

Append to `pkg/fitting/fit.go`:

```go
// ShipFit pairs a ship with its MaxFit result for the queried module.
type ShipFit struct {
	Ship   Ship
	Result FitResult
}

// ShipsThatFit returns every ship that can fit at least `count` copies of module
// m, sorted by tier ascending then name. Each entry carries the per-ship MaxFit
// result.
func ShipsThatFit(ships []Ship, m Module, count int, eng Engineering) []ShipFit {
	var out []ShipFit
	for _, s := range ships {
		r := MaxFit(s, m, eng)
		if r.MaxCount >= count {
			out = append(out, ShipFit{Ship: s, Result: r})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ship.Tier != out[j].Ship.Tier {
			return out[i].Ship.Tier < out[j].Ship.Tier
		}
		return out[i].Ship.Name < out[j].Ship.Name
	})
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/fitting/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/fitting/fit.go pkg/fitting/fit_test.go
git commit -m "feat(fitting): add ShipsThatFit reverse query"
```

---

## Task 5: Catalog loader

**Files:**
- Create: `pkg/fitting/catalog.go`
- Create: `pkg/fitting/catalog_test.go`
- Create: `pkg/fitting/testdata/catalog_ships.json`
- Create: `pkg/fitting/testdata/catalog_items.json`
- Create: `pkg/fitting/testdata/catalog_skills.json`

- [ ] **Step 1: Create the fixture files**

Create `pkg/fitting/testdata/catalog_ships.json`:

```json
{
  "items": [
    {
      "id": "cobble", "name": "Cobble",
      "cpu_capacity": 12, "power_capacity": 24,
      "weapon_slots": 1, "defense_slots": 1, "utility_slots": 2,
      "tier": 0, "default_modules": ["mining_laser_i", "autocannon_i"]
    },
    {
      "id": "hauler", "name": "Hauler",
      "cpu_capacity": 200, "power_capacity": 200,
      "weapon_slots": 0, "defense_slots": 2, "utility_slots": 6,
      "tier": 2, "default_modules": []
    }
  ]
}
```

Create `pkg/fitting/testdata/catalog_items.json`:

```json
{
  "items": [
    {
      "id": "advanced_drone_bay", "name": "Advanced Drone Bay",
      "type": "utility", "slot": "utility",
      "cpu_usage": 12, "power_usage": 15
    },
    {
      "id": "nuclear_reactor_ii", "name": "Nuclear Reactor II",
      "type": "utility", "slot": "utility",
      "cpu_usage": 14, "power_usage": 8, "power_bonus": 30,
      "required_skills": {"mining": 8}
    },
    {
      "id": "steel_plate", "name": "Steel Plate", "category": "component"
    }
  ]
}
```

Create `pkg/fitting/testdata/catalog_skills.json`:

```json
{
  "items": [
    {
      "id": "engineering", "name": "Engineering",
      "bonus_per_level": {"cpuEfficiency": 1, "powerEfficiency": 1},
      "max_level": 100
    }
  ]
}
```

- [ ] **Step 2: Write the failing test**

Create `pkg/fitting/catalog_test.go`:

```go
package fitting

import "testing"

func TestLoadCatalog(t *testing.T) {
	cat, err := LoadCatalog("testdata")
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	// Ships loaded.
	s, ok := cat.Ship("cobble")
	if !ok {
		t.Fatal("cobble ship not found")
	}
	if s.CPUCapacity != 12 || s.UtilitySlots != 2 || s.Tier != 0 {
		t.Errorf("cobble = %+v, unexpected fields", s)
	}
	if len(cat.Ships()) != 2 {
		t.Errorf("Ships() len = %d, want 2", len(cat.Ships()))
	}

	// Modules loaded; non-module (steel_plate, no slot) skipped.
	m, ok := cat.Module("advanced_drone_bay")
	if !ok {
		t.Fatal("advanced_drone_bay module not found")
	}
	if m.Slot != "utility" || m.CPUUsage != 12 || m.PowerUsage != 15 {
		t.Errorf("drone bay = %+v, unexpected fields", m)
	}
	if _, ok := cat.Module("steel_plate"); ok {
		t.Error("steel_plate should be skipped (no slot)")
	}
	r, _ := cat.Module("nuclear_reactor_ii")
	if r.PowerBonus != 30 || r.RequiredSkills["mining"] != 8 {
		t.Errorf("reactor = %+v, unexpected fields", r)
	}

	// Engineering efficiency read from skills.
	eng := cat.Engineering(10)
	if eng.Level != 10 || eng.CPUEffPerLevel != 1 || eng.PowerEffPerLevel != 1 {
		t.Errorf("Engineering(10) = %+v, want level 10 / eff 1 / 1", eng)
	}
}

func TestLoadCatalog_MissingDir(t *testing.T) {
	if _, err := LoadCatalog("testdata/does-not-exist"); err == nil {
		t.Error("expected error for missing dir, got nil")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/fitting/...`
Expected: FAIL — `undefined: LoadCatalog`.

- [ ] **Step 4: Write minimal implementation**

Create `pkg/fitting/catalog.go`:

```go
package fitting

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Catalog holds the loaded ships, modules, and Engineering efficiency values.
type Catalog struct {
	ships            map[string]Ship
	modules          map[string]Module
	cpuEffPerLevel   float64
	powerEffPerLevel float64
}

// rawListFile is the common shape of the catalog JSON files: a top-level
// "items" array (other keys are ignored).
type rawListFile struct {
	Items []json.RawMessage `json:"items"`
}

type rawShip struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	CPUCapacity    int      `json:"cpu_capacity"`
	PowerCapacity  int      `json:"power_capacity"`
	WeaponSlots    int      `json:"weapon_slots"`
	DefenseSlots   int      `json:"defense_slots"`
	UtilitySlots   int      `json:"utility_slots"`
	Tier           int      `json:"tier"`
	DefaultModules []string `json:"default_modules"`
}

type rawModule struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Type           string         `json:"type"`
	Slot           string         `json:"slot"`
	CPUUsage       int            `json:"cpu_usage"`
	PowerUsage     int            `json:"power_usage"`
	CPUBonus       int            `json:"cpu_bonus"`
	PowerBonus     int            `json:"power_bonus"`
	RequiredSkills map[string]int `json:"required_skills"`
}

type rawSkill struct {
	ID            string             `json:"id"`
	BonusPerLevel map[string]float64 `json:"bonus_per_level"`
}

// LoadCatalog reads catalog_ships.json, catalog_items.json, and
// catalog_skills.json from dir and returns a populated Catalog. Items without a
// "slot" field are not modules and are skipped. Engineering efficiency defaults
// to 1%/level if the skill or its bonus keys are absent.
func LoadCatalog(dir string) (*Catalog, error) {
	c := &Catalog{
		ships:            map[string]Ship{},
		modules:          map[string]Module{},
		cpuEffPerLevel:   1,
		powerEffPerLevel: 1,
	}

	if err := c.loadShips(filepath.Join(dir, "catalog_ships.json")); err != nil {
		return nil, err
	}
	if err := c.loadModules(filepath.Join(dir, "catalog_items.json")); err != nil {
		return nil, err
	}
	if err := c.loadSkills(filepath.Join(dir, "catalog_skills.json")); err != nil {
		return nil, err
	}
	return c, nil
}

func readItems(path string) ([]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f rawListFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return f.Items, nil
}

func (c *Catalog) loadShips(path string) error {
	items, err := readItems(path)
	if err != nil {
		return err
	}
	for _, raw := range items {
		var rs rawShip
		if err := json.Unmarshal(raw, &rs); err != nil {
			return fmt.Errorf("parse ship in %s: %w", path, err)
		}
		c.ships[rs.ID] = Ship{
			ID: rs.ID, Name: rs.Name,
			CPUCapacity: rs.CPUCapacity, PowerCapacity: rs.PowerCapacity,
			WeaponSlots: rs.WeaponSlots, DefenseSlots: rs.DefenseSlots,
			UtilitySlots: rs.UtilitySlots, Tier: rs.Tier,
			DefaultModules: rs.DefaultModules,
		}
	}
	return nil
}

func (c *Catalog) loadModules(path string) error {
	items, err := readItems(path)
	if err != nil {
		return err
	}
	for _, raw := range items {
		var rm rawModule
		if err := json.Unmarshal(raw, &rm); err != nil {
			return fmt.Errorf("parse item in %s: %w", path, err)
		}
		if rm.Slot == "" { // not a module
			continue
		}
		c.modules[rm.ID] = Module{
			ID: rm.ID, Name: rm.Name, Type: rm.Type, Slot: rm.Slot,
			CPUUsage: rm.CPUUsage, PowerUsage: rm.PowerUsage,
			CPUBonus: rm.CPUBonus, PowerBonus: rm.PowerBonus,
			RequiredSkills: rm.RequiredSkills,
		}
	}
	return nil
}

func (c *Catalog) loadSkills(path string) error {
	items, err := readItems(path)
	if err != nil {
		return err
	}
	for _, raw := range items {
		var rsk rawSkill
		if err := json.Unmarshal(raw, &rsk); err != nil {
			return fmt.Errorf("parse skill in %s: %w", path, err)
		}
		if rsk.ID != "engineering" {
			continue
		}
		if v, ok := rsk.BonusPerLevel["cpuEfficiency"]; ok {
			c.cpuEffPerLevel = v
		}
		if v, ok := rsk.BonusPerLevel["powerEfficiency"]; ok {
			c.powerEffPerLevel = v
		}
	}
	return nil
}

// Ship returns the ship with the given id.
func (c *Catalog) Ship(id string) (Ship, bool) {
	s, ok := c.ships[id]
	return s, ok
}

// Module returns the module with the given id.
func (c *Catalog) Module(id string) (Module, bool) {
	m, ok := c.modules[id]
	return m, ok
}

// Ships returns all loaded ships sorted by tier then name.
func (c *Catalog) Ships() []Ship {
	out := make([]Ship, 0, len(c.ships))
	for _, s := range c.ships {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tier != out[j].Tier {
			return out[i].Tier < out[j].Tier
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Engineering returns an Engineering at the given level using the catalog's
// efficiency-per-level values.
func (c *Catalog) Engineering(level int) Engineering {
	return Engineering{Level: level, CPUEffPerLevel: c.cpuEffPerLevel, PowerEffPerLevel: c.powerEffPerLevel}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/fitting/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/fitting/catalog.go pkg/fitting/catalog_test.go pkg/fitting/testdata/
git commit -m "feat(fitting): add catalog JSON loader"
```

---

## Task 6: CLI output formatter

**Files:**
- Create: `cmd/tools/fit/format.go`
- Create: `cmd/tools/fit/format_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/tools/fit/format_test.go`:

```go
package main

import (
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/fitting"
)

func TestFormatCheck(t *testing.T) {
	res := fitting.FitResult{
		Fits: true, MaxCount: 1, SlotType: "utility",
		SlotsUsed: 1, SlotsAvail: 2, CPUUsed: 12, CPUAvail: 12,
		PowerUsed: 15, PowerAvail: 24, BindingConstraint: "power",
		SkillWarnings: []string{"requires mining 8"},
	}
	out := formatCheck("Cobble", "Advanced Drone Bay", res)
	for _, want := range []string{"Cobble", "Advanced Drone Bay", "max 1", "utility 1/2", "CPU 12/12", "power 15/24", "requires mining 8"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

func TestFormatShips(t *testing.T) {
	fits := []fitting.ShipFit{
		{Ship: fitting.Ship{Name: "Hauler", Tier: 2}, Result: fitting.FitResult{MaxCount: 6}},
	}
	out := formatShips("Advanced Drone Bay", 5, fits)
	for _, want := range []string{"Advanced Drone Bay", "Hauler", "6"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	empty := formatShips("X", 5, nil)
	if !strings.Contains(empty, "No ships") {
		t.Errorf("empty output should say No ships: %q", empty)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/tools/fit/...`
Expected: FAIL — `undefined: formatCheck`, `undefined: formatShips`.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/tools/fit/format.go`:

```go
package main

import (
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/fitting"
)

// formatCheck renders a MaxFit result for one ship + module.
func formatCheck(shipName, moduleName string, r fitting.FitResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s on %s: max %d\n", moduleName, shipName, r.MaxCount)
	fmt.Fprintf(&b, "  %s %d/%d   CPU %d/%d   power %d/%d\n",
		r.SlotType, r.SlotsUsed, r.SlotsAvail, r.CPUUsed, r.CPUAvail, r.PowerUsed, r.PowerAvail)
	if r.BindingConstraint != "" {
		fmt.Fprintf(&b, "  limited by: %s\n", r.BindingConstraint)
	}
	for _, w := range r.SkillWarnings {
		fmt.Fprintf(&b, "  note: %s\n", w)
	}
	return b.String()
}

// formatShips renders the reverse-query result list.
func formatShips(moduleName string, count int, fits []fitting.ShipFit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Ships that fit >= %d x %s:\n", count, moduleName)
	if len(fits) == 0 {
		b.WriteString("  No ships qualify.\n")
		return b.String()
	}
	for _, sf := range fits {
		fmt.Fprintf(&b, "  [t%d] %-24s max %d\n", sf.Ship.Tier, sf.Ship.Name, sf.Result.MaxCount)
	}
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/tools/fit/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tools/fit/format.go cmd/tools/fit/format_test.go
git commit -m "feat(fit-cli): add output formatters"
```

---

## Task 7: CLI main (wiring + subcommands)

**Files:**
- Create: `cmd/tools/fit/main.go`

- [ ] **Step 1: Write the implementation**

Create `cmd/tools/fit/main.go`:

```go
// Command fit is an offline ship-fitting calculator. It reads the catalog JSON
// snapshots and answers two questions:
//
//	fit check --ship <id> --module <id> [--count N] [--eng_skill_level 0]
//	fit ships --module <id> --count N            [--eng_skill_level 0]
//
// Global flag --catalog-dir defaults to data/game-api/latest.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/rsned/spacemolt/pkg/fitting"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "check":
		runCheck(os.Args[2:])
	case "ships":
		runShips(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `fit - offline ship-fitting calculator

Usage:
  fit check --ship <id> --module <id> [--count N] [--eng_skill_level 0] [--catalog-dir DIR]
  fit ships --module <id> --count N            [--eng_skill_level 0] [--catalog-dir DIR]
`)
}

func runCheck(args []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	ship := fs.String("ship", "", "ship id (required)")
	module := fs.String("module", "", "module id (required)")
	count := fs.Int("count", 0, "optional target count to pass/fail against")
	eng := fs.Int("eng_skill_level", 0, "Engineering skill level")
	dir := fs.String("catalog-dir", "data/game-api/latest", "catalog directory")
	_ = fs.Parse(args)

	if *ship == "" || *module == "" {
		fmt.Fprintln(os.Stderr, "check: --ship and --module are required")
		os.Exit(2)
	}

	cat := mustLoad(*dir)
	s, ok := cat.Ship(*ship)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown ship %q\n", *ship)
		os.Exit(1)
	}
	m, ok := cat.Module(*module)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown module %q\n", *module)
		os.Exit(1)
	}

	res := fitting.MaxFit(s, m, cat.Engineering(*eng))
	fmt.Print(formatCheck(s.Name, m.Name, res))
	if *count > 0 {
		if res.MaxCount >= *count {
			fmt.Printf("=> YES, fits %d (max %d)\n", *count, res.MaxCount)
		} else {
			fmt.Printf("=> NO, only %d fit (wanted %d)\n", res.MaxCount, *count)
		}
	}
}

func runShips(args []string) {
	fs := flag.NewFlagSet("ships", flag.ExitOnError)
	module := fs.String("module", "", "module id (required)")
	count := fs.Int("count", 1, "minimum count required")
	eng := fs.Int("eng_skill_level", 0, "Engineering skill level")
	dir := fs.String("catalog-dir", "data/game-api/latest", "catalog directory")
	_ = fs.Parse(args)

	if *module == "" {
		fmt.Fprintln(os.Stderr, "ships: --module is required")
		os.Exit(2)
	}

	cat := mustLoad(*dir)
	m, ok := cat.Module(*module)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown module %q\n", *module)
		os.Exit(1)
	}

	fits := fitting.ShipsThatFit(cat.Ships(), m, *count, cat.Engineering(*eng))
	fmt.Print(formatShips(m.Name, *count, fits))
}

func mustLoad(dir string) *fitting.Catalog {
	cat, err := fitting.LoadCatalog(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load catalog: %v\n", err)
		os.Exit(1)
	}
	return cat
}
```

- [ ] **Step 2: Build the binary to bin/**

Run: `go build -o bin/fit ./cmd/tools/fit`
Expected: builds with no output.

- [ ] **Step 3: Smoke-test against real catalog data**

Run: `./bin/fit ships --module advanced_drone_bay --count 5`
Expected: prints a "Ships that fit >= 5 x Advanced Drone Bay:" header followed by a list of hulls (sorted by tier then name). Then run:

Run: `./bin/fit check --ship cobble --module advanced_drone_bay`
Expected: prints the Cobble max-fit line plus the resource breakdown and "limited by:" line.

- [ ] **Step 4: Commit**

```bash
git add cmd/tools/fit/main.go
git commit -m "feat(fit-cli): wire check and ships subcommands"
```

---

## Task 8: Full build, test, and lint

**Files:** none (verification only)

- [ ] **Step 1: Build everything**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 2: Run the full test suite**

Run: `go test ./pkg/fitting/... ./cmd/tools/fit/...`
Expected: all PASS.

- [ ] **Step 3: Lint**

Run: `golangci-lint run ./pkg/fitting/... ./cmd/tools/fit/...`
Expected: no new findings. Fix any reported issues (common: unchecked errors, `gofmt`), then re-run until clean.

- [ ] **Step 4: Final commit (if lint required changes)**

```bash
git add -A
git commit -m "chore(fitting): satisfy linter"
```

---

## Self-Review Notes

- **Spec coverage:** data source = JSON snapshots (Task 5); reusable pkg + thin CLI (Tasks 1-5 vs 6-7); `MaxFit` (Task 2), `CheckFit` (Task 3), `ShipsThatFit` (Task 4); CPU+power+slots+ceil rounding (Tasks 1-2); capacity-adding modules via iteration (Task 2, `TestMaxFit_ReactorBudgetIterative`); bare-hull (DefaultModules parsed but unused in math); `--eng_skill_level` default 0 (Task 7); required-skill warnings (Task 2/3); both CLI subcommands (Task 7); TDD fixtures (Task 5). All covered.
- **Type consistency:** `Engineering`, `Module`, `Ship`, `FitResult`, `ModuleCount`, `ShipFit`, `Catalog` used consistently across tasks; `Catalog.Engineering(level)` and `DefaultEngineering(level)` both produce `Engineering`; formatter consumes `fitting.FitResult` / `fitting.ShipFit` fields exactly as defined.
- **Deferred (per spec, out of scope):** live-character skills, non-Engineering skills, drone-bandwidth modelling, live-server rounding validation.
