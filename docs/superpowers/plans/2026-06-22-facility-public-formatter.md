# facility list public_facilities + production-row enrichments — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render the server's new `public_facilities` array in `facility list` output, and enrich the shared production table with `facility_id`, `rent_per_cycle` (where present), and an owning-faction column for public facilities.

**Architecture:** A small lightweight `FactionTag` KB lookup feeds a play_as owner resolver (tag → faction_id fallback, nil-KB safe). The shared `renderProductionFacilityTable` gains three *conditional* trailing columns (rendered only when ≥1 row populates them), fed by an extended `productionFacility` struct. A new default-on "Public Facilities" section decodes `public_facilities` (all production facilities) and renders through that same table.

**Tech Stack:** Go 1.24+, SQLite (`pkg/knowledge`), the play_as REPL formatter (`cmd/tools/play_as`).

**Spec:** `docs/superpowers/specs/2026-06-22-facility-public-formatter-design.md`

## Global Constraints

- `go build ./...` clean, `go test ./...` green, no new `golangci-lint` findings (use the `golangci-lint` tool).
- `ticks_per_run` stays `float64` (live server sends fractional values) — do not change `facilityProduction.TicksPerRun`.
- Graceful degradation: with no `--db-path` / nil `globalKB`, owner resolution must fall back to the raw `faction_id` and never panic.
- No `Base`-interface change; the tag lookup lives on `*knowledge.SQLiteKB`.
- Treat `public_facilities` entries as production facilities; entries lacking a `production` block are skipped from the table.
- Existing facility formatter tests assert via `strings.Contains` (substring), so additive columns must not remove any currently-asserted substring.

---

### Task 1: `FactionTag` lightweight KB lookup

**Files:**
- Modify: `pkg/knowledge/faction_store.go` (add method; add `errors` import)
- Test: `pkg/knowledge/faction_store_test.go` (add `TestFactionTag`)

**Interfaces:**
- Consumes: nothing.
- Produces: `func (kb *SQLiteKB) FactionTag(ctx context.Context, factionID string) (string, bool, error)` — returns the faction's tag and `ok=true`, or `("", false, nil)` when the faction is unknown or has no tag.

- [ ] **Step 1: Write the failing test**

Add to `pkg/knowledge/faction_store_test.go` (it already has `package knowledge`, imports `context`/`testing`/`time`, and a `newTestKB(t)` helper returning `*SQLiteKB`):

```go
func TestFactionTag(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	if err := kb.StoreFaction(ctx, FactionRecord{FactionID: "f1", Name: "Crafters Union", Tag: "CRFT", CapturedAt: time.Now()}); err != nil {
		t.Fatalf("StoreFaction: %v", err)
	}

	tag, ok, err := kb.FactionTag(ctx, "f1")
	if err != nil || !ok || tag != "CRFT" {
		t.Fatalf("FactionTag(f1) = %q, %v, %v; want \"CRFT\", true, nil", tag, ok, err)
	}

	tag, ok, err = kb.FactionTag(ctx, "unknown")
	if err != nil || ok || tag != "" {
		t.Fatalf("FactionTag(unknown) = %q, %v, %v; want \"\", false, nil", tag, ok, err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/knowledge/ -run TestFactionTag`
Expected: FAIL — compile error `kb.FactionTag undefined`.

- [ ] **Step 3: Add the method**

In `pkg/knowledge/faction_store.go`, add `"errors"` to the import block (so it reads `context`, `database/sql`, `errors`, `fmt`, `time`), then add:

```go
// FactionTag returns the tag recorded for a faction and ok=true, or
// ("", false, nil) when the faction is unknown or has no tag stored. Used for
// cheap id→tag display lookups without loading the full faction view.
func (kb *SQLiteKB) FactionTag(ctx context.Context, factionID string) (string, bool, error) {
	var tag sql.NullString
	err := kb.db.QueryRowContext(ctx, `SELECT tag FROM factions WHERE faction_id = ?`, factionID).Scan(&tag)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("faction tag: %w", err)
	}
	if !tag.Valid || tag.String == "" {
		return "", false, nil
	}
	return tag.String, true, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/knowledge/ -run TestFactionTag`
Expected: PASS.

- [ ] **Step 5: Lint and commit**

Run: `golangci-lint run ./pkg/knowledge/` → expect 0 issues.

```bash
git add pkg/knowledge/faction_store.go pkg/knowledge/faction_store_test.go
git commit -m "feat(knowledge): add FactionTag lightweight id->tag lookup"
```

