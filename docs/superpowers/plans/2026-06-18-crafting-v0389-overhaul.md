# Crafting v0.389.0 Overhaul Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to execute this plan. Each task below is a self-contained, test-first unit. Dispatch one subagent per task in order; a task that adds a `GameClient` interface method MUST update both mocks in the same task. Do not batch tasks — each ends with a green build, green test suite, and a commit.

**Goal:** Bring the Go game client, `play_as` REPL, and crafting agent loops in line with the server's v0.389.0 async, storage-backed, queued crafting model (craft/recycle jobs, `crafting_update` push, facility production queue).

**Architecture:** `craft` and `recycle` become async commands that return a single `type=ok` job frame (terminated via `terminateOnActionOrOK`) and later deliver output through a new `crafting_update` push event handled in `handleResponse` with an `OnCraftingUpdate` callback. New typed response structs in `serverapi` decode the four `CraftJobResponse` oneOf variants; `play_as` parses the new flags and renders all four variants (recycle reuses craft rendering); the legacy withdraw→craft-to-cargo→deposit agent loops are replaced by queue-once-then-monitor.

**Tech Stack:** Go 1.24+, single WebSocket protocol (`internal/protocol`), `pkg/game` client, `cmd/tools/play_as` REPL. JSON over WebSocket. OpenAPI source of truth: `server_docs/openapi.json` (→ `openapi.20260618.json`, v0.389.0).

## Global Constraints

Copy these verbatim into every task's working context:

- **Go 1.24+.** Use modern features where applicable (range-over-int, `b.Loop()` in benchmarks instead of `for i := 0; i < b.N; i++`).
- **JSON Schema Draft 2020-12** in any schema specification work.
- All new code MUST pass `golangci-lint` with **no new findings**.
- **Every task ends with `go build ./...` AND `go test ./...`** — `go build` does NOT catch missing mock methods; only `go test ./...` does (see memory `feedback_gameclient_interface_mocks.md`).
- **Async terminator (binding):** `craft` and `recycle` use `WithTerminator(terminateOnActionOrOK)` so the queued-job `ok` frame (non-pending) is treated as terminal. Keep `WithTimeout(SleepTick*3)` on the single/dry-run/bulk submit.
- **`deliver_to` (binding):** remove `cargo` everywhere as a valid value (client docstring, `play_as` parser/help). The server enum is **`storage` (default) | `faction`**. Default empty → server uses `storage`.
- **Struct strategy (binding):** add NEW typed structs (`CraftJobQueued`, `CraftQueueListing` + `CraftJobEntry`, `CraftBulkResponse` + `CraftBulkResult`, `CraftDryRunResponse`, `EscrowCost` + `EscrowInput`, `CraftingUpdateEvent` + `CraftingUpdateJob`). `RecycleResponse` reuses the same shapes. Keep the old `CraftResponse` as a compile shim until Task 10 deletes its last use.
- **`crafting_update` event (binding):** add `TypeCraftingUpdate = "crafting_update"`; handle in `handleResponse` mirroring `TypeMiningYield` (log + optional state); add an `OnCraftingUpdate(func(CraftingUpdateEvent))` callback mirroring `SetOnChatMessage` (field + setter + fire site). Add to `eventExpectedFields`.
- **`MaxCraftBatchSize` (binding):** stop using it in the single-craft path (Task 3); remove the function and its remaining two callers when rewriting `CraftItems` / `CraftingLoop` (Task 10). Do NOT delete the function while callers remain.
- **Agent-loop rewrite (binding):** replace withdraw→craft-to-cargo→deposit with queue-once-then-monitor. Never re-issue an identical craft to "make progress" — that double-spends.
- **Pre-existing failures to leave alone (TWO packages):** (1) `pkg/galaxy/graph_test.go` mockKB lacks `RecordPassengers`; (2) `pkg/actionspace` `TestLoadFromOpenAPIContainsAllHardcoded` fails with `hardcoded action "claim_commission" missing from OpenAPI registry` (a catalog-sync drift unrelated to crafting — the prior openapi lacked it too). Both fail at the branch base. Do NOT try to fix either. When a task's `go test ./...` shows ONLY these two packages failing for these reasons, treat the suite as green for that task.

---

## File Structure

| File | Status | Responsibility |
|------|--------|----------------|
| `internal/protocol/messages.go` | modify | Add `TypeCraftingUpdate = "crafting_update"` constant. |
| `pkg/game/serverapi/responses.go` | modify | Add the new craft/recycle/crafting_update structs; keep `CraftResponse` shim. |
| `pkg/game/crafting.go` | modify | Fix `CraftWithOptions` semantics (Task 3); rewrite `CraftItems` (Task 10); delete `MaxCraftBatchSize` (Task 10). |
| `pkg/game/crafting_recycle.go` | create | `Recycle` / `RecycleWithOptions` client methods. |
| `pkg/game/crafting_recycle_test.go` | create | Tests for recycle payload building. |
| `pkg/game/crafting_test.go` | modify/create | Tests for `CraftWithOptions` payload + terminator. |
| `pkg/game/interface.go` | modify | Add `Recycle` / `RecycleWithOptions` to `GameClient`. |
| `pkg/game/mcp_game_client_commands.go` | modify | Mirror `Recycle` / `RecycleWithOptions`. |
| `pkg/game/client.go` | modify | `crafting_update` handling + `OnCraftingUpdate` callback (field/setter/fire). |
| `pkg/game/client_crafting_update_test.go` | create | Test the `crafting_update` callback fires with decoded event. |
| `pkg/game/client_api_monitor.go` | modify | Add `crafting_update` to `eventExpectedFields`. |
| `pkg/game/crafting_loop.go` | modify | Rewrite `craftRecipe`/`CraftingLoop` to queue-and-monitor (Task 10). |
| `pkg/agent/runner_test.go` | modify | Add `Recycle`/`RecycleWithOptions` mock stubs. |
| `pkg/skills/client_dispatcher_test.go` | modify | Add `Recycle`/`RecycleWithOptions` mock stubs. |
| `cmd/tools/play_as/main.go` | modify | New craft parser/flags, `formatCraft` rewrite, `recycle` command+formatter, facility job sub-action formatters, help text, dispatch registration. |

---

## Task 1 — Protocol constant + `crafting_update` event stub + `eventExpectedFields`

**Files**
- modify `internal/protocol/messages.go` (add constant near `TypeFacilityRentWarning` at :65)
- modify `pkg/game/client.go` (add a no-op `case protocol.TypeCraftingUpdate:` in the `handleResponse` switch, mirroring `TypeMiningYield` at :2347)
- modify `pkg/game/client_api_monitor.go` (add `protocol.TypeCraftingUpdate` entry to `eventExpectedFields`, after `TypeMiningYield` at :412)
- create `pkg/game/client_crafting_update_test.go`

**Interfaces**
- Produces: `protocol.TypeCraftingUpdate string = "crafting_update"`.
- Produces: a `handleResponse` branch that logs `crafting_update` (no state mutation yet; the callback is wired in Task 9).

Steps:

- [ ] 1. Write failing test `pkg/game/client_crafting_update_test.go`:

```go
package game

import (
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestCraftingUpdateTypeConstant(t *testing.T) {
	if protocol.TypeCraftingUpdate != "crafting_update" {
		t.Fatalf("TypeCraftingUpdate = %q, want %q", protocol.TypeCraftingUpdate, "crafting_update")
	}
}

func TestCraftingUpdateEventInExpectedFields(t *testing.T) {
	fields, ok := eventExpectedFields[protocol.TypeCraftingUpdate]
	if !ok {
		t.Fatal("eventExpectedFields missing crafting_update")
	}
	for _, want := range []string{"tick", "jobs"} {
		if !fields[want] {
			t.Errorf("eventExpectedFields[crafting_update] missing %q", want)
		}
	}
}
```

- [ ] 2. Run-to-fail: `go test ./pkg/game/ -run TestCraftingUpdate`. Expect: `undefined: protocol.TypeCraftingUpdate` (compile error).

- [ ] 3. Add the constant in `internal/protocol/messages.go` (in the Facility events block):

```go
	// Facility events
	TypeFacilityRentWarning = "facility_rent_warning"

	// Crafting events
	TypeCraftingUpdate = "crafting_update"
```

- [ ] 4. Add the no-op handler branch in `pkg/game/client.go` `handleResponse`, immediately after the `case protocol.TypeMiningYield:` block (~:2347):

```go
	case protocol.TypeCraftingUpdate:
		// Async crafting progress push (server v0.389.0+). Output items are
		// deposited to station/faction storage server-side; we only log here.
		// The OnCraftingUpdate callback is wired in a later task.
		if tick, ok := resp.Payload["tick"].(float64); ok {
			c.debugLogger.Printf("[CRAFTING_UPDATE] tick=%d", int64(tick))
		}
```

- [ ] 5. Add the `eventExpectedFields` entry in `pkg/game/client_api_monitor.go`, after the `TypeMiningYield` block (~:423):

```go
	protocol.TypeCraftingUpdate: {
		"tick": true,
		"jobs": true,
	},
```

- [ ] 6. Run-to-pass: `go test ./pkg/game/ -run TestCraftingUpdate`. Expect: `ok`.
- [ ] 7. `go build ./...` and `go test ./...` (ignore the pre-existing `pkg/galaxy` failure).
- [ ] 8. Commit:

```
git add -A && git commit -m 'feat(crafting): add crafting_update protocol constant + event stub

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>'
```

---

## Task 2 — New response structs (keep old `CraftResponse` shim)

**Files**
- modify `pkg/game/serverapi/responses.go` (add new structs after `CraftSourceItem` at :1422; keep `CraftResponse` :1393 unchanged)
- create `pkg/game/serverapi/crafting_responses_test.go`

