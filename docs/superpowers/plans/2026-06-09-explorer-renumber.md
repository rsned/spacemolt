# Explorer Renumbering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Renumber the 10 explorer agents so each agent's trailing number lands in its correct empire band, parking 2 surplus solarian explorers at 11/12 and leaving slots 9/10 as outerrim placeholder stubs — updating directories, personality.json ids, knowledge/daily-summary DB rows, and all report files.

**Architecture:** A one-shot Go CLI tool at `cmd/data/renumber-explorers/`, following the existing `cmd/data/*` migration-tool pattern. The renumber map is a hardcoded source of truth. All mutations are two-phase (temp-staged) to survive the permutation, gated behind an `--apply` flag (default = dry-run). Pure logic (map validation, band checks, id rewriting, report substitution, DB discovery/update) is unit-tested; the tool ends with a verification pass.

**Tech Stack:** Go 1.24, `database/sql` + `modernc.org/sqlite` (driver name `"sqlite"`), stdlib `os`/`regexp`/`encoding/json`.

---

## Background facts (do not re-derive)

- Module path: `github.com/rsned/spacemolt`.
- SQLite: `import _ "modernc.org/sqlite"`, open via `sql.Open("sqlite", path)`.
- Agent dirs live under `data/agents/<id>/`. Each has `personality.json` (tracked
  by git), plus gitignored `credentials.json`, `mbox.db*`, `.spacemolt-session.json`,
  `play_as_history.txt`.
- `personality.json` is 2-space-indented JSON; the id line looks exactly like:
  `  "id": "explorer-7",`.
- `credentials.json` already carries a matching `empire` field — **do not touch it.**
- Renumbering is local-only; game-server `username`/`player_id` are server-side and
  travel with the dir. **No credential/session edits.**
- Git surface: only `personality.json` files are tracked. `data/reports/` and the
  `.db` files are gitignored. The final commit captures only personality.json
  moves + 2 new placeholder stubs.

### The renumber map (source of truth)

| From | → To | Empire (travels) |
|------|------|------------------|
| explorer-7  | explorer-1  | nebula |
| explorer-10 | explorer-2  | nebula |
| explorer-1  | explorer-3  | solarian |
| explorer-2  | explorer-4  | solarian |
| explorer-3  | explorer-5  | voidborn |
| explorer-4  | explorer-6  | voidborn |
| explorer-5  | explorer-7  | crimson |
| explorer-6  | explorer-8  | crimson |
| explorer-8  | explorer-11 | solarian (parked) |
| explorer-9  | explorer-12 | solarian (parked) |

Placeholders created fresh (outerrim): `explorer-9`, `explorer-10`.

### File structure

- `cmd/data/renumber-explorers/plan.go` — map, expected-empire table, validation, id parsing.
- `cmd/data/renumber-explorers/fs.go` — staged dir renames, personality id rewrite, placeholder creation.
- `cmd/data/renumber-explorers/db.go` — DB backup, column discovery, staged UPDATEs.
- `cmd/data/renumber-explorers/reports.go` — single-pass report token rewrite.
- `cmd/data/renumber-explorers/verify.go` — post-run verification.
- `cmd/data/renumber-explorers/main.go` — flags + orchestration + dry-run printing.
- `cmd/data/renumber-explorers/*_test.go` — unit tests per file.

---

## Task 1: Map + validation + id parsing

**Files:**
- Create: `cmd/data/renumber-explorers/plan.go`
- Test: `cmd/data/renumber-explorers/plan_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import "testing"

func TestValidateRenamesIsBijection(t *testing.T) {
	if err := validateRenames(explorerRenames); err != nil {
		t.Fatalf("explorerRenames invalid: %v", err)
	}
	if len(explorerRenames) != 10 {
		t.Fatalf("want 10 renames, got %d", len(explorerRenames))
	}
}

func TestRenameTargetsMatchExpectedEmpire(t *testing.T) {
	// Every target slot must have an expected-empire entry.
	for _, r := range explorerRenames {
		n, err := explorerNum(r.To)
		if err != nil {
			t.Fatalf("bad target %q: %v", r.To, err)
		}
		if _, ok := expectedEmpire[n]; !ok {
			t.Fatalf("target %q (slot %d) has no expectedEmpire entry", r.To, n)
		}
	}
	for _, id := range placeholderSlots {
		n, _ := explorerNum(id)
		if expectedEmpire[n] != "outerrim" {
			t.Fatalf("placeholder slot %d should be outerrim, got %q", n, expectedEmpire[n])
		}
	}
}

func TestExplorerNum(t *testing.T) {
	n, err := explorerNum("explorer-12")
	if err != nil || n != 12 {
		t.Fatalf("got (%d,%v), want (12,nil)", n, err)
	}
	if _, err := explorerNum("explorer-x"); err == nil {
		t.Fatal("want error for non-numeric suffix")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/data/renumber-explorers/ -run TestValidate -v`