---

### Task 2: Production table — conditional facility_id / rent_per_cycle / owner columns

**Files:**
- Modify: `cmd/tools/play_as/main.go` (`productionFacility` struct ~2081; `renderProductionFacilityTable` ~2090; faction wiring in `renderFactionFacilities` ~2210; station wiring ~2515)
- Test: `cmd/tools/play_as/facility_format_test.go` (add `TestFormatFacilityList_ProductionShowsIDAndRent`)

**Interfaces:**
- Consumes: existing `facilityProduction`, `factionFacilityRow` (has `FacilityID string`, `RentPerCycle int64`, `displayName()`), and the station facility decode struct (has `FacilityID string`, `Production *facilityProduction`, `Name string`).
- Produces: extended `productionFacility{ name string; prod *facilityProduction; facilityID string; rentPerCycle *int64; owner string }` and a `renderProductionFacilityTable` that appends Facility ID / Rent/cycle / Owner columns when any row populates them. Task 4 builds `productionFacility` values with all five fields.

- [ ] **Step 1: Write the failing test**

Add to `cmd/tools/play_as/facility_format_test.go`. This uses a `facility list` with a faction production facility carrying `facility_id` and `rent_per_cycle`, and asserts the new columns/values render:

```go
func TestFormatFacilityList_ProductionShowsIDAndRent(t *testing.T) {
	raw := []byte(`{"base_id":"grand_exchange_station","faction_facilities":[{"active":true,"facility_id":"fac-abc123","name":"Iron Refinery","faction_service":"refining","rent_per_cycle":210,"status":"online","type":"iron_refinery","production":{"backlog_ticks":0,"items_per_hour":12,"output_per_run":3,"public":false,"queued_items":0,"queued_runs":0,"recipe":"Refine Iron","rental_fee_per_run":40,"ticks_per_run":2.50}}]}`)
	out := formatFacilityList(raw)
	for _, want := range []string{"Faction Production", "Iron Refinery", "⚙ Refine Iron", "Facility ID", "fac-abc123", "Rent/cycle", "210"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatFacilityList output missing %q\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestFormatFacilityList_ProductionShowsIDAndRent`
