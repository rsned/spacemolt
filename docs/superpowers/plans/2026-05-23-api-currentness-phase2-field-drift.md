# API Currentness — Phase 2: Field-Drift Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring 25 existing `serverapi` response structs into line with server spec v0.322.1 by adding missing top-level fields (and fixing two structs that wrongly nest fields the server sends flat), so the API monitor stops flagging real server fields as unknown and so the structs actually capture the data.

**Architecture:** Each action already has a struct registered in `actionResponseTypes` (`pkg/game/client_api_monitor.go`). The server's "ok" frame flattens the spec's `result` object to the top level, so the fix is to make each struct's TOP-LEVEL json tags cover every field the spec's `result` schema declares. 23 actions need plain field additions; 2 (`read_note`, `complete_mission`) need a structural flatten because they currently nest under a sub-object the server does not nest; 1 (`self_destruct`) is fixed by remapping it to the existing `PendingActionResponse` in the monitor map. The `message`/`action`/`player`/`ship`/`modules` keys are envelope-level (`commonOKFields`) and must NOT be added to structs.

**Tech Stack:** Go 1.25, `golangci-lint` gate, table-driven tests in `package game` reusing the `assertActionFields` helper added in Phase 1 (`pkg/game/drone_actions_test.go`).

---

## Conventions used in every task

**Field typing (matches Phase 1):** money/credit/xp/reward/tax/price values → `int64`; pure counts, quantities, fuel units, pagination indices → `int`; booleans → `bool`; strings → `string`; arrays and nested objects → `json.RawMessage` (the monitor only checks top-level tag names; we avoid guessing nested shapes). `responses.go` already imports `encoding/json`.

**Do NOT add envelope fields:** `message`, `action`, `player`, `ship`, `modules` are in `commonOKFields` (`pkg/game/client_api_monitor.go`) and are allowed on every OK frame without being in the struct. Several actions list `message` as "missing" in raw diffs — that is a false positive; do not add `message`.

**Test pattern (per task, own file):** reuse the shared `assertActionFields(t, map[string][]string{...})` helper from `pkg/game/drone_actions_test.go` (same `package game`). It asserts each action is registered in `actionResponseTypes` and its struct's top-level json tags include the listed fields. Do NOT redefine the helper.

**After editing `responses.go` or the monitor map, run `gofmt -w` on the touched file** (gofmt realigns struct-field and map-value columns).

**Per-task verification (run from `/home/robert/spacemolt/spacemolt`):**
```bash
go test ./pkg/game/ -run <TestName> -count=1 && go build ./... && golangci-lint run ./pkg/game/...
```
Expected: test PASS, build OK, `0 issues`.

**IMPORTANT — full-suite check:** unlike Phase 1, these edits only change struct fields, so they will NOT break other packages' interface mocks. Still, the FINAL verification (after all tasks) MUST run `go test ./...` (the whole suite), because `go build ./...` does not compile test files. (This is the gap that bit Phase 1.)

**Commit:** explicit `git add` of only the listed files (never `git add -A` — the tree has unrelated untracked files like `aa`, `b.json`). End every commit message with:
```
Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

### Task 1: Ack-frame fields (dock, jump, mine, self_destruct)

These actions are queued mutations whose server "ok" ack frame is a `PendingActionResponse{command, message, pending}`. `dock`/`jump`/`mine` map to rich structs that lack `command`/`pending`; `self_destruct` maps to the bare `MessageResponse`.

**Files:**
- Modify: `pkg/game/serverapi/responses.go` (add 2 fields each to `DockResponse`, `JumpResponse`, `MineResponse`)
- Modify: `pkg/game/client_api_monitor.go` (remap `self_destruct` → `PendingActionResponse`)
- Test: `pkg/game/drift_ackframe_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/drift_ackframe_test.go`:
```go
package game

import "testing"

func TestDriftAckFrameFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"dock":          {"command", "pending"},
		"jump":          {"command", "pending"},
		"mine":          {"command", "pending"},
		"self_destruct": {"command", "pending", "message"},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestDriftAckFrameFields -count=1`
Expected: FAIL — `command`/`pending` absent from DockResponse/JumpResponse/MineResponse; self_destruct maps to MessageResponse (only `message`).

- [ ] **Step 3: Add `command`/`pending` to the three structs**

In `pkg/game/serverapi/responses.go`, add these two fields to `DockResponse` (after the `Action` field):
```go
	Command string `json:"command,omitempty"`
	Pending bool   `json:"pending,omitempty"`
```
Add the identical two fields to `JumpResponse` and to `MineResponse` (after each struct's `Action` field).

- [ ] **Step 4: Remap self_destruct in the monitor map**

In `pkg/game/client_api_monitor.go`, find the `actionResponseTypes` entry:
```go
	"self_destruct": reflect.TypeOf(serverapi.MessageResponse{}),
```
and change it to:
```go
	"self_destruct": reflect.TypeOf(serverapi.PendingActionResponse{}),
```
Then run `gofmt -w pkg/game/serverapi/responses.go pkg/game/client_api_monitor.go`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/game/ -run TestDriftAckFrameFields -count=1 && go build ./... && golangci-lint run ./pkg/game/...`
Expected: PASS, build OK, `0 issues`.

- [ ] **Step 6: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/client_api_monitor.go pkg/game/drift_ackframe_test.go
git commit -m "fix(serverapi): add command/pending ack fields to dock/jump/mine; remap self_destruct to PendingActionResponse

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Flatten wrongly-nested structs (read_note, complete_mission)

The spec schemas for both are FLAT (`additionalProperties: false`), but our structs nest the data under a sub-object the server never sends nested (`ReadNoteResponse.Note`, `CompleteMissionResponse.Rewards`). Neither struct has any external consumers (verified), and the `Note` and `MissionRewards` types remain in use elsewhere (`GetNotesResponse.Notes`, the knowledge mission-catalog layer), so we keep those types and only change these two response structs.

**Files:**
- Modify: `pkg/game/serverapi/responses.go` (rewrite `ReadNoteResponse` and `CompleteMissionResponse`)
- Test: `pkg/game/drift_nesting_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/drift_nesting_test.go`:
```go
package game

import "testing"

func TestDriftNestingFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"read_note": {"note_id", "title", "content", "created_by", "created_at", "updated_at", "value"},
		"complete_mission": {
			"mission_id", "title", "chain_next", "credits_earned", "items_received",
			"skill_xp_gained", "community_contributed", "community_progress", "community_percent",
		},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestDriftNestingFields -count=1`
Expected: FAIL — `ReadNoteResponse` top-level tags are `{action, note}`; `CompleteMissionResponse` top-level tags are `{message, mission_id, title, rewards}`. The flat field names are absent.

- [ ] **Step 3: Rewrite `ReadNoteResponse` (flat)**

In `pkg/game/serverapi/responses.go`, replace the existing `ReadNoteResponse`:
```go
type ReadNoteResponse struct {
	Action string `json:"action,omitempty"`
	Note   Note   `json:"note"`
}
```
with the flat shape matching the spec (`additionalProperties:false`; note the spec uses `created_by`, NOT the `Note` type's `author_id`/`author_name`):
```go
// ReadNoteResponse is returned by read_note. The server sends note fields at
// the top level (not nested), so they are flattened here.
//   - read_note
type ReadNoteResponse struct {
	Action    string `json:"action,omitempty"`
	NoteID    string `json:"note_id"`
	Title     string `json:"title"`
	Content   string `json:"content,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
	Value     int    `json:"value,omitempty"`
}
```

- [ ] **Step 4: Rewrite `CompleteMissionResponse` (flat)**

In `pkg/game/serverapi/responses.go`, replace the existing `CompleteMissionResponse`:
```go
type CompleteMissionResponse struct {
	Message   string          `json:"message"`
	MissionID string          `json:"mission_id"`
	Title     string          `json:"title"`
	Rewards   *MissionRewards `json:"rewards,omitempty"`
}
```
with the flat shape matching the spec:
```go
// CompleteMissionResponse is returned by complete_mission. The server sends
// reward fields at the top level (not nested under "rewards").
//   - complete_mission
type CompleteMissionResponse struct {
	Message              string          `json:"message"`
	MissionID            string          `json:"mission_id"`
	Title                string          `json:"title"`
	ChainNext            string          `json:"chain_next,omitempty"`
	CreditsEarned        int64           `json:"credits_earned,omitempty"`
	ItemsReceived        json.RawMessage `json:"items_received,omitempty"`
	SkillXPGained        json.RawMessage `json:"skill_xp_gained,omitempty"`
	CommunityContributed json.RawMessage `json:"community_contributed,omitempty"`
	CommunityProgress    json.RawMessage `json:"community_progress,omitempty"`
	CommunityPercent     float64         `json:"community_percent,omitempty"`
}
```
Then run `gofmt -w pkg/game/serverapi/responses.go`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/game/ -run TestDriftNestingFields -count=1 && go build ./... && golangci-lint run ./pkg/game/...`
Expected: PASS, build OK, `0 issues`. (The `MissionRewards` and `Note` types are still referenced elsewhere, so no "unused" errors.)