Expected: FAIL — `undefined: validateRenames` etc.

- [ ] **Step 3: Write minimal implementation**

```go
package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Rename is one explorer id move (from -> to).
type Rename struct {
	From string
	To   string
}

// explorerRenames is the source of truth for the renumbering. Every existing
// explorer moves; slots 9 and 10 are vacated for outerrim placeholders.
var explorerRenames = []Rename{
	{"explorer-7", "explorer-1"},   // nebula
	{"explorer-10", "explorer-2"},  // nebula
	{"explorer-1", "explorer-3"},   // solarian
	{"explorer-2", "explorer-4"},   // solarian
	{"explorer-3", "explorer-5"},   // voidborn
	{"explorer-4", "explorer-6"},   // voidborn
	{"explorer-5", "explorer-7"},   // crimson
	{"explorer-6", "explorer-8"},   // crimson
	{"explorer-8", "explorer-11"},  // solarian (parked surplus)
	{"explorer-9", "explorer-12"},  // solarian (parked surplus)
}

// placeholderSlots are new outerrim placeholder agents created after renames.
var placeholderSlots = []string{"explorer-9", "explorer-10"}

// expectedEmpire maps a final explorer slot number to its required empire.
// 11 and 12 are parked-surplus solarian slots outside the band scheme.
var expectedEmpire = map[int]string{
	1: "nebula", 2: "nebula",
	3: "solarian", 4: "solarian",
	5: "voidborn", 6: "voidborn",
	7: "crimson", 8: "crimson",
	9: "outerrim", 10: "outerrim",
	11: "solarian", 12: "solarian",
}

// explorerNum extracts the trailing integer from an explorer id.
func explorerNum(id string) (int, error) {
	suffix, ok := strings.CutPrefix(id, "explorer-")
	if !ok {
		return 0, fmt.Errorf("id %q lacks explorer- prefix", id)
	}
	return strconv.Atoi(suffix)
}

// validateRenames checks the map is a clean permutation: distinct sources,
// distinct targets, all well-formed explorer ids.
func validateRenames(rs []Rename) error {
	froms := map[string]bool{}
	tos := map[string]bool{}
	for _, r := range rs {
		if _, err := explorerNum(r.From); err != nil {
			return fmt.Errorf("bad from %q: %w", r.From, err)
		}
		if _, err := explorerNum(r.To); err != nil {
			return fmt.Errorf("bad to %q: %w", r.To, err)
		}
		if froms[r.From] {
			return fmt.Errorf("duplicate source %q", r.From)
		}
		if tos[r.To] {
			return fmt.Errorf("duplicate target %q", r.To)
		}
		froms[r.From] = true
		tos[r.To] = true
	}
	return nil
}

// renameMap returns the from->to lookup used for report rewriting.
func renameMap(rs []Rename) map[string]string {
	m := make(map[string]string, len(rs))
	for _, r := range rs {
		m[r.From] = r.To
	}
	return m
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/data/renumber-explorers/ -run 'TestValidate|TestRename|TestExplorerNum' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/data/renumber-explorers/plan.go cmd/data/renumber-explorers/plan_test.go
git commit -m "feat(renumber): explorer rename map + validation"
```

---

## Task 2: Staged directory renames

