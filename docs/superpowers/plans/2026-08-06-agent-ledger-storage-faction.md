# Agent Ledger — Storage + Faction Slices Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the agent capability ledger to record what the fleet *holds* — per-base storage for every agent and per-base storage plus treasury for every faction — and fix the eligibility flapping the first slices left behind, so "what can we source for free" and "is the faction treasury healthy" become queries.

**Architecture:** Two new capture commands (`capture_storage`, `capture_faction`) in the existing `pkg/assets` package, feeding five new tables in the same `data/assets.db`. Both are driven by the server's prose `hint` string, which enumerates every base holding items — so base discovery needs no sweep of all 64 stations. A new read layer (`Load*`) serves both the storage merge and the eligibility fallback. A `BaseResolver` handles the dual-named-station trap at read time, keeping `pkg/assets` free of any dependency on spacemolt-knowledge.db.

**Tech Stack:** Go 1.24, `modernc.org/sqlite` (driver name `"sqlite"`), existing `pkg/game` client, `pkg/worker` dispatch, `pkg/ovdash`, React 19 + TypeScript (frontend panel).

## Scope

This plan implements **build-order slices 5–6** of `docs/superpowers/specs/2026-08-01-agent-asset-profile-design.md` as refined by `docs/superpowers/specs/2026-08-06-agent-ledger-storage-faction-design.md`:

- the read layer (`LoadProfile`, `LoadCarrier`, `LoadHulls`, `LoadStorage`) and `BaseResolver`
- the eligibility fallback (the flapping fix)
- `agent_storage` + `agent_storage_items` + `capture_storage`
- `faction_profile` + `faction_storage` + `faction_storage_items` + `capture_faction`
- worker / `play_as` wiring, `roles.yaml` schedule entries
- coverage extension and the ovdash panel

**Continues on branch `feat/agent-capability-ledger`** (currently at `3fab0a5`), worktree
`.claude/worktrees/feat+agent-capability-ledger`. Everything stays **inert until a worker is
launched with `--assets-db-path`**, exactly as slices 1–4.

**Out of scope:** faction ship garages (response shape unverified, no faction has built one),
module/fitting capture, the `spread:` scheduler flag, unclaimed gifts (`ViewStorageResponse.Gifts`),
ship-class capacity lookup, and assignment logic.

## Global Constraints

- Go 1.24. Use modern idioms: `for i := range n`, `b.Loop()` in benchmarks.
- All new code must pass `golangci-lint` with **no new findings**. Run it after each task.
- Run `go build ./...` and `go test ./...` before every commit.
- Any sleep or pause must use a predefined constant from `pkg/game/constants.go`. This plan uses
  **`game.SleepQuick`** (2s) between sweep calls and introduces no new constants.
