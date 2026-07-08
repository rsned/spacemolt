# API Struct / Command Drift Audit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Audit the client's API surface against the authoritative v0.473.0 server snapshot, fix the clear/mechanical drift, add a durable reverse-coverage guardrail, and end with an honest statement of what `BuiltForAPIVersion` actually covers.

**Architecture:** A throwaway in-package Go test (`pkg/game`) reflects over the committed `actionResponseTypes` map (command → `serverapi` response struct) and diffs three layers — command coverage, payload shape, response fields — against `get_commands.json`, `openapi.json`, and live sample JSON. Findings drive targeted fixes. A new permanent test locks in reverse command coverage so drift can't silently recur.

**Tech Stack:** Go 1.24+, standard library `reflect`/`encoding/json`/`go/*`, existing `pkg/game/serverapi` structs, `pkg/actionspace` registry.

## Deviation from spec (approved design)

The approved spec proposed a throwaway `scratchpad/drift_audit.py` that regex-parses Go structs. During planning we found `pkg/game/client_api_monitor.go:49` `actionResponseTypes` — a 204-entry committed map of command name → `serverapi` response struct. Reflecting over it in an **in-package Go test** is materially more accurate than regex-parsing `responses.go` for the exact thing we're auditing (struct fields), and still throwaway (deleted in the final task). The three-layer scope, deliverable, and guardrail are unchanged.

## Global Constraints

- Go 1.24+; use modern features (`range`-over-int, `b.Loop()` in benchmarks) where relevant.
- Every code-fix task is gated by **both** `go build ./...` and `go test ./...`. `go test` is mandatory — new/renamed interface methods break `pkg/agent` and `pkg/skills` mocks, which `go build` alone does not catch (`feedback_gameclient_interface_mocks`).
- New code must pass `golangci-lint` with no new findings.
- Authoritative sources (all ~v0.473, dated 2026-07-06/07):
  - `data/game-api/latest/get_commands.json` — 214 commands (`name`, `format`, `is_mutation`).
  - `server_docs/openapi.json` (→ `openapi.20260706.json`) — 256 component schemas incl. a `*Response` per command.
  - `data/game-api/latest/*.json` — ~28 live response samples (ground truth).
- Client surface audited: `pkg/game/client_api_monitor.go` (`actionResponseTypes`), `pkg/game/serverapi/responses.go`, `pkg/game/client_commands.go` (+ other `client*.go`), `pkg/actionspace/actions.go` (`AllActions`).
- Verdict rules for every candidate finding:
  - **Real drift, mechanical** → fix now (missing command, retired/renamed command, unambiguous 1:1 field rename).
  - **Real drift, risky/ambiguous** → document only (semantic change, restructured payload, unclear field meaning).
  - **False positive** → note why (const/helper Type, openapi generator noise, optional field legitimately absent from a sample).
- Live sample JSON wins over `openapi.json` when both describe the same struct; openapi-only findings are lower-confidence.

---

## Task 1: Throwaway drift-audit diff engine (produces the three-layer findings)

**Files:**
- Create (throwaway, deleted in Task 5): `pkg/game/drift_audit_test.go`
- Reads: `data/game-api/latest/get_commands.json`, `server_docs/openapi.json`, `data/game-api/latest/*.json`
- Reflects over: `pkg/game/client_api_monitor.go` `actionResponseTypes`

**Interfaces:**
- Produces: a findings file `scratchpad/drift-findings.md` (written by the test) with three sections — Layer 1 (command coverage), Layer 2 (payload shape), Layer 3 (response fields) — each row `command/struct | verdict-candidate | detail`. Later tasks consume this file.

- [ ] **Step 1: Write the diff-engine test**

Create `pkg/game/drift_audit_test.go` (package `game`, so it can read the unexported `actionResponseTypes`). It is a `Test`-prefixed function only so `go test -run` executes it; it makes no assertions, it writes a report.