**Files:**
- Create: `cmd/data/renumber-explorers/fs.go`
- Test: `cmd/data/renumber-explorers/fs_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func mkAgent(t *testing.T, agentsDir, id, empire string) {
	t.Helper()
	dir := filepath.Join(agentsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "empire": "` + empire + `",
  "id": "` + id + `",
  "role": "Explorer"
}`
	if err := os.WriteFile(filepath.Join(dir, "personality.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStageRenameDirsPermutation(t *testing.T) {
	agentsDir := t.TempDir()
	// Minimal 2-cycle: a->b, b->a, which naive renaming would clobber.
	mkAgent(t, agentsDir, "explorer-1", "solarian")
	mkAgent(t, agentsDir, "explorer-3", "voidborn")
	rs := []Rename{{"explorer-1", "explorer-3"}, {"explorer-3", "explorer-1"}}

	if err := stageRenameDirs(agentsDir, rs, true); err != nil {
		t.Fatal(err)
	}
	// explorer-3 dir should now hold the formerly-explorer-1 (solarian) content.
	got := readEmpire(t, filepath.Join(agentsDir, "explorer-3", "personality.json"))
	if got != "solarian" {
		t.Fatalf("explorer-3 empire = %q, want solarian", got)
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "explorer-1.staging")); !os.IsNotExist(err) {
		t.Fatal("staging dir left behind")
	}
}

func readEmpire(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// crude extract for test only
	s := string(b)
	i := indexAfter(s, `"empire": "`)
	j := i
	for j < len(s) && s[j] != '"' {
		j++
	}
	return s[i:j]
}

func indexAfter(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i + len(sub)
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/data/renumber-explorers/ -run TestStageRenameDirs -v`
Expected: FAIL — `undefined: stageRenameDirs`

- [ ] **Step 3: Write minimal implementation**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// stageRenameDirs renames every from-dir to its to-dir using a two-phase
// staging move so a full permutation never clobbers a live directory.
// When apply is false it only validates that every source exists.
func stageRenameDirs(agentsDir string, rs []Rename, apply bool) error {
	for _, r := range rs {
		src := filepath.Join(agentsDir, r.From)
		if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
			return fmt.Errorf("source dir missing: %s", src)
		}
	}
	if !apply {
		return nil
	}
	// Phase 1: from -> from.staging
	for _, r := range rs {
		src := filepath.Join(agentsDir, r.From)
		stg := filepath.Join(agentsDir, r.From+".staging")
		if err := os.Rename(src, stg); err != nil {
			return fmt.Errorf("stage %s: %w", r.From, err)
		}
	}
	// Phase 2: from.staging -> to
	for _, r := range rs {
		stg := filepath.Join(agentsDir, r.From+".staging")
		dst := filepath.Join(agentsDir, r.To)
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("target dir already exists: %s", dst)
		}
		if err := os.Rename(stg, dst); err != nil {
			return fmt.Errorf("finalize %s -> %s: %w", r.From, r.To, err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/data/renumber-explorers/ -run TestStageRenameDirs -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/data/renumber-explorers/fs.go cmd/data/renumber-explorers/fs_test.go
git commit -m "feat(renumber): two-phase staged directory renames"
```

---

## Task 3: personality.json id rewrite + placeholder creation

**Files:**
- Modify: `cmd/data/renumber-explorers/fs.go`
- Modify: `cmd/data/renumber-explorers/fs_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRewritePersonalityID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "personality.json")
	os.WriteFile(path, []byte("{\n  \"empire\": \"crimson\",\n  \"id\": \"explorer-5\",\n  \"role\": \"Explorer\"\n}"), 0o644)

	if err := rewritePersonalityID(path, "explorer-7"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !contains(string(b), `"id": "explorer-7"`) {
		t.Fatalf("id not rewritten:\n%s", b)
	}
	if contains(string(b), "explorer-5") {
		t.Fatalf("old id still present:\n%s", b)
	}
	if !contains(string(b), `"empire": "crimson"`) {
		t.Fatalf("empire was disturbed:\n%s", b)
	}
}

func TestCreatePlaceholder(t *testing.T) {
	agentsDir := t.TempDir()
	if err := createPlaceholder(agentsDir, "explorer-9", true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(agentsDir, "explorer-9", "personality.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"id": "explorer-9"`, `"empire": "outerrim"`, `"placeholder": true`} {
		if !contains(string(b), want) {
			t.Fatalf("placeholder missing %q:\n%s", want, b)
		}
	}
}

func contains(s, sub string) bool { return indexAfter(s, sub) >= 0 }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/data/renumber-explorers/ -run 'TestRewritePersonalityID|TestCreatePlaceholder' -v`
Expected: FAIL — `undefined: rewritePersonalityID`

- [ ] **Step 3: Write minimal implementation** (append to `fs.go`)

```go
import (
	"encoding/json"
	"regexp"
)