- Never assume server response field names — every struct field used here is verified below.
- Commit messages end with:
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`
- **Stage files explicitly.** Never `git add -A` — `data/*.json` is churn from the live fleet.
- Compiled binaries go in `bin/`, never the project root.
- The pre-commit race gate times out under fleet load. If it does, `--no-verify` is pre-approved,
  but you must then run `go build ./...`, `go test ./pkg/assets/ ./pkg/worker/ ./pkg/ovdash/`, and
  `golangci-lint` manually as the substitute gate.
- **Fixtures must come from captured live payloads, never composed.** This branch has already lost
  a feature twice to invented fixtures (`browse_ships`, then `owned_ships`): a golden test built on
  a hand-written wrapper the server never sends passes forever while the real code path is dead.
  Every payload in this plan was captured from a live agent on 2026-08-06 and is quoted verbatim.

## Verified facts this plan depends on

Do not re-derive these. Everything here was confirmed against live agents or the current code
during design on 2026-08-06.

### Raw-JSON cache keys (`client.GetRawJSON(key)`)

| Command | Key | How `storeRawJSON` classifies it |
|---|---|---|
| `view_storage` / `ViewStorageAt` | **`"storage"`** | payload has `base_id`, no `faction_id`, and `action` is empty or `view_storage` (`pkg/game/client.go:4452-4491`); plus an action-switch fallback at `:4941` |
| `view_faction_storage` | **`"faction_storage"`** | same shape check, but `faction_id` present |
| `faction_info` | **`"faction_info"`** | payload has `faction`, `is_member`, or `leader_id` (`pkg/game/client.go:4710-4722`) |

The keys are **not** `view_storage` / `view_faction_storage`. Getting this wrong is the exact
failure that left `owned_ships` empty from the day it was added.

### `GameClient` methods (`pkg/game/interface.go`)

```go
ViewStorage(ctx context.Context) error                              // :90
ViewStorageAt(ctx context.Context, stationID string) error          // :91
FactionInfo(ctx context.Context) error                              // :122
ViewFactionStorage(ctx context.Context) error                       // :156
ViewFactionStorageAt(ctx context.Context, stationID string) error   // :157
```

### Response structs

`serverapi.ViewStorageResponse` (`pkg/game/serverapi/responses.go:988`):

```go
type ViewStorageResponse struct {
	BaseID  string        `json:"base_id"`
	Credits int           `json:"credits"`
	Items   []CargoItem   `json:"items"`
	Ships   []StorageShip `json:"ships,omitempty"`
	Gifts   []StorageGift `json:"gifts,omitempty"`
	Hint    string        `json:"hint,omitempty"`
	Messages json.RawMessage `json:"messages,omitempty"`
}
```

`serverapi.ViewFactionStorageResponse` (`pkg/game/serverapi/responses.go:1321`):

```go
type ViewFactionStorageResponse struct {
	FactionID      string           `json:"faction_id"`
	FactionName    string           `json:"faction_name"`
	FactionTag     string           `json:"faction_tag"`
	BaseID         string           `json:"base_id"`
	Credits        int              `json:"credits"`
	Items          []CargoItem      `json:"items"`
	RecentActivity []map[string]any `json:"recent_activity,omitempty"`
	FactionFuelCapacity int    `json:"faction_fuel_capacity,omitempty"`
	FactionFuelReserve  int    `json:"faction_fuel_reserve,omitempty"`
	Hint                string `json:"hint,omitempty"`
}
```

`serverapi.FactionInfoResponse` (`pkg/game/serverapi/responses.go:1145`) — fields this plan uses:
`ID`, `Name`, `Tag`, `LeaderID`, `MemberCount`, `OwnedBases`, `Treasury`.

**`serverapi.CargoItem`** (`pkg/game/serverapi/types.go:15`) — note `Quantity` is **`float64`**:

```go
type CargoItem struct {
	ItemID   string  `json:"item_id"`
	Name     string  `json:"name,omitempty"`
	Quantity float64 `json:"quantity"`
	Size     int     `json:"size,omitempty"`
}
```

**Store quantity as `REAL`, never `INTEGER`.** `bill_of_materials` already made the INTEGER
mistake and silently ceils fractional quantities; do not repeat it.

### Live payloads captured 2026-08-06 (the fixtures for this plan)

**A. Docked, with holdings** — `databot` at `confederacy_central_command`:

```json
{"action":"view_storage","base_id":"confederacy_central_command","hint":"920 items in storage at confederacy_central_command","items":[{"item_id":"mining_laser_i","name":"Mining Laser I","quantity":1,"size":10},{"item_id":"iron_ore","name":"Iron Ore","quantity":23,"size":1},{"item_id":"titanium_alloy","name":"Titanium Alloy","quantity":3,"size":1},{"item_id":"steel_plate","name":"Steel Plate","quantity":328,"size":1},{"item_id":"sol_alloy_ore","name":"Sol Alloy Ore","quantity":216,"size":2},{"item_id":"copper_ore","name":"Copper Ore","quantity":193,"size":1},{"item_id":"titanium_ore","name":"Titanium Ore","quantity":99,"size":1},{"item_id":"nickel_ore","name":"Nickel Ore","quantity":5,"size":1},{"item_id":"antimatter_containment_cell","name":"Antimatter Containment Cell","quantity":12,"size":3},{"item_id":"nickel_billet","name":"Nickel Billet","quantity":40,"size":1}],"ships":[{"cargo_used":0,"class_id":"catalogue","class_name":"Catalogue","modules":2,"ship_id":"c63763d53539dd8cdde94211d64916d9"}]}
```

Note: there is **no `credits` field** in this payload — storage credits are omitted when zero.

**B. Remote query to a base with no holdings** — same agent, `--station_id grand_exchange_station`:

```json
{"action":"view_storage","base_id":"grand_exchange_station","hint":"920 items in storage at confederacy_central_command","items":[],"ships":[]}
```

**C. Agent holding nothing anywhere** — `random-clark`:

```json
{"hint":"No items in storage at any station."}
```

**D. Single remote base** — `prophet-1`: `"hint":"2,268 items in storage at central_nexus"`

**E. Multi-base** — captured live from `craftsman-1` on 2026-08-06 (Task 4 canary). **The hint
does NOT truncate**: all 20 bases are listed in full, alphabetically, with no ellipsis. The `...`
in the 2026-08-01 design doc was the transcriber's elision, not the server's.

```
"hint":"2,764,074 items in storage at cargo_lanes_freight_depot, central_nexus, confederacy_central_command, crix_stronghold_station, dross_citadel_station, frontier_station, gold_run_extraction_hub, grand_exchange_station, kael_arsenal_station, market_prime_exchange, mera_sanctum_station, nyx_nexus_station, sable_port_station, thane_keep_station, the_experiment_research_station, the_rampart_checkpoint, traders_rest_resort_station, treasure_cache_trading_post, unknown_edge_waystation, voss_redoubt_station"
```

Of those 20 bases, **only 8 resolve against `pois.id`** — a naive join drops 12 of 20 (60%), and
19 of the 20 exist in `bases.id`. That is the alias trap measured on real storage data, and it is
why Tasks 1–2 build the resolver.

### What the live captures prove about the hint

1. **The hint is agent-global, not per-base.** Payload B queried a base where databot holds
   nothing and still returned the hint naming a *different* base. So **one call from anywhere
   yields the complete base list** — there is no need to be docked at a particular station, and
   the sweep does not need a per-station discovery loop.
2. **The hint includes the current base** when it has holdings (payload A), and **omits bases with
   no holdings** (grand_exchange_station is absent from B's hint). Sweep set = hint bases; no need
   to union in the queried base, though doing so is harmless.
3. **`"No items in storage at any station."` is a sentinel** (payload C). Cutting on
   `" in storage at "` yields the tail `"any station."`, which a naive parser turns into a base id
   named `any station.` — it would then be queried and, worse, would suppress the "everything went
   to zero" deletion. **The parser must special-case it and return an empty set with `ok=true`.**
4. **The leading count is the total across all listed bases, and it is verifiable.** Payload A's
   `920` equals the sum of its item quantities exactly (1+23+3+328+216+193+99+5+12+40 = 920).
   This gives a runtime truncation detector: if the swept sum ≠ the hint total, bases were
   omitted. Task 7 logs that loudly.
5. **The count is comma-grouped** (`2,268`, `2,720,379`). Splitting the whole string on `", "`
   *before* cutting on `" in storage at "` shreds the number into fake base ids. Cut first.
6. **`view_storage` without a station_id fails when undocked**: the server returns
   `{"error":"not_docked","message":"You must be docked or provide a station_id to view storage."}`
   (pinned in `cmd/tools/play_as/storage_format_test.go:13`). Task 7 therefore seeds the hint call
   with a station id whenever the agent is not docked.

### The dual-named station trap

`bases(id, poi_id)` in `data/spacemolt-knowledge.db` holds **43 rows, 15 where the two forms
differ** (verified 2026-08-06). Storage `base_id` values are the **base-id** form, so joining them
against `pois.id` silently drops all five empire capitals and all seven pirate strongholds. Sample:

```
grand_exchange_station|grand_exchange
frontier_station|mobile_capital
confederacy_central_command|sol_central
kael_arsenal_station|kael_arsenal
central_nexus|the_core
crimson_war_citadel|war_citadel
```

Two of the five capitals are genuine renames, so **stripping `_station` is not a valid shortcut**.

### Existing code this plan builds on

- `Store`, `Open(cfg Config)`, `s.DB()`, `rfc3339(t)` — `pkg/assets/store.go`
- `replaceSet(ctx, delSQL, playerID string, insert func(*sql.Tx) error) error` —
  `pkg/assets/write_profile.go:42`. Deletes every row for one agent, then inserts the new set,
  in one `IMMEDIATE` transaction.
- `ReplaceHulls` — `pkg/assets/write_hulls.go:12`, the model for a whole-set writer.
- `AgentSnapshot`, `Rules()`, `Evaluate()`, `ReplaceCapabilities` — `pkg/assets/capability.go`
- `CaptureProfile` — `pkg/assets/capture.go:18`
- `Coverage(ctx, db, now, staleAfter)` + `coverageSources` — `pkg/assets/coverage.go`
- `openTestStore(t)` — `pkg/assets/store_test.go:11`
- `WorkerDispatch{Client, KB, Market, Assets *assets.Store, Out, AgentID, Station, Rescue}` and
  the `supported` map — `pkg/worker/dispatch.go:26`, `:170`
- `globalAssets`, `globalAgentID`, `--assets-db-path` — `cmd/tools/play_as/main.go:78`, `:99`, `:257`
- `Snapshot.AssetCoverage []assets.CoverageRow` `json:"asset_coverage,omitempty"` —
  `pkg/ovdash/snapshot.go:94`; `LoadAssetCoverage` — `pkg/ovdash/assets.go:22`; merged under
  `s.mu` in `refresh()` — `cmd/overmind-dashboard/main.go:69-113`
- `OvermindPage.tsx` passes props from `useFleetStream()` —
  `frontend/src/components/overmind/OvermindPage.tsx:13`

**`roles_test.go` enforces that every command named in `data/overmind/roles.yaml` appears in the
`supported` map.** Adding a schedule line without the map entry fails the build — which is a
feature: do them in the same task (Task 10).

---

### Task 1: The read layer

**Files:**
- Create: `pkg/assets/read.go`
- Create: `pkg/assets/read_test.go`

**Interfaces:**
- Consumes: `Store`, `Profile`, `Carrier`, `Hull` from the existing package.
- Produces:
  ```go
  type BaseResolver func(baseID string) string
  func (s *Store) LoadProfile(ctx context.Context, playerID string) (Profile, bool, error)
  func (s *Store) LoadCarrier(ctx context.Context, playerID string) (Carrier, time.Time, bool, error)
  func (s *Store) LoadHulls(ctx context.Context, playerID string, r BaseResolver) ([]Hull, time.Time, bool, error)
  ```
  The `bool` is "a row existed"; the `time.Time` is that source's `captured_at`. Task 3 needs both
  to distinguish "never captured" from "captured, but stale".

- [ ] **Step 1: Write the failing test**

Create `pkg/assets/read_test.go`:

```go
package assets

import (
	"context"
	"testing"
	"time"
)

// TestLoadCarrierReportsMissingVsPresent pins the distinction the eligibility
// fallback depends on: "never captured" and "captured with zero debt" must not
// look the same. Conflating them is what made CarrierKnown's documented meaning
// false in slices 1-4.
func TestLoadCarrierReportsMissingVsPresent(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	if _, _, ok, err := st.LoadCarrier(ctx, "nobody"); err != nil || ok {
		t.Fatalf("LoadCarrier for an uncaptured agent = ok %v err %v, want ok=false err=nil", ok, err)
	}

	if err := st.UpsertCarrier(ctx, "abc123", Carrier{Tier: "licensed", OutstandingDebt: 0}, now); err != nil {
		t.Fatalf("UpsertCarrier: %v", err)
	}
	got, at, ok, err := st.LoadCarrier(ctx, "abc123")
	if err != nil || !ok {
		t.Fatalf("LoadCarrier after capture = ok %v err %v, want ok=true", ok, err)
	}
	if got.Tier != "licensed" {
		t.Errorf("Tier = %q, want licensed", got.Tier)
	}
	if !at.Equal(now) {
		t.Errorf("captured_at = %v, want %v", at, now)
	}
}

// TestLoadHullsResolvesBaseIDs pins that a caller-supplied resolver rewrites
// location_base_id into poi-id form. Without it a naive join against pois.id
// drops all five empire capitals and all seven pirate strongholds -- measured
// at 18 of craftsman-1's 20 hulls.
func TestLoadHullsResolvesBaseIDs(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	rows := []Hull{{ShipID: "s1", LocationBaseID: "confederacy_central_command"}}
	if err := st.ReplaceHulls(ctx, "abc123", rows, now); err != nil {
		t.Fatalf("ReplaceHulls: %v", err)
	}

	verbatim, _, _, err := st.LoadHulls(ctx, "abc123", nil)
	if err != nil {
		t.Fatalf("LoadHulls(nil resolver): %v", err)
	}
	if verbatim[0].LocationBaseID != "confederacy_central_command" {
		t.Errorf("nil resolver must leave the wire value alone, got %q", verbatim[0].LocationBaseID)
	}

	r := BaseResolver(func(b string) string {
		if b == "confederacy_central_command" {
			return "sol_central"
		}

		return b
	})
	resolved, _, _, err := st.LoadHulls(ctx, "abc123", r)
	if err != nil {
		t.Fatalf("LoadHulls(resolver): %v", err)
	}
	if resolved[0].LocationBaseID != "sol_central" {
		t.Errorf("LocationBaseID = %q, want sol_central", resolved[0].LocationBaseID)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/assets/ -run 'TestLoad' -count=1 -v`
Expected: FAIL — `st.LoadCarrier undefined`, `st.LoadHulls undefined`, `BaseResolver undefined`.

**`-count=1` is mandatory.** A cached PASS from a previous signature nearly hid a real compile
break on this branch already.

- [ ] **Step 3: Write the implementation**

Create `pkg/assets/read.go`:

```go
package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// BaseResolver maps a station's base-id form to its poi-id form. Station ids
// are dual-named: 15 of 43 bases differ between the two (the five empire
// capitals are genuine renames, the seven pirate strongholds carry a mechanical
// "_station" suffix, three player bases are hex pairs). Every base_id column in
// this package stores the WIRE value verbatim, so any reader joining against
// pois.id must pass through a resolver or silently under-report.
//
// A nil BaseResolver is valid and means "leave ids alone" -- degrading to
// unjoinable ids is always better than degrading to wrong ones.
type BaseResolver func(baseID string) string

// resolve applies r if non-nil.
func (r BaseResolver) resolve(baseID string) string {
	if r == nil {
		return baseID
	}

	return r(baseID)
}

// parseCapturedAt reads a captured_at column. An unparseable or empty value
// yields the zero time rather than an error: a bad timestamp must not make an
// otherwise-good row unreadable.
func parseCapturedAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}

	return t
}

// LoadProfile returns the stored profile scalars. ok=false means no row.
func (s *Store) LoadProfile(ctx context.Context, playerID string) (Profile, bool, error) {
	if s == nil || s.db == nil || playerID == "" {
		return Profile{}, false, nil
	}
	var (
		p  Profile
		at string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT player_id, username, empire, credits, home_base, docked_at_base,
		       current_system, current_poi, active_ship_id, faction_id, faction_rank,
		       experience, captured_at
		  FROM agent_profile WHERE player_id = ?`, playerID).Scan(
		&p.PlayerID, &p.Username, &p.Empire, &p.Credits, &p.HomeBase, &p.DockedAtBase,
		&p.CurrentSystem, &p.CurrentPOI, &p.ActiveShipID, &p.FactionID, &p.FactionRank,
		&p.Experience, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, false, nil
	}
	if err != nil {
		return Profile{}, false, fmt.Errorf("assets: load profile %s: %w", playerID, err)
	}
	p.CapturedAt = parseCapturedAt(at)

	return p, true, nil
}

// LoadCarrier returns the stored carrier standing and its capture time.
func (s *Store) LoadCarrier(ctx context.Context, playerID string) (Carrier, time.Time, bool, error) {
	if s == nil || s.db == nil || playerID == "" {
		return Carrier{}, time.Time{}, false, nil
	}
	var (
		c  Carrier
		at string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT tier, successful_deliveries, delivered_value, priority_deliveries,
		       returns, breaches, defaults, active_contracts, active_liability,
		       outstanding_debt, debt_blocks_acceptance, next_tier, at_maximum_tier,
		       required_successful_deliveries, remaining_successful_deliveries,
		       required_delivered_value, remaining_delivered_value,
		       active_contract_limit, active_contracts_unlimited,
		       aggregate_liability_limit, remaining_aggregate_liability,
		       single_package_liability_limit, liability_unlimited, captured_at
		  FROM agent_carrier WHERE player_id = ?`, playerID).Scan(
		&c.Tier, &c.SuccessfulDeliveries, &c.DeliveredValue, &c.PriorityDeliveries,
		&c.Returns, &c.Breaches, &c.Defaults, &c.ActiveContracts, &c.ActiveLiability,
		&c.OutstandingDebt, &c.DebtBlocksAcceptance, &c.NextTier, &c.AtMaximumTier,
		&c.RequiredSuccessfulDeliveries, &c.RemainingSuccessfulDeliveries,
		&c.RequiredDeliveredValue, &c.RemainingDeliveredValue,
		&c.ActiveContractLimit, &c.ActiveContractsUnlimited,
		&c.AggregateLiabilityLimit, &c.RemainingAggregateLiability,
		&c.SinglePackageLiabilityLimit, &c.LiabilityUnlimited, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return Carrier{}, time.Time{}, false, nil
	}
	if err != nil {
		return Carrier{}, time.Time{}, false, fmt.Errorf("assets: load carrier %s: %w", playerID, err)
	}

	return c, parseCapturedAt(at), true, nil
}

// LoadHulls returns the stored hull set, resolving location_base_id through r.
// ok=false means no rows -- which for hulls always means "never captured",
// since an agent can never own zero ships.
func (s *Store) LoadHulls(ctx context.Context, playerID string, r BaseResolver) ([]Hull, time.Time, bool, error) {
	if s == nil || s.db == nil || playerID == "" {
		return nil, time.Time{}, false, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ship_id, class_id, class_name, is_active, hull_current, hull_max, hull_raw,
		       fuel_current, fuel_max, fuel_raw, cargo_used, location, location_base_id,
		       modules, listing_id, listing_price, listing_base_id, captured_at
		  FROM agent_hulls WHERE player_id = ? ORDER BY ship_id`, playerID)
	if err != nil {
		return nil, time.Time{}, false, fmt.Errorf("assets: load hulls %s: %w", playerID, err)
	}
	defer func() { _ = rows.Close() }()

	var (
		out []Hull
		at  time.Time
	)
	for rows.Next() {
		var (
			h  Hull
			ts string
		)
		if err := rows.Scan(&h.ShipID, &h.ClassID, &h.ClassName, &h.IsActive,
			&h.HullCurrent, &h.HullMax, &h.HullRaw, &h.FuelCurrent, &h.FuelMax, &h.FuelRaw,
			&h.CargoUsed, &h.Location, &h.LocationBaseID, &h.Modules,
			&h.ListingID, &h.ListingPrice, &h.ListingBaseID, &ts); err != nil {
			return nil, time.Time{}, false, fmt.Errorf("assets: scan hull %s: %w", playerID, err)
		}
		h.LocationBaseID = r.resolve(h.LocationBaseID)
		h.ListingBaseID = r.resolve(h.ListingBaseID)
		at = parseCapturedAt(ts)
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, false, fmt.Errorf("assets: iterate hulls %s: %w", playerID, err)
	}

	return out, at, len(out) > 0, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/assets/ -run 'TestLoad' -count=1 -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Verify the whole package and lint**

Run: `go build ./... && go test ./pkg/assets/ -count=1 && golangci-lint run pkg/assets/...`
Expected: build clean, tests pass, zero lint findings.

- [ ] **Step 6: Commit**

```bash
git add pkg/assets/read.go pkg/assets/read_test.go
git commit -m "feat(assets): read layer with base-id resolution

LoadProfile/LoadCarrier/LoadHulls return the stored value plus its capture
time, which is what lets the eligibility fallback tell 'never captured' from
'captured and stale'. BaseResolver handles the dual-named station trap at read
time so the tables keep storing the wire value verbatim.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: The knowledge-db-backed resolver

**Files:**
- Modify: `pkg/assets/read.go` (append)
- Modify: `pkg/assets/read_test.go` (append)

**Interfaces:**
- Consumes: `BaseResolver` from Task 1.
- Produces: `func NewBaseResolver(ctx context.Context, db *sql.DB) (BaseResolver, error)`

This takes a `*sql.DB` the **caller** already has open, so `pkg/assets` gains no import edge to
`pkg/knowledge` and `assets.db` stays independent. The caller passes the knowledge.db handle.

- [ ] **Step 1: Write the failing test**

Append to `pkg/assets/read_test.go`:

```go
// TestNewBaseResolverMapsAliases pins that the resolver reads bases(id, poi_id)
// and leaves unknown ids alone. Live shape as of 2026-08-06: 43 rows, 15 where
// the two forms differ.
func TestNewBaseResolverMapsAliases(t *testing.T) {
	ctx := context.Background()
	db := openTestKnowledgeDB(t, [][2]string{
		{"confederacy_central_command", "sol_central"},
		{"central_nexus", "the_core"},
		{"nova_terra_central", ""}, // same-name base: poi_id empty
	})

	r, err := NewBaseResolver(ctx, db)
	if err != nil {
		t.Fatalf("NewBaseResolver: %v", err)
	}
	if got := r("confederacy_central_command"); got != "sol_central" {
		t.Errorf("capital alias = %q, want sol_central", got)
	}
	if got := r("nova_terra_central"); got != "nova_terra_central" {
		t.Errorf("empty poi_id must leave the id alone, got %q", got)
	}
	if got := r("not_a_base"); got != "not_a_base" {
		t.Errorf("unknown id must pass through, got %q", got)
	}
}
```

And add this helper to the same file:

```go
// openTestKnowledgeDB builds a throwaway db carrying only the bases columns the
// resolver reads.
func openTestKnowledgeDB(t *testing.T, rows [][2]string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("open kb: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE bases (id TEXT PRIMARY KEY, poi_id TEXT)`); err != nil {
		t.Fatalf("create bases: %v", err)
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO bases (id, poi_id) VALUES (?,?)`, r[0], r[1]); err != nil {
			t.Fatalf("insert base %s: %v", r[0], err)
		}
	}

	return db
}
```

Add `"database/sql"` and `"path/filepath"` to the test file's imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/assets/ -run TestNewBaseResolver -count=1 -v`
Expected: FAIL — `NewBaseResolver undefined`.

- [ ] **Step 3: Write the implementation**

Append to `pkg/assets/read.go`:

```go
// NewBaseResolver builds a resolver from a bases(id, poi_id) table. The caller
// supplies an ALREADY-OPEN handle -- normally spacemolt-knowledge.db, which
// pkg/worker and play_as both hold anyway -- so this package never opens, imports
// or depends on that database. assets.db stays independently rebuildable.
//
// The map is tiny (43 rows live, 15 differing) so it loads once into memory.
// Rows with an empty poi_id are skipped: those are bases whose two id forms are
// identical.
func NewBaseResolver(ctx context.Context, db *sql.DB) (BaseResolver, error) {
	if db == nil {
		return nil, nil //nolint:nilnil // a nil resolver is the documented "leave ids alone" mode
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id, poi_id FROM bases WHERE poi_id IS NOT NULL AND poi_id <> '' AND poi_id <> id`)
	if err != nil {
		return nil, fmt.Errorf("assets: load base aliases: %w", err)
	}
	defer func() { _ = rows.Close() }()

	alias := make(map[string]string, 16)
	for rows.Next() {
		var id, poiID string
		if err := rows.Scan(&id, &poiID); err != nil {
			return nil, fmt.Errorf("assets: scan base alias: %w", err)
		}
		alias[id] = poiID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("assets: iterate base aliases: %w", err)
	}

	return func(baseID string) string {
		if poiID, ok := alias[baseID]; ok {
			return poiID
		}

		return baseID
	}, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./pkg/assets/ -run TestNewBaseResolver -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Verify against the real database**

Run:
```bash
sqlite3 data/spacemolt-knowledge.db "select count(*) from bases where poi_id is not null and poi_id <> '' and poi_id <> id;"
```
Expected: `15`. If it is not 15, the alias set has changed since 2026-08-06 — record the new value
in the commit message rather than adjusting the code (the query is not count-dependent).

- [ ] **Step 6: Commit**

```bash
git add pkg/assets/read.go pkg/assets/read_test.go
git commit -m "feat(assets): resolver backed by the bases alias table

Takes an already-open handle rather than a path, so pkg/assets gains no
dependency on spacemolt-knowledge.db and assets.db stays independently
rebuildable. 15 of 43 live bases carry a differing poi_id.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: The eligibility fallback (the flapping fix)

**Files:**
- Modify: `pkg/assets/capability.go` (add staleness plumbing to `AgentSnapshot`)
- Modify: `pkg/assets/capture.go` (fall back to stored values)
- Modify: `pkg/assets/capture_test.go` (append the regression test)

**Interfaces:**
- Consumes: `LoadCarrier`, `LoadHulls` from Task 1.
- Produces: `AgentSnapshot` gains `CarrierAge` and `HullsAge` (`time.Duration`, zero when captured
  this pass). `blockingReason` strings gain a ` (<source> stale <dur>)` suffix when a rule's input
  came from storage older than its cadence.

**Why:** `CaptureProfile` currently builds `AgentSnapshot` only from the current pass, so one
transient `ListShips` failure recomputes every capability as if the agent were never captured,
while good rows sit in the tables. `CarrierKnown` is documented as "no debt vs never captured"
but actually means "not captured this pass", making the doc comment false.

- [ ] **Step 1: Write the failing test**

Append to `pkg/assets/capture_test.go`:

```go
// TestCaptureProfileFallsBackToStoredHulls pins the flapping fix: a transient
// ListShips failure on a later pass must not recompute capabilities as if the
// agent had never been captured. Before this, one dropped frame flipped haul,
// freight and mission_delivery to ineligible with "no active hull captured"
// while a perfectly good hull set sat in agent_hulls.
func TestCaptureProfileFallsBackToStoredHulls(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	// Pass 1: everything captures cleanly. newFakeClient() already seeds
	// raw["owned_ships"] with one active hull, so no setup is needed here.
	c := newFakeClient()
	if err := CaptureProfile(ctx, c, st, "agent-x", now); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	if got := capabilityEligible(t, st, "abc123", "mission_delivery"); !got {
		t.Fatalf("pass 1 mission_delivery must be eligible")
	}

	// Pass 2: ListShips fails. The stored hull set must still carry the verdict.
	c.shipsErr = errors.New("connection reset")
	if err := CaptureProfile(ctx, c, st, "agent-x", now.Add(time.Hour)); err != nil {
		t.Fatalf("pass 2: %v", err)
	}
	if got := capabilityEligible(t, st, "abc123", "mission_delivery"); !got {
		t.Errorf("pass 2 mission_delivery flipped to ineligible on a transient ListShips failure")
	}
	if reason := capabilityReason(t, st, "abc123", "mission_delivery"); reason != "" {
		t.Errorf("an eligible verdict must carry no blocking reason, got %q", reason)
	}
}
```

Add both readback helpers to the same file — **neither exists yet**, verified 2026-08-06:

```go
func capabilityEligible(t *testing.T, st *Store, playerID, capability string) bool {
	t.Helper()
	var eligible bool
	if err := st.DB().QueryRow(
		`SELECT eligible FROM agent_capability WHERE player_id=? AND capability=?`,
		playerID, capability).Scan(&eligible); err != nil {
		t.Fatalf("read capability %s: %v", capability, err)
	}

	return eligible
}

func capabilityReason(t *testing.T, st *Store, playerID, capability string) string {
	t.Helper()
	var reason string
	if err := st.DB().QueryRow(
		`SELECT blocking_reason FROM agent_capability WHERE player_id=? AND capability=?`,
		playerID, capability).Scan(&reason); err != nil {
		t.Fatalf("read reason %s: %v", capability, err)
	}

	return reason
}
```

**No new fake and no new field are needed.** `pkg/assets/capture_test.go:15` already defines
`fakeClient` (embedding `game.GameClient`, so unused methods panic if called) with exactly the
knobs this test needs — `shipsErr`, `shippingErr`, `statusErr`, `raw`, `calls` — and the
constructor `newFakeClient()` at `:46` seeds `state.Player.ID = "abc123"`, credits 15135,
smuggling level 3, pirate baseline 10, plus valid `raw["owned_ships"]` and
`raw["shipping_profile"]` payloads. Use that fake as-is. Do NOT introduce a second fake.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/assets/ -run TestCaptureProfileFallsBackToStoredHulls -count=1 -v`
Expected: FAIL — `pass 2 mission_delivery flipped to ineligible…`.

**Prove it is red for the right reason.** If it fails to compile instead, fix the fake first and
re-run until you see the assertion failure — a compile error does not prove the bug exists.

- [ ] **Step 3: Add staleness fields to the snapshot**

In `pkg/assets/capability.go`, extend `AgentSnapshot` and add the suffix helper:

```go
// AgentSnapshot is everything the rules see. CarrierKnown distinguishes "no
// debt" from "never captured" -- missing data must never read as capability.
//
// CarrierAge/HullsAge are non-zero when the value came from storage rather than
// from this pass. Rules never silently trust old data; they visibly trust it,
// by appending the age to the blocking reason.
type AgentSnapshot struct {
	Profile      Profile
	Skills       map[string]SkillRow
	Standings    map[string]StandingRow
	Carrier      Carrier
	CarrierKnown bool
	CarrierAge   time.Duration
	Hulls        []Hull
	HullsAge     time.Duration
}

// staleNote renders " (hulls stale 3h0m0s)" for a reason string, or "" when the
// data came from this pass.
func staleNote(source string, age time.Duration) string {
	if age <= 0 {
		return ""
	}

	return fmt.Sprintf(" (%s stale %s)", source, age.Round(time.Minute))
}
```

Add `"time"` to the imports. Then append the note in the three rules that read fallback-able
sources — `haul`, `freight`, `mission_delivery`:

```go
		"haul": func(s AgentSnapshot) (bool, string) {
			if _, ok := s.activeHull(); !ok {
				return false, "no active hull captured"
			}
			if s.Profile.Credits < haulMinCredits {
				return false, fmt.Sprintf("credits %.0f < %d%s",
					s.Profile.Credits, haulMinCredits, staleNote("hulls", s.HullsAge))
			}

			return true, ""
		},
		"freight": func(s AgentSnapshot) (bool, string) {
			if !s.CarrierKnown {
				return false, "carrier profile not captured"
			}
			if s.Carrier.DebtBlocksAcceptance || s.Carrier.OutstandingDebt > 0 {
				return false, fmt.Sprintf("outstanding_debt %d%s",
					s.Carrier.OutstandingDebt, staleNote("carrier", s.CarrierAge))
			}
			if _, ok := s.activeHull(); !ok {
				return false, "no active hull captured"
			}

			return true, ""
		},
```

An **eligible** verdict carries no reason at all, so the note only ever appears on a refusal —
which is where it is actionable.

- [ ] **Step 4: Wire the fallback into `CaptureProfile`**

In `pkg/assets/capture.go`, replace the carrier and hull blocks with fallback-aware versions:

```go
	// Carrier: a failed call or an undecodable body leaves agent_carrier
	// untouched AND falls back to the stored row, so a transient failure cannot
	// recompute eligibility as if the agent had never been captured. This is
	// what makes CarrierKnown's documented meaning ("no debt" vs "never
	// captured") actually true.
	var (
		carrier      Carrier
		carrierKnown bool
		carrierAge   time.Duration
	)
	if err := client.ShippingProfile(ctx); err == nil {
		if c, ok, derr := CarrierFrom(client.GetRawJSON("shipping_profile")); derr == nil && ok {
			carrier, carrierKnown = c, true
			if err := st.UpsertCarrier(ctx, playerID, c, now); err != nil {
				return err
			}
		}
	}
	if !carrierKnown {
		if c, at, ok, err := st.LoadCarrier(ctx, playerID); err == nil && ok {
			carrier, carrierKnown = c, true
			carrierAge = now.Sub(at)
		}
	}

	// Hulls: same fallback. An empty decode still means "not captured" (an agent
	// can never own zero ships -- a destroyed last hull respawns a Tier 0
	// starter), so the stored set is the honest answer, not an empty fleet.
	var (
		hulls    []Hull
		hullsOK  bool
		hullsAge time.Duration
	)
	if err := client.ListShips(ctx); err == nil {
		if hs, ok, derr := HullsFrom(client.GetRawJSON("owned_ships")); derr == nil && ok {
			hulls, hullsOK = hs, true
			if err := st.ReplaceHulls(ctx, playerID, hs, now); err != nil {
				return err
			}
		}
	}
	if !hullsOK {
		if hs, at, ok, err := st.LoadHulls(ctx, playerID, nil); err == nil && ok {
			hulls = hs
			hullsAge = now.Sub(at)
		}
	}

	return st.ReplaceCapabilities(ctx, playerID, Evaluate(AgentSnapshot{
		Profile:      Profile{PlayerID: playerID, Credits: p.Credits},
		Skills:       skillMap,
		Standings:    standingMap,
		Carrier:      carrier,
		CarrierKnown: carrierKnown,
		CarrierAge:   carrierAge,
		Hulls:        hulls,
		HullsAge:     hullsAge,
	}), now)
```

Note `LoadHulls` is called with a **nil resolver**: the rules only read `IsActive` and capacity
fields, never base ids, and resolving here would put a knowledge.db dependency inside capture.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./pkg/assets/ -count=1 -v`
Expected: PASS, including the pre-existing
`TestCaptureProfileHullsSurviveEmptyRawCache` — that guard must not regress.

- [ ] **Step 6: Correct the now-true doc comment**

`CarrierKnown`'s comment in `capability.go` says it distinguishes "no debt" from "never captured".
That is now accurate. Verify the wording still reads correctly and leave it; no edit needed unless
it says "this pass".

- [ ] **Step 7: Verify and lint**

Run: `go build ./... && go test ./pkg/assets/ -count=1 && golangci-lint run pkg/assets/...`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add pkg/assets/capability.go pkg/assets/capture.go pkg/assets/capture_test.go
git commit -m "fix(assets): eligibility falls back to stored rows

One transient ShippingProfile or ListShips failure recomputed every capability
as if the agent had never been captured, while good rows sat in the tables.
Capabilities now degrade to the stored value with the age appended to the
blocking reason, so a refusal names its own staleness instead of hiding it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Canary — the multi-base hint — ✅ DONE 2026-08-06

**Outcome: the hint does NOT truncate.** `craftsman-1` returned all 20 bases in full (payload E
above, now verbatim). No freeze was needed — craftsman-1 sits in `mission-learn-overrides.json`'s
`removed` list, is absent from the live status file, and has no worker process. Of its 20 storage
bases only 8 resolve against `pois.id`, so a naive join drops 60% — the resolver in Tasks 1–2 is
load-bearing, not defensive. The steps below are retained as the reproduction recipe.

**Files:** none (verification only).

The single unresolved wire question. Payloads A–D were captured live on 2026-08-06 and are
authoritative. Payload **E** (multi-base) was transcribed during the 2026-08-01 design pass and
contains an ellipsis that may be the transcriber's elision or may be real server truncation.

**This is no longer a blocking unknown** — Task 7's total cross-check detects truncation at
runtime — but a real multi-base fixture makes the Task 5 parser test honest.

- [ ] **Step 1: Build the canary binary from this worktree**

```bash
go build -o /home/robert/spacemolt/spacemolt/bin/play_as-canary ./cmd/tools/play_as
```

Credentials live only in the main repo, so **build here, run with cwd = the main repo**.

- [ ] **Step 2: Pick a target and freeze it if it is on a live fleet**

`craftsman-1` is the largest holder (~20 bases, ~2.7M units) and is **on the live craft fleet**.
Contending for its session causes `session_replaced` thrash.

Safe window (from `reference_overmind_launch_commands`): the worker's `SilenceTimeout` is 90s, so
`SIGSTOP` the worker, do **≤60 seconds** of `play_as`, then `SIGCONT`. Match `/proc/*/cmdline` on
the executable prefix (`bin/worker*--agent craftsman-1*`), take `head -1`, and **send the signal in
a separate tool call from the scan** — a scan whose pattern matches its own wrapper shell will
SIGSTOP the scanning script itself.

If that feels too invasive on a freshly-recovered fleet, use any off-fleet agent with multiple
bases instead; `databot`, `prophet-1` and `random-clark` are all single-base or empty, so check
`prophet-2`, `random-7`, `pirate-*` first.

- [ ] **Step 3: Capture the hint**

```bash
printf 'storage\nquit\n' | ./bin/play_as-canary --debug=1 --debug-full-payload=true <agent> 2>&1 \
  | grep -ao '"hint":"[^"]*"'
