# API Currentness — Phase 1: 19 New Actions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add full client support for 19 server actions that exist in `server_docs/openapi.json` (v0.322.1) but have no client method, response struct, or API-monitor entry.

**Architecture:** Each action gets five touch-points to stay consistent with the existing pattern: (1) a `serverapi` response struct modeling the OK-frame top-level fields, (2) a `GameClient` interface method, (3) a WebSocket `*Client` method in `client_commands.go`, (4) an MCP `*MCPGameClient` method in `mcp_game_client_commands.go`, and (5) an `actionResponseTypes` map entry in `client_api_monitor.go` so the monitor stops logging the action as "unhandled" and validates its fields. Actions are grouped into 5 subsystem tasks (Drones, Factions, Empire/Citizenship, Economy, Missions/Notes/Log).

**Tech Stack:** Go 1.25, `golangci-lint` gate, table-driven tests in `package game`.

---

## Conventions used in every task

These are reference facts. Each task below already bakes them into its code; this section explains *why* so an out-of-order reader understands the choices.

**Query vs mutation terminator (from the `x-is-mutation` flag in the spec):**
- **Query** (flag absent/false): WS uses `c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))`; MCP caches the raw result via `m.cacheResultAs(result, "<action>")`.
- **Mutation** (flag true): WS uses `c.Submit(ctx, msg, WithTimeout(SleepTick*3))`; MCP applies state via `m.updateStateFromResult(result)`.

Four actions (`petition`, `delete_note`, `captains_log_delete`, `agentlogs`) mutate server state but are **not** marked `x-is-mutation` in the spec, meaning the server ack-terminates them with a plain OK frame. Per the project rule "use the x-is-mutation flag to pick terminator," they use the **query** terminator (`WithAckOnly`). This is called out again in each relevant step.

**WS method skeleton (query):**
```go
func (c *Client) Xxx(ctx context.Context /* ,args */) error {
	msg := protocol.Message{
		Type:      "action_name",
		Payload:   map[string]any{ /* ... or omit for no-arg */ },
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}
```
For no-payload queries, omit the `Payload` line entirely (see `GetNotes` in the codebase).

**WS method skeleton (mutation):** identical but `c.Submit(ctx, msg, WithTimeout(SleepTick*3))` (no `WithAckOnly`).

**MCP method skeleton (query):**
```go
func (m *MCPGameClient) Xxx(ctx context.Context /* ,args */) error {
	result, err := m.callTool(ctx, "action_name", map[string]any{ /* or nil */ })
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "action_name")
}
```

**MCP method skeleton (mutation):** same but `return m.updateStateFromResult(result)`.

**Struct field typing:** scalars get concrete Go types (`string`, `int`, `int64`, `bool`); credit/value/fee/timestamp fields use `int64`; counts/indices/hull/ticks/capacity use `int`; arrays and nested objects use `json.RawMessage` (keeps the structs tractable and the monitor only checks top-level field names). `serverapi/responses.go` already imports `encoding/json`.

**Monitor entries** go in `pkg/game/client_api_monitor.go` inside the `actionResponseTypes` map. After editing that map, run `gofmt -w pkg/game/client_api_monitor.go` because gofmt realigns the `:` column of the whole block.

**Test pattern (per task, own file to keep tasks independent):** a table-driven test in `package game` that asserts each new action is registered in `actionResponseTypes` and its struct exposes the expected JSON field names via the existing unexported `jsonFieldNames` helper.

```go
func assertActionFields(t *testing.T, want map[string][]string) {
	t.Helper()
	for action, fields := range want {
		typ, ok := actionResponseTypes[action]
		if !ok {
			t.Errorf("action %q not registered in actionResponseTypes", action)
			continue
		}
		got := jsonFieldNames(typ)
		for _, f := range fields {
			if !got[f] {
				t.Errorf("action %q struct missing json field %q", action, f)
			}
		}
	}
}
```
Each task's test file defines its own test function and the `want` map; the helper above is duplicated per file is NOT allowed (one definition only). To avoid a redeclaration, **only Task 1 defines `assertActionFields`**; Tasks 2-5 call it. (All test files are in `package game`, same package, so the helper is shared.)

**Verifying compilation/lint after each task:**
```bash
go build ./... && go test ./pkg/game/ -run TestNewAction -count=1 && golangci-lint run ./pkg/game/...
```

---

### Task 1: Drone actions (6)

Actions: `get_drone` (query), `get_drones` (query), `load_drone` (mutation), `unload_drone` (mutation), `recall_drone` (mutation), `upload_drone_script` (mutation).

**Files:**
- Modify: `pkg/game/serverapi/responses.go` (add 6 structs)
- Modify: `pkg/game/interface.go` (add 6 interface methods)
- Modify: `pkg/game/client_commands.go` (add 6 WS methods)
- Modify: `pkg/game/mcp_game_client_commands.go` (add 6 MCP methods)
- Modify: `pkg/game/client_api_monitor.go` (add 6 monitor entries)
- Test: `pkg/game/drone_actions_test.go` (create; also defines shared `assertActionFields` helper)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/drone_actions_test.go`:
```go
package game

import "testing"