```go
package game

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestDriftAuditReport is a throwaway analysis harness (deleted at end of audit).
// Run: go test ./pkg/game -run TestDriftAuditReport -v
func TestDriftAuditReport(t *testing.T) {
	root := filepath.Join("..", "..")
	latest := filepath.Join(root, "data", "game-api", "latest")

	// --- load server command list ---
	var cmdDoc struct {
		Commands []struct {
			Name       string `json:"name"`
			Format     string `json:"format"`
			IsMutation bool   `json:"is_mutation"`
		} `json:"commands"`
	}
	readJSON(t, filepath.Join(latest, "get_commands.json"), &cmdDoc)

	// --- load openapi schemas ---
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	readJSON(t, filepath.Join(root, "server_docs", "openapi.json"), &spec)

	var b strings.Builder
	fmt.Fprintf(&b, "# Drift Audit Findings (generated)\n\n")

	// ===== LAYER 1: command coverage =====
	fmt.Fprintf(&b, "## Layer 1 — command coverage\n\n")
	covered := map[string]bool{}
	for k := range actionResponseTypes {
		covered[k] = true
	}
	serverNames := map[string]bool{}
	for _, c := range cmdDoc.Commands {
		serverNames[c.Name] = true
	}
	var missing, retired []string
	for _, c := range cmdDoc.Commands {
		if !covered[c.Name] {
			missing = append(missing, c.Name)
		}
	}
	for k := range covered {
		if !serverNames[k] {
			retired = append(retired, k) // may be an event type, not a command — triage
		}
	}
	sort.Strings(missing)
	sort.Strings(retired)
	fmt.Fprintf(&b, "### server command NOT in actionResponseTypes (%d)\n%s\n\n", len(missing), bullets(missing))
	fmt.Fprintf(&b, "### actionResponseTypes key NOT a server command (%d — includes event types, triage)\n%s\n\n", len(retired), bullets(retired))

	// ===== LAYER 2: payload shape =====
	fmt.Fprintf(&b, "## Layer 2 — payload shape (server format keys per command)\n\n")
	for _, c := range cmdDoc.Commands {
		keys := payloadKeysFromFormat(c.Format)
		if len(keys) == 0 {
			continue
		}
		fmt.Fprintf(&b, "- `%s`: %s\n", c.Name, strings.Join(keys, ", "))
	}
	fmt.Fprintf(&b, "\n_(compare each against the client Payload map in client_commands.go during triage)_\n\n")

	// ===== LAYER 3: response field drift =====
	fmt.Fprintf(&b, "## Layer 3 — response field drift\n\n")
	var actions []string
	for k := range actionResponseTypes {
		actions = append(actions, k)
	}
	sort.Strings(actions)
	for _, action := range actions {
		rt := actionResponseTypes[action]
		structName := rt.Name()
		goTags := jsonTagsOf(rt)
		schema, hasSchema := spec.Components.Schemas[structName]
		if !hasSchema {
			// try common suffix e.g. FooResponse already is the name
			fmt.Fprintf(&b, "- `%s` (%s): UNVERIFIED — no openapi schema `%s`; fields=[%s]\n",
				action, structName, structName, strings.Join(goTags, ","))
			continue
		}
		var schemaFields []string
		for k := range schema.Properties {
			schemaFields = append(schemaFields, k)
		}
		onlyGo := diff(goTags, schemaFields)
		onlySchema := diff(schemaFields, goTags)
		if len(onlyGo) == 0 && len(onlySchema) == 0 {
			continue // match
		}
		fmt.Fprintf(&b, "- `%s` (%s): go-only=[%s] schema-only=[%s]\n",
			action, structName, strings.Join(onlyGo, ","), strings.Join(onlySchema, ","))
	}

	out := filepath.Join(root, "scratchpad", "drift-findings.md")
	_ = os.MkdirAll(filepath.Dir(out), 0o755)
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write findings: %v", err)
	}
	t.Logf("wrote %s", out)
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
}

func jsonTagsOf(rt reflect.Type) []string {
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	var tags []string
	if rt.Kind() != reflect.Struct {
		return tags
	}
	for i := range rt.NumField() {
		tag := rt.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		tags = append(tags, name)
	}
	sort.Strings(tags)
	return tags
}

func payloadKeysFromFormat(format string) []string {
	// format is a human string that embeds one or more JSON examples; extract the
	// first {"type":...,"payload":{...}} object and return its payload keys.
	i := strings.Index(format, "\"payload\"")
	if i < 0 {
		return nil
	}
	brace := strings.Index(format[i:], "{")
	if brace < 0 {
		return nil
	}
	sub := format[i+brace:]
	depth, end := 0, -1
	for j, r := range sub {
		if r == '{' {
			depth++
		} else if r == '}' {
			depth--
			if depth == 0 {
				end = j
				break
			}
		}
	}
	if end < 0 {
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(sub[:end+1]), &m) != nil {
		return nil
	}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func diff(a, b []string) []string {
	set := map[string]bool{}
	for _, x := range b {
		set[x] = true
	}
	var out []string
	for _, x := range a {
		if !set[x] {
			out = append(out, x)
		}
	}
	return out
}

func bullets(xs []string) string {
	if len(xs) == 0 {
		return "_(none)_"
	}
	var b strings.Builder
	for _, x := range xs {
		fmt.Fprintf(&b, "- %s\n", x)
	}
	return b.String()
}
```