var idLineRe = regexp.MustCompile(`("id"\s*:\s*")explorer-\d+(")`)

// rewritePersonalityID replaces only the id field in a personality.json,
// leaving all other formatting and fields byte-for-byte intact.
func rewritePersonalityID(path, newID string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out := idLineRe.ReplaceAll(b, []byte("${1}"+newID+"${2}"))
	if string(out) == string(b) {
		return fmt.Errorf("no id field rewritten in %s", path)
	}
	return os.WriteFile(path, out, 0o644)
}

type placeholderDoc struct {
	ID          string `json:"id"`
	Role        string `json:"role"`
	Empire      string `json:"empire"`
	Placeholder bool   `json:"placeholder"`
}

// createPlaceholder writes a stub outerrim explorer slot with no credentials.
func createPlaceholder(agentsDir, id string, apply bool) error {
	if !apply {
		return nil
	}
	dir := filepath.Join(agentsDir, id)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("placeholder target exists: %s", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	doc := placeholderDoc{ID: id, Role: "Explorer", Empire: "outerrim", Placeholder: true}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "personality.json"), append(b, '\n'), 0o644)
}
```

Note: the `import` block above is illustrative — merge these imports into the
single existing `import (...)` block at the top of `fs.go` (`encoding/json`,
`regexp` added alongside `fmt`, `os`, `path/filepath`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/data/renumber-explorers/ -run 'TestRewritePersonalityID|TestCreatePlaceholder' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/data/renumber-explorers/fs.go cmd/data/renumber-explorers/fs_test.go
git commit -m "feat(renumber): personality id rewrite + placeholder stub"
```

---

## Task 4: DB backup, column discovery, staged UPDATEs

**Files:**
- Create: `cmd/data/renumber-explorers/db.go`
- Test: `cmd/data/renumber-explorers/db_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDiscoverAndStagedUpdate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "k.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mustExec(t, db, `CREATE TABLE experiences (agent_id TEXT, note TEXT)`)
	mustExec(t, db, `CREATE TABLE pois (detected_by TEXT, n INTEGER)`)
	mustExec(t, db, `INSERT INTO experiences VALUES ('explorer-1','a'),('explorer-3','b'),('miner-2','c')`)
	mustExec(t, db, `INSERT INTO pois VALUES ('explorer-3',1)`)

	cols, err := discoverAgentColumns(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 2 {
		t.Fatalf("want 2 agent columns, got %d: %v", len(cols), cols)
	}

	// 2-cycle permutation: 1->3, 3->1.
	rs := []Rename{{"explorer-1", "explorer-3"}, {"explorer-3", "explorer-1"}}
	if err := stagedUpdateDB(db, cols, rs, true); err != nil {
		t.Fatal(err)
	}
	// experiences row formerly explorer-1 is now explorer-3 and vice versa; no loss.
	if got := countWhere(t, db, "experiences", "agent_id", "explorer-3"); got != 1 {
		t.Fatalf("explorer-3 count = %d, want 1", got)
	}
	if got := countWhere(t, db, "experiences", "agent_id", "explorer-1"); got != 1 {
		t.Fatalf("explorer-1 count = %d, want 1", got)
	}
	if got := countLike(t, db, "experiences", "agent_id", "%__staging"); got != 0 {
		t.Fatalf("staging values left: %d", got)
	}
	// pois explorer-3 -> explorer-1
	if got := countWhere(t, db, "pois", "detected_by", "explorer-1"); got != 1 {
		t.Fatalf("pois explorer-1 count = %d, want 1", got)
	}
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func countWhere(t *testing.T, db *sql.DB, table, col, val string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM "+table+" WHERE "+col+"=?", val).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countLike(t *testing.T, db *sql.DB, table, col, pat string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM "+table+" WHERE "+col+" LIKE ?", pat).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/data/renumber-explorers/ -run TestDiscoverAndStagedUpdate -v`
Expected: FAIL — `undefined: discoverAgentColumns`

- [ ] **Step 3: Write minimal implementation**