// assertActionFields verifies that each action is registered in
// actionResponseTypes and its struct exposes the expected JSON field names.
// Shared by all api-currentness phase-1 action tests.
func assertActionFields(t *testing.T, want map[string][]string) {
	t.Helper()
	for action, fields := range want {
		typ, ok := actionResponseTypes[action]
		if !ok {
			t.Errorf("action %q not registered in actionResponseTypes", action)
			continue
		}
		got := jsonFieldNames(typ)
		for _, f := range fields {
			if !got[f] {
				t.Errorf("action %q struct missing json field %q", action, f)
			}
		}
	}
}

func TestNewActionDroneFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"get_drone": {
			"id", "type", "status", "hull", "max_hull", "cargo",
			"cargo_capacity", "cargo_used", "item_id", "poi_id", "system_id",
			"deployed_at", "loaded_at", "script", "memory", "travel_to", "travel_ticks",
		},
		"get_drones": {
			"bandwidth_total", "bandwidth_used", "bay_capacity", "bay_count",
			"deployed_count", "drones",
		},
		"load_drone":   {"bay_capacity", "bay_count", "drone_id", "drone_type", "hull", "message", "status"},
		"unload_drone": {"drone_id", "item_id", "message"},
		"recall_drone": {"message", "recalled", "skipped"},
		"upload_drone_script": {"drone_id", "message", "script_len"},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestNewActionDroneFields -count=1`
Expected: FAIL — actions not registered / `jsonFieldNames` returns empty for missing structs (errors like `action "get_drone" not registered`).

- [ ] **Step 3: Add the response structs**

Append to `pkg/game/serverapi/responses.go`:
```go
// GetDroneResponse is returned by get_drone — details of a single drone.
//   - get_drone
type GetDroneResponse struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	Status        string          `json:"status"`
	Hull          int             `json:"hull"`
	MaxHull       int             `json:"max_hull"`
	Cargo         json.RawMessage `json:"cargo,omitempty"`
	CargoCapacity int             `json:"cargo_capacity"`
	CargoUsed     int             `json:"cargo_used"`
	ItemID        string          `json:"item_id,omitempty"`
	POIID         string          `json:"poi_id,omitempty"`
	SystemID      string          `json:"system_id,omitempty"`
	DeployedAt    string          `json:"deployed_at,omitempty"`
	LoadedAt      string          `json:"loaded_at,omitempty"`
	Script        string          `json:"script,omitempty"`
	Memory        json.RawMessage `json:"memory,omitempty"`
	TravelTo      string          `json:"travel_to,omitempty"`
	TravelTicks   int             `json:"travel_ticks,omitempty"`
}

// GetDronesResponse is returned by get_drones — drone bay summary and roster.
//   - get_drones
type GetDronesResponse struct {
	BandwidthTotal int             `json:"bandwidth_total"`
	BandwidthUsed  int             `json:"bandwidth_used"`
	BayCapacity    int             `json:"bay_capacity"`
	BayCount       int             `json:"bay_count"`
	DeployedCount  int             `json:"deployed_count"`
	Drones         json.RawMessage `json:"drones,omitempty"`
}

// LoadDroneResponse is returned by load_drone.
//   - load_drone
type LoadDroneResponse struct {
	BayCapacity int    `json:"bay_capacity"`
	BayCount    int    `json:"bay_count"`
	DroneID     string `json:"drone_id"`
	DroneType   string `json:"drone_type"`
	Hull        int    `json:"hull"`
	Message     string `json:"message"`
	Status      string `json:"status"`
}

// UnloadDroneResponse is returned by unload_drone.
//   - unload_drone
type UnloadDroneResponse struct {
	DroneID string `json:"drone_id"`
	ItemID  string `json:"item_id"`
	Message string `json:"message"`
}

// RecallDroneResponse is returned by recall_drone.
//   - recall_drone
type RecallDroneResponse struct {
	Message  string `json:"message"`
	Recalled int    `json:"recalled"`
	Skipped  int    `json:"skipped"`
}

// UploadDroneScriptResponse is returned by upload_drone_script.
//   - upload_drone_script
type UploadDroneScriptResponse struct {
	DroneID   string `json:"drone_id"`
	Message   string `json:"message"`
	ScriptLen int    `json:"script_len"`
}
```

- [ ] **Step 4: Add the interface methods**

In `pkg/game/interface.go`, add these lines in the drone/ship-management region (near the other ship methods, before the `var _ GameClient` assertion):
```go
	// Drones
	GetDrone(ctx context.Context, droneID string) error
	GetDrones(ctx context.Context) error
	LoadDrone(ctx context.Context, itemID string) error
	UnloadDrone(ctx context.Context, droneID string) error
	RecallDrone(ctx context.Context, droneID string, all bool) error
	UploadDroneScript(ctx context.Context, droneID, script string) error