- [ ] **Step 2: Run the diff engine**

Run: `go test ./pkg/game -run TestDriftAuditReport -v`
Expected: PASS, logs `wrote .../scratchpad/drift-findings.md`.

- [ ] **Step 3: Read the findings**

Read `scratchpad/drift-findings.md`. Confirm all three layers are populated (Layer 1 lists a small missing set, Layer 3 lists structs with field deltas + UNVERIFIED entries). This file is the input to Tasks 2–4. Do not commit it or the throwaway test yet.

- [ ] **Step 4: Note field-name normalization caveat**

Go json tags are snake_case; openapi property names should match. If Layer 3 shows *every* struct as fully drifted, the property extraction is wrong (e.g. schemas use `$ref`/`allOf` instead of inline `properties`) — fix `payloadKeysFromFormat`/schema-reading before triaging, otherwise findings are noise. Spot-check one known-good struct (e.g. `get_status` → `GetStatusResponse`) against `data/game-api/latest/get_status.json` to calibrate.

---

## Task 2: Reverse command-coverage guardrail + Layer 1 reconciliation

**Files:**
- Create: `pkg/game/command_coverage_test.go`
- Modify (as triage dictates): `pkg/game/client_api_monitor.go` (`actionResponseTypes`), `pkg/game/client_commands.go`, `pkg/game/interface.go`, `pkg/actionspace/actions.go`
- Consumes: Layer 1 section of `scratchpad/drift-findings.md`

**Interfaces:**
- Produces: `TestServerCommandsCoveredByClient` (permanent) — fails when a `get_commands.json` command is neither in `actionResponseTypes` nor in an explicit, justified `ignoredCommands` set.

- [ ] **Step 1: Write the failing guardrail test**

Create `pkg/game/command_coverage_test.go`:

```go
package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ignoredCommands are server commands the client intentionally does not support.
// Every entry MUST carry a justification; adding one is a deliberate, reviewed act.
var ignoredCommands = map[string]string{
	"v2_get_player":  "v2 API migration not started (project_v2_api_migration)",
	"v2_get_ship":    "v2 API migration not started",
	"v2_get_cargo":   "v2 API migration not started",
	"v2_get_skills":  "v2 API migration not started",
	"v2_get_missions": "v2 API migration not started",
	"v2_get_queue":   "v2 API migration not started",
	// Streaming subscriptions — no client consumer yet:
	"subscribe_market":       "streaming; no client consumer",
	"unsubscribe_market":     "streaming; no client consumer",
	"subscribe_observation":  "streaming; no client consumer",
	"unsubscribe_observation": "streaming; no client consumer",
	// NOTE: remaining entries are FILLED IN during Step 4 from real triage,
	// each with a one-line justification. Do not pre-populate speculatively.
}

func TestServerCommandsCoveredByClient(t *testing.T) {
	path := filepath.Join("..", "..", "data", "game-api", "latest", "get_commands.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("get_commands.json not found: %v", err)
	}
	var doc struct {
		Commands []struct {
			Name string `json:"name"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, c := range doc.Commands {
		if actionResponseTypes[c.Name] != nil {
			continue
		}
		if _, ok := ignoredCommands[c.Name]; ok {
			continue
		}
		t.Errorf("server command %q is not covered by actionResponseTypes and not in ignoredCommands "+
			"(add a client method + response struct, or add to ignoredCommands with justification)", c.Name)
	}
}
```

- [ ] **Step 2: Run it to confirm it fails on real gaps**

Run: `go test ./pkg/game -run TestServerCommandsCoveredByClient -v`
Expected: FAIL, one error line per genuinely-missing command (the Layer 1 `missing` set minus the `v2_*`/`subscribe_*` already ignored).

- [ ] **Step 3: Triage each failing command against the verdict rules**

For each command the test reports, decide using `scratchpad/drift-findings.md` + `get_commands.json` notes:
- **Client should support it** (real query/action players use) → Task 2 Step 4a.
- **Intentionally unsupported** (streaming, v2, admin-only) → Task 2 Step 4b.

- [ ] **Step 4a: Add coverage for commands the client should support**

For a genuinely-missing supported command, the mechanical fix is: add a response struct (if none) to `pkg/game/serverapi/responses.go`, register it in `actionResponseTypes`, and add a send method to `client_commands.go` (+ `interface.go` if part of `GameClient`). Template — the retired-command inverse, adding `commission_quote`:

```go
// serverapi/responses.go
type CommissionQuoteResponse struct {
	Action string `json:"action"`
	// ...fields per data/game-api/latest sample / openapi CommissionQuoteResponse schema...
}

