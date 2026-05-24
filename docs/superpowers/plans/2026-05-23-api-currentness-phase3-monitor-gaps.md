# API Currentness — Phase 3: Close Monitor-Map Gaps Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Register all 55 remaining spec actions that the API monitor currently treats as "unhandled" by adding an `actionResponseTypes` entry for each, creating a `serverapi` response struct (modeled from the spec's `result` schema) where one does not already exist.

**Architecture:** These actions are already dispatched/handled elsewhere in the client; they simply lack a monitor mapping, so the monitor logs "unhandled action" and cannot validate their fields. For each: ensure a response struct exists (5 already do, 8 reuse the existing `MessageResponse`, 42 are new), then add `"<action>": reflect.TypeOf(serverapi.XxxResponse{})` to the `actionResponseTypes` map in `pkg/game/client_api_monitor.go`. No client methods or interface changes — these are response-shape registrations only. Struct top-level json tags must cover the spec `result` fields (the OK frame flattens `result` to the top level).

**Tech Stack:** Go 1.25, `golangci-lint` gate, table-driven tests in `package game` reusing the `assertActionFields` helper from Phase 1 (`pkg/game/drone_actions_test.go`).

---

## Conventions used in every task

**Field typing (matches Phases 1–2):** money/credit/fee/value fields → `int64`; counts/quantities/levels/pagination/indices → `int`; numbers → `float64`; booleans → `bool`; strings → `string`; arrays and nested objects → `json.RawMessage`. `responses.go` already imports `encoding/json`. The monitor only checks top-level json tag NAMES, so types are for correctness/clarity, not validation.

**Envelope fields:** `message`, `action`, `player`, `ship`, `modules` are in `commonOKFields` and need not be in structs — but including `message`/`action` where the spec's result lists them is harmless and we keep them for fidelity.

**Struct names:** `PascalCase(action) + "Response"` (e.g., `attack` → `AttackResponse`). All 42 new names were verified not to exist yet. If a subagent finds a name already taken, STOP and report.

**Test pattern (per task, own file):** reuse the shared `assertActionFields(t, map[string][]string{...})` helper (do NOT redefine it). For struct-bearing actions, assert the distinctive (non-envelope) fields. Task 1 (mapping-only) uses a registration-only check shown inline.

**After editing `responses.go` or the monitor map, run `gofmt -w` on the touched file(s).**

**Per-task verification (from `/home/robert/spacemolt/spacemolt`):**
```bash
go test ./pkg/game/ -run <TestName> -count=1 && go build ./... && golangci-lint run ./pkg/game/...
```
Expected: test PASS, build OK, `0 issues`.

**Final verification (after all tasks): run the WHOLE suite** `go test ./...` (not just `pkg/game`), since `go build` skips test files.

**Commit:** explicit `git add` of only the listed files (never `git add -A`). End every commit message with:
```
Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

### Task 1: Mapping-only entries (5 existing structs + 8 MessageResponse)

No new structs. Add 13 entries to `actionResponseTypes`.

**Files:**
- Modify: `pkg/game/client_api_monitor.go`
- Test: `pkg/game/gap_mapping_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/gap_mapping_test.go`:
```go
package game

import "testing"