```

`--debug-full-payload` emits nothing without `--debug=1`; that pairing is what surfaces raw frames.

- [ ] **Step 4: Record the finding**

Answer both:
1. Does the hint list every base in full, or does the server truncate with a literal `...`?
2. Does the leading total equal the sum of quantities across all listed bases?

Paste the verbatim hint into the Task 5 parser test as the multi-base fixture, replacing
payload E. If the server **does** truncate, add a parser test asserting the truncation marker is
recognised, and make Task 7 treat a truncated hint like a parse failure — **fall back to the known
base set and skip the base-deletion invariant**, because deleting bases that were merely elided
would erase real holdings.

- [ ] **Step 5: Clean up**

```bash
rm -f /home/robert/spacemolt/spacemolt/bin/play_as-canary
```

If a worker was frozen, confirm it resumed: `restarts` must not have incremented in
`data/overmind/craft-status.json`.

---

### Task 5: The hint parser

**Files:**
- Modify: `pkg/assets/parse.go` (append)
- Modify: `pkg/assets/parse_test.go` (append)

**Interfaces:**
- Produces:
  ```go
  type StorageHint struct {
      Bases []string
      Total float64
      HasTotal bool
  }
  func ParseStorageHint(hint string) (StorageHint, bool)
  ```
  The `bool` is `ok`: **false means "could not parse — do not delete anything"**. An empty
  `Bases` with `ok=true` means the agent genuinely holds nothing anywhere.

- [ ] **Step 1: Write the failing tests**

Append to `pkg/assets/parse_test.go`:

```go
// TestParseStorageHintLive drives the parser from payloads captured off live
// agents on 2026-08-06. Composed fixtures are what let owned_ships stay dead
// under a green suite; every string here came off the wire.
func TestParseStorageHintLive(t *testing.T) {
	tests := []struct {
		name      string
		hint      string
		wantOK    bool
		wantBases []string
		wantTotal float64
	}{
		{
			name:      "docked with holdings (databot)",
			hint:      "920 items in storage at confederacy_central_command",
			wantOK:    true,
			wantBases: []string{"confederacy_central_command"},
			wantTotal: 920,
		},
		{
			name:      "comma-grouped total (prophet-1)",
			hint:      "2,268 items in storage at central_nexus",
			wantOK:    true,
			wantBases: []string{"central_nexus"},
			wantTotal: 2268,
		},
		{
			// craftsman-1, the fleet's heaviest holder: 20 bases, no truncation.
			name: "multi-base (craftsman-1)",
			hint: "2,764,074 items in storage at cargo_lanes_freight_depot, central_nexus, " +
				"confederacy_central_command, crix_stronghold_station, dross_citadel_station, " +
				"frontier_station, gold_run_extraction_hub, grand_exchange_station, " +
				"kael_arsenal_station, market_prime_exchange, mera_sanctum_station, " +
				"nyx_nexus_station, sable_port_station, thane_keep_station, " +
				"the_experiment_research_station, the_rampart_checkpoint, " +
				"traders_rest_resort_station, treasure_cache_trading_post, " +
				"unknown_edge_waystation, voss_redoubt_station",
			wantOK: true,
			wantBases: []string{
				"cargo_lanes_freight_depot", "central_nexus", "confederacy_central_command",
				"crix_stronghold_station", "dross_citadel_station", "frontier_station",
				"gold_run_extraction_hub", "grand_exchange_station", "kael_arsenal_station",
				"market_prime_exchange", "mera_sanctum_station", "nyx_nexus_station",
				"sable_port_station", "thane_keep_station", "the_experiment_research_station",
				"the_rampart_checkpoint", "traders_rest_resort_station",
				"treasure_cache_trading_post", "unknown_edge_waystation", "voss_redoubt_station",
			},
			wantTotal: 2764074,
		},
		{
			name:      "nothing anywhere (random-clark)",
			hint:      "No items in storage at any station.",
			wantOK:    true,
			wantBases: nil,
			wantTotal: 0,
		},
		{
			name:   "empty hint",
			hint:   "",
			wantOK: false,
		},
		{
			name:   "unrecognised prose",
			hint:   "storage is temporarily unavailable",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseStorageHint(tt.hint)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if len(got.Bases) != len(tt.wantBases) {
				t.Fatalf("bases = %v, want %v", got.Bases, tt.wantBases)
			}
			for i := range tt.wantBases {
				if got.Bases[i] != tt.wantBases[i] {
					t.Errorf("bases[%d] = %q, want %q", i, got.Bases[i], tt.wantBases[i])
				}
			}
			if got.Total != tt.wantTotal {
				t.Errorf("total = %v, want %v", got.Total, tt.wantTotal)
			}
		})
	}
}