- [ ] **Step 6: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/drift_nesting_test.go
git commit -m "fix(serverapi): flatten read_note and complete_mission to match flat server frames

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Trading & mission fields (estimate_purchase, sell, view_orders, buy_listed_ship, accept_mission)

Plain field additions.

**Files:**
- Modify: `pkg/game/serverapi/responses.go`
- Test: `pkg/game/drift_trading_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/drift_trading_test.go`:
```go
package game

import "testing"

func TestDriftTradingFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"estimate_purchase": {"sales_tax", "sales_tax_rate_bps", "subtotal"},
		"sell":              {"smuggling_level_up", "smuggling_xp"},
		"view_orders":       {"item_filter", "order_type", "search_term"},
		"buy_listed_ship":   {"old_ship_id"},
		"accept_mission":    {"expires_at", "template_id", "type"},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestDriftTradingFields -count=1`
Expected: FAIL — listed fields absent from the respective structs.

- [ ] **Step 3: Add the fields**

In `pkg/game/serverapi/responses.go`:

Add to `EstimatePurchaseResponse`:
```go
	SalesTax        int64 `json:"sales_tax,omitempty"`
	SalesTaxRateBps int   `json:"sales_tax_rate_bps,omitempty"`
	Subtotal        int64 `json:"subtotal,omitempty"`
```
Add to `SellResponse`:
```go
	SmugglingLevelUp bool  `json:"smuggling_level_up,omitempty"`
	SmugglingXP      int64 `json:"smuggling_xp,omitempty"`
```
Add to `ViewOrdersResponse`:
```go
	ItemFilter string `json:"item_filter,omitempty"`
	OrderType  string `json:"order_type,omitempty"`
	SearchTerm string `json:"search_term,omitempty"`
```
Add to `BuyListedShipResponse`:
```go
	OldShipID string `json:"old_ship_id,omitempty"`
```
Add to `AcceptMissionResponse`:
```go
	ExpiresAt  string `json:"expires_at,omitempty"`
	TemplateID string `json:"template_id,omitempty"`
	Type       string `json:"type,omitempty"`
```
Then run `gofmt -w pkg/game/serverapi/responses.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/game/ -run TestDriftTradingFields -count=1 && go build ./... && golangci-lint run ./pkg/game/...`
Expected: PASS, build OK, `0 issues`.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/drift_trading_test.go
git commit -m "fix(serverapi): add tax/smuggling/filter/ship/mission fields (estimate_purchase, sell, view_orders, buy_listed_ship, accept_mission)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Cargo, storage & fuel fields (get_cargo, get_base, view_faction_storage, view_storage, jettison, refuel)

Plain field additions. Arrays/objects use `json.RawMessage`.

**Files:**
- Modify: `pkg/game/serverapi/responses.go`
- Test: `pkg/game/drift_storage_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/drift_storage_test.go`:
```go
package game

import "testing"

func TestDriftStorageFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"get_cargo":            {"bay_capacity", "bay_used", "carried_ships"},
		"get_base":             {"faction_fuel_capacity", "faction_fuel_reserve", "fuel_price"},
		"view_faction_storage": {"faction_fuel_capacity", "faction_fuel_reserve", "hint"},
		"view_storage":         {"messages"},
		"jettison":             {"container_id"},
		"refuel": {
			"ally_faction_id", "ally_faction_tag", "ally_fuel", "faction_fuel", "fleet_id",
			"has_pump", "members", "rescue_completed", "rescue_reward", "tax_amount",
		},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestDriftStorageFields -count=1`