**Interfaces** (Produces — these exact names/fields are consumed by Tasks 3, 6, 7, 9, 10; field tags verified against `CraftJobResponse` and `Notification_crafting_update` in openapi):

```go
type EscrowInput struct {
	ItemID   string `json:"item_id"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

type EscrowCost struct {
	Fee    int           `json:"fee"`
	Labor  int           `json:"labor"`
	Inputs []EscrowInput `json:"inputs"`
}

type CraftProduces struct {
	ItemID   string `json:"item_id"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

type CraftJobQueued struct {
	Action              string          `json:"action"`
	JobID               string          `json:"job_id"`
	Recipe              string          `json:"recipe"`
	Mode                string          `json:"mode"`
	Venue               string          `json:"venue"`
	VenueType           string          `json:"venue_type"`
	FacilityID          string          `json:"facility_id"`
	Runs                int             `json:"runs"`
	EffectiveTimePerRun float64         `json:"effective_time_per_run"`
	EstCompletionTick   int             `json:"est_completion_tick"`
	Escrowed            EscrowCost      `json:"escrowed"`
	Message             string          `json:"message"`
	Produces            []CraftProduces `json:"produces,omitempty"`
	External            bool            `json:"external,omitempty"`
}

type CraftJobEntry struct {
	JobID         string          `json:"job_id"`
	Recipe        string          `json:"recipe"`
	Mode          string          `json:"mode"`
	RunsTotal     int             `json:"runs_total"`
	RunsDone      int             `json:"runs_done"`
	RunsRemaining int             `json:"runs_remaining"`
	Progress      float64         `json:"progress"`
	ETATicks      int             `json:"eta_ticks"`
	Position      int             `json:"position"`
	Orderer       string          `json:"orderer"`
	Status        string          `json:"status"`
	FacilityID    string          `json:"facility_id"`
	External      bool            `json:"external,omitempty"`
	Venue         string          `json:"venue,omitempty"`
	Produces      []CraftProduces `json:"produces,omitempty"`
}

type CraftQueueListing struct {
	Action string          `json:"action"`
	Jobs   []CraftJobEntry `json:"jobs"`
}

type CraftBulkResult struct {
	Index     int    `json:"index"`
	Success   bool   `json:"success"`
	JobID     string `json:"job_id,omitempty"`
	Recipe    string `json:"recipe,omitempty"`
	Runs      int    `json:"runs,omitempty"`
	Venue     string `json:"venue,omitempty"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

type CraftBulkSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

type CraftBulkResponse struct {
	Action  string            `json:"action"`
	Mode    string            `json:"mode"`
	Results []CraftBulkResult `json:"results"`
	Summary CraftBulkSummary  `json:"summary"`
}

type CraftDryRunResponse struct {
	Action              string          `json:"action"`
	DryRun              bool            `json:"dry_run"`
	Recipe              string          `json:"recipe"`
	Mode                string          `json:"mode"`
	Quantity            int             `json:"quantity"`
	Runs                int             `json:"runs"`
	Venue               string          `json:"venue"`
	VenueType           string          `json:"venue_type"`
	FacilityID          string          `json:"facility_id"`
	Cost                EscrowCost      `json:"cost"`
	CreditsTotal        int             `json:"credits_total"`
	HaveInputs          bool            `json:"have_inputs"`
	HaveCredits         bool            `json:"have_credits"`
	EffectiveTimePerRun float64         `json:"effective_time_per_run"`
	EstCompletionTick   int             `json:"est_completion_tick"`
	Message             string          `json:"message"`
	Produces            []CraftProduces `json:"produces,omitempty"`
	External            bool            `json:"external,omitempty"`
}

type CraftingUpdateDeposit struct {
	ItemID   string `json:"item_id"`
	ItemName string `json:"item_name"`
	Quantity int    `json:"quantity"`
}

type CraftingUpdateJob struct {
	JobID         string                  `json:"job_id"`
	Recipe        string                  `json:"recipe"`
	Mode          string                  `json:"mode"`
	Venue         string                  `json:"venue"`
	Storage       string                  `json:"storage"`
	Deposited     []CraftingUpdateDeposit `json:"deposited"`
	RunsDone      int                     `json:"runs_done"`
	RunsRemaining int                     `json:"runs_remaining"`
	Completed     bool                    `json:"completed"`
}

type CraftingUpdateEvent struct {
	Tick int                 `json:"tick"`
	Jobs []CraftingUpdateJob `json:"jobs"`
}

// RecycleResponse reuses the queued-job shape; recycle has no preset and no XP.
type RecycleResponse = CraftJobQueued
```

Steps:

- [ ] 1. Write failing test `pkg/game/serverapi/crafting_responses_test.go` (uses fixtures matching the openapi oneOf shapes):

```go
package serverapi

import (
	"encoding/json"
	"testing"
)

func TestDecodeCraftJobQueued(t *testing.T) {
	raw := `{"action":"craft","job_id":"j1","recipe":"basic_iron_smelting","mode":"craft","venue":"Station Workshop","venue_type":"workshop","facility_id":"","runs":5,"effective_time_per_run":12.5,"est_completion_tick":1042,"escrowed":{"fee":0,"labor":0,"inputs":[{"item_id":"iron_ore","name":"Iron Ore","quantity":50}]},"message":"queued"}`
	var r CraftJobQueued
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.JobID != "j1" || r.Runs != 5 || r.EffectiveTimePerRun != 12.5 {
		t.Fatalf("bad decode: %+v", r)
	}
	if len(r.Escrowed.Inputs) != 1 || r.Escrowed.Inputs[0].ItemID != "iron_ore" || r.Escrowed.Inputs[0].Quantity != 50 {
		t.Fatalf("bad escrow inputs: %+v", r.Escrowed)
	}
}

func TestDecodeCraftQueueListing(t *testing.T) {
	raw := `{"action":"queue","jobs":[{"job_id":"j1","recipe":"r","mode":"craft","runs_total":10,"runs_done":3,"runs_remaining":7,"progress":0.3,"eta_ticks":40,"position":1,"orderer":"me","status":"running","facility_id":"f1"}]}`
	var r CraftQueueListing
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Jobs) != 1 || r.Jobs[0].RunsRemaining != 7 || r.Jobs[0].Progress != 0.3 {
		t.Fatalf("bad decode: %+v", r)
	}
}

func TestDecodeCraftBulkResponse(t *testing.T) {
	raw := `{"action":"craft","mode":"bulk","results":[{"index":0,"success":true,"job_id":"j1"},{"index":1,"success":false,"error":"no inputs","error_code":"insufficient_inputs"}],"summary":{"total":2,"succeeded":1,"failed":1}}`
	var r CraftBulkResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.Summary.Failed != 1 || len(r.Results) != 2 || r.Results[1].ErrorCode != "insufficient_inputs" {
		t.Fatalf("bad decode: %+v", r)
	}
}

func TestDecodeCraftDryRunResponse(t *testing.T) {
	raw := `{"action":"craft","dry_run":true,"recipe":"r","mode":"craft","quantity":20,"runs":2,"venue":"Workshop","venue_type":"workshop","facility_id":"","cost":{"fee":0,"labor":0,"inputs":[{"item_id":"iron_ore","name":"Iron Ore","quantity":200}]},"credits_total":0,"have_inputs":false,"have_credits":true,"effective_time_per_run":10,"est_completion_tick":1050,"message":"quote"}`
	var r CraftDryRunResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if !r.DryRun || r.Quantity != 20 || r.HaveInputs {
		t.Fatalf("bad decode: %+v", r)
	}
}

func TestDecodeCraftingUpdateEvent(t *testing.T) {
	raw := `{"tick":1043,"jobs":[{"job_id":"j1","recipe":"r","mode":"craft","venue":"Workshop","storage":"station","deposited":[{"item_id":"steel_plate","item_name":"Steel Plate","quantity":1}],"runs_done":1,"runs_remaining":4,"completed":false}]}`
	var r CraftingUpdateEvent
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.Tick != 1043 || len(r.Jobs) != 1 || r.Jobs[0].Deposited[0].ItemName != "Steel Plate" {
		t.Fatalf("bad decode: %+v", r)
	}
	if r.Jobs[0].Completed {
		t.Fatalf("expected not completed: %+v", r.Jobs[0])
	}
}