// TestParseStorageHintSentinelIsNotABase pins the single nastiest case. The
// server says "No items in storage at any station." when an agent holds
// nothing. Cutting on " in storage at " leaves the tail "any station.", which a
// naive parser turns into a base id and then QUERIES -- and worse, a non-empty
// base list suppresses the "everything went to zero" deletion, so the ledger
// would keep reporting stock the agent has already sold.
func TestParseStorageHintSentinelIsNotABase(t *testing.T) {
	got, ok := ParseStorageHint("No items in storage at any station.")
	if !ok {
		t.Fatal("the empty sentinel must parse successfully, not fail open")
	}
	if len(got.Bases) != 0 {
		t.Errorf("bases = %v, want empty (an 'any station.' entry is a parser bug)", got.Bases)
	}
}
```

If Task 4 produced a real multi-base hint, **replace the `multi-base` case's `hint` with it**
verbatim and adjust `wantBases`/`wantTotal` to match.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/assets/ -run TestParseStorageHint -count=1 -v`
Expected: FAIL — `ParseStorageHint undefined`.

- [ ] **Step 3: Write the implementation**

Append to `pkg/assets/parse.go`:

```go
// hintSeparator is the fixed phrase between the item total and the base list in
// view_storage's prose hint.
const hintSeparator = " in storage at "

// hintEmptySentinel is what the server sends when an agent holds nothing
// anywhere. It matches the general shape, so it MUST be recognised before the
// generic split: the tail "any station." would otherwise become a base id, get
// queried, and -- far worse -- make the base list non-empty, suppressing the
// deletion that should have cleared the agent's stale holdings.
const hintEmptySentinel = "No items in storage at any station."

// StorageHint is the parsed form of view_storage's hint.
//
// Total is the item count across ALL listed bases, verified against live data:
// databot's "920 items" equals the exact sum of its ten item quantities. That
// makes it a truncation detector -- if a sweep's sum falls short of Total,
// bases were omitted from the hint.
type StorageHint struct {
	Bases    []string
	Total    float64
	HasTotal bool
}

// ParseStorageHint reads the prose hint into a base list.
//
// ok=false means "unparseable" and callers MUST NOT delete anything on it: an
// empty sweep is indistinguishable from "sold everything" and would erase real
// holdings. ok=true with no bases is the genuine "holds nothing" answer.
//
// The hint is agent-global, not per-base: a query against a station where the
// agent holds nothing still returns the full list (verified 2026-08-06 against
// databot at grand_exchange_station). So one call from anywhere is enough.
func ParseStorageHint(hint string) (StorageHint, bool) {
	h := strings.TrimSpace(hint)
	if h == "" {
		return StorageHint{}, false
	}
	if h == hintEmptySentinel {
		return StorageHint{}, true
	}
	// Cut on the separator FIRST. The total is comma-grouped ("2,720,379"), so
	// splitting the whole string on ", " would shred the number into fake bases.
	head, tail, found := strings.Cut(h, hintSeparator)
	if !found || strings.TrimSpace(tail) == "" {
		return StorageHint{}, false
	}

	out := StorageHint{}
	if total, ok := parseGroupedCount(head); ok {
		out.Total, out.HasTotal = total, true
	}

	for _, part := range strings.Split(tail, ",") {
		base := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(part), "."))
		if base == "" {
			continue
		}
		out.Bases = append(out.Bases, base)
	}
	if len(out.Bases) == 0 {
		return StorageHint{}, false
	}

	return out, true
}

// parseGroupedCount reads the leading "2,720,379 items" style count. A missing
// or unreadable count is not fatal -- it only disables the truncation
// cross-check, and the base list is the load-bearing part.
func parseGroupedCount(head string) (float64, bool) {
	fields := strings.Fields(head)
	if len(fields) == 0 {
		return 0, false
	}
	n, err := strconv.ParseFloat(strings.ReplaceAll(fields[0], ",", ""), 64)
	if err != nil {
		return 0, false
	}

	return n, true
}
```

`strings` and `strconv` are already imported by `parse.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/assets/ -run TestParseStorageHint -count=1 -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Verify and lint**

Run: `go build ./... && go test ./pkg/assets/ -count=1 && golangci-lint run pkg/assets/...`

- [ ] **Step 6: Commit**

```bash
git add pkg/assets/parse.go pkg/assets/parse_test.go
git commit -m "feat(assets): parse the view_storage hint

The hint is agent-global, so one call from anywhere yields every base holding
items -- verified against a live remote query that returned another base's
holdings. Two traps are pinned by tests: the count is comma-grouped so the
separator must be cut before any comma split, and 'No items in storage at any
station.' is a sentinel that a naive split turns into a base literally named
'any station.'

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: `agent_storage` tables, decoder, and writer

**Files:**
- Modify: `pkg/assets/schema.sql` (append two tables)
- Modify: `pkg/assets/types.go` (append two types)
- Modify: `pkg/assets/parse.go` (append `StorageFrom`)
- Create: `pkg/assets/write_storage.go`
- Create: `pkg/assets/write_storage_test.go`

**Interfaces:**
- Produces:
  ```go
  type StorageBase struct {
      BaseID  string
      Credits int
      Items   []StorageItem
  }
  type StorageItem struct {
      ItemID   string
      Name     string
      Quantity float64
      Size     int
  }
  func StorageFrom(raw []byte) (StorageBase, string, bool, error)   // base, hint, ok, err
  func (s *Store) ReplaceStorage(ctx context.Context, playerID string, bases []StorageBase, now time.Time) error
  func (s *Store) LoadStorage(ctx context.Context, playerID string, r BaseResolver) ([]StorageBase, time.Time, error)
  ```

- [ ] **Step 1: Add the schema**

Append to `pkg/assets/schema.sql`:

```sql
-- Per-base storage holdings. base_id is the WIRE value (base-id form, e.g.
-- confederacy_central_command), never normalised at write time: keeping it
-- verbatim is what lets a capture be diffed field-for-field against the raw
-- frame. Readers resolve through assets.BaseResolver -- see the dual-named
-- station note above agent_profile.
--
-- Discovered from view_storage's prose hint, which enumerates every base
-- holding items and is AGENT-GLOBAL: one call from anywhere returns the full
-- list, even when queried against a base with no holdings.
CREATE TABLE IF NOT EXISTS agent_storage (
    player_id   TEXT NOT NULL,
    base_id     TEXT NOT NULL,
    credits     INTEGER NOT NULL DEFAULT 0,
    captured_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (player_id, base_id)
);
CREATE INDEX IF NOT EXISTS idx_agent_storage_base ON agent_storage(base_id);

-- quantity is REAL, not INTEGER. CargoItem.Quantity is float64 on the wire and
-- bill_of_materials already made the INTEGER mistake, where an INTEGER column
-- silently ceils fractional quantities.
CREATE TABLE IF NOT EXISTS agent_storage_items (
    player_id   TEXT NOT NULL,
    base_id     TEXT NOT NULL,
    item_id     TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    quantity    REAL NOT NULL DEFAULT 0,
    size        INTEGER NOT NULL DEFAULT 0,
    captured_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (player_id, base_id, item_id)
);
CREATE INDEX IF NOT EXISTS idx_agent_storage_items_item ON agent_storage_items(item_id);
```

- [ ] **Step 2: Add the types**

Append to `pkg/assets/types.go`:

```go
// StorageItem is one line of a base's storage manifest. Quantity is float64
// because CargoItem.Quantity is float64 on the wire.
type StorageItem struct {
	ItemID   string
	Name     string
	Quantity float64
	Size     int
}

// StorageBase is one agent's holdings at one base. Credits is often absent from
// the payload (omitted when zero), which decodes to 0 -- correct either way.
type StorageBase struct {
	BaseID  string
	Credits int
	Items   []StorageItem
}
```

- [ ] **Step 3: Write the failing decoder + writer tests**

Create `pkg/assets/write_storage_test.go`:

```go
package assets

import (
	"context"
	"testing"
	"time"
)

// liveStorageDataBot is the verbatim view_storage frame captured from databot
// on 2026-08-06 while docked at confederacy_central_command. Its ten quantities
// sum to exactly 920, matching the hint's stated total -- which is what makes
// the total usable as a truncation detector.
const liveStorageDataBot = `{"action":"view_storage","base_id":"confederacy_central_command","hint":"920 items in storage at confederacy_central_command","items":[{"item_id":"mining_laser_i","name":"Mining Laser I","quantity":1,"size":10},{"item_id":"iron_ore","name":"Iron Ore","quantity":23,"size":1},{"item_id":"titanium_alloy","name":"Titanium Alloy","quantity":3,"size":1},{"item_id":"steel_plate","name":"Steel Plate","quantity":328,"size":1},{"item_id":"sol_alloy_ore","name":"Sol Alloy Ore","quantity":216,"size":2},{"item_id":"copper_ore","name":"Copper Ore","quantity":193,"size":1},{"item_id":"titanium_ore","name":"Titanium Ore","quantity":99,"size":1},{"item_id":"nickel_ore","name":"Nickel Ore","quantity":5,"size":1},{"item_id":"antimatter_containment_cell","name":"Antimatter Containment Cell","quantity":12,"size":3},{"item_id":"nickel_billet","name":"Nickel Billet","quantity":40,"size":1}],"ships":[{"cargo_used":0,"class_id":"catalogue","class_name":"Catalogue","modules":2,"ship_id":"c63763d53539dd8cdde94211d64916d9"}]}`

// liveStorageEmptyRemote is the same agent querying a base where it holds
// nothing. The hint still names the OTHER base -- proof the hint is
// agent-global rather than per-base.
const liveStorageEmptyRemote = `{"action":"view_storage","base_id":"grand_exchange_station","hint":"920 items in storage at confederacy_central_command","items":[],"ships":[]}`

func TestStorageFromLivePayload(t *testing.T) {
	base, hint, ok, err := StorageFrom([]byte(liveStorageDataBot))
	if err != nil || !ok {
		t.Fatalf("StorageFrom = ok %v err %v, want ok=true", ok, err)
	}
	if base.BaseID != "confederacy_central_command" {
		t.Errorf("BaseID = %q", base.BaseID)
	}
	if len(base.Items) != 10 {
		t.Fatalf("items = %d, want 10", len(base.Items))
	}
	var sum float64
	for _, it := range base.Items {
		sum += it.Quantity
	}
	if sum != 920 {
		t.Errorf("quantity sum = %v, want 920 (must equal the hint total)", sum)
	}
	if hint != "920 items in storage at confederacy_central_command" {
		t.Errorf("hint = %q", hint)
	}
}

func TestStorageFromEmptyRawIsNotCaptured(t *testing.T) {
	if _, _, ok, err := StorageFrom(nil); ok || err != nil {
		t.Errorf("empty raw = ok %v err %v, want ok=false err=nil", ok, err)
	}
}

// TestReplaceStorageDropsVanishedItemsAndBases pins BOTH deletion grains. An
// item sold at a base must not linger, and a base emptied entirely must not
// linger either -- phantom stock is exactly what would poison the "what can we
// source for free" query this ledger exists to answer.
func TestReplaceStorageDropsVanishedItemsAndBases(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	first := []StorageBase{
		{BaseID: "A", Credits: 100, Items: []StorageItem{
			{ItemID: "x", Quantity: 5}, {ItemID: "y", Quantity: 7},
		}},
		{BaseID: "B", Items: []StorageItem{{ItemID: "z", Quantity: 1}}},
	}
	if err := st.ReplaceStorage(ctx, "p1", first, now); err != nil {
		t.Fatalf("first ReplaceStorage: %v", err)
	}

	second := []StorageBase{
		{BaseID: "A", Credits: 100, Items: []StorageItem{{ItemID: "x", Quantity: 5}}},
	}
	if err := st.ReplaceStorage(ctx, "p1", second, now.Add(time.Hour)); err != nil {
		t.Fatalf("second ReplaceStorage: %v", err)
	}

	var items, bases int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_storage_items WHERE player_id='p1'`).Scan(&items); err != nil {
		t.Fatalf("count items: %v", err)
	}
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_storage WHERE player_id='p1'`).Scan(&bases); err != nil {
		t.Fatalf("count bases: %v", err)
	}
	if items != 1 {
		t.Errorf("agent_storage_items = %d, want 1 (y and z must be deleted)", items)
	}
	if bases != 1 {
		t.Errorf("agent_storage = %d, want 1 (base B must be deleted)", bases)
	}
}

// TestReplaceStorageEmptySetClearsEverything pins that zero storage is
// LEGITIMATE -- the inverse of the hull rule. An agent genuinely can sell
// everything, so an empty (successful) sweep must delete. Protection against
// "empty because the call failed" lives in CaptureStorage, not here.
func TestReplaceStorageEmptySetClearsEverything(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	if err := st.ReplaceStorage(ctx, "p1", []StorageBase{
		{BaseID: "A", Items: []StorageItem{{ItemID: "x", Quantity: 5}}},
	}, now); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.ReplaceStorage(ctx, "p1", nil, now.Add(time.Hour)); err != nil {
		t.Fatalf("clear: %v", err)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_storage WHERE player_id='p1'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("agent_storage = %d, want 0", n)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./pkg/assets/ -run 'TestStorageFrom|TestReplaceStorage' -count=1 -v`
Expected: FAIL — `StorageFrom undefined`, `ReplaceStorage undefined`.

- [ ] **Step 5: Write the decoder**

Append to `pkg/assets/parse.go`:

```go
// StorageFrom decodes a raw view_storage body (cache key "storage" -- NOT
// "view_storage"; see the key table in the plan) into one base's holdings plus
// the agent-global hint.
//
// ok=false for an empty body means "nothing captured this pass" and must never
// be treated as "holds nothing": the caller distinguishes the two, because for
// storage -- unlike hulls -- zero really is reachable.
func StorageFrom(raw []byte) (StorageBase, string, bool, error) {
	if len(raw) == 0 {
		return StorageBase{}, "", false, nil
	}
	var resp serverapi.ViewStorageResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return StorageBase{}, "", false, fmt.Errorf("assets: decode view_storage: %w", err)
	}
	out := StorageBase{BaseID: resp.BaseID, Credits: resp.Credits}
	for _, it := range resp.Items {
		out.Items = append(out.Items, StorageItem{
			ItemID: it.ItemID, Name: it.Name, Quantity: it.Quantity, Size: it.Size,
		})
	}

	return out, resp.Hint, true, nil
}
```

- [ ] **Step 6: Write the writer and loader**

Create `pkg/assets/write_storage.go`:

```go
package assets

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ReplaceStorage swaps in the agent's full storage picture across every base,
// at both grains: an item that vanished from a base is deleted, and a base that
// vanished entirely is deleted too.
//
// Passing an empty slice legitimately clears everything. Unlike hulls, zero is
// reachable for storage -- an agent really can sell out. The guard against
// "empty because the calls failed" lives in CaptureStorage, which only ever
// hands this function a set it actually managed to observe.
func (s *Store) ReplaceStorage(ctx context.Context, playerID string, bases []StorageBase, now time.Time) error {
	ts := rfc3339(now)

	// One transaction covers both tables: a crash between them would leave
	// orphaned item rows pointing at a base row that no longer exists.
	return s.replaceSet(ctx, `DELETE FROM agent_storage WHERE player_id = ?`, playerID, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM agent_storage_items WHERE player_id = ?`, playerID); err != nil {
			return fmt.Errorf("assets: clear storage items for %s: %w", playerID, err)
		}
		for _, b := range bases {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO agent_storage (player_id, base_id, credits, captured_at) VALUES (?,?,?,?)`,
				playerID, b.BaseID, b.Credits, ts); err != nil {
				return fmt.Errorf("assets: insert storage %s/%s: %w", playerID, b.BaseID, err)
			}
			for _, it := range b.Items {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO agent_storage_items
						(player_id, base_id, item_id, name, quantity, size, captured_at)
					VALUES (?,?,?,?,?,?,?)`,
					playerID, b.BaseID, it.ItemID, it.Name, it.Quantity, it.Size, ts); err != nil {
					return fmt.Errorf("assets: insert storage item %s/%s/%s: %w",
						playerID, b.BaseID, it.ItemID, err)
				}
			}
		}

		return nil
	})
}