func TestGapMappingRegistered(t *testing.T) {
	actions := []string{
		// existing structs
		"faction_list_missions", "faction_rooms", "fleet", "login", "register",
		// reused MessageResponse (message-only or empty result)
		"claim", "leave_faction", "logout", "trade_cancel", "trade_decline",
		"faction_deposit_items", "faction_withdraw_items", "session",
	}
	for _, a := range actions {
		if _, ok := actionResponseTypes[a]; !ok {
			t.Errorf("action %q not registered in actionResponseTypes", a)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestGapMappingRegistered -count=1`
Expected: FAIL — all 13 unregistered.

- [ ] **Step 3: Add the 13 monitor entries**

In `pkg/game/client_api_monitor.go`, add to the `actionResponseTypes` map:
```go
	// Existing structs, previously unmapped
	"faction_list_missions": reflect.TypeOf(serverapi.FactionListMissionsResponse{}),
	"faction_rooms":         reflect.TypeOf(serverapi.FactionRoomsResponse{}),
	"fleet":                 reflect.TypeOf(serverapi.FleetResponse{}),
	"login":                 reflect.TypeOf(serverapi.LoginResponse{}),
	"register":              reflect.TypeOf(serverapi.RegisterResponse{}),
	// Message-only / empty-result actions reuse MessageResponse
	"claim":                  reflect.TypeOf(serverapi.MessageResponse{}),
	"leave_faction":          reflect.TypeOf(serverapi.MessageResponse{}),
	"logout":                 reflect.TypeOf(serverapi.MessageResponse{}),
	"trade_cancel":           reflect.TypeOf(serverapi.MessageResponse{}),
	"trade_decline":          reflect.TypeOf(serverapi.MessageResponse{}),
	"faction_deposit_items":  reflect.TypeOf(serverapi.MessageResponse{}),
	"faction_withdraw_items": reflect.TypeOf(serverapi.MessageResponse{}),
	"session":                reflect.TypeOf(serverapi.MessageResponse{}),
```
Then `gofmt -w pkg/game/client_api_monitor.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/game/ -run TestGapMappingRegistered -count=1 && go build ./... && golangci-lint run ./pkg/game/...`
Expected: PASS, build OK, `0 issues`.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/client_api_monitor.go pkg/game/gap_mapping_test.go
git commit -m "feat(game): register 13 monitor gaps (5 existing structs + 8 MessageResponse)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Missions & captain's log structs (6)

`abandon_mission`, `decline_mission`, `view_completed_mission`, `captains_log_add`, `captains_log_get`, `captains_log_list`.

**Files:**
- Modify: `pkg/game/serverapi/responses.go`, `pkg/game/client_api_monitor.go`
- Test: `pkg/game/gap_missions_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/gap_missions_test.go`:
```go
package game

import "testing"

func TestGapMissionFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"abandon_mission":        {"mission_id", "title"},
		"decline_mission":        {"giver", "template_id", "title"},
		"view_completed_mission": {"template_id", "title", "type", "chain_next", "completion_time", "difficulty", "objectives", "rewards", "repeatable", "giver", "dialog", "description"},
		"captains_log_add":       {"created_at", "index"},
		"captains_log_get":       {"created_at", "entry", "index"},
		"captains_log_list":      {"entry", "has_next", "has_prev", "index", "max_entries", "total_count"},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestGapMissionFields -count=1`
Expected: FAIL — actions not registered.

- [ ] **Step 3: Add the structs**

Append to `pkg/game/serverapi/responses.go`:
```go
// AbandonMissionResponse — abandon_mission
type AbandonMissionResponse struct {
	Message   string `json:"message"`
	MissionID string `json:"mission_id"`
	Title     string `json:"title"`
}

// DeclineMissionResponse — decline_mission
type DeclineMissionResponse struct {
	Message    string          `json:"message"`
	Giver      json.RawMessage `json:"giver,omitempty"`
	TemplateID string          `json:"template_id"`
	Title      string          `json:"title"`
}

// ViewCompletedMissionResponse — view_completed_mission
type ViewCompletedMissionResponse struct {
	TemplateID     string          `json:"template_id"`
	Title          string          `json:"title"`
	Type           string          `json:"type,omitempty"`
	ChainNext      string          `json:"chain_next,omitempty"`
	CompletionTime string          `json:"completion_time,omitempty"`
	Difficulty     int             `json:"difficulty,omitempty"`
	Objectives     json.RawMessage `json:"objectives,omitempty"`
	Rewards        json.RawMessage `json:"rewards,omitempty"`
	Repeatable     bool            `json:"repeatable,omitempty"`
	Giver          json.RawMessage `json:"giver,omitempty"`
	Dialog         json.RawMessage `json:"dialog,omitempty"`
	Description    string          `json:"description,omitempty"`
}

// CaptainsLogAddResponse — captains_log_add
type CaptainsLogAddResponse struct {
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
	Index     int    `json:"index"`
}

// CaptainsLogGetResponse — captains_log_get
type CaptainsLogGetResponse struct {
	Entry     string `json:"entry"`
	CreatedAt string `json:"created_at"`
	Index     int    `json:"index"`
}

// CaptainsLogListResponse — captains_log_list
type CaptainsLogListResponse struct {
	Entry      json.RawMessage `json:"entry,omitempty"`
	HasNext    bool            `json:"has_next"`
	HasPrev    bool            `json:"has_prev"`
	Index      int             `json:"index"`
	MaxEntries int             `json:"max_entries"`
	TotalCount int             `json:"total_count"`
}
```

- [ ] **Step 4: Add the monitor entries**

In `pkg/game/client_api_monitor.go`:
```go
	"abandon_mission":        reflect.TypeOf(serverapi.AbandonMissionResponse{}),
	"decline_mission":        reflect.TypeOf(serverapi.DeclineMissionResponse{}),
	"view_completed_mission": reflect.TypeOf(serverapi.ViewCompletedMissionResponse{}),
	"captains_log_add":       reflect.TypeOf(serverapi.CaptainsLogAddResponse{}),
	"captains_log_get":       reflect.TypeOf(serverapi.CaptainsLogGetResponse{}),
	"captains_log_list":      reflect.TypeOf(serverapi.CaptainsLogListResponse{}),
```
Then `gofmt -w pkg/game/serverapi/responses.go pkg/game/client_api_monitor.go`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/game/ -run TestGapMissionFields -count=1 && go build ./... && golangci-lint run ./pkg/game/...`
Expected: PASS, build OK, `0 issues`.

- [ ] **Step 6: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/client_api_monitor.go pkg/game/gap_missions_test.go
git commit -m "feat(game): add monitor structs for mission/captains-log actions

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Ship, combat & wreck structs (6)

`attack`, `name_ship`, `release_tow`, `repair_module`, `scrap_wreck`, `cancel_ship_listing`.

**Files:**
- Modify: `pkg/game/serverapi/responses.go`, `pkg/game/client_api_monitor.go`
- Test: `pkg/game/gap_ship_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/gap_ship_test.go`:
```go
package game

import "testing"

func TestGapShipFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"attack":              {"command", "pending"},
		"name_ship":           {"ship_id", "ship_name"},
		"release_tow":         {"wreck_id"},
		"repair_module":       {"module_id", "repair_amount", "wear_after", "wear_before", "wear_status", "xp_gained"},
		"scrap_wreck":         {"materials", "ship_class", "stored_at", "total_value", "wreck_id"},
		"cancel_ship_listing": {"class_id", "ship_id"},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestGapShipFields -count=1`
Expected: FAIL — actions not registered.

- [ ] **Step 3: Add the structs**

Append to `pkg/game/serverapi/responses.go`:
```go
// AttackResponse — attack (ack frame)
type AttackResponse struct {
	Command string `json:"command,omitempty"`
	Message string `json:"message"`
	Pending bool   `json:"pending,omitempty"`
}

// NameShipResponse — name_ship
type NameShipResponse struct {
	Message  string `json:"message"`
	ShipID   string `json:"ship_id"`
	ShipName string `json:"ship_name"`
}

// ReleaseTowResponse — release_tow
type ReleaseTowResponse struct {
	Action  string `json:"action,omitempty"`
	Message string `json:"message"`
	WreckID string `json:"wreck_id"`
}

// RepairModuleResponse — repair_module
type RepairModuleResponse struct {
	Message      string          `json:"message"`
	ModuleID     string          `json:"module_id"`
	RepairAmount float64         `json:"repair_amount,omitempty"`
	WearAfter    float64         `json:"wear_after,omitempty"`
	WearBefore   float64         `json:"wear_before,omitempty"`
	WearStatus   string          `json:"wear_status,omitempty"`
	XPGained     json.RawMessage `json:"xp_gained,omitempty"`
}

// ScrapWreckResponse — scrap_wreck
type ScrapWreckResponse struct {
	Action     string          `json:"action,omitempty"`
	Message    string          `json:"message"`
	Materials  json.RawMessage `json:"materials,omitempty"`
	ShipClass  string          `json:"ship_class,omitempty"`
	StoredAt   string          `json:"stored_at,omitempty"`
	TotalValue int64           `json:"total_value,omitempty"`
	WreckID    string          `json:"wreck_id"`
}

// CancelShipListingResponse — cancel_ship_listing
type CancelShipListingResponse struct {
	ClassID string `json:"class_id"`
	Message string `json:"message"`
	ShipID  string `json:"ship_id"`
}
```

- [ ] **Step 4: Add the monitor entries**

In `pkg/game/client_api_monitor.go`:
```go
	"attack":              reflect.TypeOf(serverapi.AttackResponse{}),
	"name_ship":           reflect.TypeOf(serverapi.NameShipResponse{}),
	"release_tow":         reflect.TypeOf(serverapi.ReleaseTowResponse{}),
	"repair_module":       reflect.TypeOf(serverapi.RepairModuleResponse{}),
	"scrap_wreck":         reflect.TypeOf(serverapi.ScrapWreckResponse{}),
	"cancel_ship_listing": reflect.TypeOf(serverapi.CancelShipListingResponse{}),
```
Then `gofmt -w pkg/game/serverapi/responses.go pkg/game/client_api_monitor.go`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/game/ -run TestGapShipFields -count=1 && go build ./... && golangci-lint run ./pkg/game/...`
Expected: PASS, build OK, `0 issues`.

- [ ] **Step 6: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/client_api_monitor.go pkg/game/gap_ship_test.go
git commit -m "feat(game): add monitor structs for ship/combat/wreck actions

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Commission, forum & agents structs (7)

`cancel_commission`, `claim_commission`, `supply_commission`, `forum_delete_reply`, `forum_delete_thread`, `forum_upvote`, `get_system_agents`.

**Files:**
- Modify: `pkg/game/serverapi/responses.go`, `pkg/game/client_api_monitor.go`
- Test: `pkg/game/gap_commission_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/gap_commission_test.go`:
```go
package game

import "testing"

func TestGapCommissionFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"cancel_commission":   {"credits_total", "materials_note", "materials_returned", "refund"},
		"claim_commission":    {"credits_left", "new_ship_id", "old_ship_id", "ship_class"},
		"supply_commission":   {"all_sourced", "commission_id", "commission_status", "credits", "item_id", "item_name", "materials", "supplied"},
		"forum_delete_reply":  {"reply_id"},
		"forum_delete_thread": {"thread_id"},
		"forum_upvote":        {"reply_id", "thread_id"},
		"get_system_agents":   {"agents", "count", "offline_collapsed", "system_id"},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestGapCommissionFields -count=1`
Expected: FAIL — actions not registered.

- [ ] **Step 3: Add the structs**

Append to `pkg/game/serverapi/responses.go`:
```go
// CancelCommissionResponse — cancel_commission
type CancelCommissionResponse struct {
	Message           string          `json:"message"`
	CreditsTotal      int64           `json:"credits_total,omitempty"`
	MaterialsNote     string          `json:"materials_note,omitempty"`
	MaterialsReturned json.RawMessage `json:"materials_returned,omitempty"`
	Refund            int64           `json:"refund,omitempty"`
}

// ClaimCommissionResponse — claim_commission
type ClaimCommissionResponse struct {
	Message     string `json:"message"`
	CreditsLeft int64  `json:"credits_left,omitempty"`
	NewShipID   string `json:"new_ship_id,omitempty"`
	OldShipID   string `json:"old_ship_id,omitempty"`
	ShipClass   string `json:"ship_class,omitempty"`
}

// SupplyCommissionResponse — supply_commission
type SupplyCommissionResponse struct {
	Message          string          `json:"message"`
	AllSourced       bool            `json:"all_sourced,omitempty"`
	CommissionID     string          `json:"commission_id"`
	CommissionStatus string          `json:"commission_status,omitempty"`
	Credits          int64           `json:"credits,omitempty"`
	ItemID           string          `json:"item_id,omitempty"`
	ItemName         string          `json:"item_name,omitempty"`
	Materials        json.RawMessage `json:"materials,omitempty"`
	Supplied         int             `json:"supplied,omitempty"`
}

// ForumDeleteReplyResponse — forum_delete_reply
type ForumDeleteReplyResponse struct {
	Message string `json:"message"`
	ReplyID string `json:"reply_id"`
}

// ForumDeleteThreadResponse — forum_delete_thread
type ForumDeleteThreadResponse struct {
	Message  string `json:"message"`
	ThreadID string `json:"thread_id"`
}

// ForumUpvoteResponse — forum_upvote
type ForumUpvoteResponse struct {
	Message  string `json:"message"`
	ReplyID  string `json:"reply_id,omitempty"`
	ThreadID string `json:"thread_id,omitempty"`
}

// GetSystemAgentsResponse — get_system_agents
type GetSystemAgentsResponse struct {
	Agents           json.RawMessage `json:"agents,omitempty"`
	Count            int             `json:"count,omitempty"`
	OfflineCollapsed int             `json:"offline_collapsed,omitempty"`
	SystemID         string          `json:"system_id,omitempty"`
}
```

- [ ] **Step 4: Add the monitor entries**

In `pkg/game/client_api_monitor.go`:
```go
	"cancel_commission":   reflect.TypeOf(serverapi.CancelCommissionResponse{}),
	"claim_commission":    reflect.TypeOf(serverapi.ClaimCommissionResponse{}),
	"supply_commission":   reflect.TypeOf(serverapi.SupplyCommissionResponse{}),
	"forum_delete_reply":  reflect.TypeOf(serverapi.ForumDeleteReplyResponse{}),
	"forum_delete_thread": reflect.TypeOf(serverapi.ForumDeleteThreadResponse{}),
	"forum_upvote":        reflect.TypeOf(serverapi.ForumUpvoteResponse{}),
	"get_system_agents":   reflect.TypeOf(serverapi.GetSystemAgentsResponse{}),
```
Then `gofmt -w pkg/game/serverapi/responses.go pkg/game/client_api_monitor.go`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/game/ -run TestGapCommissionFields -count=1 && go build ./... && golangci-lint run ./pkg/game/...`
Expected: PASS, build OK, `0 issues`.

- [ ] **Step 6: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/client_api_monitor.go pkg/game/gap_commission_test.go
git commit -m "feat(game): add monitor structs for commission/forum/agents actions

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Faction roles, rooms & membership structs (10)

`join_faction`, `faction_decline_invite`, `faction_get_invites`, `faction_create_role`, `faction_delete_role`, `faction_edit_role`, `faction_edit`, `faction_delete_room`, `faction_visit_room`, `faction_write_room`.

**Files:**
- Modify: `pkg/game/serverapi/responses.go`, `pkg/game/client_api_monitor.go`
- Test: `pkg/game/gap_faction_roles_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/gap_faction_roles_test.go`:
```go
package game

import "testing"

func TestGapFactionRolesFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"join_faction":           {"faction", "faction_id"},
		"faction_decline_invite": {"faction_id"},
		"faction_get_invites":    {"invites"},
		"faction_create_role":    {"name", "priority", "role_id"},
		"faction_delete_role":    {"reassigned_count", "role_id"},
		"faction_edit_role":      {"role_id", "updates"},
		"faction_edit":           {"hint", "updates"},
		"faction_delete_room":    {"room_id"},
		"faction_visit_room":     {"access", "author", "created_at", "description", "name", "room_id", "updated_at"},
		"faction_write_room":     {"access", "faction", "hint", "name", "room_id"},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestGapFactionRolesFields -count=1`
Expected: FAIL — actions not registered.

- [ ] **Step 3: Add the structs**

Append to `pkg/game/serverapi/responses.go`:
```go
// JoinFactionResponse — join_faction
type JoinFactionResponse struct {
	Faction   string `json:"faction"`
	FactionID string `json:"faction_id"`
	Message   string `json:"message"`
}

// FactionDeclineInviteResponse — faction_decline_invite
type FactionDeclineInviteResponse struct {
	FactionID string `json:"faction_id"`
	Message   string `json:"message"`
}

// FactionGetInvitesResponse — faction_get_invites
type FactionGetInvitesResponse struct {
	Invites json.RawMessage `json:"invites,omitempty"`
}

// FactionCreateRoleResponse — faction_create_role
type FactionCreateRoleResponse struct {
	Message  string `json:"message"`
	Name     string `json:"name"`
	Priority int    `json:"priority,omitempty"`
	RoleID   string `json:"role_id"`
}

// FactionDeleteRoleResponse — faction_delete_role
type FactionDeleteRoleResponse struct {
	Message         string `json:"message"`
	ReassignedCount int    `json:"reassigned_count,omitempty"`
	RoleID          string `json:"role_id"`
}

// FactionEditRoleResponse — faction_edit_role
type FactionEditRoleResponse struct {
	Message string          `json:"message"`
	RoleID  string          `json:"role_id"`
	Updates json.RawMessage `json:"updates,omitempty"`
}

// FactionEditResponse — faction_edit
type FactionEditResponse struct {
	Hint    string          `json:"hint,omitempty"`
	Message string          `json:"message"`
	Updates json.RawMessage `json:"updates,omitempty"`
}

// FactionDeleteRoomResponse — faction_delete_room
type FactionDeleteRoomResponse struct {
	Action  string `json:"action,omitempty"`
	Message string `json:"message"`
	RoomID  string `json:"room_id"`
}

// FactionVisitRoomResponse — faction_visit_room
type FactionVisitRoomResponse struct {
	Action      string `json:"action,omitempty"`
	Access      string `json:"access,omitempty"`
	Author      string `json:"author,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	Description string `json:"description,omitempty"`
	Name        string `json:"name,omitempty"`
	RoomID      string `json:"room_id"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// FactionWriteRoomResponse — faction_write_room
type FactionWriteRoomResponse struct {
	Action  string `json:"action,omitempty"`
	Access  string `json:"access,omitempty"`
	Faction string `json:"faction,omitempty"`
	Hint    string `json:"hint,omitempty"`
	Message string `json:"message"`
	Name    string `json:"name,omitempty"`
	RoomID  string `json:"room_id"`
}
```

- [ ] **Step 4: Add the monitor entries**

In `pkg/game/client_api_monitor.go`:
```go
	"join_faction":           reflect.TypeOf(serverapi.JoinFactionResponse{}),
	"faction_decline_invite": reflect.TypeOf(serverapi.FactionDeclineInviteResponse{}),
	"faction_get_invites":    reflect.TypeOf(serverapi.FactionGetInvitesResponse{}),
	"faction_create_role":    reflect.TypeOf(serverapi.FactionCreateRoleResponse{}),
	"faction_delete_role":    reflect.TypeOf(serverapi.FactionDeleteRoleResponse{}),
	"faction_edit_role":      reflect.TypeOf(serverapi.FactionEditRoleResponse{}),
	"faction_edit":           reflect.TypeOf(serverapi.FactionEditResponse{}),
	"faction_delete_room":    reflect.TypeOf(serverapi.FactionDeleteRoomResponse{}),
	"faction_visit_room":     reflect.TypeOf(serverapi.FactionVisitRoomResponse{}),
	"faction_write_room":     reflect.TypeOf(serverapi.FactionWriteRoomResponse{}),
```
Then `gofmt -w pkg/game/serverapi/responses.go pkg/game/client_api_monitor.go`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/game/ -run TestGapFactionRolesFields -count=1 && go build ./... && golangci-lint run ./pkg/game/...`
Expected: PASS, build OK, `0 issues`.

- [ ] **Step 6: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/client_api_monitor.go pkg/game/gap_faction_roles_test.go
git commit -m "feat(game): add monitor structs for faction role/room/membership actions

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Faction allies, enemies & intel structs (9)

`faction_accept_ally`, `faction_propose_ally`, `faction_remove_ally`, `faction_set_enemy`, `faction_intel_status`, `faction_query_intel`, `faction_query_trade_intel`, `faction_submit_trade_intel`, `faction_trade_intel_status`.

**Files:**
- Modify: `pkg/game/serverapi/responses.go`, `pkg/game/client_api_monitor.go`
- Test: `pkg/game/gap_faction_intel_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/gap_faction_intel_test.go`:
```go
package game

import "testing"

func TestGapFactionIntelFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"faction_accept_ally":        {"target_faction_id", "target_name"},
		"faction_propose_ally":       {"target_faction_id", "target_name"},
		"faction_remove_ally":        {"removed", "target_faction_id", "target_name"},
		"faction_set_enemy":          {"target_faction_id", "target_name"},
		"faction_intel_status":       {"intel_level", "reports_24h", "top_contributors", "total_reports", "unique_players", "unique_systems"},
		"faction_query_intel":        {"count", "entries", "intel_level", "limit", "offset", "total"},
		"faction_query_trade_intel":  {"entries", "intel_level", "limit", "offset", "showing", "total"},
		"faction_submit_trade_intel": {"stations_updated", "status"},
		"faction_trade_intel_status": {"intel_level", "reports_24h", "top_contributors", "total_reports", "unique_items", "unique_stations"},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestGapFactionIntelFields -count=1`
Expected: FAIL — actions not registered.

- [ ] **Step 3: Add the structs**

Append to `pkg/game/serverapi/responses.go`:
```go
// FactionAcceptAllyResponse — faction_accept_ally
type FactionAcceptAllyResponse struct {
	Message         string `json:"message"`
	TargetFactionID string `json:"target_faction_id"`
	TargetName      string `json:"target_name"`
}

// FactionProposeAllyResponse — faction_propose_ally
type FactionProposeAllyResponse struct {
	Message         string `json:"message"`
	TargetFactionID string `json:"target_faction_id"`
	TargetName      string `json:"target_name"`
}

// FactionRemoveAllyResponse — faction_remove_ally
type FactionRemoveAllyResponse struct {
	Message         string `json:"message"`
	Removed         bool   `json:"removed,omitempty"`
	TargetFactionID string `json:"target_faction_id"`
	TargetName      string `json:"target_name"`
}

// FactionSetEnemyResponse — faction_set_enemy
type FactionSetEnemyResponse struct {
	Message         string `json:"message"`
	TargetFactionID string `json:"target_faction_id"`
	TargetName      string `json:"target_name"`
}

// FactionIntelStatusResponse — faction_intel_status
type FactionIntelStatusResponse struct {
	IntelLevel      int             `json:"intel_level,omitempty"`
	Reports24h      int             `json:"reports_24h,omitempty"`
	TopContributors json.RawMessage `json:"top_contributors,omitempty"`
	TotalReports    int             `json:"total_reports,omitempty"`
	UniquePlayers   int             `json:"unique_players,omitempty"`
	UniqueSystems   int             `json:"unique_systems,omitempty"`
}

// FactionQueryIntelResponse — faction_query_intel
type FactionQueryIntelResponse struct {
	Message    string          `json:"message,omitempty"`
	Count      int             `json:"count,omitempty"`
	Entries    json.RawMessage `json:"entries,omitempty"`
	IntelLevel int             `json:"intel_level,omitempty"`
	Limit      int             `json:"limit,omitempty"`
	Offset     int             `json:"offset,omitempty"`
	Total      int             `json:"total,omitempty"`
}

// FactionQueryTradeIntelResponse — faction_query_trade_intel
type FactionQueryTradeIntelResponse struct {
	Entries    json.RawMessage `json:"entries,omitempty"`
	IntelLevel int             `json:"intel_level,omitempty"`
	Limit      int             `json:"limit,omitempty"`
	Offset     int             `json:"offset,omitempty"`
	Showing    int             `json:"showing,omitempty"`
	Total      int             `json:"total,omitempty"`
}

// FactionSubmitTradeIntelResponse — faction_submit_trade_intel
type FactionSubmitTradeIntelResponse struct {
	Message         string `json:"message"`
	StationsUpdated int    `json:"stations_updated,omitempty"`
	Status          string `json:"status,omitempty"`
}

// FactionTradeIntelStatusResponse — faction_trade_intel_status
type FactionTradeIntelStatusResponse struct {
	IntelLevel      int             `json:"intel_level,omitempty"`
	Reports24h      int             `json:"reports_24h,omitempty"`
	TopContributors json.RawMessage `json:"top_contributors,omitempty"`
	TotalReports    int             `json:"total_reports,omitempty"`
	UniqueItems     int             `json:"unique_items,omitempty"`
	UniqueStations  int             `json:"unique_stations,omitempty"`
}
```

- [ ] **Step 4: Add the monitor entries**

In `pkg/game/client_api_monitor.go`:
```go
	"faction_accept_ally":        reflect.TypeOf(serverapi.FactionAcceptAllyResponse{}),
	"faction_propose_ally":       reflect.TypeOf(serverapi.FactionProposeAllyResponse{}),
	"faction_remove_ally":        reflect.TypeOf(serverapi.FactionRemoveAllyResponse{}),
	"faction_set_enemy":          reflect.TypeOf(serverapi.FactionSetEnemyResponse{}),
	"faction_intel_status":       reflect.TypeOf(serverapi.FactionIntelStatusResponse{}),
	"faction_query_intel":        reflect.TypeOf(serverapi.FactionQueryIntelResponse{}),
	"faction_query_trade_intel":  reflect.TypeOf(serverapi.FactionQueryTradeIntelResponse{}),
	"faction_submit_trade_intel": reflect.TypeOf(serverapi.FactionSubmitTradeIntelResponse{}),
	"faction_trade_intel_status": reflect.TypeOf(serverapi.FactionTradeIntelStatusResponse{}),
```
Then `gofmt -w pkg/game/serverapi/responses.go pkg/game/client_api_monitor.go`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/game/ -run TestGapFactionIntelFields -count=1 && go build ./... && golangci-lint run ./pkg/game/...`
Expected: PASS, build OK, `0 issues`.

- [ ] **Step 6: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/client_api_monitor.go pkg/game/gap_faction_intel_test.go
git commit -m "feat(game): add monitor structs for faction ally/enemy/intel actions

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Faction orders & missions structs (4)

`faction_create_buy_order`, `faction_create_sell_order`, `faction_post_mission`, `faction_cancel_mission`.

**Files:**
- Modify: `pkg/game/serverapi/responses.go`, `pkg/game/client_api_monitor.go`
- Test: `pkg/game/gap_faction_orders_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/gap_faction_orders_test.go`:
```go
package game

import "testing"

func TestGapFactionOrdersFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"faction_create_buy_order":  {"consolidated", "faction_id", "faction_tag", "item", "item_id", "listing_fee", "order_id", "price_each", "quantity", "total_escrowed"},
		"faction_create_sell_order": {"consolidated", "faction_id", "faction_tag", "item", "item_id", "listing_fee", "order_id", "price_each", "quantity"},
		"faction_post_mission":      {"escrowed", "status", "template_id", "title"},
		"faction_cancel_mission":    {"status"},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestGapFactionOrdersFields -count=1`
Expected: FAIL — actions not registered.

- [ ] **Step 3: Add the structs**

Append to `pkg/game/serverapi/responses.go`:
```go
// FactionCreateBuyOrderResponse — faction_create_buy_order
type FactionCreateBuyOrderResponse struct {
	Action        string `json:"action,omitempty"`
	Consolidated  bool   `json:"consolidated,omitempty"`
	FactionID     string `json:"faction_id,omitempty"`
	FactionTag    string `json:"faction_tag,omitempty"`
	Item          string `json:"item,omitempty"`
	ItemID        string `json:"item_id,omitempty"`
	ListingFee    int64  `json:"listing_fee,omitempty"`
	Message       string `json:"message"`
	OrderID       string `json:"order_id,omitempty"`
	PriceEach     int64  `json:"price_each,omitempty"`
	Quantity      int    `json:"quantity,omitempty"`
	TotalEscrowed int64  `json:"total_escrowed,omitempty"`
}

// FactionCreateSellOrderResponse — faction_create_sell_order
type FactionCreateSellOrderResponse struct {
	Action       string `json:"action,omitempty"`
	Consolidated bool   `json:"consolidated,omitempty"`
	FactionID    string `json:"faction_id,omitempty"`
	FactionTag   string `json:"faction_tag,omitempty"`
	Item         string `json:"item,omitempty"`
	ItemID       string `json:"item_id,omitempty"`
	ListingFee   int64  `json:"listing_fee,omitempty"`
	Message      string `json:"message"`
	OrderID      string `json:"order_id,omitempty"`
	PriceEach    int64  `json:"price_each,omitempty"`
	Quantity     int    `json:"quantity,omitempty"`
}

// FactionPostMissionResponse — faction_post_mission
type FactionPostMissionResponse struct {
	Escrowed   json.RawMessage `json:"escrowed,omitempty"`
	Message    string          `json:"message"`
	Status     string          `json:"status,omitempty"`
	TemplateID string          `json:"template_id"`
	Title      string          `json:"title"`
}

// FactionCancelMissionResponse — faction_cancel_mission
type FactionCancelMissionResponse struct {
	Message string `json:"message"`
	Status  string `json:"status,omitempty"`
}
```

- [ ] **Step 4: Add the monitor entries**

In `pkg/game/client_api_monitor.go`:
```go
	"faction_create_buy_order":  reflect.TypeOf(serverapi.FactionCreateBuyOrderResponse{}),
	"faction_create_sell_order": reflect.TypeOf(serverapi.FactionCreateSellOrderResponse{}),
	"faction_post_mission":      reflect.TypeOf(serverapi.FactionPostMissionResponse{}),
	"faction_cancel_mission":    reflect.TypeOf(serverapi.FactionCancelMissionResponse{}),
```
Then `gofmt -w pkg/game/serverapi/responses.go pkg/game/client_api_monitor.go`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/game/ -run TestGapFactionOrdersFields -count=1 && go build ./... && golangci-lint run ./pkg/game/...`
Expected: PASS, build OK, `0 issues`.

- [ ] **Step 6: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/client_api_monitor.go pkg/game/gap_faction_orders_test.go
git commit -m "feat(game): add monitor structs for faction order/mission actions

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Final verification (after all 7 tasks)

- [ ] **Full build + WHOLE test suite + lint:**

```bash
go build ./... && go test ./... -count=1 && golangci-lint run ./pkg/game/...
```
Expected: build OK, ALL tests PASS, `0 issues`.

- [ ] **Zero remaining monitor gaps:** re-run the gap computation and confirm 0 unmapped spec actions:
```bash
python3 - <<'EOF'
import json, re
spec=json.load(open('server_docs/openapi.json'))
actions={p.lstrip('/') for p,ops in spec['paths'].items() if any(isinstance(o,dict) for o in ops.values())}
mapped=set(re.findall(r'"([a-z_0-9]+)":\s*reflect\.TypeOf', open('pkg/game/client_api_monitor.go').read()))
gap=sorted(a for a in actions if a not in mapped)
print("remaining gap:", len(gap), gap)
EOF
```
Expected: `remaining gap: 0 []`.

---

## Self-Review

**Spec coverage:** All 55 gap actions are registered: Task 1 maps 13 (5 existing structs + 8 MessageResponse); Tasks 2–7 add 42 new structs (6+6+7+10+9+4 = 42) and their entries. 13 + 42 = 55. ✓

**Placeholder scan:** No TBD/TODO; every struct is shown in full; no "similar to Task N". ✓

**Type/name consistency:** Struct names are `PascalCase(action)+"Response"`, all verified absent today. Money/fee/value fields → `int64`; counts/levels/pagination → `int`; numbers → `float64`; arrays/objects → `json.RawMessage`. Each monitor entry references a struct defined in the same task (or pre-existing for Task 1). Field json tags come directly from the spec `result` schemas extracted on 2026-05-23. ✓

**Empty-result / message-only actions:** `claim`, `leave_faction`, `logout`, `trade_cancel`, `trade_decline` (result = {message}) and `faction_deposit_items`, `faction_withdraw_items`, `session` (empty result schema) correctly reuse `MessageResponse` rather than creating trivial/empty structs. If any of these later proves to carry action-specific fields, the monitor will log them as unknown — acceptable, and a signal to add a struct then. ✓

**Risk:** Registration-only changes (plus new, consumer-free structs) cannot break other packages' interface mocks (no interface change). The final whole-suite run is still required per the Phase-1 lesson. The `ack-frame` field pattern (`command`/`pending`) is applied to `attack` consistent with Phase 2's dock/jump/mine. ✓