Expected: FAIL — listed fields absent.

- [ ] **Step 3: Add the fields**

In `pkg/game/serverapi/responses.go`:

Add to `GetCargoResponse`:
```go
	BayCapacity  int             `json:"bay_capacity,omitempty"`
	BayUsed      int             `json:"bay_used,omitempty"`
	CarriedShips json.RawMessage `json:"carried_ships,omitempty"`
```
Add to `GetBaseResponse`:
```go
	FactionFuelCapacity int   `json:"faction_fuel_capacity,omitempty"`
	FactionFuelReserve  int   `json:"faction_fuel_reserve,omitempty"`
	FuelPrice           int64 `json:"fuel_price,omitempty"`
```
Add to `ViewFactionStorageResponse`:
```go
	FactionFuelCapacity int    `json:"faction_fuel_capacity,omitempty"`
	FactionFuelReserve  int    `json:"faction_fuel_reserve,omitempty"`
	Hint                string `json:"hint,omitempty"`
```
Add to `ViewStorageResponse`:
```go
	Messages json.RawMessage `json:"messages,omitempty"`
```
Add to `JettisonResponse`:
```go
	ContainerID string `json:"container_id,omitempty"`
```
Add to `RefuelResponse`:
```go
	AllyFactionID   string          `json:"ally_faction_id,omitempty"`
	AllyFactionTag  string          `json:"ally_faction_tag,omitempty"`
	AllyFuel        int             `json:"ally_fuel,omitempty"`
	FactionFuel     int             `json:"faction_fuel,omitempty"`
	FleetID         string          `json:"fleet_id,omitempty"`
	HasPump         bool            `json:"has_pump,omitempty"`
	Members         json.RawMessage `json:"members,omitempty"`
	RescueCompleted bool            `json:"rescue_completed,omitempty"`
	RescueReward    int64           `json:"rescue_reward,omitempty"`
	TaxAmount       int64           `json:"tax_amount,omitempty"`
```
Then run `gofmt -w pkg/game/serverapi/responses.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/game/ -run TestDriftStorageFields -count=1 && go build ./... && golangci-lint run ./pkg/game/...`
Expected: PASS, build OK, `0 issues`.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/drift_storage_test.go
git commit -m "fix(serverapi): add cargo bay/faction-fuel/refuel/jettison/storage fields

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Info & query fields (catalog, chat, faction_info, get_action_log, get_nearby, get_version, survey_system, uninstall_mod)

Plain field additions. Arrays/objects use `json.RawMessage`.

**Files:**
- Modify: `pkg/game/serverapi/responses.go`
- Test: `pkg/game/drift_info_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/drift_info_test.go`:
```go
package game

import "testing"

func TestDriftInfoFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"catalog":        {"analysis", "passive_recipe_details", "recipes"},
		"chat":           {"channel"},
		"faction_info":   {"alliance_proposals", "facilities", "roles"},
		"get_action_log": {"faction_id", "page", "page_size", "total", "total_pages"},
		"get_nearby":     {"empire_npc_count", "empire_npcs", "offline_collapsed"},
		"get_version":    {"has_more", "search_term"},
		"survey_system":  {"anomaly_hint"},
		"uninstall_mod":  {"damaged"},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestDriftInfoFields -count=1`
Expected: FAIL — listed fields absent.

- [ ] **Step 3: Add the fields**

In `pkg/game/serverapi/responses.go`:

Add to `CatalogResponse`:
```go
	Analysis             json.RawMessage `json:"analysis,omitempty"`
	PassiveRecipeDetails json.RawMessage `json:"passive_recipe_details,omitempty"`
	Recipes              json.RawMessage `json:"recipes,omitempty"`
```
Add to `ChatResponse`:
```go
	Channel string `json:"channel,omitempty"`
```
Add to `FactionInfoResponse`:
```go
	AllianceProposals json.RawMessage `json:"alliance_proposals,omitempty"`
	Facilities        json.RawMessage `json:"facilities,omitempty"`
	Roles             json.RawMessage `json:"roles,omitempty"`
```
Add to `GetActionLogResponse`:
```go
	FactionID  string `json:"faction_id,omitempty"`
	Page       int    `json:"page,omitempty"`
	PageSize   int    `json:"page_size,omitempty"`
	Total      int    `json:"total,omitempty"`
	TotalPages int    `json:"total_pages,omitempty"`
```
Add to `GetNearbyResponse`:
```go
	EmpireNPCCount   int             `json:"empire_npc_count,omitempty"`
	EmpireNPCs       json.RawMessage `json:"empire_npcs,omitempty"`
	OfflineCollapsed int             `json:"offline_collapsed,omitempty"`
```
Add to `GetVersionResponse`:
```go
	HasMore    bool   `json:"has_more,omitempty"`
	SearchTerm string `json:"search_term,omitempty"`
```
Add to `SurveySystemResponse`:
```go
	AnomalyHint string `json:"anomaly_hint,omitempty"`
```
Add to `UninstallModResponse`:
```go
	Damaged bool `json:"damaged,omitempty"`
```
Then run `gofmt -w pkg/game/serverapi/responses.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/game/ -run TestDriftInfoFields -count=1 && go build ./... && golangci-lint run ./pkg/game/...`
Expected: PASS, build OK, `0 issues`.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/drift_info_test.go
git commit -m "fix(serverapi): add catalog/chat/faction_info/action_log/nearby/version/survey/uninstall fields

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Final verification (after all 5 tasks)

- [ ] **Full build + WHOLE test suite + lint:**

```bash
go build ./... && go test ./... -count=1 && golangci-lint run ./pkg/game/...
```
Expected: build OK, ALL tests PASS (run the whole `./...`, not just `pkg/game`), `0 issues`.

- [ ] **All 25 actions covered:** the five drift tests (`TestDriftAckFrameFields`, `TestDriftNestingFields`, `TestDriftTradingFields`, `TestDriftStorageFields`, `TestDriftInfoFields`) all pass, covering: dock, jump, mine, self_destruct, read_note, complete_mission, estimate_purchase, sell, view_orders, buy_listed_ship, accept_mission, get_cargo, get_base, view_faction_storage, view_storage, jettison, refuel, catalog, chat, faction_info, get_action_log, get_nearby, get_version, survey_system, uninstall_mod = 25. ✓

---

## Self-Review

**Spec coverage:** All 25 field-drift candidates from the audit (`docs/api-currentness-audit-2026-05-23.md`, Workstream 2) are addressed across Tasks 1-5: ack-frame (4), nesting flatten (2), trading/mission (5), cargo/storage/fuel (6), info/query (8) = 25. ✓

**Placeholder scan:** No TBD/TODO; every code step shows the exact fields and types; no "similar to Task N" references. ✓

**Type/name consistency:** Field names match the spec exactly (verified against `server_docs/openapi.json` for the two restructures — `created_by` not `author_id`/`author_name`; flat `credits_earned`/`items_received`/`skill_xp_gained`). Money/xp fields → `int64`; counts/fuel/pagination → `int`; arrays/objects → `json.RawMessage`. `self_destruct` uses the pre-existing `PendingActionResponse` (no new struct). `message`/`action` correctly excluded (envelope `commonOKFields`). ✓

**Risk check:** The two restructures (`read_note`, `complete_mission`) have no external consumers (verified via grep); `Note` and `MissionRewards` types are retained because other structs/layers still use them. The full-suite check is called out explicitly in Final Verification to avoid the Phase-1 mock-breakage class of miss (though field-only edits should not break mocks).

**Conditional fields note:** Several added fields (catalog `analysis`/`recipes`/`passive_recipe_details`, `view_orders` filter echoes, `get_version.search_term`, faction-fuel pairs, `get_cargo` bay fields, `faction_info` `facilities`/`alliance_proposals`) appear only in specific server modes, so happy-path cached samples don't show them; they are added on spec authority and are harmless when absent (`omitempty`). ✓