Expected: FAIL — output is missing `Facility ID` / `fac-abc123` / `Rent/cycle` (the columns don't exist yet).

- [ ] **Step 3: Extend the `productionFacility` struct**

Replace the struct at `cmd/tools/play_as/main.go:2081`:

```go
// productionFacility is the shape renderProductionFacilityTable needs: a display
// name, the production block, and optional facility_id / per-cycle rent / owner
// that render as extra columns when present. Station, faction, and public
// production facilities all map onto it so they share one renderer.
type productionFacility struct {
	name         string
	prod         *facilityProduction
	facilityID   string // "" when unknown
	rentPerCycle *int64 // nil when the facility carries no per-cycle rent
	owner        string // "" except for public facilities
}
```

- [ ] **Step 4: Rewrite `renderProductionFacilityTable` with conditional trailing columns**

Replace the whole function body (`cmd/tools/play_as/main.go:2090`–end of function ~2161) with:

```go
func renderProductionFacilityTable(b *strings.Builder, facs []productionFacility, indent, heading string) {
	type prodRow struct {
		name, typ, feehr, outrun, cycle, runcost, queued, backlog, public string
		facID, rent, owner                                                 string
	}
	rows := make([]prodRow, 0, len(facs))
	showID, showRent, showOwner := false, false, false
	for _, f := range facs {
		p := f.prod
		public := "No"
		if p.Public {
			public = "Yes"
		}
		rent := ""
		if f.rentPerCycle != nil {
			rent = formatCredits(float64(*f.rentPerCycle))
			showRent = true
		}
		if f.facilityID != "" {
			showID = true
		}
		if f.owner != "" {
			showOwner = true
		}
		rows = append(rows, prodRow{
			name:   f.name,
			typ:    "⚙ " + p.Recipe,
			feehr:  strconv.Itoa(p.ItemsPerHour),
			outrun: strconv.Itoa(p.OutputPerRun),
			// Fractional ticks/run (e.g. 0.9259…) is rounded to 2 dp.
			cycle:   strconv.FormatFloat(p.TicksPerRun, 'f', 2, 64),
			runcost: strconv.Itoa(p.RentalFeePerRun),
			queued:  strconv.Itoa(p.QueuedRuns),
			backlog: strconv.Itoa(p.BacklogTicks),
			public:  public,
			facID:   f.facilityID,
			rent:    rent,
			owner:   f.owner,
		})
	}
	// Column widths span both header lines and the values. The Type cell carries
	// a multi-byte ⚙ glyph, so measure it by rune count (display width) and pad
	// it manually rather than via %-*s (which pads bytes).
	nameW := len("Name")
	typeW := len("Type")
	feeW := max(len("Fee"), len("/hr"))
	outW := max(len("Output"), len("/run"))
	cycW := max(len("Cycle"), len("tick/run"))
	costW := max(len("Run"), len("cost"))
	queueW := max(len("Queued"), len("runs"))
	backW := max(len("Backlog"), len("ticks"))
	pubW := len("Public")
	idW := len("Facility ID")
	rentW := len("Rent/cycle")
	ownerW := len("Owner")
	for _, r := range rows {
		nameW = max(nameW, len(r.name))
		typeW = max(typeW, len([]rune(r.typ)))
		feeW = max(feeW, len(r.feehr))
		outW = max(outW, len(r.outrun))
		cycW = max(cycW, len(r.cycle))
		costW = max(costW, len(r.runcost))
		queueW = max(queueW, len(r.queued))
		backW = max(backW, len(r.backlog))
		pubW = max(pubW, len(r.public))
		idW = max(idW, len(r.facID))
		rentW = max(rentW, len(r.rent))
		ownerW = max(ownerW, len(r.owner))
	}
	padRunes := func(s string, w int) string {
		if n := len([]rune(s)); n < w {
			return s + strings.Repeat(" ", w-n)
		}
		return s
	}
	// Optional trailing columns, emitted only when some row populates them.
	type optCol struct {
		show   bool
		header string
		width  int
		value  func(prodRow) string
	}
	opts := []optCol{
		{showID, "Facility ID", idW, func(r prodRow) string { return r.facID }},
		{showRent, "Rent/cycle", rentW, func(r prodRow) string { return r.rent }},
		{showOwner, "Owner", ownerW, func(r prodRow) string { return r.owner }},
	}
	fmt.Fprintf(b, "\n%s%s:\n", indent, heading)
	// Two-line header: units (/hr, /run, tick/run, ...) sit on row two.
	fmt.Fprintf(b, "%s  %-*s | %-*s | %*s | %*s | %*s | %*s | %*s | %*s | %-*s",
		indent, nameW, "Name", typeW, "Type", feeW, "Fee", outW, "Output", cycW, "Cycle",
		costW, "Run", queueW, "Queued", backW, "Backlog", pubW, "Public")
	for _, o := range opts {
		if o.show {
			fmt.Fprintf(b, " | %-*s", o.width, o.header)
		}
	}
	b.WriteString("\n")
	fmt.Fprintf(b, "%s  %-*s | %-*s | %*s | %*s | %*s | %*s | %*s | %*s | %-*s",
		indent, nameW, "", typeW, "", feeW, "/hr", outW, "/run", cycW, "tick/run",
		costW, "cost", queueW, "runs", backW, "ticks", pubW, "")
	for _, o := range opts {
		if o.show {
			fmt.Fprintf(b, " | %-*s", o.width, "")
		}
	}
	b.WriteString("\n")
	fmt.Fprintf(b, "%s  %s-+-%s-+-%s-+-%s-+-%s-+-%s-+-%s-+-%s-+-%s",
		indent,
		strings.Repeat("-", nameW), strings.Repeat("-", typeW), strings.Repeat("-", feeW),
		strings.Repeat("-", outW), strings.Repeat("-", cycW), strings.Repeat("-", costW),
		strings.Repeat("-", queueW), strings.Repeat("-", backW), strings.Repeat("-", pubW))
	for _, o := range opts {
		if o.show {
			fmt.Fprintf(b, "-+-%s", strings.Repeat("-", o.width))
		}
	}
	b.WriteString("\n")
	for _, r := range rows {
		fmt.Fprintf(b, "%s  %-*s | %s | %*s | %*s | %*s | %*s | %*s | %*s | %-*s",
			indent, nameW, r.name, padRunes(r.typ, typeW), feeW, r.feehr, outW, r.outrun,
			cycW, r.cycle, costW, r.runcost, queueW, r.queued, backW, r.backlog, pubW, r.public)
		for _, o := range opts {
			if o.show {
				fmt.Fprintf(b, " | %-*s", o.width, o.value(r))
			}
		}
		b.WriteString("\n")
	}
}
```

- [ ] **Step 5: Wire the faction production call site**

In `renderFactionFacilities` (`cmd/tools/play_as/main.go:~2209`), replace the production-mapping loop:

```go
	if len(production) > 0 {
		pf := make([]productionFacility, 0, len(production))
		for _, f := range production {
			rent := f.RentPerCycle
			pf = append(pf, productionFacility{
				name:         f.displayName(),
				prod:         f.Production,
				facilityID:   f.FacilityID,
				rentPerCycle: &rent,
			})
		}
		renderProductionFacilityTable(b, pf, indent, fmt.Sprintf("Faction Production (%d)", len(production)))
	}
```

- [ ] **Step 6: Wire the station production call site**

In `formatFacilityList`, the station production block (`cmd/tools/play_as/main.go:~2511`), replace the mapping loop (station facilities have `facility_id` but no per-cycle rent and no custom name):

```go
		if len(production) > 0 {
			totalSections++
			pf := make([]productionFacility, 0, len(production))
			for _, f := range production {
				pf = append(pf, productionFacility{name: f.Name, prod: f.Production, facilityID: f.FacilityID})
			}
			renderProductionFacilityTable(&b, pf, "  ", fmt.Sprintf("Station Production (%d)", len(production)))
		}
```

- [ ] **Step 7: Run the new test and the full play_as package**

Run: `go test ./cmd/tools/play_as/ -run TestFormatFacilityList_ProductionShowsIDAndRent`
Expected: PASS.

Run: `go test ./cmd/tools/play_as/`
Expected: PASS. The existing facility tests assert via `strings.Contains`, so the added columns leave them green. If any unexpectedly fails because an asserted substring shifted, update only that expectation to the new rendered output — do not remove coverage.

- [ ] **Step 8: Lint and commit**

Run: `golangci-lint run ./cmd/tools/play_as/` → expect 0 issues.

```bash
git add cmd/tools/play_as/main.go cmd/tools/play_as/facility_format_test.go
git commit -m "feat(play_as): production table shows facility_id, rent_per_cycle, owner columns"
```

---

### Task 3: Owner resolver in play_as

**Files:**
- Modify: `cmd/tools/play_as/main.go` (add `factionOwnerDisplay`; ensure `context` and `knowledge` are imported — they are)
- Test: `cmd/tools/play_as/facility_format_test.go` (add `TestFactionOwnerDisplay`)

**Interfaces:**
- Consumes: `(*knowledge.SQLiteKB).FactionTag(ctx, factionID)` from Task 1; the package-global `globalKB knowledge.Base`.
- Produces: `func factionOwnerDisplay(factionID string, cache map[string]string) string` — returns `"[TAG]"` when the KB resolves a tag, else the raw `factionID`; `""` for an empty id. Task 4 calls it once per public facility with a shared cache.

- [ ] **Step 1: Write the failing test**

Add to `cmd/tools/play_as/facility_format_test.go` (the test imports `knowledge` via `github.com/rsned/spacemolt/pkg/knowledge`; add the import to the test file if absent, plus `context`/`time` if absent):

```go
func TestFactionOwnerDisplay(t *testing.T) {
	// nil KB → falls back to the raw faction_id.
	saved := globalKB
	defer func() { globalKB = saved }()
	globalKB = nil
	if got := factionOwnerDisplay("f1", map[string]string{}); got != "f1" {
		t.Errorf("nil KB: factionOwnerDisplay(f1) = %q, want \"f1\"", got)
	}
	if got := factionOwnerDisplay("", map[string]string{}); got != "" {
		t.Errorf("empty id: factionOwnerDisplay(\"\") = %q, want \"\"", got)
	}

	// Seeded KB → resolves to the bracketed tag.
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	if err := kb.StoreFaction(context.Background(), knowledge.FactionRecord{FactionID: "f1", Tag: "CRFT", CapturedAt: time.Now()}); err != nil {
		t.Fatalf("StoreFaction: %v", err)
	}
	globalKB = kb
	if got := factionOwnerDisplay("f1", map[string]string{}); got != "[CRFT]" {
		t.Errorf("seeded KB: factionOwnerDisplay(f1) = %q, want \"[CRFT]\"", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestFactionOwnerDisplay`
Expected: FAIL — compile error `undefined: factionOwnerDisplay`.

- [ ] **Step 3: Add the resolver**

Add to `cmd/tools/play_as/main.go` (near the other facility formatter helpers, e.g. just above `renderProductionFacilityTable`):

```go
// factionOwnerDisplay resolves a faction_id to "[TAG]" via the knowledge base,
// falling back to the raw faction_id when the KB is unavailable (no --db-path)
// or the tag is unknown. cache memoizes lookups within a single render so each
// distinct faction is queried at most once. An empty id yields an empty string.
func factionOwnerDisplay(factionID string, cache map[string]string) string {
	if factionID == "" {
		return ""
	}
	if v, ok := cache[factionID]; ok {
		return v
	}
	display := factionID
	if sqlite, ok := globalKB.(*knowledge.SQLiteKB); ok && sqlite != nil {
		if tag, found, err := sqlite.FactionTag(context.Background(), factionID); err == nil && found && tag != "" {
			display = "[" + tag + "]"
		}
	}
	cache[factionID] = display
	return display
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/tools/play_as/ -run TestFactionOwnerDisplay`
Expected: PASS.

- [ ] **Step 5: Lint and commit**

Run: `golangci-lint run ./cmd/tools/play_as/` → expect 0 issues.

```bash
git add cmd/tools/play_as/main.go cmd/tools/play_as/facility_format_test.go
git commit -m "feat(play_as): add factionOwnerDisplay tag resolver with id fallback"
```

---

### Task 4: Public Facilities section

**Files:**
- Modify: `cmd/tools/play_as/main.go` (`formatFacilityList`: add `public_facilities` decode + new section after the Faction section)
- Modify: `pkg/game/serverapi/responses.go` (`FacilityListResponse`: add `PublicFacilities` field — it is consumed by daily-summary, faction/collector, and client_api_monitor)
- Test: `cmd/tools/play_as/facility_format_test.go` (add `TestFormatFacilityList_PublicFacilities`)

**Interfaces:**
- Consumes: `renderProductionFacilityTable` + `productionFacility` (Task 2); `factionOwnerDisplay` (Task 3); existing `facilityProduction`.
- Produces: nothing downstream.

- [ ] **Step 1: Write the failing test**

Add to `cmd/tools/play_as/facility_format_test.go`. Payload mirrors the live shape (public production facility with `faction_id`, no top-level `rent_per_cycle`); with no KB seeded the owner falls back to the faction_id:

```go
func TestFormatFacilityList_PublicFacilities(t *testing.T) {
	saved := globalKB
	defer func() { globalKB = saved }()
	globalKB = nil // owner falls back to faction_id

	raw := []byte(`{"base_id":"grand_exchange_station","public_facilities":[{"category":"production","description":"A compact fuel line.","facility_id":"fb24fd71","faction_id":"e727c0e9","level":1,"name":"H2 Fuel Combustor","production":{"backlog_ticks":0,"items_per_hour":78000,"output_per_run":100,"public":true,"queued_items":0,"queued_runs":0,"recipe":"Manufacture Fuel Basic","rental_fee_per_run":50,"ticks_per_run":0.46},"recipe_id":"manufacture_fuel_basic","type":"h2_fuel_combustor"}]}`)
	out := formatFacilityList(raw)
	for _, want := range []string{"Public Facilities (1):", "H2 Fuel Combustor", "⚙ Manufacture Fuel Basic", "Facility ID", "fb24fd71", "Owner", "e727c0e9"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatFacilityList output missing %q\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/tools/play_as/ -run TestFormatFacilityList_PublicFacilities`
Expected: FAIL — output has no "Public Facilities" section.

- [ ] **Step 3: Decode `public_facilities` in `formatFacilityList`**

In `formatFacilityList`, extend the `var resp struct { ... }` decode target (the one starting ~`cmd/tools/play_as/main.go:2373`) by adding this field alongside `FactionFacilities`:

```go
		PublicFacilities []struct {
			Category     string              `json:"category"`
			CustomName   string              `json:"custom_name"`
			FacilityID   string              `json:"facility_id"`
			FactionID    string              `json:"faction_id"`
			Name         string              `json:"name"`
			RentPerCycle *int64              `json:"rent_per_cycle"`
			Type         string              `json:"type"`
			Production   *facilityProduction `json:"production"`
		} `json:"public_facilities"`
```

- [ ] **Step 4: Render the Public Facilities section**

Immediately after the Faction section block (after the `if len(resp.FactionFacilities) > 0 { ... }` block that ends ~`cmd/tools/play_as/main.go:2479`, and before the `if showStationFacilities && ...` block), insert:

```go
	if len(resp.PublicFacilities) > 0 {
		slices.SortFunc(resp.PublicFacilities, func(a, c struct {
			Category     string              `json:"category"`
			CustomName   string              `json:"custom_name"`
			FacilityID   string              `json:"facility_id"`
			FactionID    string              `json:"faction_id"`
			Name         string              `json:"name"`
			RentPerCycle *int64              `json:"rent_per_cycle"`
			Type         string              `json:"type"`
			Production   *facilityProduction `json:"production"`
		}) int {
			return strings.Compare(a.Name, c.Name)
		})
		ownerCache := map[string]string{}
		pf := make([]productionFacility, 0, len(resp.PublicFacilities))
		for _, f := range resp.PublicFacilities {
			if f.Production == nil {
				continue // only production facilities render in this table
			}
			name := f.Name
			if f.CustomName != "" {
				name = f.CustomName
			}
			pf = append(pf, productionFacility{
				name:         name,
				prod:         f.Production,
				facilityID:   f.FacilityID,
				rentPerCycle: f.RentPerCycle,
				owner:        factionOwnerDisplay(f.FactionID, ownerCache),
			})
		}
		if len(pf) > 0 {
			totalSections++
			renderProductionFacilityTable(&b, pf, "  ", fmt.Sprintf("Public Facilities (%d)", len(pf)))
		}
	}
```

Note: the inline anonymous-struct type in the `SortFunc` must match the decode field's element type exactly. If duplicating the struct literal is awkward, the implementer may instead lift the public-facility element type to a named `type publicFacilityRow struct { ... }` beside `factionFacilityRow` and use it in both the decode field and the sort — either is acceptable as long as `go build` and `go vet` pass.

- [ ] **Step 5: Add the serverapi field**

In `pkg/game/serverapi/responses.go`, add to `FacilityListResponse` (after `FactionFacilities`):

```go
	PublicFacilities []map[string]any `json:"public_facilities"`
```

- [ ] **Step 6: Run the new test and full package**

Run: `go test ./cmd/tools/play_as/ -run TestFormatFacilityList_PublicFacilities`
Expected: PASS.

Run: `go test ./cmd/tools/play_as/ ./pkg/game/...`
Expected: PASS (existing facility tests stay green; serverapi field is additive).

- [ ] **Step 7: Lint and commit**

Run: `golangci-lint run ./cmd/tools/play_as/ ./pkg/game/serverapi/` → expect 0 issues.

```bash
git add cmd/tools/play_as/main.go cmd/tools/play_as/facility_format_test.go pkg/game/serverapi/responses.go
git commit -m "feat(play_as): render public_facilities section in facility list"
```

---

### Task 5: Full verification

**Files:** none (verification only).

- [ ] **Step 1: Full build + test + lint**

```bash
go build ./...
go test ./...
golangci-lint run ./...
```
Expected: build clean, all tests green, 0 new lint findings.

- [ ] **Step 2: Sanity-confirm the new behavior is wired end to end**

Run: `go test ./cmd/tools/play_as/ -run 'TestFormatFacilityList_PublicFacilities|TestFormatFacilityList_ProductionShowsIDAndRent|TestFactionOwnerDisplay' -v`
Expected: all three PASS.

---

## Self-Review

**Spec coverage:**
- §1 `FactionTag` KB method → Task 1. ✓
- §2 owner resolver (tag→id fallback, nil-KB safe, cached, context.Background) → Task 3. ✓
- §3 extended `productionFacility` + 3 conditional columns → Task 2. ✓
- §4 call-site wiring (faction id+rent; station id; public id+rent+owner+custom_name) → Tasks 2 (faction/station) + 4 (public). ✓
- §5 default-on Public Facilities section after Faction → Task 4. ✓
- §6 serverapi `PublicFacilities` (consumed → add) → Task 4 Step 5. ✓
- Testing (new section test, owner-resolution test, KB test; existing substring tests stay green) → Tasks 1/2/3/4. ✓
- Constraints (float ticks_per_run untouched, graceful nil-KB, no Base change, skip non-production public entries) → honored across tasks + global constraints. ✓

**Placeholder scan:** No TBD/"handle edge cases"/"similar to Task N"; every code step shows complete code. ✓

**Type/name consistency:** `productionFacility{name,prod,facilityID,rentPerCycle *int64,owner}`, `FactionTag(ctx,factionID)(string,bool,error)`, `factionOwnerDisplay(factionID string, cache map[string]string) string`, `globalKB`, `formatCredits(float64)` — names/signatures match across Tasks 1–4 and the codebase. ✓
