# Shipping Sub-project B (Carrier Behavior) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a mission-runner earn freight income — find eligible NPC freight at its dock, take the best one, deliver it, get paid — without ever breaching a contract.

**Architecture:** A new `pkg/worker/freight.go` produces a scored freight candidate (`freightCand`) that `Missions()` compares against the best mission candidate, taking whichever nets more. Because the server only sets `deadline_tick` at accept, feasibility is checked *after* accepting and an infeasible contract is immediately `return`ed (confirmed debt-free). Before any of that, `pkg/game` must be fixed to decode tick-deferred shipping mutations, which arrive `action_result`-wrapped and currently decode silently empty.

**Tech Stack:** Go 1.24+, SQLite (`pkg/market`), existing `pkg/worker` fake-client test harness.

**Spec:** `docs/superpowers/specs/2026-07-20-shipping-carrier-subproject-b-design.md`

> **⚠️ Post-implementation corrections (2026-07-20, after merge `a602afc`).** Task bodies
> below are the historical record and were NOT retro-edited. Two things changed during
> implementation:
> 1. **The outcome enum is SIX slugs, not five** — `return_failed` was added (user-approved):
>    the `ShippingReturn` call itself errored, so the contract was never handed back and may
>    still breach. Recording the caller's original outcome would have masked the one alarm
>    the rollout gate exists to catch.
> 2. **"Any `breached` row" is a DEAD gate** — nothing in the client ever writes `breached`
>    (a breach is server-side; this client never observes it), so any task-body comment or
>    commit message below calling `breached` "the canary's stop signal" is wrong. The
>    corrected stop conditions, telemetry-liveness check, and known canary artifacts are in
>    the **Rollout** section at the bottom of this plan, which IS maintained.

## Global Constraints

- Go 1.24+ idiom: range-over-int, `b.Loop()` in benchmarks (not `for range b.N`).
- All sleeps/pauses use the constants in `pkg/game/constants.go`. Do not introduce raw durations.
- `golangci-lint` must produce **no new findings**. Run it after each task.
- `go build ./...` **and** `go test ./...` after each task. Adding a `GameClient` interface method breaks the `pkg/agent`, `pkg/skills`, and MCP mocks — `go build` does **not** catch this, only `go test ./...` does.
- Never assume server field names. Every JSON tag used here was read from `pkg/game/serverapi/responses_shipping.go`; if you need a field not listed in this plan, read that file rather than guessing.
- Do **not** `git add -A`. `data/**/*.json` churns constantly with runtime agent state. Stage the exact files each task's commit step names.
- Commit messages end with:
  `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`

## Constants introduced (all in `pkg/worker/freight.go`, Task 3)

| Constant | Value | Provenance |
|---|---|---|
| `freightPackageFootprint` | `100` | Live smoke: sealed package cargo `size` is flat 100 regardless of contents |
| `freightTicksPerHop` | `19.0` | Live smoke: 56 ticks over a 3-hop trip |
| `freightDeadlineSlack` | `1.5` | Chosen buffer; absorbs GameClock forward-drift and reconnect stalls |
| `freightMinNet` | `500.0` | Mirrors `missionMinNet` (`mission_select.go:22`) — **see the warning below** |

> **Open risk — `freightMinNet` may reject every real contract.** The only observed
> `carrier_payout` is **100**, from a *self-shipped* smoke contract. Real NPC freight rewards are
> unmeasured. If NPC rewards cluster near 100, a 500 floor rejects the entire board and the
> canary will look like "no freight exists" rather than "the floor is wrong". Task 4 therefore
> logs the **net of every rejected candidate**, so one canary pass reveals the true reward
> distribution. Treat the first canary run as a measurement of this constant, not a validation
> of it.

---

### Task 1: Decode tick-deferred shipping mutations (`pkg/game`)

Sub-project A's mutations are undecodable today. `accept`/`deliver`/`return`/`cancel`/`pay_debt` are tick-deferred and reply in an `action_result` frame — `{"command":"shipping","tick":N,"result":{"action":"accept","contract":{...}}}` — with **no top-level `action`**. Two independent bugs:

1. `Shipping()` submits `WithAckOnly()`, so `await` returns on the immediate pending ack, before the real frame arrives.
2. `storeRawJSON` keys shipping on the **top-level** `action` under `case protocol.TypeOK`, so the wrapped frame never lands under `shipping_<action>`.

Decoding the wrapper directly *succeeds* with every field zero — this fails as an empty contract, not as an error. Same shape as the 2026-07-12 craft bug.

**Files:**
- Modify: `pkg/game/client_commands.go:2662-2680` (`Shipping`)
- Modify: `pkg/game/client.go:4229` (add a `case protocol.TypeActionResult:` to `storeRawJSON`'s switch)
- Test: `pkg/game/shipping_action_result_test.go` (create)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `(*Client).Shipping(ctx, action string, payload map[string]any) error` — unchanged signature, now awaiting the real frame for mutations. After a mutation, `GetRawJSON("shipping_<action>")` returns the **inner `result` body**, decodable directly into `serverapi.ShippingContractResponse` / `ShippingSettlementResponse`.

- [ ] **Step 1: Write the failing test**

Create `pkg/game/shipping_action_result_test.go`:

```go
package game

import (
	"encoding/json"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// shippingAcceptActionResult is the live v0.531.4 wire shape of a tick-deferred
// shipping accept: the contract body sits one level down under "result", and
// there is no top-level "action". Captured during the 2026-07-19 play_as smoke.
func shippingAcceptActionResult() protocol.Response {
	return protocol.Response{
		Type: protocol.TypeActionResult,
		Payload: map[string]any{
			"command": "shipping",
			"tick":    float64(1200),
			"result": map[string]any{
				"action": "accept",
				"contract": map[string]any{
					"id":                  "ship_abc123",
					"package_id":          "ed9edd4346ed071f3c890ca73f9456b2",
					"origin_base_id":      "treasure_cache_trading_post",
					"destination_base_id": "sol_central",
					"status":              "in_transit",
					"service_level":       "standard",
					"accepted_tick":       float64(1200),
					"target_tick":         float64(1290),
					"deadline_tick":       float64(1380),
					"base_reward":         float64(100),
					"route_hops":          float64(3),
				},
			},
		},
	}
}

// A wrapped shipping mutation must be cached under "shipping_<action>" with the
// INNER result body, so callers decode the serverapi struct directly. Before the
// fix this key was absent entirely and callers silently saw a zero contract.
func TestStoreRawJSONCachesWrappedShippingMutation(t *testing.T) {
	c := &Client{latestRawJSON: map[string][]byte{}}
	c.storeRawJSON(shippingAcceptActionResult())

	raw := c.GetRawJSON("shipping_accept")
	if len(raw) == 0 {
		t.Fatal("shipping_accept must be cached from the action_result frame; got nothing")
	}
	var resp serverapi.ShippingContractResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode shipping_accept: %v", err)
	}
	if resp.Contract.ID != "ship_abc123" {
		t.Fatalf("contract id: want ship_abc123, got %q", resp.Contract.ID)
	}
	if resp.Contract.DeadlineTick != 1380 {
		t.Fatalf("deadline_tick: want 1380, got %d", resp.Contract.DeadlineTick)
	}
	if resp.Contract.RouteHops != 3 {
		t.Fatalf("route_hops: want 3, got %d", resp.Contract.RouteHops)
	}
}

// A synchronous read (list/get/profile/track) carries a top-level action and must
// keep working exactly as before — the fix must not regress the read path.
func TestStoreRawJSONCachesSynchronousShippingRead(t *testing.T) {
	c := &Client{latestRawJSON: map[string][]byte{}}
	c.storeRawJSON(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"action":    "list",
			"shipments": []any{},
			"total":     float64(0),
		},
	})
	if len(c.GetRawJSON("shipping_list")) == 0 {
		t.Fatal("shipping_list must still be cached from the synchronous top-level-action reply")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/game/ -run 'TestStoreRawJSONCachesWrappedShippingMutation|TestStoreRawJSONCachesSynchronousShippingRead' -v`

Expected: `TestStoreRawJSONCachesWrappedShippingMutation` FAILs with `shipping_accept must be cached from the action_result frame; got nothing`. The synchronous test PASSes already (it guards against regression).

If instead the test panics on `c.GetRawJSON`, the `Client` zero value needs more fields initialised — read `GetRawJSON` and add only what it dereferences.

- [ ] **Step 3: Add the `action_result` path to `storeRawJSON`**

In `pkg/game/client.go`, the `switch resp.Type` inside `storeRawJSON` currently has exactly one case (`case protocol.TypeOK:` at line 4229). Add a sibling case immediately before it:

```go
	case protocol.TypeActionResult:
		// Tick-deferred /shipping mutations (accept, deliver, return, cancel,
		// post, pay_debt) reply in an action_result frame shaped
		// {command, tick, result:{action, ...}} with NO top-level "action", so
		// the TypeOK path below never sees them. Cache the INNER result body
		// under the same shipping_<action> key the synchronous reads use, so
		// callers decode serverapi structs directly with no unwrapping.
		//
		// Decoding the wrapper instead succeeds with every field zero, which is
		// why this failed as an empty contract rather than a decode error —
		// the same trap craft hit (see pkg/worker/craft_node.go unwrapActionResult).
		if cmd, _ := resp.Payload["command"].(string); cmd == "shipping" {
			if result, ok := resp.Payload["result"].(map[string]any); ok {
				if action, ok := result["action"].(string); ok && action != "" {
					if body, err := json.Marshal(result); err == nil {
						c.rawJSONMu.Lock()
						c.latestRawJSON["shipping_"+action] = body
						c.rawJSONMu.Unlock()
					}
				}
			}
		}
		return
```

The `return` is deliberate: the `_last` cache at the top of the function has already run, and there is no content-based key to fall through to for this frame.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/game/ -run 'TestStoreRawJSONCachesWrappedShippingMutation|TestStoreRawJSONCachesSynchronousShippingRead' -v`

Expected: both PASS.

- [ ] **Step 5: Make `Shipping()` await the real frame for mutations**

In `pkg/game/client_commands.go`, replace the body of `Shipping` (lines 2662-2680). `WithAckOnly()` resolves on the pending ack; `terminateOnActionOrOK` (`pkg/game/terminator.go:36`) returns `done=false` on an ok carrying `pending:true` and `done=true` on the `action_result`, which is exactly the wait we need. Reads stay on the ack path — they are synchronous and a terminator would just add latency.

```go
// shippingMutations are the tick-deferred /shipping actions. Their real reply
// arrives later in an action_result frame, so they must await that frame rather
// than the immediate pending ack (see storeRawJSON's TypeActionResult case).
// The reads (list, get, profile, track) reply synchronously and keep the ack path.
var shippingMutations = map[string]bool{
	"accept": true, "deliver": true, "return": true,
	"cancel": true, "post": true, "pay_debt": true,
}

// Shipping sends a /shipping action with the given payload (the action is
// injected). The reply is cached under "shipping_<action>" (storeRawJSON);
// read it with GetRawJSON and unmarshal into the matching serverapi struct.
func (c *Client) Shipping(ctx context.Context, action string, payload map[string]any) error {
	// Build a fresh outbound map so we never mutate the caller's payload
	// (injecting "action" into a map the caller reuses would be a footgun).
	out := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		out[k] = v
	}
	out["action"] = action
	msg := protocol.Message{
		Type:      "shipping",
		Payload:   out,
		Timestamp: time.Now().UnixMilli(),
	}
	opts := []SubmitOption{WithTimeout(SleepMedium)}
	if shippingMutations[action] {
		opts = append(opts, WithTerminator(terminateOnActionOrOK))
	} else {
		opts = append(opts, WithAckOnly())
	}
	h, err := c.Submit(ctx, msg, opts...)
	if err == nil {
		_, err = c.await(ctx, h)
	}
	return err
}
```

- [ ] **Step 6: Run the full package suite**

Run: `go build ./... && go test ./pkg/game/... && golangci-lint run ./pkg/game/...`

Expected: build clean, tests PASS, no new lint findings.

- [ ] **Step 7: Commit**

```bash
git add pkg/game/client.go pkg/game/client_commands.go pkg/game/shipping_action_result_test.go
git commit -m "fix(shipping): decode tick-deferred mutations from action_result frames