```go
package main

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"time"
)

// tableCol identifies a text column that holds explorer agent ids.
type tableCol struct {
	Table string
	Col   string
}

// backupDB copies a sqlite file to a timestamped sibling before mutation.
func backupDB(path string) (string, error) {
	dst := fmt.Sprintf("%s.bak-renumber-%s", path, time.Now().Format("20060102-150405"))
	in, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return "", err
	}
	return dst, out.Sync()
}

// discoverAgentColumns scans every table's columns for any value matching
// 'explorer-%', returning the (table,col) pairs that hold agent ids.
func discoverAgentColumns(db *sql.DB) ([]tableCol, error) {
	tables, err := queryStrings(db, `SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		return nil, err
	}
	var cols []tableCol
	for _, t := range tables {
		rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%q)", t))
		if err != nil {
			return nil, err
		}
		var names []string
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt any
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				rows.Close()
				return nil, err
			}
			names = append(names, name)
		}
		rows.Close()
		for _, c := range names {
			var hit int
			q := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %q WHERE %q LIKE 'explorer-%%')", t, c)
			if err := db.QueryRow(q).Scan(&hit); err != nil {
				continue // non-text column; LIKE is harmless but ignore errors
			}
			if hit == 1 {
				cols = append(cols, tableCol{Table: t, Col: c})
			}
		}
	}
	return cols, nil
}