// client_api_monitor.go, in actionResponseTypes:
"commission_quote": reflect.TypeOf(serverapi.CommissionQuoteResponse{}),
```

Only add the send method if the client actually needs to issue the command (many query commands are already issued via generic passthrough). If it is issued generically, registering the response type + covering it in the test is sufficient. Do NOT invent fields — copy them from the live sample or openapi schema; if neither exists, add the struct with the fields the openapi schema lists and note it UNVERIFIED in the report.

- [ ] **Step 4b: Add intentionally-unsupported commands to `ignoredCommands`**

Add each with a one-line justification (mirroring the `v2_*` entries). Example:

```go
"get_state": "full-state dump; client uses incremental get_status/get_ship instead",
```

- [ ] **Step 5: Handle the reverse direction (retired/renamed client entries)**

From Layer 1's "actionResponseTypes key NOT a server command" list, separate genuine event types (e.g. `arrived`, `jumped`, `battle_alert`, `queue` — keep, they are server-push events not commands) from truly-retired commands. For a retired command with a rename (the `salvage_wreck` → `scrap_wreck`+`sell_wreck` pattern, already removed from `actionspace`), remove or repoint the stale `actionResponseTypes` entry and any dead client method. Document each in the report.

- [ ] **Step 6: Run the guardrail + full build/test gate**

Run: `go test ./pkg/game -run TestServerCommandsCoveredByClient -v`
Expected: PASS.
Run: `go build ./...`
Expected: no errors.
Run: `go test ./...`
Expected: PASS (catches any mock/interface break from new methods).
Run: `golangci-lint run ./pkg/game/... ./pkg/actionspace/...` (or the repo's `golangci-lint` tool)
Expected: no new findings.

- [ ] **Step 7: Commit**

```bash
git add pkg/game/command_coverage_test.go pkg/game/client_api_monitor.go pkg/game/serverapi/responses.go pkg/game/client_commands.go pkg/game/interface.go pkg/actionspace/actions.go
git commit -m "fix(api): reconcile Layer 1 command coverage + add reverse-coverage guardrail

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Layer 2 payload-shape fixes

**Files:**
- Modify (as triage dictates): `pkg/game/client_commands.go` (+ other `client*.go`)
- Consumes: Layer 2 section of `scratchpad/drift-findings.md`

- [ ] **Step 1: Diff each command's server payload keys vs the client Payload map**

For each command in Layer 2, open its send method in `client_commands.go` and compare the `payload["..."]` keys the client sets against the server `format` keys the report lists. Classify:
- **Missing required key** the client never sends → mechanical fix (add it).
- **Client sends a key the server no longer accepts / renamed** → mechanical fix (rename/remove).
- **Dynamic/conditional payload** the static diff can't resolve → mark "manual-check", document, do not guess.

- [ ] **Step 2: Apply the mechanical fixes**

For a renamed key, edit the Payload map. Example pattern (renamed `ship_class` payload key):

```go
// before
payload["ship"] = shipClass
// after — server format shows "ship_class"
payload["ship_class"] = shipClass
```

Only touch keys that the server `format` unambiguously contradicts. Ambiguous ones go to the report.

- [ ] **Step 3: Build/test gate**

Run: `go build ./...` → no errors.
Run: `go test ./...` → PASS.
Run: `golangci-lint run` → no new findings.

- [ ] **Step 4: Commit**