accept/deliver/return/cancel/post/pay_debt reply in an action_result
{command,tick,result:{action,...}} with no top-level action, so
storeRawJSON never cached them and Shipping()'s WithAckOnly await
returned on the pending ack. Decoding the wrapper succeeded with every
field zero, so this failed as an empty contract rather than an error.
Cache the inner result under shipping_<action>; mutations now await the
real frame via terminateOnActionOrOK. Reads keep the ack path.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: `FreightResult` telemetry (`pkg/market`)

Per-contract outcomes, the freight analogue of `MissionResult`. Breach-rate is the canary's pass/fail signal, so this must exist before any live run.

**Files:**
- Modify: `pkg/market/types.go` (append `FreightResult` after `MissionResult`, which ends at line 331)
- Modify: `pkg/market/schema.sql` (append after the `mission_results` block, line 213)
- Create: `pkg/market/freight_results.go`
- Test: `pkg/market/freight_results_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `market.FreightResult` struct (fields below).
  - `(*Collector).RecordFreightResult(ctx context.Context, r FreightResult) error`
  - `(*Collector).GetFreightResults(ctx context.Context, agentID string, limit int) ([]FreightResult, error)`
  - Outcome slugs, used verbatim by Tasks 5-7: `delivered`, `returned_infeasible`, `returned_inflight`, `accept_failed`, `breached`, `return_failed` (6th slug added during implementation, user-approved — see the correction note at the top of this plan).

- [ ] **Step 1: Write the failing test**

Create `pkg/market/freight_results_test.go`. Mirror the setup helper used by `mission_results_test.go` — open that file first and use whatever it calls to get a `*Collector` on a temp DB (do not invent a new one).

```go
package market

import (
	"context"
	"testing"
)

func TestRecordAndGetFreightResults(t *testing.T) {
	ctx := context.Background()
	c := newTestCollector(t) // same helper mission_results_test.go uses

	r := FreightResult{
		AgentID:       "fighter-4",
		ContractID:    "ship_abc123",
		PackageID:     "ed9edd4346ed071f3c890ca73f9456b2",
		FromBaseID:    "treasure_cache_trading_post",
		ToBaseID:      "sol_central",
		ServiceLevel:  "standard",
		RouteHops:     3,
		BaseReward:    100,
		MaxSpeedBonus: 25,
		FuelCost:      40,
		CarrierPayout: 100,
		Outcome:       "delivered",
		AcceptedAt:    "2026-07-20T10:00:00Z",
		FinishedAt:    "2026-07-20T10:20:00Z",
		AcceptedTick:  1200,
		FinishedTick:  1256,
		CreatedAt:     "2026-07-20T10:20:00Z",
	}
	if err := c.RecordFreightResult(ctx, r); err != nil {
		t.Fatalf("RecordFreightResult: %v", err)
	}

	got, err := c.GetFreightResults(ctx, "fighter-4", 10)
	if err != nil {
		t.Fatalf("GetFreightResults: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d", len(got))
	}
	if got[0].ContractID != "ship_abc123" || got[0].Outcome != "delivered" {
		t.Fatalf("round-trip mismatch: %+v", got[0])
	}
	if got[0].CarrierPayout != 100 || got[0].RouteHops != 3 {
		t.Fatalf("numeric round-trip mismatch: %+v", got[0])
	}
}

// A return is a normal, expected outcome (accept-then-verify), not an error path.
// Reason must survive the round trip so the canary can tell infeasible-at-accept
// from a buffer collapse in flight.
func TestFreightResultRecordsReturnReason(t *testing.T) {
	ctx := context.Background()
	c := newTestCollector(t)

	if err := c.RecordFreightResult(ctx, FreightResult{
		AgentID:    "fighter-4",
		ContractID: "ship_def456",
		Outcome:    "returned_infeasible",
		Reason:     "deadline 40 ticks < needed 86",
		AcceptedAt: "2026-07-20T11:00:00Z",
		FinishedAt: "2026-07-20T11:00:10Z",
		CreatedAt:  "2026-07-20T11:00:10Z",
	}); err != nil {
		t.Fatalf("RecordFreightResult: %v", err)
	}
	got, err := c.GetFreightResults(ctx, "fighter-4", 10)
	if err != nil {
		t.Fatalf("GetFreightResults: %v", err)
	}
	if len(got) != 1 || got[0].Reason != "deadline 40 ticks < needed 86" {
		t.Fatalf("reason must round-trip, got %+v", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/market/ -run TestRecordAndGetFreightResults -v`

Expected: compile failure — `undefined: FreightResult`.

- [ ] **Step 3: Add the `FreightResult` type**

Append to `pkg/market/types.go` after `MissionResult`:

```go
// FreightResult is one settled shipping contract's real outcome, the carrier
// analogue of MissionResult. Outcome is one of: delivered, returned_infeasible
// (accept-then-verify rejected it), returned_inflight (deadline buffer collapsed
// mid-trip), accept_failed, breached. A nonzero breached count is the canary's
// stop signal — the carrier is designed so no path chooses a breach.
type FreightResult struct {
	ID            int64   `json:"id"`
	AgentID       string  `json:"agent_id"`
	ContractID    string  `json:"contract_id"`
	PackageID     string  `json:"package_id"`
	FromBaseID    string  `json:"from_base_id"`
	ToBaseID      string  `json:"to_base_id"`
	ServiceLevel  string  `json:"service_level"`
	RouteHops     int     `json:"route_hops"`
	BaseReward    float64 `json:"base_reward"`
	MaxSpeedBonus float64 `json:"max_speed_bonus"`
	FuelCost      float64 `json:"fuel_cost"`
	CarrierPayout float64 `json:"carrier_payout"`
	Outcome       string  `json:"outcome"`
	Reason        string  `json:"reason"`
	AcceptedAt    string  `json:"accepted_at"`
	FinishedAt    string  `json:"finished_at"`
	AcceptedTick  int64   `json:"accepted_tick"`
	FinishedTick  int64   `json:"finished_tick"`
	CreatedAt     string  `json:"created_at"`
}
```

- [ ] **Step 4: Add the table to `schema.sql`**

Append to `pkg/market/schema.sql` after the `idx_mission_results_agent_time` index (line 213):

```sql
CREATE TABLE IF NOT EXISTS freight_results (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id        TEXT NOT NULL,
    contract_id     TEXT NOT NULL,
    package_id      TEXT,
    from_base_id    TEXT,
    to_base_id      TEXT,
    service_level   TEXT,
    route_hops      INTEGER NOT NULL DEFAULT 0,
    base_reward     REAL NOT NULL DEFAULT 0,
    max_speed_bonus REAL NOT NULL DEFAULT 0,
    fuel_cost       REAL NOT NULL DEFAULT 0,
    carrier_payout  REAL NOT NULL DEFAULT 0,
    outcome         TEXT NOT NULL,
    reason          TEXT DEFAULT '',
    accepted_at     TEXT NOT NULL,
    finished_at     TEXT NOT NULL,
    accepted_tick   INTEGER,
    finished_tick   INTEGER,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_freight_results_agent_time ON freight_results(agent_id, finished_at);
CREATE INDEX IF NOT EXISTS idx_freight_results_outcome ON freight_results(outcome);
```

The `outcome` index exists so the canary's breach check is a cheap query, not a table scan.

- [ ] **Step 5: Write the store methods**

Create `pkg/market/freight_results.go`, mirroring `mission_results.go` exactly (including `writeRetry`):

```go
package market

import (
	"context"
	"database/sql"
	"fmt"
)

// RecordFreightResult writes one settled shipping-contract outcome row, the
// carrier analogue of RecordMissionResult.
func (c *Collector) RecordFreightResult(ctx context.Context, r FreightResult) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO freight_results
 (agent_id, contract_id, package_id, from_base_id, to_base_id, service_level,
  route_hops, base_reward, max_speed_bonus, fuel_cost, carrier_payout, outcome,
  reason, accepted_at, finished_at, accepted_tick, finished_tick, created_at)
 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.AgentID, r.ContractID, r.PackageID, r.FromBaseID, r.ToBaseID, r.ServiceLevel,
			r.RouteHops, r.BaseReward, r.MaxSpeedBonus, r.FuelCost, r.CarrierPayout, r.Outcome,
			r.Reason, r.AcceptedAt, r.FinishedAt, r.AcceptedTick, r.FinishedTick, r.CreatedAt)
		return err
	})
}