func TestRecycleResponseAliasesCraftJobQueued(t *testing.T) {
	var r RecycleResponse
	r.JobID = "j9"
	if CraftJobQueued(r).JobID != "j9" {
		t.Fatal("RecycleResponse is not an alias of CraftJobQueued")
	}
}
```

- [ ] 2. Run-to-fail: `go test ./pkg/game/serverapi/ -run 'TestDecodeCraft|TestDecodeCrafting|TestRecycle'`. Expect: `undefined: CraftJobQueued` etc.
- [ ] 3. Add all structs from the Interfaces block above into `pkg/game/serverapi/responses.go`, immediately after `CraftSourceItem` (ends at :1422). Do NOT modify `CraftResponse`/`CraftOutput`/`CraftSourceItem`.
- [ ] 4. Run-to-pass: `go test ./pkg/game/serverapi/ -run 'TestDecodeCraft|TestDecodeCrafting|TestRecycle'`. Expect: `ok`.
- [ ] 5. `go build ./...` and `go test ./...` (ignore `pkg/galaxy`).
- [ ] 6. Commit:

```
git add -A && git commit -m 'feat(crafting): add v0.389 craft/recycle/crafting_update response structs

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>'
```

---

## Task 3 — Fix `CraftWithOptions` semantics

**Files**
- modify `pkg/game/crafting.go` (`CraftWithOptions` :115-149; docstring of `CraftWithQuantity` :108)
- create `pkg/game/crafting_test.go` (if absent) or add to it

**Interfaces**
- Consumes: nothing new.
- Produces: `func (c *Client) CraftWithOptions(ctx context.Context, recipeID string, quantity int, deliverTo string) error` — now: no `MaxCraftBatchSize` clamp; `quantity` validated only as `>= 1`; payload always sets `quantity`; `deliver_to` only `""|storage|faction`; submit uses `WithTerminator(terminateOnActionOrOK)`.

Steps:

- [ ] 1. Write failing test in `pkg/game/crafting_test.go` capturing the sent payload via `sendOverride`:

```go
package game

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestCraftWithOptionsPayload(t *testing.T) {
	c := &Client{}
	var sent protocol.Message
	c.sendOverride = func(_ context.Context, msg protocol.Message) error {
		sent = msg
		return nil
	}
	// quantity above the old skill-based cap must NOT error now.
	_ = c.CraftWithOptions(context.Background(), "basic_iron_smelting", 250, "faction")
	if sent.Type != "craft" {
		t.Fatalf("type = %q", sent.Type)
	}
	if got := sent.Payload["recipe_id"]; got != "basic_iron_smelting" {
		t.Fatalf("recipe_id = %v", got)
	}
	if got := sent.Payload["quantity"]; got != 250 {
		t.Fatalf("quantity = %v, want 250", got)
	}
	if got := sent.Payload["deliver_to"]; got != "faction" {
		t.Fatalf("deliver_to = %v", got)
	}
}