```bash
git add -u pkg/game
git commit -m "fix(api): reconcile Layer 2 payload-shape drift

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Layer 3 response-field fixes

**Files:**
- Modify (as triage dictates): `pkg/game/serverapi/responses.go`
- Consumes: Layer 3 section of `scratchpad/drift-findings.md` + `data/game-api/latest/*.json`

**SCOPE (refined after Task 1, user decision): fix BREAKS only, document the rest.**
openapi is a *superset* — `schema-only` fields are mostly data the client intentionally
does not model, and `encoding/json` silently ignores unknown server fields, so they are
NOT breaks. Do **not** enrich structs with schema-only fields. The systematic
`go-only=[action]` and `go-only=[command,pending]` entries are false positives (the
client's `action` echo + synthetic mutation-tracking fields).

- [ ] **Step 1: Classify each Layer 3 finding**

For each finding, focus on `go-only` fields the client actually reads. Resolve authority
live-sample-first, then openapi. Classify:
- **`go-only` field the client reads, absent from BOTH live sample and schema, with an obvious `schema-only` counterpart** → real rename → mechanical: rename the json tag.
- **`go-only` field the client reads, absent from BOTH, no counterpart** → removed field → mechanical: remove it (+ fix consumers).
- **`go-only` = `action` / `command` / `pending`** → false positive → skip, no change.
- **`go-only` field present in a live sample** → server still sends it → keep, no change.
- **`schema-only` fields (server sends more than client models)** → NOT a break → **document only** in the findings report ("server emits additional fields X; client models what it uses"), do not add.
- **UNVERIFIED (no sample, no schema)** → leave as-is, list in report.

Net effect: Task 4's code changes are the narrow set of genuine renames/removals of
client-read fields. Everything else is documented, not touched.

- [ ] **Step 2: Apply the mechanical fixes**

Example — a 1:1 rename where server sample shows `credits_balance` but struct has `balance`:

```go
// serverapi/responses.go
type GetStatusResponse struct {
	// ...
	CreditsBalance int64 `json:"credits_balance"` // was: Balance `json:"balance"`
}
```

Update any code that referenced the renamed Go field (grep the field name; the compiler will also flag it). Do NOT rename a field on schema-only evidence when a live sample contradicts it.

- [ ] **Step 3: Build/test gate**

Run: `go build ./...` → no errors (surfaces every consumer of a renamed field).
Run: `go test ./...` → PASS.
Run: `golangci-lint run` → no new findings.

- [ ] **Step 4: Commit**

```bash
git add -u pkg/game/serverapi
git commit -m "fix(api): reconcile Layer 3 response-struct field drift

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Findings report, honest verdict, cleanup

**Files:**
- Create: `docs/superpowers/specs/2026-07-08-api-drift-audit-findings.md`
- Delete: `pkg/game/drift_audit_test.go` (throwaway), `scratchpad/drift-findings.md`
- Possibly modify: `pkg/version/checker.go` (only its `BuiltForAPIVersion` doc comment, if the verdict warrants a caveat)

- [ ] **Step 1: Write the findings report**

Create `docs/superpowers/specs/2026-07-08-api-drift-audit-findings.md` with, per layer: the candidate findings, the verdict assigned (mechanical-fixed / documented-deferred / false-positive with reason), and the commit that fixed each mechanical one. Include the `ignoredCommands` list with justifications and the retired-command dispositions from Task 2 Step 5.

- [ ] **Step 2: Write the honest BuiltForAPIVersion verdict**

End the report with an explicit statement: either
- "Commands + response structs are now verified at v0.473.0 for all covered commands; the only unverified surface is the N structs listed under Layer 3 UNVERIFIED (no sample, no schema)." — or —
- "Verified except for deferred items X, Y, Z (documented above)."
State plainly what the `BuiltForAPIVersion` claim does and does not now cover. If material gaps remain, add a one-line caveat to the constant's doc comment in `pkg/version/checker.go` pointing at this findings file.

- [ ] **Step 3: Remove the throwaway harness**

```bash
rm pkg/game/drift_audit_test.go scratchpad/drift-findings.md
```

Run: `go test ./...`
Expected: PASS (the permanent guardrail from Task 2 remains and still passes).

- [ ] **Step 4: Update the memory task file**

Mark `project_api_struct_drift_audit` done in memory (`MEMORY.md` line + the file), summarizing what was fixed vs deferred and linking the findings doc.

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/specs/2026-07-08-api-drift-audit-findings.md pkg/version/checker.go
git rm --cached pkg/game/drift_audit_test.go 2>/dev/null || true
git commit -m "docs(api): drift-audit findings + honest BuiltForAPIVersion verdict

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review notes

- **Spec coverage:** Layer 1 → Task 1+2; Layer 2 → Task 1+3; Layer 3 → Task 1+4; reverse guardrail → Task 2; findings report + honest verdict → Task 5; ignore-list w/ justification → Task 2 Step 1/4b. All spec sections mapped.
- **Data-dependent tasks:** Tasks 3 and 4 cannot pre-name exact edits because the specific drift is unknown until Task 1 runs — this is inherent to an audit. They instead specify the exact verdict rules, worked fix templates per category, and the mandatory build/test/lint gate. This is procedure, not placeholder.
- **Type consistency:** `TestServerCommandsCoveredByClient`, `ignoredCommands`, `actionResponseTypes`, `TestDriftAuditReport` names are used consistently across tasks.