// GetFreightResults returns the most recent freight results for agentID (all
// agents if empty), newest finished first, capped at limit (<=0 -> 500).
func (c *Collector) GetFreightResults(ctx context.Context, agentID string, limit int) ([]FreightResult, error) {
	if limit <= 0 {
		limit = 500
	}
	q := `SELECT id, agent_id, contract_id, COALESCE(package_id, ''), COALESCE(from_base_id, ''),
 COALESCE(to_base_id, ''), COALESCE(service_level, ''), route_hops, base_reward, max_speed_bonus,
 fuel_cost, carrier_payout, outcome, COALESCE(reason, ''), accepted_at, finished_at,
 accepted_tick, finished_tick, created_at
 FROM freight_results`
	args := []any{}
	if agentID != "" {
		q += ` WHERE agent_id = ?`
		args = append(args, agentID)
	}
	q += ` ORDER BY finished_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := c.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("get freight results: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []FreightResult
	for rows.Next() {
		var r FreightResult
		if err := rows.Scan(&r.ID, &r.AgentID, &r.ContractID, &r.PackageID, &r.FromBaseID,
			&r.ToBaseID, &r.ServiceLevel, &r.RouteHops, &r.BaseReward, &r.MaxSpeedBonus,
			&r.FuelCost, &r.CarrierPayout, &r.Outcome, &r.Reason, &r.AcceptedAt, &r.FinishedAt,
			&r.AcceptedTick, &r.FinishedTick, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan freight result: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./pkg/market/ -run 'TestRecordAndGetFreightResults|TestFreightResultRecordsReturnReason' -v`

Expected: both PASS.

If the table is missing at runtime, the test collector is not applying `schema.sql` — check how `mission_results_test.go`'s collector is built and follow it exactly. Do **not** add a numbered migration for a brand-new table; `schema.sql` is the right home (numbered migrations are reserved for altering existing tables — see `reference_ships_table_migration_trap`).

- [ ] **Step 7: Commit**

```bash
git add pkg/market/types.go pkg/market/schema.sql pkg/market/freight_results.go pkg/market/freight_results_test.go
git commit -m "feat(market): FreightResult telemetry for shipping contracts

Per-contract carrier outcomes, the freight analogue of MissionResult.
Outcome slugs: delivered, returned_infeasible, returned_inflight,
accept_failed, breached — breached is the canary's stop signal, indexed
so the check is a cheap query.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Freight scoring — pure functions (`pkg/worker`)

All arithmetic, no I/O. Isolating this makes the gate exhaustively testable without a fake server.

**Files:**
- Create: `pkg/worker/freight.go`
- Test: `pkg/worker/freight_test.go`

**Interfaces:**
- Consumes: `serverapi.ShippingListing`, `serverapi.ShipmentContract` (Task 1 leaves these unchanged).
- Produces:
  - `type freightCand struct { Contract serverapi.ShipmentContract; DestBaseID string; Hops int; Reward, FuelCost, Net float64 }`
  - `freightPackagesFit(cargoFree float64) int`
  - `buildFreightCand(l serverapi.ShippingListing, hops int, fuelCostFor func(jumps int) float64) (freightCand, string)` — non-empty string = skip reason
  - `selectFreightCand(cands []freightCand) *freightCand`
  - `freightDeadlineOK(hops int, deadlineTick, nowTick int64) (ok bool, reason string)`
  - Constants: `freightPackageFootprint`, `freightTicksPerHop`, `freightDeadlineSlack`, `freightMinNet`

- [ ] **Step 1: Write the failing test**

Create `pkg/worker/freight_test.go`:

```go
package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// noFuel prices every route at zero, isolating reward arithmetic.
func noFuel(jumps int) float64 { return 0 }

// flatFuel charges 10 credits per jump.
func flatFuel(jumps int) float64 { return float64(jumps) * 10 }

func listing(id string, eligible bool, reward int64, hops int) serverapi.ShippingListing {
	return serverapi.ShippingListing{
		Eligible: eligible,
		Contract: serverapi.ShipmentContract{
			ID:                id,
			DestinationBaseID: "sol_central",
			BaseReward:        reward,
			RouteHops:         hops,
			ServiceLevel:      "standard",
		},
	}
}

// The sealed package is a flat 100 units, so capacity maps to a whole-package
// count. A hold that cannot take one package must yield zero.
func TestFreightPackagesFit(t *testing.T) {
	cases := []struct {
		name      string
		cargoFree float64
		want      int
	}{
		{"empty hold", 0, 0},
		{"just under one package", 99, 0},
		{"exactly one package", 100, 1},
		{"one and a half", 150, 1},
		{"six packages", 600, 6},
		{"negative is clamped", -5, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := freightPackagesFit(tc.cargoFree); got != tc.want {
				t.Fatalf("freightPackagesFit(%v) = %d, want %d", tc.cargoFree, got, tc.want)
			}
		})
	}
}

func TestBuildFreightCandRejects(t *testing.T) {
	// Ineligible listings never become candidates — the server's flag already
	// encodes carrier tier, liability and debt, and we do not second-guess it.
	if _, reason := buildFreightCand(listing("a", false, 5000, 2), 2, noFuel); reason == "" {
		t.Fatal("an ineligible listing must be rejected")
	}
	// Below the net floor.
	if _, reason := buildFreightCand(listing("b", true, 100, 2), 2, noFuel); reason == "" {
		t.Fatal("a reward below freightMinNet must be rejected")
	}
	// Fuel can push an otherwise-acceptable reward under the floor.
	if _, reason := buildFreightCand(listing("c", true, 520, 5), 5, flatFuel); reason == "" {
		t.Fatal("net after fuel below freightMinNet must be rejected")
	}
	// No destination is unroutable.
	bad := listing("d", true, 5000, 2)
	bad.Contract.DestinationBaseID = ""
	if _, reason := buildFreightCand(bad, 2, noFuel); reason == "" {
		t.Fatal("a listing with no destination must be rejected")
	}
}

func TestBuildFreightCandAccepts(t *testing.T) {
	c, reason := buildFreightCand(listing("e", true, 5000, 3), 3, flatFuel)
	if reason != "" {
		t.Fatalf("want acceptance, got skip reason %q", reason)
	}
	if c.Net != 5000-30 {
		t.Fatalf("net = %v, want %v (reward 5000 - fuel 30)", c.Net, 5000-30)
	}
	if c.Hops != 3 || c.DestBaseID != "sol_central" {
		t.Fatalf("routing fields wrong: %+v", c)
	}
	// max_speed_bonus is upside only; it must never lift a candidate over the floor.
	low := listing("f", true, 100, 1)
	low.Contract.MaxSpeedBonus = 10000
	if _, reason := buildFreightCand(low, 1, noFuel); reason == "" {
		t.Fatal("max_speed_bonus must not count toward the net floor")
	}
}

func TestSelectFreightCandPicksHighestNet(t *testing.T) {
	if got := selectFreightCand(nil); got != nil {
		t.Fatal("no candidates must select nothing")
	}
	a, _ := buildFreightCand(listing("a", true, 1000, 1), 1, noFuel)
	b, _ := buildFreightCand(listing("b", true, 9000, 1), 1, noFuel)
	c, _ := buildFreightCand(listing("c", true, 3000, 1), 1, noFuel)
	got := selectFreightCand([]freightCand{a, b, c})
	if got == nil || got.Contract.ID != "b" {
		t.Fatalf("want highest-net candidate b, got %+v", got)
	}
}