// LoadStorage returns the stored holdings, resolving base ids through r.
// CaptureStorage uses it (with a nil resolver) to carry forward bases whose
// individual queries failed.
func (s *Store) LoadStorage(ctx context.Context, playerID string, r BaseResolver) ([]StorageBase, time.Time, error) {
	if s == nil || s.db == nil || playerID == "" {
		return nil, time.Time{}, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT base_id, credits, captured_at FROM agent_storage WHERE player_id = ? ORDER BY base_id`,
		playerID)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("assets: load storage %s: %w", playerID, err)
	}
	defer func() { _ = rows.Close() }()

	var (
		out    []StorageBase
		at     time.Time
		rawIDs []string
	)
	for rows.Next() {
		var (
			b  StorageBase
			ts string
		)
		if err := rows.Scan(&b.BaseID, &b.Credits, &ts); err != nil {
			return nil, time.Time{}, fmt.Errorf("assets: scan storage %s: %w", playerID, err)
		}
		at = parseCapturedAt(ts)
		rawIDs = append(rawIDs, b.BaseID)
		b.BaseID = r.resolve(b.BaseID)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("assets: iterate storage %s: %w", playerID, err)
	}

	// Items are fetched with the UNRESOLVED id: that is what the column holds.
	for i := range out {
		items, err := s.loadStorageItems(ctx, playerID, rawIDs[i])
		if err != nil {
			return nil, time.Time{}, err
		}
		out[i].Items = items
	}

	return out, at, nil
}

func (s *Store) loadStorageItems(ctx context.Context, playerID, baseID string) ([]StorageItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT item_id, name, quantity, size FROM agent_storage_items
		 WHERE player_id = ? AND base_id = ? ORDER BY item_id`, playerID, baseID)
	if err != nil {
		return nil, fmt.Errorf("assets: load storage items %s/%s: %w", playerID, baseID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []StorageItem
	for rows.Next() {
		var it StorageItem
		if err := rows.Scan(&it.ItemID, &it.Name, &it.Quantity, &it.Size); err != nil {
			return nil, fmt.Errorf("assets: scan storage item %s/%s: %w", playerID, baseID, err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("assets: iterate storage items %s/%s: %w", playerID, baseID, err)
	}

	return out, nil
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./pkg/assets/ -run 'TestStorageFrom|TestReplaceStorage' -count=1 -v`
Expected: PASS (4 tests).

- [ ] **Step 8: Verify and lint**

Run: `go build ./... && go test ./pkg/assets/ -count=1 && golangci-lint run pkg/assets/...`

- [ ] **Step 9: Commit**

```bash
git add pkg/assets/schema.sql pkg/assets/types.go pkg/assets/parse.go \
        pkg/assets/write_storage.go pkg/assets/write_storage_test.go
git commit -m "feat(assets): agent_storage tables, decoder and writer

Both deletion grains in one transaction: a sold item and an emptied base both
disappear. Zero storage is legitimate here -- the inverse of the hull rule --
so an empty set clears; the guard against 'empty because the call failed' lives
in CaptureStorage. quantity is REAL because CargoItem.Quantity is float64 and
bill_of_materials already proved what an INTEGER column does to fractions.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: `CaptureStorage` orchestration

**Files:**
- Create: `pkg/assets/capture_storage.go`
- Create: `pkg/assets/capture_storage_test.go`

**Interfaces:**
- Consumes: `ParseStorageHint`, `StorageFrom`, `ReplaceStorage`, `LoadStorage`, `LoadProfile`.
- Produces:
  ```go
  func CaptureStorage(ctx context.Context, client game.GameClient, st *Store, agentID string, now time.Time) error
  ```
  Same contract as `CaptureProfile`: nil store or nil client is a silent no-op, source failures
  never propagate, only store-write failures return an error.

**The algorithm**, stated once so the steps below are unambiguous:

1. `GetStatus` → `player_id`. Bail on failure (nothing identifiable).
2. Seed the hint: if `DockedAtBase != ""` call `ViewStorage`, else call `ViewStorageAt(homeBase)`.
   An undocked agent with no station id gets `not_docked` back, so the seed matters.
3. Decode with `StorageFrom(GetRawJSON("storage"))`. On failure → no capture, return nil.
4. `ParseStorageHint`. **ok=false → fall back to the previously-stored base list and set
   `allowBaseDeletion=false`.** ok=true → sweep the hint's bases, `allowBaseDeletion=true`.
5. For each base in the sweep set: reuse the seed response if it is that base, else
   `ViewStorageAt(base)` with `game.SleepQuick` between calls. A per-base failure carries that
   base's **stored** rows forward and excludes it from deletion.
6. If `allowBaseDeletion` is false, union the stored bases that were not observed back in.
7. Cross-check: if the hint had a total and the swept sum is short, log loudly.
8. One `ReplaceStorage` with the final set.

- [ ] **Step 1: Write the failing tests**

Create `pkg/assets/capture_storage_test.go`:

```go
package assets

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// storageFake serves canned view_storage frames per station id, mimicking the
// client's raw-JSON cache: the frame for the most recent call is what
// GetRawJSON("storage") returns.
type storageFake struct {
	game.GameClient
	state    *game.State
	frames   map[string][]byte // station id ("" = current dock) -> raw frame
	failFor  map[string]error  // station id -> error to return
	lastRaw  []byte
	calls    []string
	statusErr error
}

func (f *storageFake) GetStatus(context.Context) error { return f.statusErr }
func (f *storageFake) GetState() *game.State           { return f.state }

func (f *storageFake) ViewStorage(ctx context.Context) error {
	return f.serve("")
}

func (f *storageFake) ViewStorageAt(ctx context.Context, stationID string) error {
	return f.serve(stationID)
}

func (f *storageFake) serve(id string) error {
	f.calls = append(f.calls, id)
	if err, ok := f.failFor[id]; ok {
		f.lastRaw = nil

		return err
	}
	f.lastRaw = f.frames[id]

	return nil
}

func (f *storageFake) GetRawJSON(key string) []byte {
	if key != "storage" {
		return nil
	}

	return f.lastRaw
}

func newStorageFake(playerID, dockedAt string) *storageFake {
	return &storageFake{
		state: &game.State{Player: game.Player{
			ID: playerID, DockedAtBase: dockedAt, HomeBase: dockedAt,
		}},
		frames:  map[string][]byte{},
		failFor: map[string]error{},
	}
}

// TestCaptureStorageSweepsHintBases pins the core flow: one seed call yields
// the agent-global hint, and every base it names gets swept.
func TestCaptureStorageSweepsHintBases(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	f := newStorageFake("p1", "base_a")
	f.frames[""] = []byte(`{"base_id":"base_a","hint":"15 items in storage at base_a, base_b",` +
		`"items":[{"item_id":"x","quantity":5}]}`)
	f.frames["base_b"] = []byte(`{"base_id":"base_b","hint":"15 items in storage at base_a, base_b",` +
		`"items":[{"item_id":"y","quantity":10}]}`)

	if err := CaptureStorage(ctx, f, st, "agent-x", now); err != nil {
		t.Fatalf("CaptureStorage: %v", err)
	}

	bases, _, err := st.LoadStorage(ctx, "p1", nil)
	if err != nil {
		t.Fatalf("LoadStorage: %v", err)
	}
	if len(bases) != 2 {
		t.Fatalf("bases = %d, want 2", len(bases))
	}
	// The seed response must be reused, not re-fetched: base_a is already in hand.
	for _, c := range f.calls {
		if c == "base_a" {
			t.Errorf("base_a was re-fetched; the seed response should be reused")
		}
	}
}

// TestCaptureStorageUnparseableHintDeletesNothing pins the most dangerous
// failure. An unparseable hint must fall back to the known base set and skip
// the base-deletion invariant -- an empty sweep is indistinguishable from "sold
// everything" and would erase real holdings.
func TestCaptureStorageUnparseableHintDeletesNothing(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	seed := []StorageBase{{BaseID: "base_a", Items: []StorageItem{{ItemID: "x", Quantity: 5}}}}
	if err := st.ReplaceStorage(ctx, "p1", seed, now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	f := newStorageFake("p1", "base_a")
	f.frames[""] = []byte(`{"base_id":"base_a","hint":"storage subsystem offline","items":[]}`)

	if err := CaptureStorage(ctx, f, st, "agent-x", now.Add(time.Hour)); err != nil {
		t.Fatalf("CaptureStorage: %v", err)
	}

	bases, _, err := st.LoadStorage(ctx, "p1", nil)
	if err != nil {
		t.Fatalf("LoadStorage: %v", err)
	}
	if len(bases) != 1 {
		t.Fatalf("bases = %d, want 1 -- an unparseable hint must never delete", len(bases))
	}
}

// TestCaptureStorageEmptySentinelClears pins the other side of the same coin:
// the server's explicit "holds nothing" sentinel is authoritative and DOES
// clear, because zero storage is genuinely reachable.
func TestCaptureStorageEmptySentinelClears(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	if err := st.ReplaceStorage(ctx, "p1", []StorageBase{
		{BaseID: "base_a", Items: []StorageItem{{ItemID: "x", Quantity: 5}}},
	}, now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	f := newStorageFake("p1", "base_a")
	f.frames[""] = []byte(`{"base_id":"base_a","hint":"No items in storage at any station.","items":[]}`)

	if err := CaptureStorage(ctx, f, st, "agent-x", now.Add(time.Hour)); err != nil {
		t.Fatalf("CaptureStorage: %v", err)
	}

	bases, _, err := st.LoadStorage(ctx, "p1", nil)
	if err != nil {
		t.Fatalf("LoadStorage: %v", err)
	}
	if len(bases) != 0 {
		t.Errorf("bases = %d, want 0 -- the empty sentinel is authoritative", len(bases))
	}
}

// TestCaptureStorageKeepsBasesThatFailedMidSweep pins per-base degradation: one
// failed ViewStorageAt must not delete that base's holdings.
func TestCaptureStorageKeepsBasesThatFailedMidSweep(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	if err := st.ReplaceStorage(ctx, "p1", []StorageBase{
		{BaseID: "base_b", Items: []StorageItem{{ItemID: "y", Quantity: 10}}},
	}, now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	f := newStorageFake("p1", "base_a")
	f.frames[""] = []byte(`{"base_id":"base_a","hint":"15 items in storage at base_a, base_b",` +
		`"items":[{"item_id":"x","quantity":5}]}`)
	f.failFor["base_b"] = errors.New("timeout")

	if err := CaptureStorage(ctx, f, st, "agent-x", now.Add(time.Hour)); err != nil {
		t.Fatalf("CaptureStorage: %v", err)
	}

	bases, _, err := st.LoadStorage(ctx, "p1", nil)
	if err != nil {
		t.Fatalf("LoadStorage: %v", err)
	}
	if len(bases) != 2 {
		t.Fatalf("bases = %d, want 2 (base_b carried forward)", len(bases))
	}
	for _, b := range bases {
		if b.BaseID == "base_b" && len(b.Items) != 1 {
			t.Errorf("base_b items = %d, want 1 carried forward", len(b.Items))
		}
	}
}

// TestCaptureStorageUndockedSeedsWithHomeBase pins that an undocked agent still
// captures. view_storage without a station_id returns not_docked, so the seed
// call must supply one.
func TestCaptureStorageUndockedSeedsWithHomeBase(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	f := newStorageFake("p1", "")
	f.state.Player.HomeBase = "base_a"
	f.frames["base_a"] = []byte(`{"base_id":"base_a","hint":"5 items in storage at base_a",` +
		`"items":[{"item_id":"x","quantity":5}]}`)

	if err := CaptureStorage(ctx, f, st, "agent-x", now); err != nil {
		t.Fatalf("CaptureStorage: %v", err)
	}
	if len(f.calls) == 0 || f.calls[0] != "base_a" {
		t.Errorf("seed call = %v, want the first call to target base_a", f.calls)
	}
}
```

The embedded `game.GameClient` in `storageFake` supplies the ~150 methods this test does not care
about; only the five overridden here matter. If the existing `capture_test.go` fake already uses
this embedding trick, follow its exact style instead.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/assets/ -run TestCaptureStorage -count=1 -v`
Expected: FAIL — `CaptureStorage undefined`.

- [ ] **Step 3: Write the implementation**

Create `pkg/assets/capture_storage.go`:

```go
package assets

import (
	"context"
	"log"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// CaptureStorage records what one agent holds at every base, discovered from
// view_storage's agent-global hint.
//
// The hint enumerates every base with holdings and is returned by ANY
// view_storage call -- including one aimed at a base where the agent holds
// nothing (verified live 2026-08-06). So base discovery is one seed call plus
// one targeted call per base, never a sweep of all 64 stations.
//
// Failure policy matches CaptureProfile: a source failure degrades to "less
// captured this pass" and returns nil. Only a store write propagates. The one
// rule that matters more than the rest: an unparseable hint must NEVER delete,
// because an empty sweep is indistinguishable from "sold everything".
func CaptureStorage(ctx context.Context, client game.GameClient, st *Store, agentID string, now time.Time) error {
	if st == nil || client == nil {
		return nil
	}
	if err := client.GetStatus(ctx); err != nil {
		return nil //nolint:nilerr // a source failure must never fail the pass
	}
	state := client.GetState()
	if state == nil || state.Player.ID == "" {
		return nil
	}
	playerID := state.Player.ID

	// Seed. An undocked agent MUST supply a station id: bare view_storage
	// answers "You must be docked or provide a station_id to view storage."
	seedStation := ""
	if state.Player.DockedAtBase == "" {
		seedStation = state.Player.HomeBase
		if seedStation == "" {
			return nil // undocked with no home base: nothing to aim the seed at
		}
	}
	seed, ok := fetchStorage(ctx, client, seedStation)
	if !ok {
		return nil
	}

	stored, _, err := st.LoadStorage(ctx, playerID, nil)
	if err != nil {
		return err
	}
	storedByBase := make(map[string]StorageBase, len(stored))
	for _, b := range stored {
		storedByBase[b.BaseID] = b
	}

	hint, hintOK := ParseStorageHint(seed.hint)
	sweep := hint.Bases
	allowBaseDeletion := true
	if !hintOK {
		// Fall back to what we already knew and delete nothing. Logged loudly:
		// a hint format change would otherwise silently freeze the ledger.
		log.Printf("assets: %s: unparseable storage hint %q; falling back to %d known base(s), no deletion",
			agentID, seed.hint, len(stored))
		sweep = make([]string, 0, len(stored))
		for _, b := range stored {
			sweep = append(sweep, b.BaseID)
		}
		allowBaseDeletion = false
	}

	final := make([]StorageBase, 0, len(sweep))
	observed := make(map[string]bool, len(sweep))
	for _, baseID := range sweep {
		if baseID == seed.base.BaseID {
			final = append(final, seed.base)
			observed[baseID] = true

			continue
		}
		time.Sleep(game.SleepQuick)
		got, ok := fetchStorage(ctx, client, baseID)
		if !ok {
			// Carry the stored rows forward: a failed query is not evidence of
			// an empty base.
			if prev, had := storedByBase[baseID]; had {
				final = append(final, prev)
				observed[baseID] = true
			}
			log.Printf("assets: %s: storage query failed at %s; carrying previous rows forward", agentID, baseID)

			continue
		}
		final = append(final, got.base)
		observed[baseID] = true
	}

	if !allowBaseDeletion {
		for _, b := range stored {
			if !observed[b.BaseID] {
				final = append(final, b)
			}
		}
	}

	// Truncation cross-check. The hint's leading count is the total across every
	// listed base (verified: databot's "920 items" equals the exact sum of its
	// ten quantities), so a short sweep means bases were omitted from the hint.
	if hintOK && hint.HasTotal {
		var sum float64
		for _, b := range final {
			for _, it := range b.Items {
				sum += it.Quantity
			}
		}
		if sum < hint.Total {
			log.Printf("assets: %s: swept %.0f of %.0f hinted items across %d base(s) -- the hint may be truncated",
				agentID, sum, hint.Total, len(final))
		}
	}

	return st.ReplaceStorage(ctx, playerID, final, now)
}

// storageFetch is one decoded view_storage response.
type storageFetch struct {
	base StorageBase
	hint string
}

// fetchStorage issues one view_storage call and decodes it. stationID == ""
// means "the current dock". ok=false covers both a call failure and an
// undecodable body -- callers must treat it as "not observed", never as "empty".
func fetchStorage(ctx context.Context, client game.GameClient, stationID string) (storageFetch, bool) {
	var err error
	if stationID == "" {
		err = client.ViewStorage(ctx)
	} else {
		err = client.ViewStorageAt(ctx, stationID)
	}
	if err != nil {
		return storageFetch{}, false
	}
	base, hint, ok, derr := StorageFrom(client.GetRawJSON("storage"))
	if derr != nil || !ok {
		return storageFetch{}, false
	}

	return storageFetch{base: base, hint: hint}, true
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/assets/ -run TestCaptureStorage -count=1 -v`
Expected: PASS (5 tests).

The sweep sleeps `game.SleepQuick` (2s) per extra base, so the multi-base tests take a few seconds.
That is acceptable; do **not** add a configurable delay just to speed the test up — a test-only
knob in production code is the anti-pattern this codebase already refuses.

- [ ] **Step 5: Verify and lint**

Run: `go build ./... && go test ./pkg/assets/ -count=1 && golangci-lint run pkg/assets/...`

- [ ] **Step 6: Commit**

```bash
git add pkg/assets/capture_storage.go pkg/assets/capture_storage_test.go
git commit -m "feat(assets): capture_storage sweeps the hinted bases

One seed call yields the agent-global hint, then one targeted ViewStorageAt per
base holding items -- ~20 free queries for the heaviest agent, once a day. An
unparseable hint falls back to the known base set and deletes nothing; a
per-base failure carries that base's rows forward; the explicit empty sentinel
is authoritative and clears. The hint's total doubles as a truncation detector.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Faction tables, decoders, and writers

**Files:**
- Modify: `pkg/assets/schema.sql` (append three tables)
- Modify: `pkg/assets/types.go` (append two types)
- Modify: `pkg/assets/parse.go` (append two decoders)
- Create: `pkg/assets/write_faction.go`
- Create: `pkg/assets/write_faction_test.go`

**Interfaces:**
- Produces:
  ```go
  type FactionProfile struct {
      FactionID, Name, Tag, LeaderID string
      Treasury, MemberCount, OwnedBases int
  }
  type FactionStorageBase struct {
      BaseID string
      Credits, FuelReserve, FuelCapacity int
      Items []StorageItem
  }
  func FactionProfileFrom(raw []byte) (FactionProfile, bool, error)
  func FactionStorageFrom(raw []byte) (FactionStorageBase, string, bool, error) // base, hint, ok, err
  func (s *Store) UpsertFactionProfile(ctx context.Context, p FactionProfile, now time.Time) error
  func (s *Store) ReplaceFactionStorage(ctx context.Context, factionID string, bases []FactionStorageBase, now time.Time) error
  func (s *Store) LoadFactionStorage(ctx context.Context, factionID string, r BaseResolver) ([]FactionStorageBase, time.Time, error)
  ```

**Note the shape decision:** the original outline had a fourth table, `faction_fuel_bunkers`.
It is **dropped** — `faction_fuel_reserve` / `faction_fuel_capacity` ride the
`view_faction_storage` response per base, so bunker state is two columns, not a table.

`replaceSet` takes a `playerID` parameter but is really "the id this delete is keyed on"; pass
`factionID`. Do not generalise or rename it — it is used by four existing callers.

- [ ] **Step 1: Add the schema**

Append to `pkg/assets/schema.sql`:

```sql
-- Faction assets get their OWN tables rather than a holder_type discriminator
-- on the agent ones: player_id and faction_id are different id spaces, faction
-- storage carries fuel-bunker columns agents do not have, and no reader can
-- forget a WHERE holder_type filter that does not exist.
CREATE TABLE IF NOT EXISTS faction_profile (
    faction_id   TEXT PRIMARY KEY,
    name         TEXT NOT NULL DEFAULT '',
    tag          TEXT NOT NULL DEFAULT '',
    leader_id    TEXT NOT NULL DEFAULT '',
    treasury     INTEGER NOT NULL DEFAULT 0,
    member_count INTEGER NOT NULL DEFAULT 0,
    owned_bases  INTEGER NOT NULL DEFAULT 0,
    captured_at  TEXT NOT NULL DEFAULT ''
);

-- fuel_reserve/fuel_capacity are the faction's bunker at that base. They ride
-- the view_faction_storage response, so they are columns here rather than a
-- separate faction_fuel_bunkers table.
CREATE TABLE IF NOT EXISTS faction_storage (
    faction_id    TEXT NOT NULL,
    base_id       TEXT NOT NULL,
    credits       INTEGER NOT NULL DEFAULT 0,
    fuel_reserve  INTEGER NOT NULL DEFAULT 0,
    fuel_capacity INTEGER NOT NULL DEFAULT 0,
    captured_at   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (faction_id, base_id)
);
CREATE INDEX IF NOT EXISTS idx_faction_storage_base ON faction_storage(base_id);

CREATE TABLE IF NOT EXISTS faction_storage_items (
    faction_id  TEXT NOT NULL,
    base_id     TEXT NOT NULL,
    item_id     TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    quantity    REAL NOT NULL DEFAULT 0,
    size        INTEGER NOT NULL DEFAULT 0,
    captured_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (faction_id, base_id, item_id)
);
CREATE INDEX IF NOT EXISTS idx_faction_storage_items_item ON faction_storage_items(item_id);
```

- [ ] **Step 2: Add the types**

Append to `pkg/assets/types.go`:

```go
// FactionProfile is the scalar half of faction_info: one row per faction.
type FactionProfile struct {
	FactionID   string
	Name        string
	Tag         string
	LeaderID    string
	Treasury    int
	MemberCount int
	OwnedBases  int
}

// FactionStorageBase is one faction's shared holdings at one base, including
// its fuel bunker there.
type FactionStorageBase struct {
	BaseID       string
	Credits      int
	FuelReserve  int
	FuelCapacity int
	Items        []StorageItem
}
```

- [ ] **Step 3: Write the failing tests**

Create `pkg/assets/write_faction_test.go`:

```go
package assets

import (
	"context"
	"testing"
	"time"
)

func TestFactionProfileFromDecodesTreasury(t *testing.T) {
	raw := []byte(`{"id":"fac1","name":"Iron Compact","tag":"IRON","leader_id":"L1",` +
		`"member_count":7,"owned_bases":2,"treasury":329427,"is_member":true}`)
	got, ok, err := FactionProfileFrom(raw)
	if err != nil || !ok {
		t.Fatalf("FactionProfileFrom = ok %v err %v", ok, err)
	}
	if got.FactionID != "fac1" || got.Treasury != 329427 || got.MemberCount != 7 {
		t.Errorf("decoded = %+v", got)
	}
}

func TestFactionStorageFromDecodesFuelBunker(t *testing.T) {
	raw := []byte(`{"faction_id":"fac1","base_id":"central_nexus","credits":1000,` +
		`"faction_fuel_reserve":4200,"faction_fuel_capacity":50000,` +
		`"hint":"9 items in storage at central_nexus",` +
		`"items":[{"item_id":"iron_ore","name":"Iron Ore","quantity":9,"size":1}]}`)
	got, hint, ok, err := FactionStorageFrom(raw)
	if err != nil || !ok {
		t.Fatalf("FactionStorageFrom = ok %v err %v", ok, err)
	}
	if got.FuelReserve != 4200 || got.FuelCapacity != 50000 {
		t.Errorf("fuel bunker = %d/%d, want 4200/50000", got.FuelReserve, got.FuelCapacity)
	}
	if hint != "9 items in storage at central_nexus" {
		t.Errorf("hint = %q", hint)
	}
}

// TestReplaceFactionStorageDropsVanishedBases pins the same two deletion grains
// as the agent tables, keyed on faction_id.
func TestReplaceFactionStorageDropsVanishedBases(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	first := []FactionStorageBase{
		{BaseID: "A", Items: []StorageItem{{ItemID: "x", Quantity: 5}}},
		{BaseID: "B", Items: []StorageItem{{ItemID: "y", Quantity: 7}}},
	}
	if err := st.ReplaceFactionStorage(ctx, "fac1", first, now); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := st.ReplaceFactionStorage(ctx, "fac1", first[:1], now.Add(time.Hour)); err != nil {
		t.Fatalf("second: %v", err)
	}

	var bases, items int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM faction_storage WHERE faction_id='fac1'`).Scan(&bases); err != nil {
		t.Fatalf("count bases: %v", err)
	}
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM faction_storage_items WHERE faction_id='fac1'`).Scan(&items); err != nil {
		t.Fatalf("count items: %v", err)
	}
	if bases != 1 || items != 1 {
		t.Errorf("bases=%d items=%d, want 1/1", bases, items)
	}
}

func TestUpsertFactionProfileOverwrites(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	p := FactionProfile{FactionID: "fac1", Name: "Iron Compact", Treasury: 100}
	if err := st.UpsertFactionProfile(ctx, p, now); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	p.Treasury = 250
	if err := st.UpsertFactionProfile(ctx, p, now.Add(time.Hour)); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	var treasury, n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*), MAX(treasury) FROM faction_profile WHERE faction_id='fac1'`).Scan(&n, &treasury); err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != 1 || treasury != 250 {
		t.Errorf("rows=%d treasury=%d, want 1/250", n, treasury)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./pkg/assets/ -run 'TestFaction|TestUpsertFaction|TestReplaceFaction' -count=1 -v`
Expected: FAIL — decoders and writers undefined.

- [ ] **Step 5: Write the decoders**

Append to `pkg/assets/parse.go`:

```go
// FactionProfileFrom decodes a raw faction_info body (cache key "faction_info").
func FactionProfileFrom(raw []byte) (FactionProfile, bool, error) {
	if len(raw) == 0 {
		return FactionProfile{}, false, nil
	}
	var resp serverapi.FactionInfoResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return FactionProfile{}, false, fmt.Errorf("assets: decode faction_info: %w", err)
	}
	if resp.ID == "" {
		return FactionProfile{}, false, nil
	}

	return FactionProfile{
		FactionID:   resp.ID,
		Name:        resp.Name,
		Tag:         strings.TrimSpace(resp.Tag), // the server pads tags to a fixed width
		LeaderID:    resp.LeaderID,
		Treasury:    resp.Treasury,
		MemberCount: resp.MemberCount,
		OwnedBases:  resp.OwnedBases,
	}, true, nil
}

// FactionStorageFrom decodes a raw view_faction_storage body (cache key
// "faction_storage" -- the classifier routes a storage-shaped payload there
// whenever faction_id is present).
func FactionStorageFrom(raw []byte) (FactionStorageBase, string, bool, error) {
	if len(raw) == 0 {
		return FactionStorageBase{}, "", false, nil
	}
	var resp serverapi.ViewFactionStorageResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return FactionStorageBase{}, "", false, fmt.Errorf("assets: decode view_faction_storage: %w", err)
	}
	out := FactionStorageBase{
		BaseID:       resp.BaseID,
		Credits:      resp.Credits,
		FuelReserve:  resp.FactionFuelReserve,
		FuelCapacity: resp.FactionFuelCapacity,
	}
	for _, it := range resp.Items {
		out.Items = append(out.Items, StorageItem{
			ItemID: it.ItemID, Name: it.Name, Quantity: it.Quantity, Size: it.Size,
		})
	}

	return out, resp.Hint, true, nil
}
```

- [ ] **Step 6: Write the writers**

Create `pkg/assets/write_faction.go` following `write_storage.go` exactly, but keyed on
`factionID`:

```go
package assets

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// UpsertFactionProfile writes the faction's scalars. One row per faction,
// refreshed by whichever member captured this cycle.
func (s *Store) UpsertFactionProfile(ctx context.Context, p FactionProfile, now time.Time) error {
	if s == nil || s.db == nil || p.FactionID == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO faction_profile
			(faction_id, name, tag, leader_id, treasury, member_count, owned_bases, captured_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(faction_id) DO UPDATE SET
			name=excluded.name, tag=excluded.tag, leader_id=excluded.leader_id,
			treasury=excluded.treasury, member_count=excluded.member_count,
			owned_bases=excluded.owned_bases, captured_at=excluded.captured_at`,
		p.FactionID, p.Name, p.Tag, p.LeaderID, p.Treasury, p.MemberCount, p.OwnedBases,
		rfc3339(now)); err != nil {
		return fmt.Errorf("assets: upsert faction profile %s: %w", p.FactionID, err)
	}

	return nil
}

// ReplaceFactionStorage swaps in the faction's full storage picture at both
// grains, exactly as ReplaceStorage does for an agent.
func (s *Store) ReplaceFactionStorage(ctx context.Context, factionID string, bases []FactionStorageBase, now time.Time) error {
	ts := rfc3339(now)

	return s.replaceSet(ctx, `DELETE FROM faction_storage WHERE faction_id = ?`, factionID, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM faction_storage_items WHERE faction_id = ?`, factionID); err != nil {
			return fmt.Errorf("assets: clear faction storage items for %s: %w", factionID, err)
		}
		for _, b := range bases {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO faction_storage
					(faction_id, base_id, credits, fuel_reserve, fuel_capacity, captured_at)
				VALUES (?,?,?,?,?,?)`,
				factionID, b.BaseID, b.Credits, b.FuelReserve, b.FuelCapacity, ts); err != nil {
				return fmt.Errorf("assets: insert faction storage %s/%s: %w", factionID, b.BaseID, err)
			}
			for _, it := range b.Items {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO faction_storage_items
						(faction_id, base_id, item_id, name, quantity, size, captured_at)
					VALUES (?,?,?,?,?,?,?)`,
					factionID, b.BaseID, it.ItemID, it.Name, it.Quantity, it.Size, ts); err != nil {
					return fmt.Errorf("assets: insert faction storage item %s/%s/%s: %w",
						factionID, b.BaseID, it.ItemID, err)
				}
			}
		}

		return nil
	})
}