// stagedUpdateDB rewrites agent ids in every discovered column using a
// two-phase update so the permutation never collides on a UNIQUE/PK column.
func stagedUpdateDB(db *sql.DB, cols []tableCol, rs []Rename, apply bool) error {
	if !apply {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rolled back unless Commit succeeds
	for _, tc := range cols {
		// Phase 1: from -> from||'__staging'
		for _, r := range rs {
			q := fmt.Sprintf("UPDATE %q SET %q=? WHERE %q=?", tc.Table, tc.Col, tc.Col)
			if _, err := tx.Exec(q, r.From+"__staging", r.From); err != nil {
				return fmt.Errorf("%s.%s phase1 %s: %w", tc.Table, tc.Col, r.From, err)
			}
		}
		// Phase 2: from||'__staging' -> to
		for _, r := range rs {
			q := fmt.Sprintf("UPDATE %q SET %q=? WHERE %q=?", tc.Table, tc.Col, tc.Col)
			if _, err := tx.Exec(q, r.To, r.From+"__staging"); err != nil {
				return fmt.Errorf("%s.%s phase2 %s: %w", tc.Table, tc.Col, r.From, err)
			}
		}
	}
	return tx.Commit()
}

func queryStrings(db *sql.DB, q string) ([]string, error) {
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/data/renumber-explorers/ -run TestDiscoverAndStagedUpdate -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/data/renumber-explorers/db.go cmd/data/renumber-explorers/db_test.go
git commit -m "feat(renumber): db backup, column discovery, staged updates"
```

---

## Task 5: Single-pass report rewrite

**Files:**
- Create: `cmd/data/renumber-explorers/reports.go`
- Test: `cmd/data/renumber-explorers/reports_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRewriteReportsSinglePass(t *testing.T) {
	dir := t.TempDir()
	// 1->3 and 3->5: a chained replacement would turn explorer-1 into explorer-5.
	p := filepath.Join(dir, "daily.md")
	os.WriteFile(p, []byte("explorer-1 mined; explorer-3 scouted; explorer-10 idle; miner-1 ok"), 0o644)
	m := map[string]string{"explorer-1": "explorer-3", "explorer-3": "explorer-5", "explorer-10": "explorer-2"}

	n, err := rewriteReports(dir, m, true)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("changed files = %d, want 1", n)
	}
	got, _ := os.ReadFile(p)
	want := "explorer-3 mined; explorer-5 scouted; explorer-2 idle; miner-1 ok"
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/data/renumber-explorers/ -run TestRewriteReports -v`
Expected: FAIL — `undefined: rewriteReports`

- [ ] **Step 3: Write minimal implementation**

```go
package main

import (
	"os"
	"path/filepath"
	"regexp"
)

var explorerTokenRe = regexp.MustCompile(`explorer-\d+`)

// rewriteReports walks reportsDir and replaces explorer ids per m in a single
// pass over each file (mapping the original token, so chained renames do not
// compound). Returns the number of files changed.
func rewriteReports(reportsDir string, m map[string]string, apply bool) (int, error) {
	var changed int
	err := filepath.WalkDir(reportsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out := explorerTokenRe.ReplaceAllStringFunc(string(b), func(tok string) string {
			if to, ok := m[tok]; ok {
				return to
			}
			return tok
		})
		if out == string(b) {
			return nil
		}
		changed++
		if apply {
			return os.WriteFile(path, []byte(out), 0o644)
		}
		return nil
	})
	return changed, err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/data/renumber-explorers/ -run TestRewriteReports -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/data/renumber-explorers/reports.go cmd/data/renumber-explorers/reports_test.go
git commit -m "feat(renumber): single-pass report id rewrite"
```

---

## Task 6: Verification pass

**Files:**
- Create: `cmd/data/renumber-explorers/verify.go`
- Test: `cmd/data/renumber-explorers/verify_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"path/filepath"
	"testing"
)

func TestVerifyAgentsBands(t *testing.T) {
	agentsDir := t.TempDir()
	// Correct: slot 1 nebula, slot 9 outerrim.
	mkAgent(t, agentsDir, "explorer-1", "nebula")
	mkAgent(t, agentsDir, "explorer-9", "outerrim")
	if probs := verifyAgents(agentsDir, []string{"explorer-1", "explorer-9"}); len(probs) != 0 {
		t.Fatalf("unexpected problems: %v", probs)
	}
	// Wrong: slot 2 should be nebula, give it crimson.
	mkAgent(t, agentsDir, "explorer-2", "crimson")
	probs := verifyAgents(agentsDir, []string{"explorer-2"})
	if len(probs) == 0 {
		t.Fatal("expected a band mismatch problem")
	}
	_ = filepath.Join // keep import used if trimmed
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/data/renumber-explorers/ -run TestVerifyAgentsBands -v`
Expected: FAIL — `undefined: verifyAgents`

- [ ] **Step 3: Write minimal implementation**

The complete `verify.go` — two functions, `verifyAgents` and `verifyDBProblems`:

```go
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// verifyAgents confirms each listed agent dir has a personality.json whose id
// matches the dir and whose empire matches its slot's expected empire.
func verifyAgents(agentsDir string, ids []string) []string {
	var probs []string
	for _, id := range ids {
		path := filepath.Join(agentsDir, id, "personality.json")
		b, err := os.ReadFile(path)
		if err != nil {
			probs = append(probs, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		var doc struct {
			ID     string `json:"id"`
			Empire string `json:"empire"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			probs = append(probs, fmt.Sprintf("%s: bad json: %v", id, err))
			continue
		}
		if doc.ID != id {
			probs = append(probs, fmt.Sprintf("%s: id field is %q", id, doc.ID))
		}
		n, err := explorerNum(id)
		if err != nil {
			probs = append(probs, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		if want := expectedEmpire[n]; doc.Empire != want {
			probs = append(probs, fmt.Sprintf("%s: empire %q, want %q", id, doc.Empire, want))
		}
	}
	return probs
}

// verifyDBProblems checks discovered columns for leftover staging values or
// explorer ids whose number falls outside the valid 1..12 range.
func verifyDBProblems(db *sql.DB, cols []tableCol) []string {
	var probs []string
	for _, tc := range cols {
		var stg int
		q := fmt.Sprintf("SELECT count(*) FROM %q WHERE %q LIKE 'explorer-%%__staging'", tc.Table, tc.Col)
		if err := db.QueryRow(q).Scan(&stg); err == nil && stg > 0 {
			probs = append(probs, fmt.Sprintf("%s.%s: %d staging values left", tc.Table, tc.Col, stg))
		}
		ids, _ := queryStrings(db, fmt.Sprintf("SELECT DISTINCT %q FROM %q WHERE %q LIKE 'explorer-%%'", tc.Col, tc.Table, tc.Col))
		for _, id := range ids {
			if n, err := explorerNum(id); err != nil || n < 1 || n > 12 {
				probs = append(probs, fmt.Sprintf("%s.%s: bad id %q", tc.Table, tc.Col, id))
			}
		}
	}
	return probs
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/data/renumber-explorers/ -run TestVerifyAgentsBands -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/data/renumber-explorers/verify.go cmd/data/renumber-explorers/verify_test.go
git commit -m "feat(renumber): agent-band + db verification"
```

---

## Task 7: main() orchestration + flags

**Files:**
- Create: `cmd/data/renumber-explorers/main.go`

- [ ] **Step 1: Write the implementation**

```go
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func main() {
	dataDir := flag.String("data-dir", "data", "path to the data directory")
	apply := flag.Bool("apply", false, "apply changes (default: dry-run)")
	flag.Parse()

	if err := validateRenames(explorerRenames); err != nil {
		log.Fatalf("invalid rename map: %v", err)
	}

	agentsDir := filepath.Join(*dataDir, "agents")
	reportsDir := filepath.Join(*dataDir, "reports")
	dbs := []string{
		filepath.Join(*dataDir, "spacemolt-knowledge.db"),
		filepath.Join(*dataDir, "daily-summary.db"),
	}
	m := renameMap(explorerRenames)

	mode := "DRY-RUN"
	if *apply {
		mode = "APPLY"
	}
	fmt.Printf("=== explorer renumber (%s) ===\n", mode)
	for _, r := range explorerRenames {
		fmt.Printf("  rename dir  %-12s -> %s\n", r.From, r.To)
	}
	for _, id := range placeholderSlots {
		fmt.Printf("  placeholder %s (outerrim)\n", id)
	}

	// 1. Directory renames (staged).
	if err := stageRenameDirs(agentsDir, explorerRenames, *apply); err != nil {
		log.Fatalf("dir rename: %v", err)
	}
	// 2. personality.json id rewrites at the new locations.
	if *apply {
		for _, r := range explorerRenames {
			path := filepath.Join(agentsDir, r.To, "personality.json")
			if err := rewritePersonalityID(path, r.To); err != nil {
				log.Fatalf("id rewrite %s: %v", r.To, err)
			}
		}
	}
	// 3. Placeholder stubs.
	for _, id := range placeholderSlots {
		if err := createPlaceholder(agentsDir, id, *apply); err != nil {
			log.Fatalf("placeholder %s: %v", id, err)
		}
	}
	// 4. Databases.
	for _, dbPath := range dbs {
		if err := processDB(dbPath, m, *apply); err != nil {
			log.Fatalf("db %s: %v", dbPath, err)
		}
	}
	// 5. Reports.
	n, err := rewriteReports(reportsDir, m, *apply)
	if err != nil {
		log.Fatalf("reports: %v", err)
	}
	fmt.Printf("  reports: %d file(s) %s\n", n, map[bool]string{true: "rewritten", false: "would change"}[*apply])

	// 6. Verification (only meaningful after apply).
	if *apply {
		var finalIDs []string
		for _, r := range explorerRenames {
			finalIDs = append(finalIDs, r.To)
		}
		finalIDs = append(finalIDs, placeholderSlots...)
		if probs := verifyAgents(agentsDir, finalIDs); len(probs) > 0 {
			log.Fatalf("verification failed:\n%v", probs)
		}
		for _, dbPath := range dbs {
			if probs := verifyDBProblemsAt(dbPath); len(probs) > 0 {
				log.Fatalf("db verification %s failed:\n%v", dbPath, probs)
			}
		}
		fmt.Println("  verification: OK")
	}
	fmt.Println("done.")
}

// processDB backs up, discovers agent columns, and staged-updates one database.
func processDB(dbPath string, m map[string]string, apply bool) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	cols, err := discoverAgentColumns(db)
	if err != nil {
		return err
	}
	fmt.Printf("  db %s: %d agent column(s)\n", filepath.Base(dbPath), len(cols))
	if !apply {
		return nil
	}
	bak, err := backupDB(dbPath)
	if err != nil {
		return err
	}
	fmt.Printf("    backup -> %s\n", filepath.Base(bak))
	var rs []Rename
	for from, to := range m {
		rs = append(rs, Rename{From: from, To: to})
	}
	return stagedUpdateDB(db, cols, rs, true)
}

func verifyDBProblemsAt(dbPath string) []string {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return []string{err.Error()}
	}
	defer db.Close()
	cols, err := discoverAgentColumns(db)
	if err != nil {
		return []string{err.Error()}
	}
	return verifyDBProblems(db, cols)
}
```

- [ ] **Step 2: Build the tool**

Run: `go build ./cmd/data/renumber-explorers/`
Expected: builds clean.

- [ ] **Step 3: Run full unit tests**

Run: `go test ./cmd/data/renumber-explorers/ -v`
Expected: all PASS.

- [ ] **Step 4: Lint**

Run the `golangci-lint` tool on `cmd/data/renumber-explorers/`.
Expected: no new findings.

- [ ] **Step 5: Commit**

```bash
git add cmd/data/renumber-explorers/main.go
git commit -m "feat(renumber): main orchestration + dry-run/apply flags"
```

---

## Task 8: Dry-run, apply, and commit the migration against real data

**Files:** none (operates on `data/`).

**Precondition:** Stop the spacemolt-server and any running auto-* / play_as
agents first. The knowledge/daily-summary DBs are live SQLite (WAL mode) and the
agent dirs are read at runtime — renaming and updating them while a writer holds
them risks WAL frames not captured by `backupDB` (which copies only the main
`.db` file) and torn reads. Confirm no process is writing `data/` before Step 3.

- [ ] **Step 1: Dry-run and eyeball the plan**

Run: `go run ./cmd/data/renumber-explorers/ -data-dir data`
Expected output includes all 10 `rename dir` lines, 2 `placeholder` lines, the
agent-column counts per DB (knowledge.db should report ~10 columns incl.
experiences/agents/market_snapshots/ship_listings/anomalies/pois/poi_resources/
storage_snapshots/change_snapshots/xp_observations; daily-summary.db ~2:
snapshots/faction_snapshots), and a `reports: N file(s) would change` line.
Confirm nothing was mutated: `git status data/agents/` shows no changes.

- [ ] **Step 2: Snapshot pre-state for the conservation check**

Run:
```bash
sqlite3 data/spacemolt-knowledge.db "SELECT count(*) FROM xp_observations WHERE agent_id LIKE 'explorer-%';"
```
Record the number. (It should be conserved after apply — only the ids change.)

- [ ] **Step 3: Apply**

Run: `go run ./cmd/data/renumber-explorers/ -data-dir data -apply`
Expected: ends with `verification: OK` then `done.`. If verification fails the
tool exits non-zero and you restore from the `.bak-renumber-*` DB copies and the
staging dirs before investigating.

- [ ] **Step 4: Confirm conservation + bands**

Run:
```bash
sqlite3 data/spacemolt-knowledge.db "SELECT count(*) FROM xp_observations WHERE agent_id LIKE 'explorer-%';"
for n in 1 2 3 4 5 6 7 8 9 10 11 12; do
  printf 'explorer-%s ' "$n"
  grep -o '"empire"[^,]*' data/agents/explorer-$n/personality.json | head -1
done
```
Expected: xp_observations count identical to Step 2; empires read
nebula,nebula,solarian,solarian,voidborn,voidborn,crimson,crimson,outerrim,
outerrim,solarian,solarian.

- [ ] **Step 5: Commit the tracked changes**

Only `personality.json` files are git-tracked (DBs, reports, credentials are
gitignored runtime data). Stage the renames and new stubs:

```bash
git add -A data/agents/
git status   # expect: explorer-* personality.json renames + explorer-9/10 stubs
git commit -m "chore(agents): renumber explorers to align empire bands

Renumber the 10 explorers so trailing numbers match empire bands
(1-2 nebula, 3-4 solarian, 5-6 voidborn, 7-8 crimson, 9-10 outerrim),
park 2 surplus solarian explorers at 11/12, and add outerrim placeholder
stubs at 9/10. DB rows and report files updated out-of-band by
cmd/data/renumber-explorers (gitignored runtime data).

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 6: Clean up backups (optional)**

After confirming the game still logs the agents in correctly, the
`data/*.db.bak-renumber-*` copies can be deleted. Leave them until verified live.

---

## Self-review notes

- **Spec coverage:** renumber map (Task 1), staged dir renames (Task 2),
  personality id + placeholders (Task 3), DB backup/discovery/staged update
  (Task 4), report rewrite (Task 5), verification (Task 6), orchestration +
  dry-run (Task 7), real-data apply + commit (Task 8). The known explorer-7
  username wart is documented in the spec and intentionally has no task.
- **No server/credential edits** — confirmed by design; no task touches them.
- **Permutation safety** — both dir renames and DB updates are two-phase staged;
  reports use single-pass map lookup. All three guard against the cycle
  `explorer-1 -> explorer-3 -> explorer-5 -> explorer-7 -> explorer-1`.