// The deadline is only knowable after accept, so this gate runs post-accept.
// The smoke's own contract (3 hops, 180 ticks granted) must clear it.
func TestFreightDeadlineOK(t *testing.T) {
	// 3 hops * 19 ticks * 1.5 slack = 85.5 needed.
	if ok, reason := freightDeadlineOK(3, 1380, 1200); !ok {
		t.Fatalf("the live smoke contract must clear the gate, got %q", reason)
	}
	if ok, _ := freightDeadlineOK(3, 1240, 1200); ok {
		t.Fatal("40 ticks for a 3-hop trip must fail the gate")
	}
	// A missing deadline must fail closed: without a deadline we cannot prove
	// feasibility, and guessing is how you breach.
	if ok, _ := freightDeadlineOK(3, 0, 1200); ok {
		t.Fatal("a zero deadline_tick must fail the gate, not pass it")
	}
	// Zero hops (same-base contract) still needs a positive deadline window.
	if ok, _ := freightDeadlineOK(0, 1201, 1200); !ok {
		t.Fatal("a zero-hop contract with a live deadline must pass")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/worker/ -run 'TestFreightPackagesFit|TestBuildFreightCand|TestSelectFreightCand|TestFreightDeadlineOK' -v`

Expected: compile failure — `undefined: freightPackagesFit`, `undefined: buildFreightCand`, etc.

- [ ] **Step 3: Write the implementation**

Create `pkg/worker/freight.go`:

```go
package worker

import (
	"fmt"
	"math"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

const (
	// freightPackageFootprint is the cargo a sealed shipping package occupies.
	// Measured live (2026-07-19): flat 100 regardless of contents — ten size-1
	// iron_ore still measured 100, because the container's 100-item capacity is
	// reserved whole. NOT contents-summed, and not the empty container's size (4).
	freightPackageFootprint = 100.0

	// freightTicksPerHop estimates travel cost per route hop. Measured live: a
	// 3-hop delivery took 56 ticks (~18.7/hop). Single-sample; re-tune from
	// freight_results after the canary.
	freightTicksPerHop = 19.0

	// freightDeadlineSlack is the safety multiplier on the estimated trip length.
	// The ~50% buffer absorbs GameClock forward-drift and reconnect stalls, both
	// of which cost real ticks we cannot predict at accept time.
	freightDeadlineSlack = 1.5

	// freightMinNet is the net floor a contract must clear, mirroring
	// missionMinNet so freight and missions are ranked on the same scale.
	//
	// WARNING: the only observed carrier_payout is 100, from a self-shipped
	// smoke contract; real NPC freight rewards are unmeasured. If they cluster
	// low, this floor rejects the whole board. freightCandidate logs the net of
	// every rejected candidate so one canary pass reveals the true distribution.
	freightMinNet = 500.0
)

// freightCand is an eligible freight listing with derived routing and economics,
// scored on the same scale as missionCandidate so the docked pass can rank them
// against each other.
type freightCand struct {
	Contract   serverapi.ShipmentContract
	DestBaseID string
	Hops       int
	Reward     float64
	FuelCost   float64
	Net        float64 // Reward - FuelCost
}

// freightPackagesFit reports how many whole sealed packages a hold can carry.
// The footprint is a constant, so this is knowable before any server call — it
// is the ship-capability precondition for freight. v1 acts only on >= 1; the
// count is what a future multi-package trip design will consume.
func freightPackagesFit(cargoFree float64) int {
	if cargoFree < freightPackageFootprint {
		return 0
	}
	return int(math.Floor(cargoFree / freightPackageFootprint))
}

// buildFreightCand derives economics for one listing. A non-empty reason means
// skip, and is logged verbatim so a canary pass shows why the board emptied out.
func buildFreightCand(l serverapi.ShippingListing, hops int, fuelCostFor func(jumps int) float64) (freightCand, string) {
	if !l.Eligible {
		reason := l.Reason
		if reason == "" {
			reason = "server marked ineligible"
		}
		return freightCand{}, reason
	}
	if l.Contract.DestinationBaseID == "" {
		return freightCand{}, "no destination_base_id"
	}
	reward := float64(l.Contract.BaseReward)
	fuel := 0.0
	if fuelCostFor != nil {
		fuel = fuelCostFor(hops)
	}
	// max_speed_bonus is deliberately excluded: it is upside, never a reason to
	// take a contract whose base reward does not stand on its own.
	net := reward - fuel
	if net < freightMinNet {
		return freightCand{}, fmt.Sprintf("net %.0f below floor %.0f (reward %.0f, fuel %.0f)", net, freightMinNet, reward, fuel)
	}
	return freightCand{
		Contract:   l.Contract,
		DestBaseID: l.Contract.DestinationBaseID,
		Hops:       hops,
		Reward:     reward,
		FuelCost:   fuel,
		Net:        net,
	}, ""
}

// selectFreightCand returns the highest-net candidate, or nil when there are none.
func selectFreightCand(cands []freightCand) *freightCand {
	var best *freightCand
	for i := range cands {
		if best == nil || cands[i].Net > best.Net {
			best = &cands[i]
		}
	}
	return best
}

// freightDeadlineOK reports whether the remaining tick window covers the
// estimated trip with slack. Runs POST-accept: a posted listing carries no
// deadline_tick — the server sets it at accept — so this cannot gate acceptance.
// Fails closed on a missing deadline: an unprovable deadline is a breach waiting
// to happen, and `return` is free.
func freightDeadlineOK(hops int, deadlineTick, nowTick int64) (bool, string) {
	if deadlineTick <= 0 {
		return false, "contract carries no deadline_tick"
	}
	remaining := deadlineTick - nowTick
	needed := float64(hops) * freightTicksPerHop * freightDeadlineSlack
	if float64(remaining) < needed {
		return false, fmt.Sprintf("deadline %d ticks < needed %.0f (%d hops)", remaining, needed, hops)
	}
	return true, ""
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestFreightPackagesFit|TestBuildFreightCand|TestSelectFreightCand|TestFreightDeadlineOK' -v`

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/freight.go pkg/worker/freight_test.go
git commit -m "feat(worker): freight scoring primitives

Pure gate arithmetic: the constant >=100-cargo capability precondition
(sealed packages are a flat 100), net-after-fuel scoring with
max_speed_bonus excluded as upside-only, highest-net selection, and the
post-accept deadline check that fails closed on a missing deadline_tick.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Board read + debt guard (`freightCandidate`)

The I/O layer that turns a dock into a scored candidate.

**Files:**
- Modify: `pkg/worker/freight.go`
- Test: `pkg/worker/freight_test.go`
- Modify: `pkg/worker/dispatch_test.go` (add shipping fakes to `fakeClient`)

**Interfaces:**
- Consumes: Task 3's `buildFreightCand`, `selectFreightCand`, `freightPackagesFit`; Task 1's `shipping_list` / `shipping_profile` raw-JSON keys.
- Produces:
  - `type freightInputs struct { CargoFree float64; FuelCostFor func(jumps int) float64; HopsTo func(destBaseID string) (int, bool) }`
  - `freightCandidate(ctx context.Context, deps MissionDeps, in freightInputs, out io.Writer) (*freightCand, string)` — the string is a skip reason (`""` when a candidate is returned).

- [ ] **Step 1: Add shipping fakes to the shared `fakeClient`**

In `pkg/worker/dispatch_test.go`, add fields to the `fakeClient` struct (after `withdrawErr`):

```go
	shippingErr    map[string]error // per-action error, keyed by shipping action
	shippingCalls  []string         // shipping actions issued, in order
```

and methods (place them next to the other command fakes):

```go
func (f *fakeClient) ShippingList(ctx context.Context, sort string) error {
	f.calls = append(f.calls, "shipping_list")
	f.shippingCalls = append(f.shippingCalls, "list")
	return f.shippingErr["list"]
}
func (f *fakeClient) ShippingProfile(ctx context.Context) error {
	f.calls = append(f.calls, "shipping_profile")
	f.shippingCalls = append(f.shippingCalls, "profile")
	return f.shippingErr["profile"]
}
func (f *fakeClient) ShippingAccept(ctx context.Context, shipmentID, carrier string) error {
	f.calls = append(f.calls, "shipping_accept:"+shipmentID)
	f.shippingCalls = append(f.shippingCalls, "accept")
	return f.shippingErr["accept"]
}
func (f *fakeClient) ShippingDeliver(ctx context.Context, shipmentID string) error {
	f.calls = append(f.calls, "shipping_deliver:"+shipmentID)
	f.shippingCalls = append(f.shippingCalls, "deliver")
	return f.shippingErr["deliver"]
}
func (f *fakeClient) ShippingReturn(ctx context.Context, shipmentID string) error {
	f.calls = append(f.calls, "shipping_return:"+shipmentID)
	f.shippingCalls = append(f.shippingCalls, "return")
	return f.shippingErr["return"]
}
func (f *fakeClient) ShippingGet(ctx context.Context, shipmentID string) error {
	f.calls = append(f.calls, "shipping_get:"+shipmentID)
	f.shippingCalls = append(f.shippingCalls, "get")
	return f.shippingErr["get"]
}
```

- [ ] **Step 2: Write the failing test**

Append to `pkg/worker/freight_test.go`:

```go
import (
	"context"
	"encoding/json"
	"io"
	"strings"
	// ...existing imports
)

func shippingListJSON(t *testing.T, listings ...serverapi.ShippingListing) []byte {
	t.Helper()
	b, err := json.Marshal(serverapi.ShippingListResponse{
		Action: "list", Shipments: listings, Total: len(listings),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func shippingProfileJSON(t *testing.T, blocked bool, reason string) []byte {
	t.Helper()
	b, err := json.Marshal(serverapi.ShippingProfileResponse{
		Action:               "profile",
		Profile:              serverapi.CarrierProfile{Tier: "probationary"},
		DebtBlocksAcceptance: blocked,
		DebtBlockReason:      reason,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// freightTestInputs routes every destination at 2 hops with free fuel.
func freightTestInputs(cargoFree float64) freightInputs {
	return freightInputs{
		CargoFree:   cargoFree,
		FuelCostFor: noFuel,
		HopsTo:      func(string) (int, bool) { return 2, true },
	}
}

// A hold too small for a package must short-circuit before ANY server call.
// This is the cheapest possible rejection and must stay that way.
func TestFreightCandidateSkipsWhenHoldTooSmall(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	deps := MissionDeps{Client: f, AgentID: "fighter-4"}

	cand, reason := freightCandidate(context.Background(), deps, freightTestInputs(99), io.Discard)
	if cand != nil {
		t.Fatal("a 99-unit hold cannot carry a 100-unit package")
	}
	if reason == "" {
		t.Fatal("want a skip reason")
	}
	if len(f.shippingCalls) != 0 {
		t.Fatalf("must make zero shipping calls, made %v", f.shippingCalls)
	}
}

// Freight debt blocks acceptance server-side; we skip freight without spending
// a list call, and never auto-pay.
func TestFreightCandidateSkipsWhenDebtBlocked(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		raw:   map[string][]byte{"shipping_profile": shippingProfileJSON(t, true, "unpaid failure debt")},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4"}
	var log strings.Builder

	cand, reason := freightCandidate(context.Background(), deps, freightTestInputs(500), &log)
	if cand != nil {
		t.Fatal("debt-blocked carriers must not take freight")
	}
	if !strings.Contains(reason, "unpaid failure debt") {
		t.Fatalf("skip reason must carry the server's debt_block_reason, got %q", reason)
	}
	for _, c := range f.shippingCalls {
		if c == "list" {
			t.Fatal("must not list the board while debt-blocked")
		}
		if c == "pay_debt" {
			t.Fatal("must never auto-pay debt")
		}
	}
}

func TestFreightCandidatePicksBestEligible(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		raw: map[string][]byte{
			"shipping_profile": shippingProfileJSON(t, false, ""),
			"shipping_list": shippingListJSON(t,
				listing("low", true, 800, 2),
				listing("high", true, 6000, 2),
				listing("ineligible", false, 99000, 2),
			),
		},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4"}

	cand, reason := freightCandidate(context.Background(), deps, freightTestInputs(500), io.Discard)
	if cand == nil {
		t.Fatalf("want a candidate, got skip: %s", reason)
	}
	if cand.Contract.ID != "high" {
		t.Fatalf("want the highest-net eligible contract, got %q", cand.Contract.ID)
	}
}

// Rejected candidates must have their net logged. This is how the canary
// measures the real NPC reward distribution against freightMinNet.
func TestFreightCandidateLogsRejectedNets(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		raw: map[string][]byte{
			"shipping_profile": shippingProfileJSON(t, false, ""),
			"shipping_list":    shippingListJSON(t, listing("cheap", true, 100, 2)),
		},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4"}
	var log strings.Builder

	cand, _ := freightCandidate(context.Background(), deps, freightTestInputs(500), &log)
	if cand != nil {
		t.Fatal("a 100-credit reward is below the floor")
	}
	if !strings.Contains(log.String(), "100") {
		t.Fatalf("the rejected reward must appear in the log; got %q", log.String())
	}
}

// An unroutable destination is skipped, not guessed at.
func TestFreightCandidateSkipsUnroutableDestination(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		raw: map[string][]byte{
			"shipping_profile": shippingProfileJSON(t, false, ""),
			"shipping_list":    shippingListJSON(t, listing("far", true, 9000, 2)),
		},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4"}
	in := freightTestInputs(500)
	in.HopsTo = func(string) (int, bool) { return 0, false }

	if cand, _ := freightCandidate(context.Background(), deps, in, io.Discard); cand != nil {
		t.Fatal("an unroutable destination must not become a candidate")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./pkg/worker/ -run TestFreightCandidate -v`

Expected: compile failure — `undefined: freightCandidate`, `undefined: freightInputs`.

- [ ] **Step 4: Write the implementation**

Append to `pkg/worker/freight.go` (add `context`, `encoding/json`, `io` to the imports):

```go
// freightInputs are the per-pass facts freightCandidate needs from the caller.
// Passing them in (rather than deriving them) keeps the gate testable without a
// live galaxy graph or fuel-price source.
type freightInputs struct {
	// CargoFree is the ship's remaining hold, in cargo units.
	CargoFree float64
	// FuelCostFor prices the fuel for a jump count (nil -> free).
	FuelCostFor func(jumps int) float64
	// HopsTo resolves jump distance to a destination base; ok=false means
	// unroutable, and the contract is skipped rather than guessed at.
	HopsTo func(destBaseID string) (int, bool)
}

// freightCandidate evaluates freight at the current dock and returns the best
// scored candidate, or nil plus a skip reason. Failure is always "skip the
// pass", never an error: the caller falls through to missions/exploration
// exactly as it does when the mission board is empty.
func freightCandidate(ctx context.Context, deps MissionDeps, in freightInputs, out io.Writer) (*freightCand, string) {
	if out == nil {
		out = io.Discard
	}
	// Capability precondition first: a ship that cannot hold a package has no
	// business talking to /shipping, so this costs zero server calls.
	if freightPackagesFit(in.CargoFree) < 1 {
		return nil, fmt.Sprintf("hold has %.0f free, a package needs %.0f", in.CargoFree, freightPackageFootprint)
	}

	// Debt guard. Freight debt blocks acceptance server-side, so listing the
	// board would be wasted. We never auto-pay: an operator settles debt, so a
	// systematic breach bug cannot buy back its own ability to keep breaching.
	if err := deps.Client.ShippingProfile(ctx); err != nil {
		return nil, fmt.Sprintf("shipping profile: %v", err)
	}
	var prof serverapi.ShippingProfileResponse
	if raw := deps.Client.GetRawJSON("shipping_profile"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &prof); err != nil {
			return nil, fmt.Sprintf("decode shipping profile: %v", err)
		}
	}
	if prof.DebtBlocksAcceptance {
		reason := prof.DebtBlockReason
		if reason == "" {
			reason = "freight debt blocks acceptance"
		}
		fmt.Fprintf(out, "freight: skipping, %s (outstanding %d) — operator must settle\n", reason, prof.Profile.OutstandingDebt) //nolint:errcheck
		return nil, reason
	}

	if err := deps.Client.ShippingList(ctx, ""); err != nil {
		return nil, fmt.Sprintf("shipping list: %v", err)
	}
	var board serverapi.ShippingListResponse
	if raw := deps.Client.GetRawJSON("shipping_list"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &board); err != nil {
			return nil, fmt.Sprintf("decode shipping list: %v", err)
		}
	}
	if len(board.Shipments) == 0 {
		reason := board.EmptyReason
		if reason == "" {
			reason = "no freight posted here"
		}
		return nil, reason
	}

	cands := make([]freightCand, 0, len(board.Shipments))
	for _, l := range board.Shipments {
		hops := l.Contract.RouteHops
		if in.HopsTo != nil {
			h, ok := in.HopsTo(l.Contract.DestinationBaseID)
			if !ok {
				fmt.Fprintf(out, "freight: skip %s: no route to %s\n", l.Contract.ID, l.Contract.DestinationBaseID) //nolint:errcheck
				continue
			}
			hops = h
		}
		c, reason := buildFreightCand(l, hops, in.FuelCostFor)
		if reason != "" {
			// Logged at every rejection on purpose: the net distribution here is
			// the only evidence for whether freightMinNet is set sanely against
			// real NPC rewards, which are unmeasured.
			fmt.Fprintf(out, "freight: skip %s: %s\n", l.Contract.ID, reason) //nolint:errcheck
			continue
		}
		cands = append(cands, c)
	}

	best := selectFreightCand(cands)
	if best == nil {
		return nil, "no freight cleared the gate"
	}
	fmt.Fprintf(out, "freight: best %s to %s, %d hops, net %.0f\n", best.Contract.ID, best.DestBaseID, best.Hops, best.Net) //nolint:errcheck
	return best, ""
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./pkg/worker/ -run TestFreightCandidate -v`

Expected: all five PASS.

- [ ] **Step 6: Verify the interface mocks still build**

Run: `go build ./... && go test ./... 2>&1 | grep -v '^ok' | head -20`

Expected: no failures. `fakeClient` embeds `game.GameClient`, so the new shipping methods only shadow the embedded ones — no other mock should need changing. If `pkg/agent` or `pkg/skills` fails to compile, their mocks need the same methods (`feedback_gameclient_interface_mocks`).

- [ ] **Step 7: Commit**

```bash
git add pkg/worker/freight.go pkg/worker/freight_test.go pkg/worker/dispatch_test.go
git commit -m "feat(worker): freight board read with capability and debt guards

freightCandidate turns a dock into a best-scored freight candidate. The
>=100-cargo precondition short-circuits before any server call, the debt
guard skips freight without listing (and never auto-pays), and every
rejected candidate's net is logged so the canary measures the real NPC
reward distribution against freightMinNet.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Accept-then-verify with the `return` escape

The safety core. Because a posted listing has no `deadline_tick`, feasibility can only be checked after committing — so we accept, read the real deadline, and immediately `return` anything that does not clear. `return` is confirmed debt-free (full `shipper_refund`, liability released, `outstanding_debt` unchanged).

**Files:**
- Modify: `pkg/worker/freight.go`
- Test: `pkg/worker/freight_test.go`

**Interfaces:**
- Consumes: Task 3's `freightDeadlineOK`; Task 1's `shipping_accept` raw-JSON key; Task 2's `market.FreightResult`.
- Produces: `freightAccept(ctx context.Context, deps MissionDeps, cand *freightCand, out io.Writer) (accepted *serverapi.ShipmentContract, ok bool)` — `ok=false` means the candidate was released (returned or failed) and the caller should fall through.

- [ ] **Step 1: Write the failing test**

Append to `pkg/worker/freight_test.go`:

```go
func shippingContractJSON(t *testing.T, action string, c serverapi.ShipmentContract) []byte {
	t.Helper()
	b, err := json.Marshal(serverapi.ShippingContractResponse{Action: action, Contract: c})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// acceptedContract is the smoke's real shape: deadline set AT accept.
func acceptedContract(deadlineTick int64) serverapi.ShipmentContract {
	return serverapi.ShipmentContract{
		ID:                "high",
		PackageID:         "pkg_hash",
		OriginBaseID:      "haven_station",
		DestinationBaseID: "sol_central",
		ServiceLevel:      "standard",
		Status:            "in_transit",
		AcceptedTick:      1200,
		TargetTick:        1290,
		DeadlineTick:      deadlineTick,
		BaseReward:        6000,
		RouteHops:         3,
	}
}

func TestFreightAcceptProceedsWhenDeadlineFeasible(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		raw:   map[string][]byte{"shipping_accept": shippingContractJSON(t, "accept", acceptedContract(1380))},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeMissionStore{}}
	cand := &freightCand{Contract: serverapi.ShipmentContract{ID: "high"}, Hops: 3, Net: 6000}

	got, ok := freightAccept(context.Background(), deps, cand, io.Discard)
	if !ok || got == nil {
		t.Fatal("a feasible contract must proceed")
	}
	if got.DeadlineTick != 1380 {
		t.Fatalf("the accepted contract's real deadline must be read back, got %d", got.DeadlineTick)
	}
	for _, c := range f.shippingCalls {
		if c == "return" {
			t.Fatal("a feasible contract must not be returned")
		}
	}
}

// The whole point of accept-then-verify: an infeasible deadline is discovered
// after committing, and `return` is the debt-free escape.
func TestFreightAcceptReturnsWhenDeadlineInfeasible(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{
		state: &game.State{},
		raw:   map[string][]byte{"shipping_accept": shippingContractJSON(t, "accept", acceptedContract(1210))},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	cand := &freightCand{Contract: serverapi.ShipmentContract{ID: "high"}, Hops: 3, Net: 6000}

	got, ok := freightAccept(context.Background(), deps, cand, io.Discard)
	if ok || got != nil {
		t.Fatal("an infeasible contract must be released")
	}
	if !slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must return the contract, calls were %v", f.shippingCalls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "returned_infeasible" {
		t.Fatalf("must record returned_infeasible, got %+v", store.results)
	}
}

// A lost race (someone else took it) is a normal skip, recorded for the canary.
func TestFreightAcceptRecordsAcceptFailure(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{
		state:       &game.State{},
		shippingErr: map[string]error{"accept": errors.New("contract already accepted")},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	cand := &freightCand{Contract: serverapi.ShipmentContract{ID: "high"}, Hops: 3, Net: 6000}

	if _, ok := freightAccept(context.Background(), deps, cand, io.Discard); ok {
		t.Fatal("a failed accept must not proceed")
	}
	if len(store.results) != 1 || store.results[0].Outcome != "accept_failed" {
		t.Fatalf("must record accept_failed, got %+v", store.results)
	}
}
```

Add the freight store fake near `fakeMissionStore` in `pkg/worker/mission_test.go`:

```go
// fakeFreightStore records freight results and satisfies the mission-store
// surface the freight path shares with missions.
type fakeFreightStore struct {
	fakeMissionStore
	results []market.FreightResult
}

func (s *fakeFreightStore) RecordFreightResult(ctx context.Context, r market.FreightResult) error {
	s.results = append(s.results, r)
	return nil
}
```

and extend the `MissionStore` interface in `pkg/worker/mission.go:47` with:

```go
	// RecordFreightResult persists one settled shipping-contract outcome.
	RecordFreightResult(ctx context.Context, r market.FreightResult) error
```

Then add the same method to `fakeMissionStore` so existing mission tests still satisfy the interface:

```go
func (s *fakeMissionStore) RecordFreightResult(ctx context.Context, r market.FreightResult) error {
	return nil
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/worker/ -run TestFreightAccept -v`

Expected: compile failure — `undefined: freightAccept`.

- [ ] **Step 3: Write the implementation**

Append to `pkg/worker/freight.go`:

```go
// freightRecord persists one settled contract outcome. Telemetry failures are
// logged and swallowed: losing a metrics row must never abort a trip.
func freightRecord(ctx context.Context, deps MissionDeps, out io.Writer, c serverapi.ShipmentContract, cand *freightCand, payout float64, outcome, reason string) {
	if deps.Market == nil {
		return
	}
	now := missionNow(deps)
	fuel := 0.0
	hops := c.RouteHops
	if cand != nil {
		fuel = cand.FuelCost
		if hops == 0 {
			hops = cand.Hops
		}
	}
	r := market.FreightResult{
		AgentID:       deps.AgentID,
		ContractID:    c.ID,
		PackageID:     c.PackageID,
		FromBaseID:    c.OriginBaseID,
		ToBaseID:      c.DestinationBaseID,
		ServiceLevel:  c.ServiceLevel,
		RouteHops:     hops,
		BaseReward:    float64(c.BaseReward),
		MaxSpeedBonus: float64(c.MaxSpeedBonus),
		FuelCost:      fuel,
		CarrierPayout: payout,
		Outcome:       outcome,
		Reason:        reason,
		AcceptedAt:    c.AcceptedAt,
		FinishedAt:    rfc(now),
		AcceptedTick:  c.AcceptedTick,
		FinishedTick:  missionTick(deps),
		CreatedAt:     rfc(now),
	}
	if err := deps.Market.RecordFreightResult(ctx, r); err != nil {
		fmt.Fprintf(out, "freight: record %s result: %v\n", outcome, err) //nolint:errcheck
	}
}

// freightAccept takes the candidate and then verifies it — the server only sets
// deadline_tick at accept, so feasibility is unknowable beforehand. An infeasible
// contract is immediately returned, which the live smoke confirmed is debt-free
// (status returned_intact, full shipper_refund, liability released,
// outstanding_debt unchanged). ok=false means the candidate was released.
func freightAccept(ctx context.Context, deps MissionDeps, cand *freightCand, out io.Writer) (*serverapi.ShipmentContract, bool) {
	if out == nil {
		out = io.Discard
	}
	id := cand.Contract.ID
	if err := deps.Client.ShippingAccept(ctx, id, "player"); err != nil {
		fmt.Fprintf(out, "freight: accept %s: %v\n", id, err) //nolint:errcheck
		freightRecord(ctx, deps, out, cand.Contract, cand, 0, "accept_failed", err.Error())
		return nil, false
	}

	var resp serverapi.ShippingContractResponse
	if raw := deps.Client.GetRawJSON("shipping_accept"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &resp); err != nil {
			fmt.Fprintf(out, "freight: decode accept %s: %v\n", id, err) //nolint:errcheck
		}
	}
	c := resp.Contract
	if c.ID == "" {
		// The accept reply did not decode. We may well be holding a contract we
		// cannot reason about, so release it rather than transit blind.
		fmt.Fprintf(out, "freight: accept %s returned no contract; returning it\n", id) //nolint:errcheck
		freightReturn(ctx, deps, out, cand.Contract, cand, "returned_infeasible", "accept reply did not decode")
		return nil, false
	}

	hops := c.RouteHops
	if hops == 0 {
		hops = cand.Hops
	}
	if ok, reason := freightDeadlineOK(hops, c.DeadlineTick, missionTick(deps)); !ok {
		fmt.Fprintf(out, "freight: %s infeasible (%s); returning\n", id, reason) //nolint:errcheck
		freightReturn(ctx, deps, out, c, cand, "returned_infeasible", reason)
		return nil, false
	}
	fmt.Fprintf(out, "freight: accepted %s to %s (deadline tick %d)\n", id, c.DestinationBaseID, c.DeadlineTick) //nolint:errcheck
	return &c, true
}

// freightReturn hands a contract back and records the outcome. A failed return
// is logged loudly: it is the only situation in which a breach becomes possible
// despite the design, so it must be visible in the canary logs.
func freightReturn(ctx context.Context, deps MissionDeps, out io.Writer, c serverapi.ShipmentContract, cand *freightCand, outcome, reason string) {
	if err := deps.Client.ShippingReturn(ctx, c.ID); err != nil {
		fmt.Fprintf(out, "freight: RETURN FAILED for %s (%s): %v — breach now possible\n", c.ID, reason, err) //nolint:errcheck
		freightRecord(ctx, deps, out, c, cand, 0, outcome, "return failed: "+err.Error())
		return
	}
	freightRecord(ctx, deps, out, c, cand, 0, outcome, reason)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/worker/ -run TestFreightAccept -v`

Expected: all three PASS.

Add `"errors"` and `"slices"` to the test imports and `"github.com/rsned/spacemolt/pkg/market"` to `freight.go`'s imports if the compiler asks.

- [ ] **Step 5: Run the full suite**

Run: `go build ./... && go test ./... && golangci-lint run ./pkg/worker/... ./pkg/market/...`

Expected: all PASS, no new lint findings. Adding `RecordFreightResult` to `MissionStore` may break other implementors — `go test ./...` is what surfaces it.

- [ ] **Step 6: Commit**

```bash
git add pkg/worker/freight.go pkg/worker/freight_test.go pkg/worker/mission.go pkg/worker/mission_test.go
git commit -m "feat(worker): accept-then-verify freight with the debt-free return escape

A posted listing carries no deadline_tick — the server sets it at accept
— so feasibility is checked after committing and anything that fails is
immediately returned (confirmed debt-free: full shipper_refund, liability
released, no debt). An undecodable accept reply is also returned rather
than transited blind. Every terminal state records a FreightResult.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Run the trip — withdraw, navigate, deliver

**Files:**
- Modify: `pkg/worker/freight.go`
- Test: `pkg/worker/freight_test.go`

**Interfaces:**
- Consumes: Task 5's `freightAccept`, `freightRecord`, `freightReturn`.
- Produces: `freightRunTrip(ctx context.Context, deps MissionDeps, c *serverapi.ShipmentContract, cand *freightCand, nav func(ctx context.Context, baseID string) error, out io.Writer) error`

- [ ] **Step 1: Write the failing test**

Append to `pkg/worker/freight_test.go`:

```go
func shippingSettlementJSON(t *testing.T, action string, c serverapi.ShipmentContract, payout int64) []byte {
	t.Helper()
	b, err := json.Marshal(serverapi.ShippingSettlementResponse{
		Action: action, Contract: c, CarrierPayout: payout,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFreightRunTripDeliversAndRecordsPayout(t *testing.T) {
	store := &fakeFreightStore{}
	delivered := acceptedContract(1380)
	delivered.Status = "delivered"
	f := &fakeClient{
		state: &game.State{},
		raw:   map[string][]byte{"shipping_deliver": shippingSettlementJSON(t, "deliver", delivered, 6000)},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	c := acceptedContract(1380)
	navigated := ""
	nav := func(ctx context.Context, baseID string) error { navigated = baseID; return nil }

	if err := freightRunTrip(context.Background(), deps, &c, &freightCand{Hops: 3}, nav, io.Discard); err != nil {
		t.Fatalf("freightRunTrip: %v", err)
	}
	if navigated != "sol_central" {
		t.Fatalf("must navigate to destination_base_id, went to %q", navigated)
	}
	// The package must be pulled from origin storage into the hold before transit.
	if !slices.Contains(f.calls, "withdraw:package:pkg_hash") {
		t.Fatalf("must withdraw the package: prefix item, calls were %v", f.calls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "delivered" {
		t.Fatalf("want a delivered result, got %+v", store.results)
	}
	if store.results[0].CarrierPayout != 6000 {
		t.Fatalf("payout must come from the settlement reply, got %v", store.results[0].CarrierPayout)
	}
}

// If the package cannot be pulled into the hold we must not transit — a contract
// we cannot physically carry is a guaranteed breach.
func TestFreightRunTripReturnsWhenWithdrawFails(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{
		state:       &game.State{},
		withdrawErr: errors.New("no such item in storage"),
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	c := acceptedContract(1380)
	navigated := false
	nav := func(ctx context.Context, baseID string) error { navigated = true; return nil }

	_ = freightRunTrip(context.Background(), deps, &c, &freightCand{Hops: 3}, nav, io.Discard)
	if navigated {
		t.Fatal("must not transit a package we could not load")
	}
	if !slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must return the contract, calls were %v", f.shippingCalls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "returned_infeasible" {
		t.Fatalf("want returned_infeasible, got %+v", store.results)
	}
}
```

Ensure `fakeClient.WithdrawItems` records `"withdraw:"+itemID` in `f.calls` and honours `withdrawErr` — check `dispatch_test.go`; if it records a different string, use that string in the assertion rather than changing the fake.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/worker/ -run TestFreightRunTrip -v`

Expected: compile failure — `undefined: freightRunTrip`.

- [ ] **Step 3: Write the implementation**

Append to `pkg/worker/freight.go`:

```go
// freightPackageItemID is the cargo/storage item id for a contract's package.
// The contract carries the bare hash in package_id; storage and cargo address it
// with a "package:" prefix (confirmed live).
func freightPackageItemID(packageID string) string {
	return "package:" + packageID
}

// freightRunTrip carries an accepted contract to its destination and delivers it.
// Returns nil on every handled outcome — like Missions and Haul, a failed trip
// logs and idles rather than killing the worker loop.
func freightRunTrip(ctx context.Context, deps MissionDeps, c *serverapi.ShipmentContract, cand *freightCand, nav func(ctx context.Context, baseID string) error, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	// Accept placed the sealed package in personal storage at origin; pull it
	// into the hold before leaving. If it will not load we are holding a contract
	// we cannot physically carry, which is a guaranteed breach — return instead.
	item := freightPackageItemID(c.PackageID)
	if err := deps.Client.WithdrawItems(ctx, item, 1); err != nil {
		fmt.Fprintf(out, "freight: withdraw %s: %v; returning contract\n", item, err) //nolint:errcheck
		freightReturn(ctx, deps, out, *c, cand, "returned_infeasible", "package would not load: "+err.Error())
		return nil
	}

	if nav != nil {
		if err := nav(ctx, c.DestinationBaseID); err != nil {
			// Navigation failed but the deadline may still hold; leave the
			// contract in flight and let the next pass re-check the buffer
			// (freightInFlightCheck) rather than returning on a transient error.
			fmt.Fprintf(out, "freight: navigate to %s: %v\n", c.DestinationBaseID, err) //nolint:errcheck
			return nil
		}
	}

	if err := deps.Client.ShippingDeliver(ctx, c.ID); err != nil {
		fmt.Fprintf(out, "freight: deliver %s: %v\n", c.ID, err) //nolint:errcheck
		return nil
	}
	var settle serverapi.ShippingSettlementResponse
	if raw := deps.Client.GetRawJSON("shipping_deliver"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &settle); err != nil {
			fmt.Fprintf(out, "freight: decode deliver %s: %v\n", c.ID, err) //nolint:errcheck
		}
	}
	final := settle.Contract
	if final.ID == "" {
		final = *c
	}
	fmt.Fprintf(out, "freight: delivered %s, payout %d\n", c.ID, settle.CarrierPayout) //nolint:errcheck
	freightRecord(ctx, deps, out, final, cand, float64(settle.CarrierPayout), "delivered", "")
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/worker/ -run TestFreightRunTrip -v`

Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/freight.go pkg/worker/freight_test.go
git commit -m "feat(worker): freight trip execution — withdraw, navigate, deliver

Pulls the sealed package from origin storage (package: prefixed item id)
into the hold, routes to destination_base_id, delivers, and records the
settlement payout. A package that will not load returns the contract
rather than transiting toward a guaranteed breach; a transient nav
failure leaves it in flight for the next pass's buffer re-check.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: In-flight buffer re-check and restart reconciliation

Workers restart often and there is no `captains_log` resume, so an in-memory task does not survive. Both paths here exist to stop an orphaned or doomed contract from becoming a breach.

**Files:**
- Modify: `pkg/worker/freight.go`
- Test: `pkg/worker/freight_test.go`

**Interfaces:**
- Consumes: Task 5's `freightReturn`; Task 6's `freightRunTrip`.
- Produces:
  - `freightInFlightCheck(ctx context.Context, deps MissionDeps, c *serverapi.ShipmentContract, cand *freightCand, remainingHops int, out io.Writer) bool` — `false` means the contract was returned.
  - `freightReconcile(ctx context.Context, deps MissionDeps, out io.Writer) (*serverapi.ShipmentContract, bool)`

- [ ] **Step 1: Write the failing test**

Append to `pkg/worker/freight_test.go`:

```go
func TestFreightInFlightCheckReturnsWhenBufferCollapses(t *testing.T) {
	store := &fakeFreightStore{}
	f := &fakeClient{state: &game.State{}}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: store}
	c := acceptedContract(1210) // only 10 ticks left at tick 1200

	if ok := freightInFlightCheck(context.Background(), deps, &c, &freightCand{}, 3, io.Discard); ok {
		t.Fatal("a collapsed buffer must not keep the contract")
	}
	if !slices.Contains(f.shippingCalls, "return") {
		t.Fatalf("must return, calls were %v", f.shippingCalls)
	}
	if len(store.results) != 1 || store.results[0].Outcome != "returned_inflight" {
		t.Fatalf("want returned_inflight, got %+v", store.results)
	}
}

func TestFreightInFlightCheckKeepsHealthyContract(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeFreightStore{}}
	c := acceptedContract(1380)

	if ok := freightInFlightCheck(context.Background(), deps, &c, &freightCand{}, 1, io.Discard); !ok {
		t.Fatal("a healthy buffer must keep the contract")
	}
	if slices.Contains(f.shippingCalls, "return") {
		t.Fatal("must not return a healthy contract")
	}
}

// After a restart the in-memory task is gone; the server is the only source of
// truth for what we are holding.
func TestFreightReconcileFindsHeldContract(t *testing.T) {
	held := acceptedContract(1380)
	f := &fakeClient{
		state: &game.State{},
		raw: map[string][]byte{
			"shipping_profile": func() []byte {
				b, _ := json.Marshal(serverapi.ShippingProfileResponse{
					Action:  "profile",
					Profile: serverapi.CarrierProfile{ActiveContracts: 1},
				})
				return b
			}(),
			"shipping_list": shippingListJSON(t, serverapi.ShippingListing{
				Eligible: true, Contract: held,
			}),
		},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeFreightStore{}}

	got, ok := freightReconcile(context.Background(), deps, io.Discard)
	if !ok || got == nil {
		t.Fatal("a held contract must be discovered from server state")
	}
	if got.ID != "high" {
		t.Fatalf("wrong contract: %+v", got)
	}
}

func TestFreightReconcileNoActiveContracts(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		raw:   map[string][]byte{"shipping_profile": shippingProfileJSON(t, false, "")},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeFreightStore{}}

	if got, ok := freightReconcile(context.Background(), deps, io.Discard); ok || got != nil {
		t.Fatalf("no active contracts must reconcile to nothing, got %+v", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/worker/ -run 'TestFreightInFlightCheck|TestFreightReconcile' -v`

Expected: compile failure — `undefined: freightInFlightCheck`, `undefined: freightReconcile`.

- [ ] **Step 3: Write the implementation**

Append to `pkg/worker/freight.go`:

```go
// freightInFlightCheck re-verifies the deadline buffer while carrying. Called
// every pass: a long disconnect can burn the whole margin between passes, and a
// contract that has become unwinnable is worth more returned than breached.
// Returns false when the contract was released.
func freightInFlightCheck(ctx context.Context, deps MissionDeps, c *serverapi.ShipmentContract, cand *freightCand, remainingHops int, out io.Writer) bool {
	if out == nil {
		out = io.Discard
	}
	if ok, reason := freightDeadlineOK(remainingHops, c.DeadlineTick, missionTick(deps)); !ok {
		fmt.Fprintf(out, "freight: in-flight buffer collapsed for %s (%s); returning\n", c.ID, reason) //nolint:errcheck
		freightReturn(ctx, deps, out, *c, cand, "returned_inflight", reason)
		return false
	}
	return true
}

// freightReconcile recovers an in-flight contract from server state after a
// restart or reconnect. The worker's in-memory task does not survive a restart
// (no captains_log resume yet), and an orphaned package rides to a breach in
// silence, so this runs before taking any new work.
func freightReconcile(ctx context.Context, deps MissionDeps, out io.Writer) (*serverapi.ShipmentContract, bool) {
	if out == nil {
		out = io.Discard
	}
	if err := deps.Client.ShippingProfile(ctx); err != nil {
		fmt.Fprintf(out, "freight: reconcile profile: %v\n", err) //nolint:errcheck
		return nil, false
	}
	var prof serverapi.ShippingProfileResponse
	if raw := deps.Client.GetRawJSON("shipping_profile"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &prof); err != nil {
			fmt.Fprintf(out, "freight: reconcile decode profile: %v\n", err) //nolint:errcheck
			return nil, false
		}
	}
	if prof.Profile.ActiveContracts == 0 {
		return nil, false
	}

	// The profile reports only a count, so the board read supplies the contract
	// itself — an in_transit contract we are contracted on comes back in list.
	if err := deps.Client.ShippingList(ctx, ""); err != nil {
		fmt.Fprintf(out, "freight: reconcile list: %v\n", err) //nolint:errcheck
		return nil, false
	}
	var board serverapi.ShippingListResponse
	if raw := deps.Client.GetRawJSON("shipping_list"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &board); err != nil {
			fmt.Fprintf(out, "freight: reconcile decode list: %v\n", err) //nolint:errcheck
			return nil, false
		}
	}
	for _, l := range board.Shipments {
		if l.Contract.Status == "in_transit" {
			c := l.Contract
			fmt.Fprintf(out, "freight: reconciled held contract %s to %s (deadline tick %d)\n", c.ID, c.DestinationBaseID, c.DeadlineTick) //nolint:errcheck
			return &c, true
		}
	}
	fmt.Fprintf(out, "freight: profile reports %d active contract(s) but none found in the board read\n", prof.Profile.ActiveContracts) //nolint:errcheck
	return nil, false
}
```

> **Live-verify note for the canary:** `freightReconcile` assumes an `in_transit`
> contract the agent is contracted on appears in the `list` read. That was not
> exercised by the smoke. If the canary logs "profile reports N active contract(s)
> but none found in the board read", switch the lookup to `ShippingTrack`/`ShippingGet`
> against a contract id persisted locally at accept. Do not silently ignore that log line.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestFreightInFlightCheck|TestFreightReconcile' -v`

Expected: all four PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/freight.go pkg/worker/freight_test.go
git commit -m "feat(worker): in-flight deadline re-check and restart reconciliation

A long disconnect can burn the whole deadline margin between passes, so
carrying workers re-verify the buffer every pass and return rather than
ride into a breach. On reconnect the carrier recovers any held contract
from server state, since the in-memory task does not survive a restart
and an orphaned package breaches silently.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Wire freight into the docked pass

Freight becomes co-equal with the mission board: whichever nets more wins, exploration stays the fallback.

**Files:**
- Modify: `pkg/worker/mission.go` (the docked pass in `Missions()`; `fuelCostFor` is built at line 365)
- Test: `pkg/worker/freight_test.go`

**Interfaces:**
- Consumes: Tasks 4-7's `freightCandidate`, `freightAccept`, `freightRunTrip`, `freightReconcile`.
- Produces: no new exported surface — `MissionDeps` gains one optional field:
  `EnableFreight bool` (default false, so existing fleets are unaffected until the canary opts in).

- [ ] **Step 1: Write the failing test**

Append to `pkg/worker/freight_test.go`:

```go
// Freight must not run unless a fleet opts in, so the pool is unaffected until
// the canary flips the flag.
func TestMissionsSkipsFreightWhenDisabled(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		raw: map[string][]byte{
			"shipping_profile": shippingProfileJSON(t, false, ""),
			"shipping_list":    shippingListJSON(t, listing("high", true, 9000, 2)),
		},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4", Market: &fakeFreightStore{}, KB: missionKB()}
	// EnableFreight deliberately left false.

	_ = Missions(context.Background(), deps)
	if len(f.shippingCalls) != 0 {
		t.Fatalf("freight must be inert when disabled, but issued %v", f.shippingCalls)
	}
}

// With freight enabled and a hold too small, the pass must still complete
// normally — freight is additive, never a new way for a pass to fail.
func TestMissionsWithFreightEnabledStillCompletes(t *testing.T) {
	f := &fakeClient{state: &game.State{}}
	deps := MissionDeps{
		Client: f, AgentID: "fighter-4", Market: &fakeFreightStore{},
		KB: missionKB(), EnableFreight: true,
	}
	if err := Missions(context.Background(), deps); err != nil {
		t.Fatalf("a freight-enabled pass must not error: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/worker/ -run 'TestMissionsSkipsFreightWhenDisabled|TestMissionsWithFreightEnabledStillCompletes' -v`

Expected: compile failure — `unknown field EnableFreight`.

- [ ] **Step 3: Add the opt-in flag**

In `pkg/worker/mission.go`, add to `MissionDeps` (after `SetActivity`):

```go
	// EnableFreight opts this worker into the /shipping carrier path, evaluated
	// co-equally with the mission board. Default false so existing fleets are
	// unchanged until the canary opts in explicitly.
	EnableFreight bool
```

- [ ] **Step 4: Wire the freight pass in**

In `Missions()`, after `fuelCostFor` is defined (line 365) and before the mission board is scored, insert:

```go
	// Freight is evaluated co-equally with the mission board: whichever nets
	// more wins, exploration stays the fallback. Any freight failure degrades to
	// "no freight this pass" — it must never be a new way for the pass to fail.
	var freightBest *freightCand
	if deps.EnableFreight {
		// Reconcile first: a restart loses the in-memory task, and an orphaned
		// in-flight package breaches in silence.
		if held, ok := freightReconcile(ctx, deps, out); ok {
			if freightInFlightCheck(ctx, deps, held, nil, held.RouteHops, out) {
				if deps.SetActivity != nil {
					deps.SetActivity("Freight " + held.ID + " to " + held.DestinationBaseID)
				}
				return freightRunTrip(ctx, deps, held, nil, func(ctx context.Context, baseID string) error {
					return missionNavToBase(ctx, deps, baseID)
				}, out)
			}
			return nil
		}
		in := freightInputs{
			CargoFree:   float64(cargoFreeSpace(state)),
			FuelCostFor: fuelCostFor,
			HopsTo: func(destBaseID string) (int, bool) {
				return missionHopsToBase(ctx, deps, destBaseID)
			},
		}
		cand, skip := freightCandidate(ctx, deps, in, out)
		if cand == nil {
			fmt.Fprintf(out, "freight: no candidate (%s)\n", skip) //nolint:errcheck
		}
		freightBest = cand
	}
```

then, at the point where the best mission trip's net is known (immediately before the accept loop), add the co-equal comparison:

```go
	if freightBest != nil && freightBest.Net > tripNet(trip) {
		fmt.Fprintf(out, "freight: taking %s (net %.0f) over the mission trip (net %.0f)\n", freightBest.Contract.ID, freightBest.Net, tripNet(trip)) //nolint:errcheck
		accepted, ok := freightAccept(ctx, deps, freightBest, out)
		if !ok {
			// Released (returned or accept failed) — fall through to missions.
			freightBest = nil
		} else {
			if deps.SetActivity != nil {
				deps.SetActivity("Freight " + accepted.ID + " to " + accepted.DestinationBaseID)
			}
			return freightRunTrip(ctx, deps, accepted, freightBest, func(ctx context.Context, baseID string) error {
				return missionNavToBase(ctx, deps, baseID)
			}, out)
		}
	}
```

- [ ] **Step 5: Define the base-id routing adapters**

A contract addresses its destination by `destination_base_id`, but `deps.nav` takes `(system, poi)` and the mission distance map is keyed by **system**. The KB cannot resolve base → system directly (`knowledge.Base` has `GetBase` and `GetBaseByPOI`, but no POI-by-id lookup, and `SpaceBase` carries only `POIID`). Ask the server's router instead — the same trick `haulResolveSellSystem` (`haul.go:928`) uses for the moving capital. `FindRoute` accepts a station/base target and returns `[]game.RouteStep` whose last hop is the destination system.

Append to `pkg/worker/freight.go`:

```go
// missionHopsToBase resolves jump distance to a base id via the server's router.
// The KB cannot map base -> system (no POI-by-id lookup, and SpaceBase carries
// only POIID), and the contract addresses its destination by base id only, so
// the router is the authority — the same approach haulResolveSellSystem uses for
// the moving capital. ok=false means unroutable, and the caller skips the
// contract rather than guessing a distance.
func missionHopsToBase(ctx context.Context, deps MissionDeps, destBaseID string) (int, bool) {
	if destBaseID == "" {
		return 0, false
	}
	route, err := deps.Client.FindRoute(ctx, destBaseID)
	if err != nil || len(route) == 0 {
		return 0, false
	}
	// RouteStep.Jumps is cumulative, so the last hop carries the total. Fall back
	// to the step count when the server omits it.
	if hops := route[len(route)-1].Jumps; hops > 0 {
		return hops, true
	}
	return len(route), true
}

// missionNavToBase routes to a base id, resolving its system through the router
// and reusing the pass's existing navigation rather than adding a second path.
func missionNavToBase(ctx context.Context, deps MissionDeps, destBaseID string) error {
	route, err := deps.Client.FindRoute(ctx, destBaseID)
	if err != nil {
		return fmt.Errorf("route to %s: %w", destBaseID, err)
	}
	if len(route) == 0 {
		return fmt.Errorf("no route to %s", destBaseID)
	}
	destSystem := route[len(route)-1].SystemID
	if destSystem == "" {
		return fmt.Errorf("router returned no system for %s", destBaseID)
	}
	return deps.nav(ctx, destSystem, destBaseID)
}
```

Add a test for the unroutable case to `pkg/worker/freight_test.go`:

```go
func TestMissionHopsToBaseUnroutable(t *testing.T) {
	f := &fakeClient{state: &game.State{}, routeErr: errors.New("no route")}
	deps := MissionDeps{Client: f, AgentID: "fighter-4"}
	if _, ok := missionHopsToBase(context.Background(), deps, "sol_central"); ok {
		t.Fatal("a router error must report unroutable, not a guessed distance")
	}
	if _, ok := missionHopsToBase(context.Background(), deps, ""); ok {
		t.Fatal("an empty base id must report unroutable")
	}
}

func TestMissionHopsToBaseUsesCumulativeJumps(t *testing.T) {
	f := &fakeClient{
		state: &game.State{},
		route: []game.RouteStep{{SystemID: "a", Jumps: 1}, {SystemID: "sol", Jumps: 3}},
	}
	deps := MissionDeps{Client: f, AgentID: "fighter-4"}
	hops, ok := missionHopsToBase(context.Background(), deps, "sol_central")
	if !ok || hops != 3 {
		t.Fatalf("want 3 cumulative hops, got %d (ok=%v)", hops, ok)
	}
}
```

Note `deps.nav` is defaulted to the real autopilot at `mission.go:230-231`, so `missionNavToBase` must only be called from inside `Missions()` after that defaulting has run — which is where Step 4 places it.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./pkg/worker/ -run 'TestMissions|TestFreight|TestMissionHopsToBase' -v`

Expected: all PASS, including the pre-existing mission tests (freight is inert for them because `EnableFreight` defaults false).

- [ ] **Step 7: Full verification**

Run: `go build ./... && go test ./... && golangci-lint run`

Expected: build clean, all tests PASS, no new lint findings.

- [ ] **Step 8: Commit**

```bash
git add pkg/worker/mission.go pkg/worker/freight.go pkg/worker/freight_test.go
git commit -m "feat(worker): evaluate freight co-equally with the mission board

Behind EnableFreight (default false, so existing fleets are unchanged).
The pass reconciles any held contract first, then scores freight against
the best mission trip and takes whichever nets more; exploration stays
the fallback. Every freight failure degrades to 'no freight this pass'
rather than failing the pass.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Rollout (after all tasks land)

Not part of the implementation, but the plan is not done until these happen:

1. **Land the `client_api_monitor` / `kind` fix first** — its own standalone task
   (`project_kind_discriminator_drift`). Until it lands, every shipping call spams
   `[SERVER API CHANGE]`, which will bury the canary's real signal.
2. **Canary one mission-runner** with `EnableFreight: true`. Nothing else in the pool changes.
3. **First, verify telemetry is actually landing.** `RecordFreightResult` errors are logged
   and swallowed (correct for metrics), so a missing or broken `freight_results` table is
   INVISIBLE — a clean canary and a dead pipeline look identical. After the canary's first
   freight attempt, run one query against the live DB before trusting anything else:

   ```
   sqlite3 data/market.db "SELECT outcome, COUNT(*), MIN(finished_at) FROM freight_results GROUP BY outcome;"
   ```

   Zero rows after an observed freight attempt = investigate the pipeline, not the economics.
4. **Stop conditions** (⚠️ corrected 2026-07-20 — the original "any `breached` row" gate is
   dead: nothing in the client ever writes `breached`; a breach is a server-side event this
   client never observes, so that gate reads clean by construction):

   ```
   sqlite3 data/market.db "SELECT * FROM freight_results WHERE outcome IN ('breached','return_failed');"
   ```

   - **Any row from that query** — stop the rollout. `return_failed` is the client-observable
     alarm: the `ShippingReturn` call itself errored, the contract was never handed back, and
     the worker parks (the pass aborts by design — a parked worker holding a contract IS the
     operator signal). `breached` stays in the query as insurance only.
   - **A known contract id with no terminal row at all** — cross-check accepted contract ids
     from the logs (`freight: taking <id>` / `Freight <id> to <dest>` activity lines) against
     `freight_results.contract_id`; an orphan is presumed breached until proven otherwise.
   - **Server-side ground truth**: `shipping --action=profile` on the canary —
     `outstanding_debt > 0` or a `breaches` increment is definitive regardless of what our
     telemetry says.
5. **Read the canary's logs for these, in order:**
   - Any `RETURN FAILED` line — pairs with the `return_failed` row above.
   - The distribution of `freight: skip <id>: net N below floor 500` lines — this is the
     measurement of whether `freightMinNet` is set sanely against real NPC rewards. If the
     board is uniformly rejected, lower the floor before concluding freight is unavailable.
   - Any `profile reports N active contract(s) but none found in the board read` line — the
     reconciliation lookup assumption is wrong and needs the `ShippingTrack` fallback. The
     `ActiveContracts` guard in `freightCandidate` fails closed against this (no second
     accept), so it is a fix-soon signal, not an emergency.
   - On the first successful delivery, capture `deliver`'s ack frame: `pending:true` is
     directly observed for `accept` but only inferred for `deliver` from api.md's general
     queued-mutation contract. One logged frame closes the last inference.
   - **Known artifacts, don't misread as failures:** `returned_infeasible` rows can be
     dock-ordering artifacts (reconcile runs before the dock/recovery block, so a restart with
     the package still in storage while undocked returns rather than docking first);
     `returned_inflight` rows can fire one hop from delivery because the in-flight re-check
     prices `RouteHops` as TOTAL hops (server refresh to remaining hops on `in_transit` is
     unverified — these rows are also the data that answers that question).
6. **Re-tune** `freightTicksPerHop` and `freightDeadlineSlack` from `freight_results` timings.
7. **Roll to the pool**, then consider Sub-project C (multi-package trips for large holds).