// LoadFactionStorage returns the stored faction holdings, resolving base ids
// through r. CaptureFaction uses it (nil resolver) to carry failed bases forward.
func (s *Store) LoadFactionStorage(ctx context.Context, factionID string, r BaseResolver) ([]FactionStorageBase, time.Time, error) {
	if s == nil || s.db == nil || factionID == "" {
		return nil, time.Time{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT base_id, credits, fuel_reserve, fuel_capacity, captured_at
		  FROM faction_storage WHERE faction_id = ? ORDER BY base_id`, factionID)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("assets: load faction storage %s: %w", factionID, err)
	}
	defer func() { _ = rows.Close() }()

	var (
		out    []FactionStorageBase
		at     time.Time
		rawIDs []string
	)
	for rows.Next() {
		var (
			b  FactionStorageBase
			ts string
		)
		if err := rows.Scan(&b.BaseID, &b.Credits, &b.FuelReserve, &b.FuelCapacity, &ts); err != nil {
			return nil, time.Time{}, fmt.Errorf("assets: scan faction storage %s: %w", factionID, err)
		}
		at = parseCapturedAt(ts)
		rawIDs = append(rawIDs, b.BaseID)
		b.BaseID = r.resolve(b.BaseID)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("assets: iterate faction storage %s: %w", factionID, err)
	}

	for i := range out {
		items, err := s.loadFactionStorageItems(ctx, factionID, rawIDs[i])
		if err != nil {
			return nil, time.Time{}, err
		}
		out[i].Items = items
	}

	return out, at, nil
}

func (s *Store) loadFactionStorageItems(ctx context.Context, factionID, baseID string) ([]StorageItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT item_id, name, quantity, size FROM faction_storage_items
		 WHERE faction_id = ? AND base_id = ? ORDER BY item_id`, factionID, baseID)
	if err != nil {
		return nil, fmt.Errorf("assets: load faction storage items %s/%s: %w", factionID, baseID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []StorageItem
	for rows.Next() {
		var it StorageItem
		if err := rows.Scan(&it.ItemID, &it.Name, &it.Quantity, &it.Size); err != nil {
			return nil, fmt.Errorf("assets: scan faction storage item %s/%s: %w", factionID, baseID, err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("assets: iterate faction storage items %s/%s: %w", factionID, baseID, err)
	}

	return out, nil
}
```

Add `"strings"` to `parse.go`'s imports if it is not already there (it is — `ParseCurrentMax`
uses it).

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./pkg/assets/ -run 'TestFaction|TestUpsertFaction|TestReplaceFaction' -count=1 -v`
Expected: PASS (4 tests).

- [ ] **Step 8: Verify and lint**

Run: `go build ./... && go test ./pkg/assets/ -count=1 && golangci-lint run pkg/assets/...`

- [ ] **Step 9: Commit**

```bash
git add pkg/assets/schema.sql pkg/assets/types.go pkg/assets/parse.go \
        pkg/assets/write_faction.go pkg/assets/write_faction_test.go
git commit -m "feat(assets): faction profile and storage tables

Own tables rather than a holder_type discriminator: player_id and faction_id
are different id spaces and faction storage carries fuel-bunker columns agents
do not have. The planned faction_fuel_bunkers table collapses into two columns
on faction_storage, since bunker state rides the view_faction_storage response.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: `CaptureFaction` with coordination-free designation

**Files:**
- Create: `pkg/assets/capture_faction.go`
- Create: `pkg/assets/capture_faction_test.go`

**Interfaces:**
- Produces:
  ```go
  func CaptureFaction(ctx context.Context, client game.GameClient, st *Store, agentID string, now time.Time) error
  func (s *Store) IsFactionCaptor(ctx context.Context, playerID, factionID string, now time.Time) (bool, error)
  ```

**Designation rule.** Faction assets are per-faction, so one member's capture covers all. The
captor is the member with the **lowest `player_id`** among `agent_profile` rows for that
`faction_id` whose `captured_at` is fresher than 24h. Every worker evaluates this locally against
its own `assets.db` — no claim file, no lock, no shared state. If the freshness set is empty (a
cold ledger), the caller captures anyway: two members capturing once is harmless because every
write is an idempotent upsert or whole-set replacement.

- [ ] **Step 1: Write the failing tests**

Create `pkg/assets/capture_faction_test.go`:

```go
package assets

import (
	"context"
	"testing"
	"time"
)

// TestIsFactionCaptorPicksLowestPlayerID pins the coordination-free election.
// Every member evaluates the same rule against the same data and exactly one
// concludes it is the captor -- no claim file, no lock.
func TestIsFactionCaptorPicksLowestPlayerID(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	for _, id := range []string{"ccc", "aaa", "bbb"} {
		if err := st.UpsertProfile(ctx, Profile{
			PlayerID: id, FactionID: "fac1", CapturedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	for _, tc := range []struct {
		playerID string
		want     bool
	}{{"aaa", true}, {"bbb", false}, {"ccc", false}} {
		got, err := st.IsFactionCaptor(ctx, tc.playerID, "fac1", now)
		if err != nil {
			t.Fatalf("IsFactionCaptor(%s): %v", tc.playerID, err)
		}
		if got != tc.want {
			t.Errorf("IsFactionCaptor(%s) = %v, want %v", tc.playerID, got, tc.want)
		}
	}
}

// TestIsFactionCaptorIgnoresStaleMembers pins that a member whose profile has
// gone stale cannot hold the designation hostage -- otherwise a dead worker
// with the lowest id would silently stop all faction capture.
func TestIsFactionCaptorIgnoresStaleMembers(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	if err := st.UpsertProfile(ctx, Profile{
		PlayerID: "aaa", FactionID: "fac1", CapturedAt: now.Add(-72 * time.Hour),
	}); err != nil {
		t.Fatalf("seed stale: %v", err)
	}
	if err := st.UpsertProfile(ctx, Profile{
		PlayerID: "bbb", FactionID: "fac1", CapturedAt: now,
	}); err != nil {
		t.Fatalf("seed fresh: %v", err)
	}

	got, err := st.IsFactionCaptor(ctx, "bbb", "fac1", now)
	if err != nil {
		t.Fatalf("IsFactionCaptor: %v", err)
	}
	if !got {
		t.Error("a stale lowest-id member must not block a fresh member from capturing")
	}
}

// TestIsFactionCaptorBootstrapsWhenLedgerIsCold pins that an empty ledger does
// not deadlock: with nothing to compare against, the caller captures.
func TestIsFactionCaptorBootstrapsWhenLedgerIsCold(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	got, err := st.IsFactionCaptor(ctx, "aaa", "fac1", now)
	if err != nil {
		t.Fatalf("IsFactionCaptor: %v", err)
	}
	if !got {
		t.Error("a cold ledger must bootstrap by capturing, not by waiting forever")
	}
}

// TestCaptureFactionSkipsNonMembers pins that an agent with no faction does
// nothing at all -- most of the fleet is unaffiliated and must not spend calls.
func TestCaptureFactionSkipsNonMembers(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	f := newFactionFake("p1", "") // no faction
	if err := CaptureFaction(ctx, f, st, "agent-x", now); err != nil {
		t.Fatalf("CaptureFaction: %v", err)
	}
	if f.factionInfoCalls != 0 {
		t.Errorf("faction_info called %d times for a non-member, want 0", f.factionInfoCalls)
	}
}
```

Build `newFactionFake` on the same pattern as `storageFake` from Task 7: embed
`game.GameClient`, override `GetStatus`, `GetState`, `FactionInfo`, `ViewFactionStorage`,
`ViewFactionStorageAt`, and `GetRawJSON` (serving keys `"faction_info"` and `"faction_storage"`),
and count `factionInfoCalls`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/assets/ -run 'TestIsFactionCaptor|TestCaptureFaction' -count=1 -v`
Expected: FAIL — `IsFactionCaptor undefined`, `CaptureFaction undefined`.

- [ ] **Step 3: Write the implementation**

Create `pkg/assets/capture_faction.go`:

```go
package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// factionCaptorFreshness bounds which members count toward the designation. A
// member whose profile has not refreshed inside this window is presumed dead
// and cannot hold the captor role hostage.
const factionCaptorFreshness = 24 * time.Hour

// IsFactionCaptor reports whether this player is the designated captor for its
// faction this cycle.
//
// Faction assets are per-faction, so one member's capture covers every member.
// The designation is the member with the lowest player_id among those with a
// fresh profile -- a pure function of data every worker already has, so it needs
// no claim file, no lock and no shared state.
//
// A cold ledger (no fresh rows at all) returns true: bootstrapping by capturing
// is right, because every faction write is an idempotent upsert or a whole-set
// replacement, so a duplicate capture costs two free queries and nothing else.
func (s *Store) IsFactionCaptor(ctx context.Context, playerID, factionID string, now time.Time) (bool, error) {
	if s == nil || s.db == nil || playerID == "" || factionID == "" {
		return false, nil
	}
	cutoff := rfc3339(now.Add(-factionCaptorFreshness))
	// MIN() over zero matching rows returns ONE row containing SQL NULL -- not
	// sql.ErrNoRows -- so this must scan into a NullString. Scanning NULL into a
	// plain string fails with "converting NULL to string is unsupported", which
	// would turn a cold ledger into an error instead of a bootstrap.
	var lowest sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT MIN(player_id) FROM agent_profile
		 WHERE faction_id = ? AND captured_at >= ?`, factionID, cutoff).Scan(&lowest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("assets: faction captor %s: %w", factionID, err)
	}
	if !lowest.Valid || lowest.String == "" {
		return true, nil // cold ledger: bootstrap
	}

	return lowest.String == playerID, nil
}

// CaptureFaction records the agent's faction treasury and shared storage.
//
// A no-op for unaffiliated agents (most of the fleet) and for members who are
// not this cycle's designated captor. Failure policy matches CaptureProfile:
// source failures degrade silently, only store writes propagate.
func CaptureFaction(ctx context.Context, client game.GameClient, st *Store, agentID string, now time.Time) error {
	if st == nil || client == nil {
		return nil
	}
	if err := client.GetStatus(ctx); err != nil {
		return nil //nolint:nilerr // a source failure must never fail the pass
	}
	state := client.GetState()
	if state == nil || state.Player.ID == "" || state.Player.FactionID == "" {
		return nil
	}
	playerID, factionID := state.Player.ID, state.Player.FactionID

	captor, err := st.IsFactionCaptor(ctx, playerID, factionID, now)
	if err != nil {
		return err
	}
	if !captor {
		return nil
	}

	if err := client.FactionInfo(ctx); err != nil {
		return nil //nolint:nilerr // degrade to no capture this pass
	}
	profile, ok, derr := FactionProfileFrom(client.GetRawJSON("faction_info"))
	if derr != nil || !ok {
		return nil
	}
	if err := st.UpsertFactionProfile(ctx, profile, now); err != nil {
		return err
	}

	// Seed the storage sweep from wherever the agent is. Like the agent hint,
	// the faction hint enumerates every base with holdings.
	seedStation := ""
	if state.Player.DockedAtBase == "" {
		seedStation = state.Player.HomeBase
		if seedStation == "" {
			return nil
		}
	}
	seed, ok := fetchFactionStorage(ctx, client, seedStation)
	if !ok {
		return nil
	}

	stored, _, err := st.LoadFactionStorage(ctx, factionID, nil)
	if err != nil {
		return err
	}
	storedByBase := make(map[string]FactionStorageBase, len(stored))
	for _, b := range stored {
		storedByBase[b.BaseID] = b
	}

	hint, hintOK := ParseStorageHint(seed.hint)
	sweep := hint.Bases
	allowBaseDeletion := true
	if !hintOK {
		log.Printf("assets: %s: unparseable faction storage hint %q; falling back to %d known base(s), no deletion",
			agentID, seed.hint, len(stored))
		sweep = make([]string, 0, len(stored))
		for _, b := range stored {
			sweep = append(sweep, b.BaseID)
		}
		allowBaseDeletion = false
	}

	final := make([]FactionStorageBase, 0, len(sweep))
	observed := make(map[string]bool, len(sweep))
	for _, baseID := range sweep {
		if baseID == seed.base.BaseID {
			final = append(final, seed.base)
			observed[baseID] = true

			continue
		}
		time.Sleep(game.SleepQuick)
		got, ok := fetchFactionStorage(ctx, client, baseID)
		if !ok {
			if prev, had := storedByBase[baseID]; had {
				final = append(final, prev)
				observed[baseID] = true
			}
			log.Printf("assets: %s: faction storage query failed at %s; carrying previous rows forward",
				agentID, baseID)

			continue
		}
		final = append(final, got.base)
		observed[baseID] = true
	}
	if !allowBaseDeletion {
		for _, b := range stored {
			if !observed[b.BaseID] {
				final = append(final, b)
			}
		}
	}

	return st.ReplaceFactionStorage(ctx, factionID, final, now)
}

// factionStorageFetch is one decoded view_faction_storage response.
type factionStorageFetch struct {
	base FactionStorageBase
	hint string
}

func fetchFactionStorage(ctx context.Context, client game.GameClient, stationID string) (factionStorageFetch, bool) {
	var err error
	if stationID == "" {
		err = client.ViewFactionStorage(ctx)
	} else {
		err = client.ViewFactionStorageAt(ctx, stationID)
	}
	if err != nil {
		return factionStorageFetch{}, false
	}
	base, hint, ok, derr := FactionStorageFrom(client.GetRawJSON("faction_storage"))
	if derr != nil || !ok {
		return factionStorageFetch{}, false
	}

	return factionStorageFetch{base: base, hint: hint}, true
}
```

The `sql.NullString` above is not defensive styling — it is required. `MIN()` over zero matching
rows returns one row holding SQL NULL rather than `sql.ErrNoRows`, and scanning NULL into a plain
`string` fails with "converting NULL to string is unsupported". With a plain string, the
cold-ledger bootstrap test fails on a scan error rather than returning `true`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/assets/ -run 'TestIsFactionCaptor|TestCaptureFaction' -count=1 -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Verify and lint**

Run: `go build ./... && go test ./pkg/assets/ -count=1 && golangci-lint run pkg/assets/...`

- [ ] **Step 6: Commit**

```bash
git add pkg/assets/capture_faction.go pkg/assets/capture_faction_test.go
git commit -m "feat(assets): capture_faction with a coordination-free captor

Faction assets are per-faction, so one member's capture covers all. The captor
is the lowest player_id among members with a fresh profile -- a pure function of
data every worker already holds, needing no claim file or lock. A stale member
cannot hold the role hostage, and a cold ledger bootstraps by capturing, since
every faction write is idempotent.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Wire both commands into the worker and `play_as`

**Files:**
- Modify: `pkg/worker/dispatch.go` (`supported` map + two dispatch cases)
- Modify: `pkg/worker/dispatch_test.go` (append)
- Modify: `cmd/tools/play_as/main.go` (two command cases + two help lines)
- Modify: `data/overmind/roles.yaml` (schedule entries)

**Interfaces:**
- Consumes: `assets.CaptureStorage`, `assets.CaptureFaction` from Tasks 7 and 9.

**`roles_test.go` enforces that every command in `roles.yaml` appears in the `supported` map**, so
the map entry and the schedule line must land in the same commit or the build breaks.

- [ ] **Step 1: Write the failing test**

Append to `pkg/worker/dispatch_test.go`:

```go
// TestCaptureStorageAndFactionAreSupported pins both new commands into the
// curated vocabulary. roles_test.go requires every command named in
// data/overmind/roles.yaml to appear here, so a schedule line without a map
// entry fails the build.
func TestCaptureStorageAndFactionAreSupported(t *testing.T) {
	d := &WorkerDispatch{}
	for _, cmd := range []string{"capture_storage", "capture_faction"} {
		if !d.Supports(cmd) {
			t.Errorf("%s must be in the supported command set", cmd)
		}
	}
}

// TestCaptureStorageWithoutStoreIsANoOp pins that a worker launched without
// --assets-db-path runs the command harmlessly rather than erroring every
// scheduled pass.
func TestCaptureStorageWithoutStoreIsANoOp(t *testing.T) {
	d := &WorkerDispatch{Client: &fakeClient{state: &game.State{}}, Out: io.Discard}
	for _, cmd := range []string{"capture_storage", "capture_faction"} {
		if err := d.Run(context.Background(), []string{cmd}); err != nil {
			t.Errorf("%s without a store must be a no-op, got %v", cmd, err)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/worker/ -run TestCaptureStorageAndFactionAreSupported -count=1 -v`
Expected: FAIL — `capture_storage must be in the supported command set`.

- [ ] **Step 3: Add the dispatch cases and map entries**

In `pkg/worker/dispatch.go`, extend the `supported` map (add to the `capture_profile` line):

```go
	"update_market": true, "capture_fuel": true, "capture_profile": true,
	"capture_storage": true, "capture_faction": true,
```

And add the cases next to `capture_profile` in `Run`:

```go
	case "capture_profile":
		// Nil store = capture disabled; not an error. See the Assets field.
		return assets.CaptureProfile(ctx, d.Client, d.Assets, d.AgentID, time.Now())
	case "capture_storage":
		return assets.CaptureStorage(ctx, d.Client, d.Assets, d.AgentID, time.Now())
	case "capture_faction":
		return assets.CaptureFaction(ctx, d.Client, d.Assets, d.AgentID, time.Now())
```

- [ ] **Step 4: Add the `play_as` commands**

In `cmd/tools/play_as/main.go`, next to the `capture_profile` case (around line 8551):

```go
	case "capture_storage":
		if globalAssets == nil {
			fmt.Println("capture_storage: no assets DB configured (use --assets-db-path)")

			return nil
		}

		return assets.CaptureStorage(ctx, client, globalAssets, globalAgentID, time.Now())
	case "capture_faction":
		if globalAssets == nil {
			fmt.Println("capture_faction: no assets DB configured (use --assets-db-path)")

			return nil
		}

		return assets.CaptureFaction(ctx, client, globalAssets, globalAgentID, time.Now())
```

And the help lines next to the `capture_profile` one (around line 9578):

```go
	fmt.Println("  capture_storage           - Capture this agent's storage holdings at every base")
	fmt.Println("  capture_faction           - Capture the agent's faction treasury and shared storage")
```

- [ ] **Step 5: Add the schedule entries**

In `data/overmind/roles.yaml`, add a `daily` line to **every role that already has
`capture_profile`** (`resident`, `resident_gas`, `resident_ice`, `hauler`, `craftsman`,
`missionrunner`). For example, under `resident`:

```yaml
      - { every: hourly, command: "capture_profile" }
      - { every: daily, command: "capture_storage" }
      - { every: daily, command: "capture_faction" }
```

`capture_faction` self-gates on membership, so it costs an unaffiliated agent one `get_status`
and nothing more.

**Stage `roles.yaml` explicitly.** `data/` is full of live-fleet churn; never `git add -A`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./pkg/worker/ -count=1`
Expected: PASS, including `roles_test.go`'s cross-check of `roles.yaml` against `supported`.

- [ ] **Step 7: Verify the whole tree**

Run: `go build ./... && go test ./... -count=1 && golangci-lint run`
Expected: clean. `go test ./...` (not just the two packages) is required here — a `GameClient`
change breaks mocks in `pkg/agent` and `pkg/skills` that `go build` alone does not catch. This
task adds no interface methods, but run it anyway.

- [ ] **Step 8: Commit**

```bash
git add pkg/worker/dispatch.go pkg/worker/dispatch_test.go \
        cmd/tools/play_as/main.go data/overmind/roles.yaml
git commit -m "feat(worker): wire capture_storage and capture_faction

Both scheduled daily on every role that already captures a profile. A nil store
keeps them harmless no-ops, so this stays inert until a worker is launched with
--assets-db-path.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Coverage extension and the ovdash panel

**Files:**
- Modify: `pkg/assets/coverage.go` (extend `coverageSources`)
- Modify: `pkg/assets/coverage_test.go` (append)
- Modify: `frontend/src/lib/useFleetStream.ts` (add `asset_coverage` to the snapshot types)
- Create: `frontend/src/components/overmind/AssetCoveragePanel.tsx`
- Modify: `frontend/src/components/overmind/OvermindPage.tsx` (render the panel)

**Interfaces:**
- Consumes: `Snapshot.AssetCoverage []assets.CoverageRow` `json:"asset_coverage,omitempty"`, which
  already exists (`pkg/ovdash/snapshot.go:94`) and is already merged under `s.mu` in
  `refresh()` (`cmd/overmind-dashboard/main.go:69-113`). **No Go plumbing changes are needed
  beyond the source list** — the data already reaches the browser; nothing renders it.

`CoverageRow` is `{Source, Agents, Oldest, Stale}` with lowercase JSON tags.

- [ ] **Step 1: Write the failing Go test**

Append to `pkg/assets/coverage_test.go`:

```go
// TestCoverageIncludesStorageAndFaction pins the new sources into the
// dashboard's freshness report. A capture that nothing watches is the failure
// mode this ledger was built to avoid -- daily-summary went silent for 25 days.
func TestCoverageIncludesStorageAndFaction(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	rows, err := Coverage(ctx, st.DB(), now, 48*time.Hour)
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		seen[r.Source] = true
	}
	for _, want := range []string{"agent_storage", "faction_storage"} {
		if !seen[want] {
			t.Errorf("Coverage is missing source %q", want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/assets/ -run TestCoverageIncludesStorageAndFaction -count=1 -v`
Expected: FAIL — `Coverage is missing source "agent_storage"`.

- [ ] **Step 3: Extend the source list**

In `pkg/assets/coverage.go`:

```go
// coverageSources are the tables Coverage reports on, in display order.
//
// faction_storage is keyed on faction_id rather than player_id, so its "agents"
// column counts FACTIONS. The panel labels it accordingly -- the alternative,
// leaving it out, would let a stalled faction capture go unnoticed, which is
// exactly the silence this query exists to break.
var coverageSources = []string{
	"agent_profile", "agent_carrier", "agent_hulls", "agent_skills",
	"agent_storage", "faction_storage",
}
```

`Coverage` builds its query as `SELECT COUNT(DISTINCT player_id) … FROM %s`, which fails on
`faction_storage` (no `player_id` column). Change the loop to select the key column per table:

```go
// coverageKeyColumn is the identity column each source is counted by.
func coverageKeyColumn(table string) string {
	if strings.HasPrefix(table, "faction_") {
		return "faction_id"
	}

	return "player_id"
}
```

and substitute it into the query in place of the hardcoded `player_id` — **two occurrences**, in
`COUNT(DISTINCT player_id)` and in the `CASE WHEN … THEN player_id END`. Add `"strings"` to the
imports. The `#nosec G201` comment still applies: both the table and the column come from fixed
lists, never user input.

- [ ] **Step 4: Run the Go tests to verify they pass**

Run: `go test ./pkg/assets/ ./pkg/ovdash/ -count=1 -v`
Expected: PASS. The pre-existing coverage tests must still pass — in particular the one pinning
that `Stale` counts distinct agents rather than rows.

- [ ] **Step 5: Add the TypeScript types**

In `frontend/src/lib/useFleetStream.ts`, add the row type and extend `Snapshot`:

```ts
export interface AssetCoverageRow {
  source: string;
  agents: number;
  oldest: string;
  stale: number;
}
```

```ts
interface Snapshot {
  agents: AgentState[] | null;
  off_map: AgentState[] | null;
  stale_fleets: string[] | null;
  removed?: Record<string, string[]>;
  overminds?: Record<string, OvermindInfo>;
  current_overmind?: string;
  current_worker?: string;
  asset_coverage?: AssetCoverageRow[];
}
```

Add `assetCoverage: AssetCoverageRow[]` to the `FleetStream` interface, initialise it to `[]`, and
set it in the `snapshot` event handler alongside the other snapshot-only fields (the pattern at
`useFleetStream.ts:151-158`). **Leave the `delta` handler untouched** — coverage only ever arrives
on a full snapshot, and those fields persist across deltas by design.

- [ ] **Step 6: Write the panel**

Create `frontend/src/components/overmind/AssetCoveragePanel.tsx`:

```tsx
import type { AssetCoverageRow } from '../../lib/useFleetStream';

// Per-source cadences, in hours. A source is flagged once it exceeds 2x its
// cadence: one missed boundary is churn, two is a stall worth looking at.
const CADENCE_HOURS: Record<string, number> = {
  agent_profile: 1,
  agent_carrier: 1,
  agent_hulls: 1,
  agent_skills: 1,
  agent_storage: 24,
  faction_storage: 24,
};

// faction_storage is keyed on faction_id, so its count is factions, not agents.
const COUNT_LABEL: Record<string, string> = {
  faction_storage: 'factions',
};

function ageHours(oldest: string): number | null {
  if (!oldest) return null;
  const t = Date.parse(oldest);
  if (Number.isNaN(t)) return null;

  return (Date.now() - t) / 3_600_000;
}

export function AssetCoveragePanel({ coverage }: { coverage: AssetCoverageRow[] }) {
  if (!coverage.length) {
    // The ledger is not deployed (no worker running with --assets-db-path).
    // Render nothing rather than an empty table claiming zero coverage.
    return null;
  }

  return (
    <section className="asset-coverage">
      <h3>Asset ledger freshness</h3>
      <table>
        <thead>
          <tr>
            <th>source</th>
            <th>known</th>
            <th>stale</th>
            <th>oldest</th>
          </tr>
        </thead>
        <tbody>
          {coverage.map((row) => {
            const cadence = CADENCE_HOURS[row.source] ?? 24;
            const age = ageHours(row.oldest);
            const alarm = row.stale > 0 || (age !== null && age > cadence * 2);

            return (
              <tr key={row.source} className={alarm ? 'stale' : undefined}>
                <td>{row.source}</td>
                <td>
                  {row.agents} {COUNT_LABEL[row.source] ?? 'agents'}
                </td>
                <td>{row.stale}</td>
                <td>{age === null ? '—' : `${age.toFixed(1)}h`}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </section>
  );
}
```

Match the styling approach of the sibling components — if `AccountingStrip.tsx` uses CSS modules
or inline styles rather than plain class names, follow that instead of introducing a new pattern.

- [ ] **Step 7: Render it**

In `frontend/src/components/overmind/OvermindPage.tsx`, import the panel and render it below
`<AccountingStrip …/>`:

```tsx
<AssetCoveragePanel coverage={stream.assetCoverage} />
```

- [ ] **Step 8: Build the frontend**

Run:
```bash
cd frontend && npm run build
```
Expected: no TypeScript errors. A type error here is the point of the exercise — the `Snapshot`
interface genuinely lacked `asset_coverage` before this task.

- [ ] **Step 9: Verify the whole tree**

Run: `go build ./... && go test ./... -count=1 && golangci-lint run`

- [ ] **Step 10: Commit**

```bash
git add pkg/assets/coverage.go pkg/assets/coverage_test.go \
        frontend/src/lib/useFleetStream.ts \
        frontend/src/components/overmind/AssetCoveragePanel.tsx \
        frontend/src/components/overmind/OvermindPage.tsx
git commit -m "feat(ovdash): render asset ledger freshness

Coverage gains the storage and faction sources, counted by faction_id where the
table is keyed that way. The panel is the piece slices 1-4 specified but never
built: the coverage JSON already reached the browser and nothing displayed it,
so the rollout step 'watch the panel' could not be performed.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Rollout

The branch stays inert until a worker gets `--assets-db-path`. When rolling out:

1. **Canary first, off-fleet.** Build `play_as` from the worktree, run from the main repo:
   ```bash
   printf 'capture_profile\ncapture_storage\ncapture_faction\nquit\n' \
     | bin/play_as-canary --assets-db-path /tmp/canary-assets.db --debug=1 <agent>
   ```
   Verify every row against the raw frames field-for-field. `databot` (1 storage base) and
   `prophet-1` (1 base, 2,268 items) are known-good targets; `random-clark` exercises the empty
   sentinel.
2. **Then one live worker**, not the fleet. Watch `agent_storage` row counts and the ovdash panel.
3. **Then the fleet**, remembering all workers fire `capture_storage` at the same daily boundary
   (UTC midnight) because the `spread:` flag is still deferred. Watch for `SQLITE_BUSY` in the
   worker logs at that boundary; if it appears, `spread:` moves from "follow-on" to "next".
4. `assets.db` is a separate file precisely so any of this can be deleted and rebuilt without
   touching `market.db` or `spacemolt-knowledge.db`.

## Follow-on work, unchanged

1. `spread: true` scheduler flag (tier-2 jitter).
2. Module/fitting capture, for "can this agent be refitted for the role".
3. Faction garages, once one is built.
4. Ship-class capacity lookup, which removes the `freight` rule's documented over-reporting.
5. A read API. This plan still ships **substrate, not answers**: "what can we source for free"
   remains a hand-written `sqlite3` query. That is a deliberate v1 boundary, but it is now the
   largest gap between what the ledger holds and what anyone can ask it.