func TestCraftWithOptionsRejectsBadQuantity(t *testing.T) {
	c := &Client{}
	if err := c.CraftWithOptions(context.Background(), "r", 0, ""); err == nil {
		t.Fatal("expected error for quantity 0")
	}
}
```

> Note: verify `sendOverride` is the hook `Submit` honors when no live connection exists; it is declared at `client.go:127`. If `Submit` requires more wiring for a bare `&Client{}`, follow the existing `pkg/game` unit-test pattern that exercises `sendOverride` (grep `sendOverride` in `pkg/game/*_test.go`) and mirror its setup.

- [ ] 2. Run-to-fail: `go test ./pkg/game/ -run TestCraftWithOptions`. Expect failure: old code clamps `quantity` to `MaxCraftBatchSize` and rejects 250.
- [ ] 3. Replace `CraftWithOptions` and the `CraftWithQuantity` docstring in `pkg/game/crafting.go`:

```go
// CraftWithQuantity queues a crafting job for the given recipe. quantity is the
// number of output items wanted; the server rounds it up to whole production
// runs. Inputs are escrowed from station storage and output is delivered there.
func (c *Client) CraftWithQuantity(ctx context.Context, recipeID string, quantity int) error {
	return c.CraftWithOptions(ctx, recipeID, quantity, "")
}

// CraftWithOptions queues a crafting job. quantity is the number of output items
// wanted (server rounds up to whole runs). deliverTo may be "" (server default:
// station storage) or "faction" (faction storage; requires a Faction Workshop
// facility and manage-treasury permission, pulling inputs from faction storage).
// Crafting is async: the server replies with a single ok job frame and delivers
// output later via crafting_update; this method returns once the job is queued.
func (c *Client) CraftWithOptions(ctx context.Context, recipeID string, quantity int, deliverTo string) error {
	if quantity < 1 {
		return fmt.Errorf("invalid quantity: %d (must be >= 1)", quantity)
	}

	payload := map[string]any{
		"recipe_id": recipeID,
		"quantity":  quantity,
	}
	if deliverTo != "" {
		payload["deliver_to"] = deliverTo
	}

	msg := protocol.Message{
		Type:      "craft",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	// v0.389: craft is async-queued. The server replies with a single
	// non-pending ok carrying the job body; there is no action_result. Treat
	// that ok as terminal via terminateOnActionOrOK.
	h, err := c.Submit(ctx, msg, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}
```

- [ ] 4. Run-to-pass: `go test ./pkg/game/ -run TestCraftWithOptions`. Expect `ok`. (`MaxCraftBatchSize` is now used only by `CraftItems`/`craftRecipe`; it stays until Task 10 — do not delete it here.)
- [ ] 5. `go build ./...` and `go test ./...` (ignore `pkg/galaxy`).
- [ ] 6. Commit:

```
git add -A && git commit -m 'fix(crafting): make CraftWithOptions async queue-aware; drop batch clamp and cargo

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>'
```

---

## Task 4 — Add `Recycle` client method(s) + interface + MCP mirror + both mock stubs

**Files**
- create `pkg/game/crafting_recycle.go`
- create `pkg/game/crafting_recycle_test.go`
- modify `pkg/game/interface.go` (add to `GameClient`, Crafting block :54-57)
- modify `pkg/game/mcp_game_client_commands.go` (mirror, after `CraftWithOptions` :409)
- modify `pkg/agent/runner_test.go` (mock stub, near craft stubs :174-181)
- modify `pkg/skills/client_dispatcher_test.go` (mock stub, near craft stubs :476-481)

**Interfaces** (Produces — consumed by Task 7):

```go
func (c *Client) Recycle(ctx context.Context, recipeID string, quantity int) error
func (c *Client) RecycleWithOptions(ctx context.Context, recipeID string, quantity int, deliverTo string) error
```

Steps:

- [ ] 1. Write failing test `pkg/game/crafting_recycle_test.go`:

```go
package game

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

func TestRecycleWithOptionsPayload(t *testing.T) {
	c := &Client{}
	var sent protocol.Message
	c.sendOverride = func(_ context.Context, msg protocol.Message) error {
		sent = msg
		return nil
	}
	_ = c.RecycleWithOptions(context.Background(), "basic_iron_smelting", 20, "")
	if sent.Type != "recycle" {
		t.Fatalf("type = %q", sent.Type)
	}
	if sent.Payload["recipe_id"] != "basic_iron_smelting" || sent.Payload["quantity"] != 20 {
		t.Fatalf("payload = %v", sent.Payload)
	}
	if _, ok := sent.Payload["deliver_to"]; ok {
		t.Fatalf("deliver_to should be omitted when empty: %v", sent.Payload)
	}
}

func TestRecycleRejectsBadQuantity(t *testing.T) {
	c := &Client{}
	if err := c.RecycleWithOptions(context.Background(), "r", 0, ""); err == nil {
		t.Fatal("expected error for quantity 0")
	}
}
```

- [ ] 2. Run-to-fail: `go test ./pkg/game/ -run TestRecycle`. Expect: `undefined: c.RecycleWithOptions`.
- [ ] 3. Create `pkg/game/crafting_recycle.go`:

```go
package game

import (
	"context"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// Recycle queues a recycling job: it consumes the recipe's outputs and returns
// a lossy fraction of its inputs over subsequent ticks. quantity is the number
// of output items to feed in (rounded up to whole recycling runs).
func (c *Client) Recycle(ctx context.Context, recipeID string, quantity int) error {
	return c.RecycleWithOptions(ctx, recipeID, quantity, "")
}

// RecycleWithOptions queues a recycling job with an optional delivery target.
// deliverTo may be "" (server default: station storage) or "faction" (requires
// manage-treasury permission). Like craft, recycle is async-queued: the server
// replies with a single ok job frame and delivers reclaimed inputs later via
// crafting_update.
func (c *Client) RecycleWithOptions(ctx context.Context, recipeID string, quantity int, deliverTo string) error {
	if quantity < 1 {
		return fmt.Errorf("invalid quantity: %d (must be >= 1)", quantity)
	}

	payload := map[string]any{
		"recipe_id": recipeID,
		"quantity":  quantity,
	}
	if deliverTo != "" {
		payload["deliver_to"] = deliverTo
	}

	msg := protocol.Message{
		Type:      "recycle",
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
	h, err := c.Submit(ctx, msg, WithTerminator(terminateOnActionOrOK), WithTimeout(SleepTick*3))
	if err == nil {
		_, err = h.Result(ctx)
	}
	return err
}
```

- [ ] 4. Run-to-pass (unit): `go test ./pkg/game/ -run TestRecycle`. Expect `ok`.
- [ ] 5. Add to `pkg/game/interface.go` Crafting block (after `CraftWithOptions` at :56):

```go
	Recycle(ctx context.Context, recipeID string, quantity int) error
	RecycleWithOptions(ctx context.Context, recipeID string, quantity int, deliverTo string) error
```

- [ ] 6. Mirror in `pkg/game/mcp_game_client_commands.go`, after `CraftWithOptions` (:409):

```go
func (m *MCPGameClient) Recycle(ctx context.Context, recipeID string, quantity int) error {
	return m.RecycleWithOptions(ctx, recipeID, quantity, "")
}

func (m *MCPGameClient) RecycleWithOptions(ctx context.Context, recipeID string, quantity int, deliverTo string) error {
	payload := map[string]any{
		"recipe_id": recipeID,
		"quantity":  quantity,
	}
	if deliverTo != "" {
		payload["deliver_to"] = deliverTo
	}
	result, err := m.callTool(ctx, "recycle", payload)
	if err != nil {
		return err
	}
	return m.updateStateFromResult(result)
}
```

- [ ] 7. Add mock stubs in `pkg/agent/runner_test.go` (after the craft stubs at :181):

```go
func (m *mockGameClient) Recycle(ctx context.Context, recipeID string, quantity int) error {
	m.actionsRecorded = append(m.actionsRecorded, "recycle:"+recipeID)
	return nil
}
func (m *mockGameClient) RecycleWithOptions(ctx context.Context, recipeID string, quantity int, deliverTo string) error {
	m.actionsRecorded = append(m.actionsRecorded, "recycle:"+recipeID)
	return nil
}
```

- [ ] 8. Add mock stubs in `pkg/skills/client_dispatcher_test.go` (after the craft stubs at :481):

```go
func (m *mockGameClient) Recycle(ctx context.Context, recipeID string, quantity int) error { return nil }
func (m *mockGameClient) RecycleWithOptions(ctx context.Context, recipeID string, quantity int, deliverTo string) error {
	return nil
}
```

- [ ] 9. `go build ./...` then `go test ./...` — the latter is REQUIRED to prove both mocks satisfy the widened `GameClient` interface (ignore `pkg/galaxy`).
- [ ] 10. Commit:

```
git add -A && git commit -m 'feat(crafting): add Recycle client method, interface, MCP mirror, mocks

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>'
```

---

## Task 5 — `play_as` craft parser update

**Files**
- modify `cmd/tools/play_as/main.go` (craft REPL case :5767-5789; craft help line :8240)

**Interfaces**
- Consumes: `client.CraftWithOptions`, `client.RawCommand` (for `--dry_run`, `--facility_id`, `--preset`, `action=queue`, `--jobs`).
- Produces: a craft REPL case that builds the right command. Single craft with extra flags uses `client.RawCommand(ctx, "craft", payload)` so the new fields reach the server; the plain `craft <recipe> [qty]` path keeps using `CraftWithOptions` (which sets terminator correctly). `action=queue` and dry-run also go through `RawCommand`.

Steps:

- [ ] 1. (Parser is in the REPL dispatch — exercised via build + manual; no unit test harness exists for the REPL string parser. The verification step is build + `go test ./...` + a documented manual REPL check.) Read the current craft case at :5767.

- [ ] 2. Replace the craft REPL case with:

```go
	case "craft":
		craftArgs, flags := partitionFlags(parts[1:])
		// `craft queue` (or --action=queue) lists current jobs instead of queuing.
		if (len(craftArgs) >= 1 && craftArgs[0] == "queue") || flags["action"] == "queue" {
			return client.RawCommand(ctx, "craft", map[string]any{"action": "queue"})
		}
		if len(craftArgs) < 1 {
			return fmt.Errorf("usage: craft <recipe-id> [quantity] [--deliver_to=storage|faction] [--facility_id=ID] [--preset=fast|cheap|workshop] [--dry_run] | craft queue")
		}
		recipeID := craftArgs[0]
		qty := 1
		if len(craftArgs) >= 2 {
			n, err := strconv.Atoi(craftArgs[1])
			if err != nil {
				return fmt.Errorf("invalid quantity: %w", err)
			}
			qty = n
		}
		deliverTo := flags["deliver_to"]
		switch deliverTo {
		case "", "storage", "faction":
		default:
			return fmt.Errorf("invalid deliver_to %q (must be storage or faction)", deliverTo)
		}
		preset := flags["preset"]
		switch preset {
		case "", "fast", "cheap", "workshop":
		default:
			return fmt.Errorf("invalid preset %q (must be fast, cheap, or workshop)", preset)
		}
		_, dryRun := flags["dry_run"]
		facilityID := flags["facility_id"]
		// Fast path: plain craft with no advanced flags uses the typed client
		// method (correct async terminator, validated quantity).
		if !dryRun && preset == "" && facilityID == "" {
			return simpleCommand(client, func(ctx context.Context) error {
				return client.CraftWithOptions(ctx, recipeID, qty, deliverTo)
			}, ctx, 5*time.Second, cmd, format)
		}
		// Advanced path: build the full payload and submit generically.
		payload := map[string]any{"recipe_id": recipeID, "quantity": qty}
		if deliverTo != "" {
			payload["deliver_to"] = deliverTo
		}
		if facilityID != "" {
			payload["facility_id"] = facilityID
		}
		if preset != "" {
			payload["preset"] = preset
		}
		if dryRun {
			payload["dry_run"] = true
		}
		return client.RawCommand(ctx, "craft", payload)
```

> Confirm `partitionFlags` parses a bare `--dry_run` into a present-but-empty key (so `flags["dry_run"]` exists). If it requires `--dry_run=true`, adjust the `_, dryRun := flags["dry_run"]` check to `flags["dry_run"] == "true" || _, ok := flags["dry_run"]; ok` per `partitionFlags`' actual contract (grep its definition in `main.go`).

- [ ] 3. Update craft help (line :8240) — remove `cargo`, add new flags:

```go
		"  craft <recipe> [qty] [--deliver_to=storage|faction] [--facility_id=ID] [--preset=fast|cheap|workshop] [--dry_run] - Queue a crafting job\n" +
		"  craft queue - List your current crafting jobs\n" +
```

- [ ] 4. `go build ./...` and `go test ./...` (ignore `pkg/galaxy`). Manual REPL check (document in commit, not automated): `craft basic_iron_smelting 20 --dry_run` produces a quote, `craft queue` lists jobs.
- [ ] 5. Commit:

```
git add -A && git commit -m 'feat(play_as): craft parser supports dry_run/facility_id/preset/queue; drop cargo

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>'
```

---

## Task 6 — `play_as` `formatCraft` rewrite (decode all four variants) + dispatch

**Files**
- modify `cmd/tools/play_as/main.go` (`formatCraft` :3565-3651; styled dispatch `case "craft"` :709)

**Interfaces**
- Consumes: `serverapi.CraftJobQueued`, `CraftQueueListing`, `CraftBulkResponse`, `CraftDryRunResponse` (Task 2). (May decode locally with anonymous structs to keep `play_as` free of imports it lacks — but prefer importing `serverapi` if already imported; grep the import block.)
- Produces: a `formatCraft(raw []byte) string` that probes which variant arrived and renders it. The `craft queue` command routes through the same `case "craft"` styled dispatch.

Steps:

- [ ] 1. Read the current `formatCraft` (:3565) and the styled dispatch `case "craft"` (:709). Confirm `serverapi` is imported in `main.go` (`grep '"github.com/rsned/spacemolt/pkg/game/serverapi"' cmd/tools/play_as/main.go`). If imported, decode into the `serverapi` structs; if not, use local anonymous structs with identical JSON tags. The code below uses local structs to avoid coupling assumptions.

- [ ] 2. Replace `formatCraft` with a variant-dispatching version:

```go
// formatCraft renders the v0.389 craft/recycle job responses. The server returns
// one of four shapes (single queued job, queue listing, bulk results, dry-run
// quote); we probe distinguishing keys and render the matching one.
func formatCraft(raw []byte) string {
	raw = unwrapActionResult(raw)
	var probe struct {
		Action string          `json:"action"`
		DryRun bool            `json:"dry_run"`
		Jobs   json.RawMessage `json:"jobs"`
		Results json.RawMessage `json:"results"`
		JobID  string          `json:"job_id"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	switch {
	case probe.DryRun:
		return formatCraftDryRun(raw)
	case len(probe.Results) > 0:
		return formatCraftBulk(raw)
	case len(probe.Jobs) > 0:
		return formatCraftQueue(raw)
	case probe.JobID != "":
		return formatCraftJobQueued(raw)
	}
	return ""
}

func formatCraftJobQueued(raw []byte) string {
	var r struct {
		JobID               string  `json:"job_id"`
		Recipe              string  `json:"recipe"`
		Mode                string  `json:"mode"`
		Venue               string  `json:"venue"`
		Runs                int     `json:"runs"`
		EffectiveTimePerRun float64 `json:"effective_time_per_run"`
		EstCompletionTick   int     `json:"est_completion_tick"`
		Message             string  `json:"message"`
		Escrowed            struct {
			Fee    int `json:"fee"`
			Labor  int `json:"labor"`
			Inputs []struct {
				Name     string `json:"name"`
				ItemID   string `json:"item_id"`
				Quantity int    `json:"quantity"`
			} `json:"inputs"`
		} `json:"escrowed"`
		Produces []struct {
			Name     string `json:"name"`
			Quantity int    `json:"quantity"`
		} `json:"produces"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🛠  Queued %s — job %s @ %s (%d runs, ~%.0fs/run, ETA tick %d)\n",
		r.Recipe, r.JobID, r.Venue, r.Runs, r.EffectiveTimePerRun, r.EstCompletionTick)
	for _, p := range r.Produces {
		fmt.Fprintf(&b, "  → produces %s x %d\n", p.Name, p.Quantity)
	}
	if len(r.Escrowed.Inputs) > 0 {
		b.WriteString("  Escrowed inputs:\n")
		for _, in := range r.Escrowed.Inputs {
			fmt.Fprintf(&b, "    %d x %s\n", in.Quantity, in.Name)
		}
	}
	if r.Escrowed.Fee > 0 || r.Escrowed.Labor > 0 {
		fmt.Fprintf(&b, "  Escrowed credits: labor %d + fee %d\n", r.Escrowed.Labor, r.Escrowed.Fee)
	}
	if r.Message != "" {
		fmt.Fprintf(&b, "  %s\n", r.Message)
	}
	return b.String()
}

func formatCraftQueue(raw []byte) string {
	var r struct {
		Jobs []struct {
			JobID         string  `json:"job_id"`
			Recipe        string  `json:"recipe"`
			RunsDone      int     `json:"runs_done"`
			RunsRemaining int     `json:"runs_remaining"`
			RunsTotal     int     `json:"runs_total"`
			Progress      float64 `json:"progress"`
			ETATicks      int     `json:"eta_ticks"`
			Position      int     `json:"position"`
			Status        string  `json:"status"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return ""
	}
	var b strings.Builder
	if len(r.Jobs) == 0 {
		return "🛠  Crafting queue: (empty)\n"
	}
	fmt.Fprintf(&b, "🛠  Crafting queue (%d jobs):\n", len(r.Jobs))
	for _, j := range r.Jobs {
		fmt.Fprintf(&b, "  #%d %s [%s] %d/%d runs (%.0f%%) ETA %d ticks — %s\n",
			j.Position, j.Recipe, j.JobID, j.RunsDone, j.RunsTotal, j.Progress*100, j.ETATicks, j.Status)
	}
	return b.String()
}

func formatCraftBulk(raw []byte) string {
	var r struct {
		Results []struct {
			Index     int    `json:"index"`
			Success   bool   `json:"success"`
			JobID     string `json:"job_id"`
			Recipe    string `json:"recipe"`
			Runs      int    `json:"runs"`
			Error     string `json:"error"`
			ErrorCode string `json:"error_code"`
		} `json:"results"`
		Summary struct {
			Total     int `json:"total"`
			Succeeded int `json:"succeeded"`
			Failed    int `json:"failed"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🛠  Bulk craft: %d total, %d ok, %d failed\n",
		r.Summary.Total, r.Summary.Succeeded, r.Summary.Failed)
	for _, res := range r.Results {
		if res.Success {
			fmt.Fprintf(&b, "  ✅ [%d] %s job %s (%d runs)\n", res.Index, res.Recipe, res.JobID, res.Runs)
		} else {
			fmt.Fprintf(&b, "  ❌ [%d] %s — %s (%s)\n", res.Index, res.Recipe, res.Error, res.ErrorCode)
		}
	}
	return b.String()
}

func formatCraftDryRun(raw []byte) string {
	var r struct {
		Recipe              string  `json:"recipe"`
		Quantity            int     `json:"quantity"`
		Runs                int     `json:"runs"`
		Venue               string  `json:"venue"`
		CreditsTotal        int     `json:"credits_total"`
		HaveInputs          bool    `json:"have_inputs"`
		HaveCredits         bool    `json:"have_credits"`
		EffectiveTimePerRun float64 `json:"effective_time_per_run"`
		EstCompletionTick   int     `json:"est_completion_tick"`
		Message             string  `json:"message"`
		Cost                struct {
			Fee    int `json:"fee"`
			Labor  int `json:"labor"`
			Inputs []struct {
				Name     string `json:"name"`
				Quantity int    `json:"quantity"`
			} `json:"inputs"`
		} `json:"cost"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📋 Dry run: %s x%d → %d runs @ %s (~%.0fs/run, ETA tick %d)\n",
		r.Recipe, r.Quantity, r.Runs, r.Venue, r.EffectiveTimePerRun, r.EstCompletionTick)
	if len(r.Cost.Inputs) > 0 {
		b.WriteString("  Inputs needed:\n")
		for _, in := range r.Cost.Inputs {
			fmt.Fprintf(&b, "    %d x %s\n", in.Quantity, in.Name)
		}
	}
	fmt.Fprintf(&b, "  Credits: %d (labor %d + fee %d)\n", r.CreditsTotal, r.Cost.Labor, r.Cost.Fee)
	okMark := func(ok bool) string {
		if ok {
			return "✅"
		}
		return "❌"
	}
	fmt.Fprintf(&b, "  Have inputs: %s   Have credits: %s\n", okMark(r.HaveInputs), okMark(r.HaveCredits))
	if r.Message != "" {
		fmt.Fprintf(&b, "  %s\n", r.Message)
	}
	return b.String()
}
```

- [ ] 3. The styled dispatch `case "craft":` at :709 already calls `formatCraft(raw)` — no change needed there (it now covers `craft queue` too, since that response routes through the same command name).

- [ ] 4. `go build ./...` and `go test ./...` (ignore `pkg/galaxy`).
- [ ] 5. Commit:

```
git add -A && git commit -m 'feat(play_as): formatCraft renders all four v0.389 craft job variants

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>'
```

---

## Task 7 — `play_as` `recycle` command + formatter (reuse craft rendering)

**Files**
- modify `cmd/tools/play_as/main.go` (add `case "recycle"` near the craft case ~:5790; add styled dispatch `case "recycle"` near :709; add recycle help near craft help :8240)

**Interfaces**
- Consumes: `client.RecycleWithOptions` (Task 4), `client.RawCommand`, `formatCraft` (Task 6 — recycle responses share the `CraftJobQueued`/queue/bulk shapes).
- Produces: a `recycle` REPL command and a `case "recycle": return formatCraft(raw)` dispatch entry (DRY: recycle has no preset and no distinct response shape, so it reuses craft rendering).

Steps:

- [ ] 1. Add the recycle REPL case (after the craft case):

```go
	case "recycle":
		recArgs, flags := partitionFlags(parts[1:])
		if (len(recArgs) >= 1 && recArgs[0] == "queue") || flags["action"] == "queue" {
			// recycle jobs appear in the shared craft queue.
			return client.RawCommand(ctx, "craft", map[string]any{"action": "queue"})
		}
		if len(recArgs) < 1 {
			return fmt.Errorf("usage: recycle <recipe-id> [quantity] [--deliver_to=storage|faction] [--facility_id=ID] [--dry_run]")
		}
		recipeID := recArgs[0]
		qty := 1
		if len(recArgs) >= 2 {
			n, err := strconv.Atoi(recArgs[1])
			if err != nil {
				return fmt.Errorf("invalid quantity: %w", err)
			}
			qty = n
		}
		deliverTo := flags["deliver_to"]
		switch deliverTo {
		case "", "storage", "faction":
		default:
			return fmt.Errorf("invalid deliver_to %q (must be storage or faction)", deliverTo)
		}
		_, dryRun := flags["dry_run"]
		facilityID := flags["facility_id"]
		if !dryRun && facilityID == "" {
			return simpleCommand(client, func(ctx context.Context) error {
				return client.RecycleWithOptions(ctx, recipeID, qty, deliverTo)
			}, ctx, 5*time.Second, cmd, format)
		}
		payload := map[string]any{"recipe_id": recipeID, "quantity": qty}
		if deliverTo != "" {
			payload["deliver_to"] = deliverTo
		}
		if facilityID != "" {
			payload["facility_id"] = facilityID
		}
		if dryRun {
			payload["dry_run"] = true
		}
		return client.RawCommand(ctx, "recycle", payload)
```

- [ ] 2. Add the styled dispatch entry near the `case "craft":` registration (:709):

```go
	case "recycle":
		return formatCraft(raw)
```

- [ ] 3. Add recycle help near craft help (:8240):

```go
		"  recycle <recipe> [qty] [--deliver_to=storage|faction] [--facility_id=ID] [--dry_run] - Queue a recycling job (lossy)\n" +
```

- [ ] 4. `go build ./...` and `go test ./...` (ignore `pkg/galaxy`).
- [ ] 5. Commit:

```
git add -A && git commit -m 'feat(play_as): add recycle command reusing craft job rendering

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>'
```

---

## Task 8 — Facility job/business sub-action formatters + help text

**Files**
- modify `cmd/tools/play_as/main.go` (`formatFacility` switch :1502; add `job_list` formatter reusing `formatCraftQueue`; add small formatters for `job_add`/`job_cancel`/`job_reorder`/`set_output_price`/`set_access`/`upgrade`; facility help :7214)

**Interfaces**
- Consumes: `formatCraftQueue` (Task 6 — `facility job_list` returns the same `jobs[]` queue-listing shape as `craft action=queue`). The facility REPL command already exists and passes through via `RawCommand`; these sub-actions only need parser-flag plumbing already present (`--facility_id`, `--recipe_id`, `--quantity`, `--job_id`, `--position`, `--item_id`, `--price`, `--access`) and formatter rendering.
- Produces: `formatFacility` switch cases for `job_list` (and graceful default for the action-result mutations).

Steps:

- [ ] 1. Confirm the facility REPL command already forwards arbitrary `--flags` into the payload (grep the `case "facility":` block, ~:7382 region passes `args` to `RawCommand(ctx, "facility", args)`). If `job_add`/`job_cancel`/etc. flags are not already plumbed, add them to the facility arg builder so `recipe_id`, `quantity`, `job_id`, `position`, `item_id`, `price`, `access`, `facility_id` all reach the payload. (Field names verified against `/facility` openapi request body.)

- [ ] 2. Add `job_list` to the `formatFacility` switch (after `case "owned":` at :1515). `facility job_list` returns `{action, jobs:[...]}` — the same shape `formatCraftQueue` already renders:

```go
	case "job_list":
		return formatCraftQueue(unwrapActionResult(raw))
```

- [ ] 3. Add a small generic formatter for the mutation sub-actions (`job_add`, `job_cancel`, `job_reorder`, `set_output_price`, `set_access`, `upgrade`) that surfaces the server `message`. Add a switch case covering them:

```go
	case "job_add", "job_cancel", "job_reorder", "set_output_price", "set_access", "upgrade":
		return formatFacilityActionMessage(raw)
```

and the helper:

```go
// formatFacilityActionMessage renders the simple {action, message, ...} result
// of facility job/business mutations (job_add, job_cancel, job_reorder,
// set_output_price, set_access, upgrade). It shows the action and the server's
// human message, falling back to "" so the caller prints JSON when absent.
func formatFacilityActionMessage(raw []byte) string {
	var r struct {
		Action     string `json:"action"`
		Message    string `json:"message"`
		JobID      string `json:"job_id"`
		FacilityID string `json:"facility_id"`
	}
	if err := json.Unmarshal(unwrapActionResult(raw), &r); err != nil {
		return ""
	}
	if r.Message == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🏭 facility %s: %s", r.Action, r.Message)
	if r.JobID != "" {
		fmt.Fprintf(&b, " (job %s)", r.JobID)
	}
	b.WriteString("\n")
	return b.String()
}
```

> Note: the `produces`/`escrowed` body of a `job_add` may match the single-queued-job shape. If `formatFacilityActionMessage` returns "" for a populated `job_add` (no top-level `message`), the JSON fallback in `printResponse` still renders it — acceptable. Do not over-engineer; the queue is inspectable via `facility job_list`.

- [ ] 4. Update facility help (:7214) to add the new actions:

```go
		"  actions: types, build, list, owned, toggle, upgrades, upgrade,\n" +
		"           job_add, job_list, job_cancel, job_reorder, set_output_price, set_access,\n" +
		"           list_for_sale, browse_for_sale, buy_listing, cancel_listing,\n" +
		"           faction_build, faction_upgrade, faction_list, faction_owned, faction_toggle,\n" +
		"           transfer, personal_build, personal_decorate, personal_visit, help\n" +
		"  job flags: --facility_id ID --recipe_id ID --quantity N --job_id ID --position N\n" +
		"  business flags: --item_id ID --price N --access private|public\n" +
		"  flags:   --show_station_facilities  (list: also show the station's own facilities)"
```

- [ ] 5. `go build ./...` and `go test ./...` (ignore `pkg/galaxy`).
- [ ] 6. Commit:

```
git add -A && git commit -m 'feat(play_as): facility job/business sub-action formatters + help

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>'
```

---

## Task 9 — `crafting_update` push handling WITH `OnCraftingUpdate` callback (promote Task 1 stub)

**Files**
- modify `pkg/game/client.go` (add `onCraftingUpdate` field + `onCraftingMu` near `onChatMessage` :134; `SetOnCraftingUpdate` setter near `SetOnChatMessage` :422; promote the `TypeCraftingUpdate` branch :2347-region to decode + fire callback, mirroring the `TypeChatMessage` fire site :2588)
- modify `pkg/game/client_crafting_update_test.go` (extend Task 1 test file with a callback test)

**Interfaces**
- Consumes: `serverapi.CraftingUpdateEvent` (Task 2).
- Produces: `func (c *Client) SetOnCraftingUpdate(fn func(ev serverapi.CraftingUpdateEvent))`.

Steps:

- [ ] 1. Add a failing callback test to `pkg/game/client_crafting_update_test.go`:

```go
func TestOnCraftingUpdateCallbackFires(t *testing.T) {
	c := newTestClient(t) // use the package's standard test-client constructor
	var got *serverapi.CraftingUpdateEvent
	c.SetOnCraftingUpdate(func(ev serverapi.CraftingUpdateEvent) {
		got = &ev
	})
	resp := protocol.Response{
		Type: protocol.TypeCraftingUpdate,
		Payload: map[string]any{
			"tick": float64(1043),
			"jobs": []any{
				map[string]any{
					"job_id": "j1", "recipe": "r", "mode": "craft",
					"venue": "Workshop", "storage": "station",
					"deposited": []any{
						map[string]any{"item_id": "steel_plate", "item_name": "Steel Plate", "quantity": float64(1)},
					},
					"runs_done": float64(1), "runs_remaining": float64(4), "completed": false,
				},
			},
		},
	}
	c.handleResponse(resp)
	if got == nil {
		t.Fatal("callback not fired")
	}
	if got.Tick != 1043 || len(got.Jobs) != 1 || got.Jobs[0].Deposited[0].ItemName != "Steel Plate" {
		t.Fatalf("bad event: %+v", got)
	}
}
```

> Note: replace `newTestClient(t)` with whatever constructor existing `pkg/game` tests use to build a `*Client` that can run `handleResponse` (grep `func newTestClient` / `&Client{` in `pkg/game/*_test.go`). If `handleResponse` is unexported and callable on a bare `&Client{}` with a non-nil `debugLogger`, set `debugLogger` to `log.New(io.Discard, "", 0)`. Mirror the setup used by the existing `TypeChatMessage`/`TypeMiningYield` tests if present.

- [ ] 2. Run-to-fail: `go test ./pkg/game/ -run TestOnCraftingUpdateCallbackFires`. Expect: `undefined: c.SetOnCraftingUpdate`.

- [ ] 3. Add the field + mutex near `onChatMessage` (`client.go` :134):

```go
	// Crafting update callback — fired when a crafting_update push event is received
	onCraftingUpdate func(ev serverapi.CraftingUpdateEvent)
	onCraftingMu     sync.RWMutex
```

- [ ] 4. Add the setter near `SetOnChatMessage` (:422):

```go
// SetOnCraftingUpdate registers a callback that fires when a crafting_update
// push event is received (server v0.389.0+). This lets consumers track async
// crafting job progress and storage deposits without polling.
func (c *Client) SetOnCraftingUpdate(fn func(ev serverapi.CraftingUpdateEvent)) {
	c.onCraftingMu.Lock()
	defer c.onCraftingMu.Unlock()
	c.onCraftingUpdate = fn
}
```

- [ ] 5. Promote the Task 1 stub branch (`case protocol.TypeCraftingUpdate:` ~:2347) to decode and fire, mirroring `TypeChatMessage` (:2588):

```go
	case protocol.TypeCraftingUpdate:
		// Async crafting progress push (server v0.389.0+). Output items are
		// deposited to station/faction storage server-side. Decode and fire the
		// OnCraftingUpdate callback so consumers can track job progress.
		var ev serverapi.CraftingUpdateEvent
		if data, err := json.Marshal(resp.Payload); err == nil {
			if err := json.Unmarshal(data, &ev); err == nil {
				c.debugLogger.Printf("[CRAFTING_UPDATE] tick=%d jobs=%d", ev.Tick, len(ev.Jobs))
				c.onCraftingMu.RLock()
				cb := c.onCraftingUpdate
				c.onCraftingMu.RUnlock()
				if cb != nil {
					cb(ev)
				}
			}
		}
```

(Remove the Task 1 no-op `debugLogger.Printf("[CRAFTING_UPDATE] tick=%d", ...)` body — this replaces it.)

- [ ] 6. Run-to-pass: `go test ./pkg/game/ -run 'TestCraftingUpdate|TestOnCraftingUpdate'`. Expect `ok`.
- [ ] 7. `go build ./...` and `go test ./...` (ignore `pkg/galaxy`).
- [ ] 8. Commit:

```
git add -A && git commit -m 'feat(crafting): handle crafting_update push with OnCraftingUpdate callback

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>'
```

---

## Task 10 — Rewrite `CraftItems` / `CraftingLoop` around queue-and-monitor; remove `MaxCraftBatchSize`

**Files**
- modify `pkg/game/crafting.go` (`CraftItems` :385-545; delete `MaxCraftBatchSize` :15-27)
- modify `pkg/game/crafting_loop.go` (`craftRecipe` :425-462; remove `MaxCraftBatchSize` caller :431)
- modify/create `pkg/game/crafting_test.go` (test the new `CraftItems` behavior: it does NOT withdraw to cargo and does NOT re-issue duplicate crafts)

**Interfaces**
- Consumes: `client.CraftWithQuantity` (Task 3), `client.SetOnCraftingUpdate` (Task 9), `serverapi.CraftingUpdateEvent`.
- Produces: a `CraftItems` that, per craftable recipe, queues ONE craft for the full computed quantity and returns; and a `craftRecipe` that queues once (no batch loop, no cargo checks, no `MaxCraftBatchSize`). `MaxCraftBatchSize` is deleted (no callers remain).

Steps:

- [ ] 1. Write a failing test in `pkg/game/crafting_test.go` proving the new contract: `CraftItems` issues exactly one craft per craftable recipe and never withdraws to cargo. Use a recording fake at the `Client` level via `sendOverride`, or — preferred, since `CraftItems` calls many client methods — extract the per-recipe queue step into a small testable helper. The minimal-risk approach: assert `MaxCraftBatchSize` is gone and that `craftRecipe` queues once.

Note: `MaxCraftBatchSize` removal is proven by the `grep` assertion (step 9) and `go build` — do NOT add an empty/assertion-less test for it.

```go
func TestCraftRecipeQueuesOnce(t *testing.T) {
	c := &Client{}
	calls := 0
	c.sendOverride = func(_ context.Context, msg protocol.Message) error {
		if msg.Type == "craft" {
			calls++
		}
		return nil
	}
	n, err := craftRecipe(c, log.New(io.Discard, "", 0), context.Background(), "basic_iron_smelting", 200)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("craftRecipe issued %d craft commands, want exactly 1", calls)
	}
	if n != 200 {
		t.Fatalf("craftRecipe reported %d items queued, want 200", n)
	}
}
```

> The test calls `craftRecipe(client, logger, ctx, recipeID, quantity)` — note the NEW signature adds an explicit `quantity int` (the old one computed batches internally). Update the signature accordingly. Add `"io"` and `"log"` imports to the test file if missing.

- [ ] 2. Run-to-fail: `go test ./pkg/game/ -run TestCraftRecipeQueuesOnce`. Expect: signature mismatch / `craftRecipe` takes 4 args not 5.

- [ ] 3. Rewrite `craftRecipe` in `crafting_loop.go` to queue once (no batch loop, no cargo check, no `MaxCraftBatchSize`):

```go
// craftRecipe queues a single crafting job for the given recipe and quantity.
// v0.389: crafting is async-queued from station storage; there is no per-tick
// batch loop and no cargo involvement. Returns the quantity queued.
func craftRecipe(client GameClient, logger *log.Logger, ctx context.Context, recipeID string, quantity int) (int, error) {
	if quantity < 1 {
		return 0, fmt.Errorf("invalid quantity: %d", quantity)
	}
	logger.Printf("🔨 Queuing %d x %s...", quantity, recipeID)
	if err := client.CraftWithQuantity(ctx, recipeID, quantity); err != nil {
		return 0, fmt.Errorf("craft command failed: %w", err)
	}
	logger.Printf("✅ Queued %d x %s (output lands in station storage)", quantity, recipeID)
	return quantity, nil
}
```

- [ ] 4. Update the `craftRecipe` call site inside `CraftingLoop` (grep `craftRecipe(` in `crafting_loop.go`) to pass an explicit quantity (use the recipe's target quantity the surrounding code already computed; default `200` matches the old `batchesPerLoop` intent if no per-recipe figure exists). Remove any now-dead cargo-space checks tied to the old batch loop in the loop's craft step.

- [ ] 5. Rewrite `CraftItems` in `crafting.go` to queue-and-walk-away: deposit cargo to storage once, query craftable recipes from storage, and for each craftable recipe queue ONE craft for `CanCraftQuantity` (no withdraw-to-cargo, no per-batch loop, no final deposit, no re-issue). Replace lines :385-545:

```go
// CraftItems queues crafting jobs for everything currently craftable from the
// station storage at the docked base. v0.389: crafting reads inputs from and
// delivers output to station storage, and runs asynchronously over ticks — so
// this deposits any cargo into storage, queries craftable recipes, and queues
// each one exactly once. It does NOT withdraw to cargo and does NOT re-issue a
// craft to "make progress" (that would double-spend). Output lands in storage
// over the following ticks; consumers can observe it via OnCraftingUpdate or
// `craft action=queue`. Returns the total output quantity queued.
func (c *Client) CraftItems(ctx context.Context, logger *log.Logger, config *CraftingConfig) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	// Deposit any cargo so it counts toward craftable inputs in storage.
	state := c.GetState()
	if len(state.Ship.Cargo) > 0 {
		logger.Printf("📦 Depositing cargo into station storage...")
		if err := c.DepositAllItems(ctx); err != nil {
			logger.Printf("⚠️  Deposit failed: %v", err)
		} else if err := sleepCtx(ctx, SleepTick); err != nil {
			return 0, err
		}
	}

	// Read current storage contents.
	if err := c.ViewStorage(ctx); err != nil {
		return 0, fmt.Errorf("view storage failed: %w", err)
	}
	if err := sleepCtx(ctx, SleepQuick); err != nil {
		return 0, err
	}
	storageItems := c.getStorageItems()
	if len(storageItems) == 0 {
		logger.Printf("ℹ️  Storage is empty, nothing to craft")
		return 0, nil
	}

	// Determine what is craftable from storage.
	components := make([]Component, 0, len(storageItems))
	for itemID, qty := range storageItems {
		components = append(components, Component{ID: itemID, Quantity: qty})
	}
	result, err := c.QueryCraftableFromComponents(ctx, components, config)
	if err != nil {
		return 0, fmt.Errorf("craft query failed: %w", err)
	}
	if len(result.FullyCraftable) == 0 {
		logger.Printf("ℹ️  No craftable recipes from storage contents")
		return 0, nil
	}

	// Queue each craftable recipe exactly once for its full available quantity.
	totalQueued := 0
	for _, recipe := range result.FullyCraftable {
		if err := ctx.Err(); err != nil {
			return totalQueued, err
		}
		if recipe.CanCraftQuantity <= 0 {
			continue
		}
		logger.Printf("🔨 Queuing %d x %s...", recipe.CanCraftQuantity, recipe.RecipeName)
		if err := c.CraftWithQuantity(ctx, recipe.RecipeID, recipe.CanCraftQuantity); err != nil {
			logger.Printf("   ⚠️  Queue failed: %v", err)
			continue
		}
		totalQueued += recipe.CanCraftQuantity
		if err := sleepCtx(ctx, SleepShort); err != nil {
			return totalQueued, err
		}
	}

	logger.Printf("═══ Queued %d items across %d recipes (output lands in storage) ═══",
		totalQueued, len(result.FullyCraftable))
	return totalQueued, nil
}
```

- [ ] 6. Delete `MaxCraftBatchSize` from `crafting.go` (:15-27). Grep first to confirm no callers remain: `grep -rn 'MaxCraftBatchSize' --include=*.go .` should return only the function definition (and any test referencing it — remove those references). Then delete the function.

- [ ] 7. Remove now-unused imports if `strings` (used only by the deleted `action_pending` retry in old `CraftItems`) is no longer referenced in `crafting.go` — run `goimports`/`golangci-lint` to catch it and drop the import.

- [ ] 8. Run-to-pass: `go test ./pkg/game/ -run 'TestCraftRecipeQueuesOnce'`. Expect `ok`.
- [ ] 9. Verify removal: `grep -rn 'MaxCraftBatchSize' --include=*.go .` returns nothing.
- [ ] 10. `golangci-lint run ./pkg/game/...` — no new findings. `go build ./...` and `go test ./...` (ignore `pkg/galaxy`).
- [ ] 11. Commit:

```
git add -A && git commit -m 'refactor(crafting): rewrite agent loops as queue-and-monitor; remove MaxCraftBatchSize

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>'
```

---

## Task 11 — Register new v0.389 actions in the api-monitor + `FacilityOwnedResponse` struct

**Why:** `pkg/game/client_api_monitor.go`'s `actionResponseTypes` map (`:49`) drives the dev-only `[SERVER API CHANGE]` logger. `CheckOKResponseFields` (`:624`) warns "Unhandled action %q" when an OK frame's `action` is unregistered, and "New fields in %q response" when a registered struct's top-level JSON tags don't cover the payload's top-level keys. v0.389 introduced/changed several action responses that are unregistered or mismapped: `owned` (observed live — unhandled), `recycle` (new), `craft` (still mapped to the OLD `CraftResponse`, but the new job shape's top-level keys differ), and facility `job_list`. Only TOP-LEVEL payload keys are checked, so each struct only needs to cover top-level fields.

**Files**
- modify `pkg/game/serverapi/responses.go` (add `FacilityOwnedResponse` + `OwnedFacility` + `FacilityRentSummary` near the other facility structs ~`:741`)
- modify `pkg/game/client_api_monitor.go` (`actionResponseTypes` map `:49` — add/replace entries)
- modify/create `pkg/game/serverapi/crafting_responses_test.go` (decode test) and `pkg/game/client_api_monitor_test.go` (registration test, if a monitor test file exists; else add to the serverapi test)

**Interfaces** (Produces):

```go
type OwnedFacility struct {
	FacilityID        string `json:"facility_id"`
	Name              string `json:"name"`
	Type              string `json:"type"`
	BaseID            string `json:"base_id"`
	BaseName          string `json:"base_name"`
	SystemID          string `json:"system_id"`
	RentPerCycle      int    `json:"rent_per_cycle"`
	LaborPerRun       int    `json:"labor_per_run,omitempty"`
	ArrearsOwed       int    `json:"arrears_owed,omitempty"`
	MissedRentCycles  int    `json:"missed_rent_cycles,omitempty"`
	Active            bool   `json:"active"`
	UnderConstruction bool   `json:"under_construction,omitempty"`
}

type FacilityRentSummary struct {
	Facilities        int    `json:"facilities"`
	TotalRentPerCycle int    `json:"total_rent_per_cycle"`
	EstRentPerDay     int    `json:"est_rent_per_day"`
	ArrearsOwed       int    `json:"arrears_owed,omitempty"`
	GraceCycles       int    `json:"grace_cycles,omitempty"`
	Note              string `json:"note,omitempty"`
}

// FacilityOwnedResponse models the `facility action=owned` OK frame. Top-level
// keys (action, facilities, hint, rent) must be covered so the api-monitor does
// not flag it. Mirrors the local struct in play_as formatFacilityOwned.
type FacilityOwnedResponse struct {
	Action     string              `json:"action"`
	Facilities []OwnedFacility     `json:"facilities"`
	Hint       string              `json:"hint,omitempty"`
	Rent       FacilityRentSummary `json:"rent"`
}
```

Steps:

- [ ] 1. Write a failing decode test (use the REAL observed payload) — add to `pkg/game/serverapi/crafting_responses_test.go`:

```go
func TestDecodeFacilityOwnedResponse(t *testing.T) {
	raw := `{"action":"owned","facilities":[{"active":true,"base_id":"grand_exchange_station","base_name":"Grand Exchange Station","facility_id":"38f50d8a118ff2757ba3aaf0f9119672","name":"Signal Relay","rent_per_cycle":10,"system_id":"haven","type":"signal_relay"}],"hint":"Use action 'list' while docked for full per-facility detail at that station.","rent":{"est_rent_per_day":2580,"facilities":3,"note":"Rent is auto-deducted...","total_rent_per_cycle":30}}`
	var r FacilityOwnedResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.Action != "owned" || len(r.Facilities) != 1 || r.Facilities[0].RentPerCycle != 10 || !r.Facilities[0].Active {
		t.Fatalf("bad decode: %+v", r)
	}
	if r.Rent.TotalRentPerCycle != 30 || r.Rent.EstRentPerDay != 2580 || r.Rent.Facilities != 3 {
		t.Fatalf("bad rent: %+v", r.Rent)
	}
}

func TestActionResponseTypesRegistersV0389Actions(t *testing.T) {
	// This test lives in package game (not serverapi) — see step 4. Placed here
	// only as a reminder; implement it in pkg/game where actionResponseTypes is visible.
}
```

- [ ] 2. Run-to-fail: `go test ./pkg/game/serverapi/ -run TestDecodeFacilityOwnedResponse`. Expect: `undefined: FacilityOwnedResponse`.
- [ ] 3. Add the three structs from the Interfaces block to `pkg/game/serverapi/responses.go` near the other facility structs. Run-to-pass the decode test.
- [ ] 4. Add a registration test in `pkg/game` (where `actionResponseTypes` is package-visible) — append to the existing `pkg/game/client_crafting_update_test.go` (same package `game`):

```go
func TestActionResponseTypesRegistersV0389Actions(t *testing.T) {
	for _, action := range []string{"owned", "recycle", "job_list", "craft"} {
		if _, ok := actionResponseTypes[action]; !ok {
			t.Errorf("actionResponseTypes missing %q", action)
		}
	}
}
```

- [ ] 5. Run-to-fail: `go test ./pkg/game/ -run TestActionResponseTypesRegistersV0389Actions`. Expect failure on `owned`/`recycle`/`job_list`.
- [ ] 6. Update `actionResponseTypes` in `pkg/game/client_api_monitor.go` (`:49`). REPLACE the `"craft"` entry and ADD the others:

```go
	"craft":           reflect.TypeOf(serverapi.CraftJobQueued{}),   // was CraftResponse (old instant shape)
	"recycle":         reflect.TypeOf(serverapi.RecycleResponse{}),
	"owned":           reflect.TypeOf(serverapi.FacilityOwnedResponse{}),
	"job_list":        reflect.TypeOf(serverapi.CraftQueueListing{}),
```

(Place `owned`/`job_list` near the other facility entries `:206-208`; `recycle` near `craft`. `RecycleResponse` is an alias of `CraftJobQueued`, so `reflect.TypeOf` yields `CraftJobQueued` — that is fine.)

> Note: the facility mutation sub-actions (`job_add`, `job_cancel`, `job_reorder`, `set_output_price`, `set_access`) are intentionally LEFT UNREGISTERED here — their live OK-frame shapes are not yet confirmed, and registering a guessed struct would itself trigger "new fields" warnings. The monitor's one-time "Unhandled action" hint when one is first seen IS the intended drift-detection behavior; register them in a follow-up once a real payload is captured. Do not guess.

- [ ] 7. Run-to-pass: `go test ./pkg/game/ -run TestActionResponseTypesRegistersV0389Actions` and `go test ./pkg/game/serverapi/ -run TestDecodeFacilityOwnedResponse`. Expect `ok`.
- [ ] 8. `golangci-lint run ./pkg/game/...` (no new findings); `go build ./...` and `go test ./...` (ignore `pkg/galaxy` + `pkg/actionspace`).
- [ ] 9. Commit:

```
git add -A && git commit -m 'feat(crafting): register owned/recycle/craft/job_list in api-monitor + FacilityOwnedResponse

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>'
```

---

## Task 12 — `formatFacilityList`: render the `production` sub-struct (busyness + cost)

**Why:** `facility list --show_station_facilities` lists production-category station facilities but drops their `production` block, so a player can't see how busy a facility is or what it costs to rent. The local `stationFacility` struct in `formatFacilityList` (`cmd/tools/play_as/main.go` ~`:2029`) has no `production` field, so those details are silently discarded. The station render loop is ~`:2129-2160`.

**Goal of the render:** surface BUSYNESS (`queued_runs`/`queued_items`, `backlog_ticks`, throughput from `items_per_hour` & `ticks_per_run`) and COST (`rental_fee_per_run`, `public`) so a player can decide whether to rent a public facility.

**Files**
- modify `cmd/tools/play_as/main.go` (`formatFacilityList` — `stationFacility` struct + station render loop)
- modify/create `cmd/tools/play_as/facility_format_test.go` (render test)

**Interfaces** — add to the `stationFacility` struct (inside `formatFacilityList`):

```go
		Description string `json:"description"`
		Production  *struct {
			Recipe          string `json:"recipe"`
			RecipeID        string `json:"recipe_id"`
			ItemsPerHour    int    `json:"items_per_hour"`
			OutputPerRun    int    `json:"output_per_run"`
			TicksPerRun     int    `json:"ticks_per_run"`
			QueuedRuns      int    `json:"queued_runs"`
			QueuedItems     int    `json:"queued_items"`
			BacklogTicks    int    `json:"backlog_ticks"`
			RentalFeePerRun int    `json:"rental_fee_per_run"`
			Public          bool   `json:"public"`
		} `json:"production"`
```

(Use a pointer so non-production facilities — which have no `production` key — decode to `nil` and render normally.)

Steps:

- [ ] 1. Write a failing render test in `cmd/tools/play_as/facility_format_test.go` using the REAL observed shape:

```go
func TestFormatFacilityList_ShowsProductionDetails(t *testing.T) {
	showStationFacilities = true
	defer func() { showStationFacilities = false }()
	raw := []byte(`{"base_id":"grand_exchange_station","station_facilities":[{"active":true,"category":"production","description":"Pressurized containment lab...","facility_id":"42eb7b38","level":1,"maintenance_satisfied":true,"name":"Argon Cell Lab","recipe_id":"synthesize_argon_power_cell","type":"argon_cell_lab","production":{"backlog_ticks":0,"items_per_hour":22,"output_per_run":2,"public":true,"queued_items":0,"queued_runs":0,"recipe":"Synthesize Argon Power Cell","rental_fee_per_run":225,"ticks_per_run":32}}]}`)
	out := formatFacilityList(raw)
	for _, want := range []string{"Argon Cell Lab", "Synthesize Argon Power Cell", "225", "22"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatFacilityList output missing %q\n%s", want, out)
		}
	}
}
```

- [ ] 2. Run-to-fail: `go test ./cmd/tools/play_as/ -run TestFormatFacilityList_ShowsProductionDetails`. Expect: the rental fee / throughput / recipe substrings are absent (the production block isn't rendered).
- [ ] 3. Add the `Description` + `Production` fields to the `stationFacility` struct, and in the station render loop emit a detail line for facilities where `f.Production != nil`. Render busyness + cost compactly, e.g. (match the section's existing indentation/style):

```go
		if f.Production != nil {
			p := f.Production
			access := "private"
			if p.Public {
				access = "public"
			}
			fmt.Fprintf(&b, "      ⚙ %s — %d/hr, %d/run, %d ticks/run | rent %d/run | queued %d runs (backlog %d ticks) | %s\n",
				p.Recipe, p.ItemsPerHour, p.OutputPerRun, p.TicksPerRun, p.RentalFeePerRun, p.QueuedRuns, p.BacklogTicks, access)
		}
```

(Place the detail line so it renders directly under the facility's table row. If the existing loop builds a table with `tabwriter`/aligned columns, emit the detail line after flushing the row, or integrate per the section's actual structure — read the loop first and match its style.)

- [ ] 4. Run-to-pass: `go test ./cmd/tools/play_as/ -run TestFormatFacilityList_ShowsProductionDetails`. Expect `ok`.
- [ ] 5. `golangci-lint run ./cmd/tools/play_as/...` (no new findings); `go build ./...` and `go test ./...` (ignore `pkg/galaxy` + `pkg/actionspace`).
- [ ] 6. Commit:

```
git add -A && git commit -m 'feat(play_as): show production facility busyness + rental cost in facility list

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>'
```

---

## Self-Review

**Spec coverage** (every gap-analysis item → task):
- async craft is queued / single ok job frame → Tasks 2, 3.
- inputs from storage, `deliver_to` storage|faction, cargo removed → Tasks 3, 4, 5.
- `quantity` = output items, `MaxCraftBatchSize` obsolete → Tasks 3, 10.
- new craft payload fields `facility_id`/`preset`/`dry_run`/`action=queue`/`jobs` → Task 5 (parser), Task 6 (formatters).
- new top-level `recycle` → Tasks 4 (client/interface/MCP/mocks), 7 (play_as).
- facility new actions (job_add/job_list/job_cancel/job_reorder/set_output_price/set_access/upgrade) → Task 8.
- `crafting_update` push event + schema → Tasks 1 (constant/stub/expected-fields), 9 (callback).
- four `CraftJobResponse` shapes → Task 2 structs, Task 6 rendering.
- mock blast radius (both mocks) → Task 4 (Recycle adds interface methods → both mocks updated + `go test ./...`).
- keep `CraftResponse` shim until last use → Task 2 keeps it; `client_api_monitor.go:105` stays pointing at `CraftResponse` (a kept type); Task 10 does not touch it (the monitor mapping is cosmetic, left as a shim).
- pre-existing `pkg/galaxy` failure → noted in Global Constraints; every task's `go test ./...` step says "ignore `pkg/galaxy`".

**Placeholder scan:** no `TBD`/`TODO`/"add appropriate"/"similar to Task N" left as implementation substitutes. The two `>` Notes (sendOverride setup; partitionFlags bare-flag contract; newTestClient constructor; facility flag plumbing) are explicit verification instructions with a concrete grep to resolve, not deferred work.

**Type consistency:** struct/field/method names are identical across tasks: `CraftJobQueued`, `CraftQueueListing`/`CraftJobEntry`, `CraftBulkResponse`/`CraftBulkResult`/`CraftBulkSummary`, `CraftDryRunResponse`, `EscrowCost`/`EscrowInput`, `CraftingUpdateEvent`/`CraftingUpdateJob`/`CraftingUpdateDeposit`, `RecycleResponse` (alias). Methods `Recycle`/`RecycleWithOptions`, `SetOnCraftingUpdate`, `craftRecipe(client, logger, ctx, recipeID, quantity)`. `play_as` helpers `formatCraftJobQueued`/`formatCraftQueue`/`formatCraftBulk`/`formatCraftDryRun` reused by Tasks 7 and 8 (`formatCraftQueue` for `facility job_list`).

> One inconsistency resolved inline: Task 2 declares typed `serverapi` structs, but Task 6/8 `play_as` formatters decode with LOCAL anonymous structs (identical JSON tags) to avoid assuming `serverapi` is imported in `main.go`. This is intentional DRY-vs-coupling trade-off and is called out in Task 6 step 1 (switch to importing `serverapi` if the import already exists). Field names/tags match the Task 2 structs exactly.