```

- [ ] **Step 5: Add the WS client methods**

Append to `pkg/game/client_commands.go`:
```go
// GetDrone fetches details for a single drone.
func (c *Client) GetDrone(ctx context.Context, droneID string) error {
	msg := protocol.Message{
		Type:      "get_drone",
		Payload:   map[string]any{"drone_id": droneID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// GetDrones fetches the drone bay summary and roster.
func (c *Client) GetDrones(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_drones",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// LoadDrone loads an item from cargo into the drone bay as a drone.
func (c *Client) LoadDrone(ctx context.Context, itemID string) error {
	msg := protocol.Message{
		Type:      "load_drone",
		Payload:   map[string]any{"item_id": itemID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// UnloadDrone unloads a drone from the bay back into cargo.
func (c *Client) UnloadDrone(ctx context.Context, droneID string) error {
	msg := protocol.Message{
		Type:      "unload_drone",
		Payload:   map[string]any{"drone_id": droneID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// RecallDrone recalls a deployed drone (or all drones when all is true).
func (c *Client) RecallDrone(ctx context.Context, droneID string, all bool) error {
	payload := map[string]any{"all": all}
	if droneID != "" {
		payload["drone_id"] = droneID
	}
	msg := protocol.Message{
		Type:      "recall_drone",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// UploadDroneScript uploads an automation script to a deployed drone.
func (c *Client) UploadDroneScript(ctx context.Context, droneID, script string) error {
	msg := protocol.Message{
		Type:      "upload_drone_script",
		Payload:   map[string]any{"drone_id": droneID, "script": script},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}
```

- [ ] **Step 6: Add the MCP client methods**

Append to `pkg/game/mcp_game_client_commands.go`:
```go
// GetDrone fetches details for a single drone.
func (m *MCPGameClient) GetDrone(ctx context.Context, droneID string) error {
	result, err := m.callTool(ctx, "get_drone", map[string]any{"drone_id": droneID})
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "get_drone")
}

// GetDrones fetches the drone bay summary and roster.
func (m *MCPGameClient) GetDrones(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_drones", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "get_drones")
}

// LoadDrone loads an item from cargo into the drone bay as a drone.
func (m *MCPGameClient) LoadDrone(ctx context.Context, itemID string) error {
	result, err := m.callTool(ctx, "load_drone", map[string]any{"item_id": itemID})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

// UnloadDrone unloads a drone from the bay back into cargo.
func (m *MCPGameClient) UnloadDrone(ctx context.Context, droneID string) error {
	result, err := m.callTool(ctx, "unload_drone", map[string]any{"drone_id": droneID})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

// RecallDrone recalls a deployed drone (or all drones when all is true).
func (m *MCPGameClient) RecallDrone(ctx context.Context, droneID string, all bool) error {
	args := map[string]any{"all": all}
	if droneID != "" {
		args["drone_id"] = droneID
	}
	result, err := m.callTool(ctx, "recall_drone", args)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

// UploadDroneScript uploads an automation script to a deployed drone.
func (m *MCPGameClient) UploadDroneScript(ctx context.Context, droneID, script string) error {
	result, err := m.callTool(ctx, "upload_drone_script", map[string]any{
		"drone_id": droneID,
		"script":   script,
	})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}
```

- [ ] **Step 7: Add the monitor entries**

In `pkg/game/client_api_monitor.go`, inside the `actionResponseTypes` map, add (a `// Drones` comment group is fine):
```go
	// Drones
	"get_drone":           reflect.TypeOf(serverapi.GetDroneResponse{}),
	"get_drones":          reflect.TypeOf(serverapi.GetDronesResponse{}),
	"load_drone":          reflect.TypeOf(serverapi.LoadDroneResponse{}),
	"unload_drone":        reflect.TypeOf(serverapi.UnloadDroneResponse{}),
	"recall_drone":        reflect.TypeOf(serverapi.RecallDroneResponse{}),
	"upload_drone_script": reflect.TypeOf(serverapi.UploadDroneScriptResponse{}),
```
Then run `gofmt -w pkg/game/client_api_monitor.go`.

- [ ] **Step 8: Run test to verify it passes**

Run: `go build ./... && go test ./pkg/game/ -run TestNewActionDroneFields -count=1 && golangci-lint run ./pkg/game/...`
Expected: build OK, test PASS, `0 issues`.

- [ ] **Step 9: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/interface.go pkg/game/client_commands.go pkg/game/mcp_game_client_commands.go pkg/game/client_api_monitor.go pkg/game/drone_actions_test.go
git commit -m "feat(game): add drone actions (get_drone, get_drones, load/unload/recall/upload_drone_script)"
```

---

### Task 2: Faction actions (3)

Actions: `faction_accept_invite` (mutation), `faction_withdraw_invite` (mutation), `faction_remove_enemy` (mutation).

**Files:**
- Modify: `pkg/game/serverapi/responses.go`
- Modify: `pkg/game/interface.go`
- Modify: `pkg/game/client_commands.go`
- Modify: `pkg/game/mcp_game_client_commands.go`
- Modify: `pkg/game/client_api_monitor.go`
- Test: `pkg/game/faction_new_actions_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/faction_new_actions_test.go`:
```go
package game

import "testing"

func TestNewActionFactionFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"faction_accept_invite":   {"faction", "faction_id", "message"},
		"faction_withdraw_invite": {"message", "player_id"},
		"faction_remove_enemy":    {"message", "removed", "target_faction_id", "target_name"},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestNewActionFactionFields -count=1`
Expected: FAIL — actions not registered.

- [ ] **Step 3: Add the response structs**

Append to `pkg/game/serverapi/responses.go`:
```go
// FactionAcceptInviteResponse is returned by faction_accept_invite.
//   - faction_accept_invite
type FactionAcceptInviteResponse struct {
	Faction   string `json:"faction"`
	FactionID string `json:"faction_id"`
	Message   string `json:"message"`
}

// FactionWithdrawInviteResponse is returned by faction_withdraw_invite.
//   - faction_withdraw_invite
type FactionWithdrawInviteResponse struct {
	Message  string `json:"message"`
	PlayerID string `json:"player_id"`
}

// FactionRemoveEnemyResponse is returned by faction_remove_enemy.
//   - faction_remove_enemy
type FactionRemoveEnemyResponse struct {
	Message         string `json:"message"`
	Removed         bool   `json:"removed"`
	TargetFactionID string `json:"target_faction_id"`
	TargetName      string `json:"target_name"`
}
```

- [ ] **Step 4: Add the interface methods**

In `pkg/game/interface.go`, in the faction region:
```go
	FactionAcceptInvite(ctx context.Context, factionID string) error
	FactionWithdrawInvite(ctx context.Context, playerID string) error
	FactionRemoveEnemy(ctx context.Context, targetFactionID string) error
```

- [ ] **Step 5: Add the WS client methods**

Append to `pkg/game/client_commands.go`:
```go
// FactionAcceptInvite accepts a pending invitation to join a faction.
func (c *Client) FactionAcceptInvite(ctx context.Context, factionID string) error {
	msg := protocol.Message{
		Type:      "faction_accept_invite",
		Payload:   map[string]any{"faction_id": factionID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// FactionWithdrawInvite withdraws an invitation previously sent to a player.
func (c *Client) FactionWithdrawInvite(ctx context.Context, playerID string) error {
	msg := protocol.Message{
		Type:      "faction_withdraw_invite",
		Payload:   map[string]any{"player_id": playerID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// FactionRemoveEnemy removes a faction from this faction's enemy list.
func (c *Client) FactionRemoveEnemy(ctx context.Context, targetFactionID string) error {
	msg := protocol.Message{
		Type:      "faction_remove_enemy",
		Payload:   map[string]any{"target_faction_id": targetFactionID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}
```

- [ ] **Step 6: Add the MCP client methods**

Append to `pkg/game/mcp_game_client_commands.go`:
```go
// FactionAcceptInvite accepts a pending invitation to join a faction.
func (m *MCPGameClient) FactionAcceptInvite(ctx context.Context, factionID string) error {
	result, err := m.callTool(ctx, "faction_accept_invite", map[string]any{"faction_id": factionID})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

// FactionWithdrawInvite withdraws an invitation previously sent to a player.
func (m *MCPGameClient) FactionWithdrawInvite(ctx context.Context, playerID string) error {
	result, err := m.callTool(ctx, "faction_withdraw_invite", map[string]any{"player_id": playerID})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

// FactionRemoveEnemy removes a faction from this faction's enemy list.
func (m *MCPGameClient) FactionRemoveEnemy(ctx context.Context, targetFactionID string) error {
	result, err := m.callTool(ctx, "faction_remove_enemy", map[string]any{"target_faction_id": targetFactionID})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}
```

- [ ] **Step 7: Add the monitor entries**

In `pkg/game/client_api_monitor.go`, in the `actionResponseTypes` map (faction group):
```go
	"faction_accept_invite":   reflect.TypeOf(serverapi.FactionAcceptInviteResponse{}),
	"faction_withdraw_invite": reflect.TypeOf(serverapi.FactionWithdrawInviteResponse{}),
	"faction_remove_enemy":    reflect.TypeOf(serverapi.FactionRemoveEnemyResponse{}),
```
Then run `gofmt -w pkg/game/client_api_monitor.go`.

- [ ] **Step 8: Run test to verify it passes**

Run: `go build ./... && go test ./pkg/game/ -run TestNewActionFactionFields -count=1 && golangci-lint run ./pkg/game/...`
Expected: build OK, test PASS, `0 issues`.

- [ ] **Step 9: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/interface.go pkg/game/client_commands.go pkg/game/mcp_game_client_commands.go pkg/game/client_api_monitor.go pkg/game/faction_new_actions_test.go
git commit -m "feat(game): add faction_accept_invite, faction_withdraw_invite, faction_remove_enemy"
```

---

### Task 3: Empire & citizenship actions (3)

Actions: `citizenship` (mutation), `get_empire_info` (query), `petition` (query terminator — see Conventions; it mutates but is not flagged `x-is-mutation`).

**Files:**
- Modify: `pkg/game/serverapi/responses.go`
- Modify: `pkg/game/interface.go`
- Modify: `pkg/game/client_commands.go`
- Modify: `pkg/game/mcp_game_client_commands.go`
- Modify: `pkg/game/client_api_monitor.go`
- Test: `pkg/game/empire_actions_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/empire_actions_test.go`:
```go
package game

import "testing"

func TestNewActionEmpireFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"citizenship": {
			"citizenship", "citizenships", "empire_id", "empires", "fee_paid",
			"fee_refunded", "message", "origin", "pending_petitions", "petition",
			"petition_id", "recent_decisions", "renounced", "rules", "status",
		},
		"get_empire_info": {"action", "empires"},
		"petition":        {"empire_id", "empire_name", "message"},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestNewActionEmpireFields -count=1`
Expected: FAIL — actions not registered.

- [ ] **Step 3: Add the response structs**

Append to `pkg/game/serverapi/responses.go`:
```go
// CitizenshipResponse is returned by the citizenship action, which handles
// petitioning for, granting, renouncing, and querying empire citizenship.
// Field presence varies by the citizenship sub-action requested.
//   - citizenship
type CitizenshipResponse struct {
	Citizenship      json.RawMessage `json:"citizenship,omitempty"`
	Citizenships     json.RawMessage `json:"citizenships,omitempty"`
	EmpireID         string          `json:"empire_id,omitempty"`
	Empires          json.RawMessage `json:"empires,omitempty"`
	FeePaid          int64           `json:"fee_paid,omitempty"`
	FeeRefunded      int64           `json:"fee_refunded,omitempty"`
	Message          string          `json:"message,omitempty"`
	Origin           string          `json:"origin,omitempty"`
	PendingPetitions json.RawMessage `json:"pending_petitions,omitempty"`
	Petition         json.RawMessage `json:"petition,omitempty"`
	PetitionID       string          `json:"petition_id,omitempty"`
	RecentDecisions  json.RawMessage `json:"recent_decisions,omitempty"`
	Renounced        json.RawMessage `json:"renounced,omitempty"`
	Rules            json.RawMessage `json:"rules,omitempty"`
	Status           string          `json:"status,omitempty"`
}

// GetEmpireInfoResponse is returned by get_empire_info.
//   - get_empire_info
type GetEmpireInfoResponse struct {
	Action  string          `json:"action"`
	Empires json.RawMessage `json:"empires,omitempty"`
}

// PetitionResponse is returned by petition (submit a citizenship petition).
//   - petition
type PetitionResponse struct {
	EmpireID   string `json:"empire_id"`
	EmpireName string `json:"empire_name"`
	Message    string `json:"message"`
}
```

- [ ] **Step 4: Add the interface methods**

In `pkg/game/interface.go`, in the faction/empire region:
```go
	// Empire & citizenship
	Citizenship(ctx context.Context, action, empireID string) error
	GetEmpireInfo(ctx context.Context, empireID string) error
	Petition(ctx context.Context, empireID, message string) error
```

- [ ] **Step 5: Add the WS client methods**

Append to `pkg/game/client_commands.go`. Note `citizenship` is a mutation; `get_empire_info` and `petition` use the query terminator (`petition` mutates but is not flagged `x-is-mutation`):
```go
// Citizenship performs a citizenship sub-action (e.g. petition, renounce,
// grant, list). empireID is optional depending on the action.
func (c *Client) Citizenship(ctx context.Context, action, empireID string) error {
	payload := map[string]any{"action": action}
	if empireID != "" {
		payload["empire_id"] = empireID
	}
	msg := protocol.Message{
		Type:      "citizenship",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// GetEmpireInfo fetches empire information. empireID is optional; when empty
// the server returns all empires.
func (c *Client) GetEmpireInfo(ctx context.Context, empireID string) error {
	payload := map[string]any{}
	if empireID != "" {
		payload["empire_id"] = empireID
	}
	msg := protocol.Message{
		Type:      "get_empire_info",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// Petition submits a citizenship petition message to an empire. The server
// ack-terminates this action (not flagged x-is-mutation), so it uses the
// query terminator.
func (c *Client) Petition(ctx context.Context, empireID, message string) error {
	msg := protocol.Message{
		Type:      "petition",
		Payload:   map[string]any{"empire_id": empireID, "message": message},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}
```

- [ ] **Step 6: Add the MCP client methods**

Append to `pkg/game/mcp_game_client_commands.go`:
```go
// Citizenship performs a citizenship sub-action.
func (m *MCPGameClient) Citizenship(ctx context.Context, action, empireID string) error {
	args := map[string]any{"action": action}
	if empireID != "" {
		args["empire_id"] = empireID
	}
	result, err := m.callTool(ctx, "citizenship", args)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}

// GetEmpireInfo fetches empire information.
func (m *MCPGameClient) GetEmpireInfo(ctx context.Context, empireID string) error {
	args := map[string]any{}
	if empireID != "" {
		args["empire_id"] = empireID
	}
	result, err := m.callTool(ctx, "get_empire_info", args)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "get_empire_info")
}

// Petition submits a citizenship petition message to an empire.
func (m *MCPGameClient) Petition(ctx context.Context, empireID, message string) error {
	result, err := m.callTool(ctx, "petition", map[string]any{
		"empire_id": empireID,
		"message":   message,
	})
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "petition")
}
```

- [ ] **Step 7: Add the monitor entries**

In `pkg/game/client_api_monitor.go`, in the `actionResponseTypes` map:
```go
	"citizenship":     reflect.TypeOf(serverapi.CitizenshipResponse{}),
	"get_empire_info": reflect.TypeOf(serverapi.GetEmpireInfoResponse{}),
	"petition":        reflect.TypeOf(serverapi.PetitionResponse{}),
```
Then run `gofmt -w pkg/game/client_api_monitor.go`.

- [ ] **Step 8: Run test to verify it passes**

Run: `go build ./... && go test ./pkg/game/ -run TestNewActionEmpireFields -count=1 && golangci-lint run ./pkg/game/...`
Expected: build OK, test PASS, `0 issues`.

- [ ] **Step 9: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/interface.go pkg/game/client_commands.go pkg/game/mcp_game_client_commands.go pkg/game/client_api_monitor.go pkg/game/empire_actions_test.go
git commit -m "feat(game): add citizenship, get_empire_info, petition actions"
```

---

### Task 4: Economy actions (3)

Actions: `get_tax_estimate` (query), `view_insurance` (query), `scrap_ship` (mutation).

**Files:**
- Modify: `pkg/game/serverapi/responses.go`
- Modify: `pkg/game/interface.go`
- Modify: `pkg/game/client_commands.go`
- Modify: `pkg/game/mcp_game_client_commands.go`
- Modify: `pkg/game/client_api_monitor.go`
- Test: `pkg/game/economy_actions_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/economy_actions_test.go`:
```go
package game

import "testing"

func TestNewActionEconomyFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"get_tax_estimate": {
			"action", "assessed_property_by_ship", "assessed_property_value",
			"income_tax", "income_tax_total", "last_assessed_at",
			"last_property_assessed_at", "next_assessment_approx_seconds", "note",
			"property_tax", "property_tax_total", "sales_tax_rates",
			"tax_collection_active", "taxable_income_by_source", "taxable_income_to_date",
		},
		"view_insurance": {"message", "policies"},
		"scrap_ship": {
			"cargo_note", "cargo_to_storage", "message", "modules_note",
			"modules_to_storage", "scrapped_class", "scrapped_ship_id",
		},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestNewActionEconomyFields -count=1`
Expected: FAIL — actions not registered.

- [ ] **Step 3: Add the response structs**

Append to `pkg/game/serverapi/responses.go`:
```go
// GetTaxEstimateResponse is returned by get_tax_estimate.
//   - get_tax_estimate
type GetTaxEstimateResponse struct {
	Action                      string          `json:"action"`
	AssessedPropertyByShip      json.RawMessage `json:"assessed_property_by_ship,omitempty"`
	AssessedPropertyValue       int64           `json:"assessed_property_value"`
	IncomeTax                   json.RawMessage `json:"income_tax,omitempty"`
	IncomeTaxTotal              int64           `json:"income_tax_total"`
	LastAssessedAt              int64           `json:"last_assessed_at,omitempty"`
	LastPropertyAssessedAt      int64           `json:"last_property_assessed_at,omitempty"`
	NextAssessmentApproxSeconds int64           `json:"next_assessment_approx_seconds,omitempty"`
	Note                        string          `json:"note,omitempty"`
	PropertyTax                 json.RawMessage `json:"property_tax,omitempty"`
	PropertyTaxTotal            int64           `json:"property_tax_total"`
	SalesTaxRates               json.RawMessage `json:"sales_tax_rates,omitempty"`
	TaxCollectionActive         bool            `json:"tax_collection_active"`
	TaxableIncomeBySource       json.RawMessage `json:"taxable_income_by_source,omitempty"`
	TaxableIncomeToDate         int64           `json:"taxable_income_to_date"`
}

// ViewInsuranceResponse is returned by view_insurance.
//   - view_insurance
type ViewInsuranceResponse struct {
	Message  string          `json:"message,omitempty"`
	Policies json.RawMessage `json:"policies,omitempty"`
}

// ScrapShipResponse is returned by scrap_ship.
//   - scrap_ship
type ScrapShipResponse struct {
	CargoNote        string          `json:"cargo_note,omitempty"`
	CargoToStorage   json.RawMessage `json:"cargo_to_storage,omitempty"`
	Message          string          `json:"message"`
	ModulesNote      string          `json:"modules_note,omitempty"`
	ModulesToStorage json.RawMessage `json:"modules_to_storage,omitempty"`
	ScrappedClass    string          `json:"scrapped_class,omitempty"`
	ScrappedShipID   string          `json:"scrapped_ship_id"`
}
```

- [ ] **Step 4: Add the interface methods**

In `pkg/game/interface.go`, in the economy / ship-management region:
```go
	GetTaxEstimate(ctx context.Context) error
	ViewInsurance(ctx context.Context) error
	ScrapShip(ctx context.Context, shipID string) error
```

- [ ] **Step 5: Add the WS client methods**

Append to `pkg/game/client_commands.go`:
```go
// GetTaxEstimate fetches the player's current tax assessment estimate.
func (c *Client) GetTaxEstimate(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "get_tax_estimate",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// ViewInsurance lists the player's active insurance policies.
func (c *Client) ViewInsurance(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "view_insurance",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// ScrapShip scraps a ship, moving its cargo and modules to storage.
func (c *Client) ScrapShip(ctx context.Context, shipID string) error {
	msg := protocol.Message{
		Type:      "scrap_ship",
		Payload:   map[string]any{"ship_id": shipID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}
```

- [ ] **Step 6: Add the MCP client methods**

Append to `pkg/game/mcp_game_client_commands.go`:
```go
// GetTaxEstimate fetches the player's current tax assessment estimate.
func (m *MCPGameClient) GetTaxEstimate(ctx context.Context) error {
	result, err := m.callTool(ctx, "get_tax_estimate", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "get_tax_estimate")
}

// ViewInsurance lists the player's active insurance policies.
func (m *MCPGameClient) ViewInsurance(ctx context.Context) error {
	result, err := m.callTool(ctx, "view_insurance", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "view_insurance")
}

// ScrapShip scraps a ship, moving its cargo and modules to storage.
func (m *MCPGameClient) ScrapShip(ctx context.Context, shipID string) error {
	result, err := m.callTool(ctx, "scrap_ship", map[string]any{"ship_id": shipID})
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}
```

- [ ] **Step 7: Add the monitor entries**

In `pkg/game/client_api_monitor.go`, in the `actionResponseTypes` map:
```go
	"get_tax_estimate": reflect.TypeOf(serverapi.GetTaxEstimateResponse{}),
	"view_insurance":   reflect.TypeOf(serverapi.ViewInsuranceResponse{}),
	"scrap_ship":       reflect.TypeOf(serverapi.ScrapShipResponse{}),
```
Then run `gofmt -w pkg/game/client_api_monitor.go`.

- [ ] **Step 8: Run test to verify it passes**

Run: `go build ./... && go test ./pkg/game/ -run TestNewActionEconomyFields -count=1 && golangci-lint run ./pkg/game/...`
Expected: build OK, test PASS, `0 issues`.

- [ ] **Step 9: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/interface.go pkg/game/client_commands.go pkg/game/mcp_game_client_commands.go pkg/game/client_api_monitor.go pkg/game/economy_actions_test.go
git commit -m "feat(game): add get_tax_estimate, view_insurance, scrap_ship actions"
```

---

### Task 5: Missions, notes & log actions (4)

Actions: `completed_missions` (query), `delete_note` (query terminator — mutates but not flagged), `captains_log_delete` (query terminator — mutates but not flagged), `agentlogs` (query terminator — write-only telemetry, no response body; reuses `serverapi.MessageResponse`).

**Files:**
- Modify: `pkg/game/serverapi/responses.go` (add 3 structs; `agentlogs` reuses `MessageResponse`)
- Modify: `pkg/game/interface.go`
- Modify: `pkg/game/client_commands.go`
- Modify: `pkg/game/mcp_game_client_commands.go`
- Modify: `pkg/game/client_api_monitor.go`
- Test: `pkg/game/mission_log_actions_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `pkg/game/mission_log_actions_test.go`. `agentlogs` maps to `MessageResponse` (which exposes `message`), so we assert `message`:
```go
package game

import "testing"

func TestNewActionMissionLogFields(t *testing.T) {
	assertActionFields(t, map[string][]string{
		"completed_missions":  {"missions", "total_count"},
		"delete_note":         {"message", "note_id", "title"},
		"captains_log_delete": {"index", "message", "remaining_count"},
		"agentlogs":           {"message"},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestNewActionMissionLogFields -count=1`
Expected: FAIL — actions not registered.

- [ ] **Step 3: Add the response structs**

Append to `pkg/game/serverapi/responses.go` (no struct for `agentlogs` — it reuses `MessageResponse`):
```go
// CompletedMissionsResponse is returned by completed_missions.
//   - completed_missions
type CompletedMissionsResponse struct {
	Missions   json.RawMessage `json:"missions,omitempty"`
	TotalCount int             `json:"total_count"`
}

// DeleteNoteResponse is returned by delete_note.
//   - delete_note
type DeleteNoteResponse struct {
	Message string `json:"message"`
	NoteID  string `json:"note_id"`
	Title   string `json:"title,omitempty"`
}

// CaptainsLogDeleteResponse is returned by captains_log_delete.
//   - captains_log_delete
type CaptainsLogDeleteResponse struct {
	Index          int    `json:"index"`
	Message        string `json:"message"`
	RemainingCount int    `json:"remaining_count"`
}
```

- [ ] **Step 4: Add the interface methods**

In `pkg/game/interface.go`, near the existing mission / note / captains-log methods:
```go
	CompletedMissions(ctx context.Context) error
	DeleteNote(ctx context.Context, noteID string) error
	CaptainsLogDelete(ctx context.Context, index int) error
	AgentLogs(ctx context.Context, category, severity, message string, data map[string]any) error
```

- [ ] **Step 5: Add the WS client methods**

Append to `pkg/game/client_commands.go`. All four use the query terminator (`completed_missions` is a true query; the other three mutate but are not flagged `x-is-mutation`):
```go
// CompletedMissions lists the player's completed missions.
func (c *Client) CompletedMissions(ctx context.Context) error {
	msg := protocol.Message{
		Type:      "completed_missions",
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// DeleteNote deletes a saved note by ID. The server ack-terminates this
// action (not flagged x-is-mutation), so it uses the query terminator.
func (c *Client) DeleteNote(ctx context.Context, noteID string) error {
	msg := protocol.Message{
		Type:      "delete_note",
		Payload:   map[string]any{"note_id": noteID},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// CaptainsLogDelete deletes a captain's-log entry by index. The server
// ack-terminates this action (not flagged x-is-mutation), so it uses the
// query terminator.
func (c *Client) CaptainsLogDelete(ctx context.Context, index int) error {
	msg := protocol.Message{
		Type:      "captains_log_delete",
		Payload:   map[string]any{"index": index},
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}

// AgentLogs submits an agent telemetry log entry. data is optional structured
// context. The action is write-only (no response body) and ack-terminated.
func (c *Client) AgentLogs(ctx context.Context, category, severity, message string, data map[string]any) error {
	payload := map[string]any{
		"category": category,
		"severity": severity,
		"message":  message,
	}
	if data != nil {
		payload["data"] = data
	}
	msg := protocol.Message{
		Type:      "agentlogs",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithAckOnly(), WithTimeout(SleepMedium))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}
```

- [ ] **Step 6: Add the MCP client methods**

Append to `pkg/game/mcp_game_client_commands.go`:
```go
// CompletedMissions lists the player's completed missions.
func (m *MCPGameClient) CompletedMissions(ctx context.Context) error {
	result, err := m.callTool(ctx, "completed_missions", nil)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "completed_missions")
}

// DeleteNote deletes a saved note by ID.
func (m *MCPGameClient) DeleteNote(ctx context.Context, noteID string) error {
	result, err := m.callTool(ctx, "delete_note", map[string]any{"note_id": noteID})
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "delete_note")
}

// CaptainsLogDelete deletes a captain's-log entry by index.
func (m *MCPGameClient) CaptainsLogDelete(ctx context.Context, index int) error {
	result, err := m.callTool(ctx, "captains_log_delete", map[string]any{"index": index})
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "captains_log_delete")
}

// AgentLogs submits an agent telemetry log entry.
func (m *MCPGameClient) AgentLogs(ctx context.Context, category, severity, message string, data map[string]any) error {
	args := map[string]any{
		"category": category,
		"severity": severity,
		"message":  message,
	}
	if data != nil {
		args["data"] = data
	}
	result, err := m.callTool(ctx, "agentlogs", args)
	if err != nil {
		return err
	}
	return m.cacheResultAs(result, "agentlogs")
}
```

- [ ] **Step 7: Add the monitor entries**

In `pkg/game/client_api_monitor.go`, in the `actionResponseTypes` map (`agentlogs` reuses `MessageResponse`):
```go
	"completed_missions":  reflect.TypeOf(serverapi.CompletedMissionsResponse{}),
	"delete_note":         reflect.TypeOf(serverapi.DeleteNoteResponse{}),
	"captains_log_delete": reflect.TypeOf(serverapi.CaptainsLogDeleteResponse{}),
	"agentlogs":           reflect.TypeOf(serverapi.MessageResponse{}),
```
Then run `gofmt -w pkg/game/client_api_monitor.go`.

- [ ] **Step 8: Run test to verify it passes**

Run: `go build ./... && go test ./pkg/game/ -run TestNewActionMissionLogFields -count=1 && golangci-lint run ./pkg/game/...`
Expected: build OK, test PASS, `0 issues`.

- [ ] **Step 9: Commit**

```bash
git add pkg/game/serverapi/responses.go pkg/game/interface.go pkg/game/client_commands.go pkg/game/mcp_game_client_commands.go pkg/game/client_api_monitor.go pkg/game/mission_log_actions_test.go
git commit -m "feat(game): add completed_missions, delete_note, captains_log_delete, agentlogs"
```

---

## Final verification (after all 5 tasks)

- [ ] **Full build + test + lint:**

```bash
go build ./... && go test ./pkg/game/... -count=1 && golangci-lint run ./pkg/game/...
```
Expected: build OK, all tests PASS, `0 issues`.

- [ ] **Interface assertions hold:** the `var _ GameClient = (*Client)(nil)` (interface.go) and `var _ GameClient = (*MCPGameClient)(nil)` (mcp_game_client.go) assertions compile — confirming all 19 methods exist on both clients. (A missing method on either client is a compile error, already caught by `go build`.)

- [ ] **Monitor coverage:** the 19 actions no longer appear in the "unhandled action" audit. Optionally re-run any monitor-gap report to confirm the unmapped-action count dropped by 19.

---

## Self-Review

**Spec coverage:** All 19 actions from the audit are covered — Drones (6): get_drone, get_drones, load_drone, unload_drone, recall_drone, upload_drone_script; Factions (3): faction_accept_invite, faction_withdraw_invite, faction_remove_enemy; Empire/Citizenship (3): citizenship, get_empire_info, petition; Economy (3): get_tax_estimate, view_insurance, scrap_ship; Missions/Notes/Log (4): completed_missions, delete_note, captains_log_delete, agentlogs. Total = 19. ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete code; no "similar to Task N" references. ✓

**Type consistency:** `assertActionFields` is defined once (Task 1) and reused by Tasks 2-5 (same `package game`). Struct names referenced in monitor entries match the struct definitions exactly. `agentlogs` deliberately has no dedicated struct and reuses `serverapi.MessageResponse` (documented in Task 5 Steps 3, 7). Method signatures in the interface (Step 4 of each task) match the WS (Step 5) and MCP (Step 6) implementations. ✓

**Terminator classification:** mutations (load_drone, unload_drone, recall_drone, upload_drone_script, faction_accept_invite, faction_withdraw_invite, faction_remove_enemy, citizenship, scrap_ship) use `WithTimeout(SleepTick*3)` / `updateStateFromResult`; queries and unflagged-but-mutating actions (get_drone, get_drones, get_empire_info, petition, get_tax_estimate, view_insurance, completed_missions, delete_note, captains_log_delete, agentlogs) use `WithAckOnly()` / `cacheResultAs`. ✓
