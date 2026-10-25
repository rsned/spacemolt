# Contraband Pipeline Dashboard (plan 3 of 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A read-only web dashboard that shows each contraband production line as a chain of coloured nodes and links, names the agents on every node, reports units produced and sold per day, and raises the spec's alert texts — moonshine line first.

**Architecture:** A new package `pkg/linedash` turns a declarative line file (`data/overmind/lines.yaml`) plus the data the fleet already writes (fleet yamls, overrides, secondment ledger, `*-status.json`, `assets.db` storage snapshots and action-log events, `market.db` orders, `shopkeep.json`) into a `LineReport` through pure, tested evaluation functions; a small Go server `cmd/tools/line-dashboard` renders the top board and the per-line SCADA view from that report (self-contained HTML + inline SVG, the craft-dashboard serving pattern) and also serves the report as JSON for the later automation phase. Nothing in it writes to any game or fleet state.

**Tech Stack:** Go 1.24 (`net/http`, `html/template`, `encoding/json`), `gopkg.in/yaml.v3`, `modernc.org/sqlite` (read-only opens), existing packages `pkg/overmind/supervisor` (LoadFleet, LoadOverrides, LoadSecondments), `pkg/overmind/balances` (StatusFile), `pkg/assets` (schema), `pkg/market` (schema in tests).

**Spec:** docs/superpowers/specs/2026-10-24-contraband-production-pipeline-design.md — section D7 (dashboard), with node/agent facts from D1, D3, D4, D5, D6, D9 and the "Measurement plumbing" facts.

## Global Constraints

- Read-only: the dashboard never calls the game, never writes a fleet yaml, schedule, override or queue file (spec D7 "Read-only"; "Automatic node reassignment" is a non-goal).
- Sources are only files/DBs that already exist: `assets.db`, `market.db`, `data/overmind/*-status.json`, `data/overmind/*-fleet.yaml` + `*-overrides.json`, `data/overmind/secondments.json`, `data/overmind/handoff-queue.json`, `data/agents/<id>/shopkeep.json` (spec D7 bullet 1).
- Demand figures exclude `market_orders.source = 'worker'` rows (spec Anti-goals: craftsman-1's vacuum bids are never "demand").
- Starshine and `energy_crystal` never appear on any line (spec Non-goals).
- Colours and grammar follow the reference screenshot: dark grid canvas, legend green "Optimal" / yellow "Starved" / red "Choked / Backed up", rounded-rectangle nodes with a status dot and a thick bottom border in the state colour, dashed curved links, a three-figure stats row, display-only sliders (spec D7).
- All new Go code passes `golangci-lint` with no new findings; binaries build into `bin/`; every sleep uses a `pkg/game/constants.go` Sleep constant (project rules).
- No new fleet-yaml fields, no worker changes: this plan is observer-only. The one data change is adding capture schedule entries (Task 8), which is additive seeding per the standing rule.

## Review Focus

Failure modes the spec implies but no spec sentence pins; each has a test in the owning task.

1. **A data source is missing or hours stale** (assets.db locked, a status file gone, no capture since yesterday) — the page must still render every node, mark the source stale in grey, and never show a green state computed from stale data as if it were live. → Task 6 `TestEvalStaleSourceGreysNode`, Task 7 `TestRenderBoardWithMissingSources`.
2. **An agent listed on a node is override-removed or seconded away** (every ice bot is in the unlock queue right now) — the Mine node must say "0 of 4 bots live, 4 in unlock" rather than "starved" with no explanation. → Task 2 `TestRosterSecondedAndRemoved`, Task 6 `TestEvalMineNodeAllBotsAway`.
3. **Gift events name agents by display name, not id** (`gift_received.sender` is `"Arthur 'Artificer' Artis"` for one agent and `"miningbot_0000"` for another) — movement must be attributed by the sender's own `gift_sent` row joined through `agents.player_id`, never by parsing names. → Task 4 `TestEventsGiftSentAttributedBySenderPlayer`.
4. **An ask is open but at a different price than shopkeep.json remembers** (operator repriced by hand, as happened 10-24) — the Sell node must show the exchange's own `my_quantity` row as truth and shopkeep.json only for posted-at age. → Task 5 `TestMarketOurAskPrefersExchangeRow`.
5. **Two lines share a Store or Sell node** (gutter flux will reuse Voss faction storage and the same shopkeepers) — the same node id in two lines must evaluate independently with each line's items, and the board must not double-count sold units. → Task 1 `TestLoadLinesSharedNodeIdsAllowedAcrossLines`, Task 6 `TestEvalSoldUnitsFilteredByLineItem`.

Known gap (no task): the spec's "facility busy" alert needs a craft-refused signal that nothing captures today (`crafting.*` action-log events record only queued/completed). The alert text is reserved in the `Alert` enum and never raised in v1; the plan records this in Task 6's doc comment and the status board.

---

## File map

| File | Responsibility |
|---|---|
| `pkg/linedash/config.go` | `lines.yaml` types, `LoadLines`, validation, threshold defaults |
| `pkg/linedash/roster.go` | fleet yamls + overrides + secondments → `map[string]RosterEntry` |
| `pkg/linedash/live.go` | `*-status.json` → `map[string]Live` |
| `pkg/linedash/tanks.go` | assets.db storage snapshots → `[]TankLevel` per node |
| `pkg/linedash/events.go` | assets.db action-log events in a window → produced / sold / moved / withdrawn |
| `pkg/linedash/market.go` | market.db demand + our open asks; `shopkeep.json` ages |
| `pkg/linedash/eval.go` | states, alerts, rates → `LineReport` (pure) |
| `pkg/linedash/layout.go` | node columns/rows → SVG coordinates (pure) |
| `pkg/linedash/render.go` | board page, line page, JSON |
| `pkg/linedash/testdata/` | fixture yaml/json/status files for tests |
| `cmd/tools/line-dashboard/main.go` | flags, handlers, cached refresh loop |
| `data/overmind/lines.yaml` | the moonshine line as declared nodes/edges |
| `docs/COLD_START.md` | one launch block for the new dashboard |
| `data/agents/{craftsman-8,craftsman-10,trader-9,trader-10,marketbot_alhena,marketbot_treasure_cache}/schedule.json` | hourly `capture_action_log` (+ hourly `capture_faction` on the two crafters) |

Shared vocabulary (defined in Task 1, used everywhere after):

```go
type State string
const (
    StateOptimal State = "optimal" // green
    StateStarved State = "starved" // yellow
    StateChoked  State = "choked"  // red
    StateUnknown State = "unknown" // grey: no fresh data to judge
)

type NodeType string // "mine" | "purchase" | "store" | "convert" | "produce" | "sell"
type EdgeKind string // "haul" | "consign" | "craft"
```

Haul links are **edges**, not nodes (my call, recorded here: the spec's node list names "Haul link" but its line-view sentence puts the hauler id and last delivery time on the *link*, and the screenshot has no haul boxes). "convert" is defined for the gutter-flux line and unused by moonshine.

---

### Task 1: Line config and the moonshine line file

**Files:**
- Create: `pkg/linedash/config.go`, `pkg/linedash/config_test.go`
- Create: `data/overmind/lines.yaml`
- Create: `pkg/linedash/testdata/lines_moonshine.yaml` (a copy of the real file, kept in sync by the test that loads both)

**Interfaces:**
- Produces:

```go
type Thresholds struct {
    HaulWindow   time.Duration `yaml:"haul_window"`   // default 4h: a haul edge with no gift_sent in this window has not moved
    MineWindow   time.Duration `yaml:"mine_window"`   // default 4h: a Mine node with no drop in this window is dry
    CraftWindow  time.Duration `yaml:"craft_window"`  // default 1h: a Produce node with no crafting.completed in this window is stalled
    SellWindow   time.Duration `yaml:"sell_window"`   // default 6h: a Sell node with no exchange_fill in this window is not selling
    SellStale    time.Duration `yaml:"sell_stale"`    // default 6h: an ask older than this at the floor = market saturated
    StaleData    time.Duration `yaml:"stale_data"`    // default 2h: a source captured longer ago than this is stale
    RateWindow   time.Duration `yaml:"rate_window"`   // default 24h: units/day window
    SellLot      int           `yaml:"sell_lot"`      // default 50: one shopkeeper ask lot
    ChokeUnits   int           `yaml:"choke_units"`   // default 1000: a drop/output tank at or above this with no outflow is choked
    BrokeCredits int           `yaml:"broke_credits"` // default 500 (spec D6)
    FuelFloorPct int           `yaml:"fuel_floor_pct"`// default 10: fuel below this % while undocked = stranded
}

type Holder struct {           // where a tank's units sit
    Agent   string `yaml:"agent,omitempty"`   // personal storage of this agent_id, OR
    Faction string `yaml:"faction,omitempty"` // faction storage of this faction tag (e.g. CRFT)
    Station string `yaml:"station"`           // base_id
}

type Node struct {
    ID          string         `yaml:"id"`
    Type        NodeType       `yaml:"type"`
    Name        string         `yaml:"name"`
    Items       []string       `yaml:"items"`                  // tank items shown on the node
    Agents      []string       `yaml:"agents,omitempty"`       // agent_ids attached
    Holders     []Holder       `yaml:"holders,omitempty"`      // tank locations
    Station     string         `yaml:"station,omitempty"`      // produce/sell/purchase station base_id
    Recipe      string         `yaml:"recipe,omitempty"`       // produce: recipe_id
    UnitsPerRun int            `yaml:"units_per_run,omitempty"`// produce: output units per run
    InputsPerRun map[string]int `yaml:"inputs_per_run,omitempty"`
    BatchRuns   int            `yaml:"batch_runs,omitempty"`   // produce: runs per craft command
}

type Edge struct {
    From     string   `yaml:"from"`
    To       string   `yaml:"to"`
    Kind     EdgeKind `yaml:"kind"`
    Item     string   `yaml:"item,omitempty"`   // the item that moves; empty on craft edges
    Agents   []string `yaml:"agents,omitempty"` // the senders whose gift_sent rows are this link's movement
    Label    string   `yaml:"label,omitempty"`  // default by kind: "haul consigned" | "consign" | "craft"
}

type Line struct {
    ID             string `yaml:"id"`
    Name           string `yaml:"name"`
    OutputItem     string `yaml:"output_item"`
    CapacityPerDay int    `yaml:"capacity_per_day"` // 100% facility figure, for "Avg Factory Eff"
    Nodes          []Node `yaml:"nodes"`
    Edges          []Edge `yaml:"edges"`
}

type Config struct {
    Thresholds Thresholds `yaml:"thresholds"`
    Lines      []Line     `yaml:"lines"`
}

func LoadLines(path string) (Config, error)       // reads, applies defaults, validates
func (c Config) Validate() error                   // rules below
func DefaultThresholds() Thresholds
```

Validation rules (each is one test case): every edge `from`/`to` names a node id in the same line; node ids unique within a line (the same id may appear in two lines); `type` is one of the six; a `produce` node has `recipe`, `units_per_run >= 1`, `batch_runs >= 1`, non-empty `inputs_per_run`; a `sell`/`purchase`/`produce` node has `station`; a `holder` has exactly one of `agent`/`faction`; no node or edge item is `energy_crystal` or `starshine` (the Global Constraint, enforced at load); `capacity_per_day >= 1`.

- [ ] **Step 1: Write the failing tests** in `config_test.go`: `TestLoadLinesMoonshineFixture` (loads `testdata/lines_moonshine.yaml`, asserts 1 line, id `moonshine`, 7 nodes, 6 edges, thresholds equal `DefaultThresholds()` where unset, `HaulWindow == 4*time.Hour`); `TestLoadLinesRealFileMatchesFixture` (loads `../../data/overmind/lines.yaml` and the fixture and asserts `reflect.DeepEqual`); `TestValidateRejectsDanglingEdge`; `TestValidateRejectsEnergyCrystal`; `TestValidateRejectsProduceWithoutRecipe`; `TestLoadLinesSharedNodeIdsAllowedAcrossLines` (two lines both with node id `voss_output` load without error).
- [ ] **Step 2: Run** `go test ./pkg/linedash/ -run 'TestLoadLines|TestValidate' -v` → FAIL (package does not exist).
- [ ] **Step 3: Implement `config.go`** with the types above, `LoadLines`, `Validate`, `DefaultThresholds`. Durations parse via yaml.v3's `time.Duration` string support (`"4h"`).
- [ ] **Step 4: Write `data/overmind/lines.yaml`** exactly as below (the moonshine line as rolled out 10-24; station ids are base ids, agents as in `craft-fleet.yaml`, `mb-fleet.yaml`, `crft-mining-fleet.yaml`), and copy it to the fixture path:

```yaml
# Contraband production lines for the line dashboard (design D7).
# Declarative: nodes are tanks/agents, edges are the links between them.
# Holders say where a tank's units sit; events are attributed by agent_id.
thresholds: { haul_window: 4h, mine_window: 4h, craft_window: 1h, sell_window: 6h, sell_stale: 6h, stale_data: 2h, rate_window: 24h, sell_lot: 50, choke_units: 1000, broke_credits: 500, fuel_floor_pct: 10 }
lines:
  - id: moonshine
    name: Pirate Moonshine
    output_item: pirate_moonshine
    capacity_per_day: 19200        # 3,200 runs x 6 at 2.7 ticks/run (spec)
    nodes:
      - { id: ice_mine, type: mine, name: "Ice Mine", items: [water_ice],
          agents: [miningbot_0000, miningbot_0001, miningbot_0002, miningbot_0003],
          holders: [ { agent: trader-9, station: frontier_station }, { agent: trader-9, station: deep_range_outpost } ] }
      - { id: culture_buy, type: purchase, name: "Culture Purchase", items: [pharmaceutical_culture],
          agents: [trader-10], station: sirius_observatory_station,
          holders: [ { agent: trader-10, station: sirius_observatory_station } ] }
      - { id: voss_inputs, type: store, name: "Voss Inputs", items: [water_ice, pharmaceutical_culture],
          holders: [ { faction: CRFT, station: voss_redoubt_station }, { agent: craftsman-8, station: voss_redoubt_station }, { agent: craftsman-10, station: voss_redoubt_station } ] }
      - { id: still, type: produce, name: "Backwater Still", items: [pirate_moonshine],
          agents: [craftsman-8, craftsman-10], station: voss_redoubt_station,
          recipe: distill_moonshine, units_per_run: 6, batch_runs: 60,
          inputs_per_run: { water_ice: 10, pharmaceutical_culture: 2 } }
      - { id: voss_output, type: store, name: "Voss Output", items: [pirate_moonshine],
          holders: [ { faction: CRFT, station: voss_redoubt_station }, { agent: craftsman-8, station: voss_redoubt_station }, { agent: craftsman-10, station: voss_redoubt_station }, { agent: trader-9, station: voss_redoubt_station } ] }
      - { id: alhena_sell, type: sell, name: "Voss Shop", items: [pirate_moonshine],
          agents: [marketbot_alhena], station: voss_redoubt_station,
          holders: [ { agent: marketbot_alhena, station: voss_redoubt_station } ] }
      - { id: tc_sell, type: sell, name: "Treasure Cache Shop", items: [pirate_moonshine],
          agents: [marketbot_treasure_cache], station: treasure_cache_trading_post,
          holders: [ { agent: marketbot_treasure_cache, station: treasure_cache_trading_post } ] }
    edges:
      - { from: ice_mine,    to: voss_inputs, kind: haul,    item: water_ice,              agents: [trader-9] }
      - { from: culture_buy, to: voss_inputs, kind: haul,    item: pharmaceutical_culture, agents: [trader-10] }
      - { from: voss_inputs, to: still,       kind: craft }
      - { from: still,       to: voss_output, kind: craft }
      - { from: voss_output, to: alhena_sell, kind: consign, item: pirate_moonshine,       agents: [craftsman-8] }
      - { from: voss_output, to: tc_sell,     kind: haul,    item: pirate_moonshine,       agents: [trader-9] }
```

- [ ] **Step 5: Run** the tests → PASS. Run `golangci-lint run ./pkg/linedash/` → clean.
- [ ] **Step 6: Commit** `git add pkg/linedash/config.go pkg/linedash/config_test.go pkg/linedash/testdata/lines_moonshine.yaml data/overmind/lines.yaml` / `feat(linedash): line config types, validation, moonshine line file`.

---

### Task 2: Roster (who is assigned where) and Live (where they are)

**Files:**
- Create: `pkg/linedash/roster.go`, `pkg/linedash/live.go`, tests, fixtures under `testdata/fleet/` (two small fleet yamls, an overrides json, a secondments json) and `testdata/status/` (two status files).

**Interfaces:**
- Consumes: `supervisor.LoadFleet(path) ([]WorkerSpec, error)`, `supervisor.LoadOverrides(path) (Overrides, error)`, `supervisor.LoadSecondments(path) (Secondments, error)` and `Secondments.Active(agentID)`; `balances.StatusFile{CapturedAt, Workers []LiveRecord}` with `LiveRecord{AgentID, System, POI, Docked, Credits, Fuel, MaxFuel, CargoUsed, CargoCapacity, Healthy, Quarantined, LastSeen}` (field tags verified in `pkg/overmind/balances/balances.go:28-67`).
- Produces:

```go
type RosterEntry struct {
    AgentID, Fleet, Role, Station string
    Pipeline    string   // raw "item,system,poi,to,recipient" or ""
    Consign     []string
    Shopkeep    []string // "item:start[:floor]"
    FactionPull []string
    Removed     bool     // in the fleet's overrides `removed`
    AwayFleet   string   // secondment away_fleet when phase is nominated/seconded/returning/failed, else ""
    Phase       string   // secondment phase or ""
}
// LoadRoster reads every <fleet>-fleet.yaml in dir, its <fleet>-overrides.json if present, and secondments.json.
// An agent listed in two yamls keeps the entry whose fleet has it NOT removed; ties keep the first by file name.
func LoadRoster(dir string) (map[string]RosterEntry, error)

// ShopFloor parses "item:start[:floor]" rules; floor defaults to start when absent.
func ShopFloor(rules []string, item string) (start, floor int, ok bool)

type Live struct {
    AgentID, Fleet, System, POI string
    Docked, Healthy, Quarantined bool
    Credits, Fuel, MaxFuel, CargoUsed, CargoCap float64
    LastSeen, CapturedAt time.Time
}
// LoadLive reads every *-status.json in dir (not only the ovdash Fleets registry: crft-mining is absent from it).
// The same agent in two files keeps the freshest captured_at (the dedupe rule ovdash already uses).
func LoadLive(dir string) (map[string]Live, error)
```

- [ ] **Step 1: Write failing tests**: `TestLoadRosterReadsPipelineFields` (fixture crft-mining yaml line for miningbot_0000 → Pipeline string, Role `pipeline_miner`); `TestRosterSecondedAndRemoved` (miningbot_0004 in crft-mining `removed` and in unlock yaml, secondment phase `seconded` away_fleet `unlock` → entry Fleet `unlock`, Removed false, AwayFleet `unlock`, Phase `seconded`; miningbot_0026 removed in both → Removed true); `TestShopFloor` (`"pirate_moonshine:3000:1500"` → 3000, 1500; `"x:200"` → 200, 200); `TestLoadLiveDedupesByFreshest`; `TestLoadLiveMissingDirIsEmptyNotError`.
- [ ] **Step 2: Run** `go test ./pkg/linedash/ -run 'TestLoadRoster|TestRosterSeconded|TestShopFloor|TestLoadLive' -v` → FAIL.
- [ ] **Step 3: Implement** `roster.go` and `live.go`. Fleet name = file name minus `-fleet.yaml`; overrides path = `<fleet>-overrides.json`. Parse times with `time.RFC3339`; an unparsable `last_seen` leaves the zero time (it is then "stale", never "fresh").
- [ ] **Step 4: Run** tests → PASS; `golangci-lint run ./pkg/linedash/` clean.
- [ ] **Step 5: Commit** `feat(linedash): roster from fleet yamls/overrides/secondments; live from status files`.

---

### Task 3: Tanks (storage snapshots)

**Files:**
- Create: `pkg/linedash/tanks.go`, `pkg/linedash/tanks_test.go` (test opens a temp assets.db via `assets.Open(assets.Config{...})` so the real `schema.sql` is applied, then inserts rows with plain SQL).

**Interfaces:**
- Consumes: assets.db tables `agents(player_id, agent_id, username)`, `agent_storage_items(player_id, base_id, item_id, quantity, captured_at)`, `faction_storage_items(faction_id, base_id, item_id, quantity, captured_at)`, `faction_profile` (to map tag `CRFT` → faction_id; verify the column names in `pkg/assets/schema.sql:199-212` before coding).
- Produces:

```go
type TankLevel struct {
    Holder     Holder
    Item       string
    Units      float64
    CapturedAt time.Time // zero when the holder has no row for the item
}
type Tanks struct{ db *sql.DB; factionIDs map[string]string /* tag -> id */ ; players map[string]string /* agent_id -> player_id */ }
func OpenTanks(ctx context.Context, assetsPath string) (*Tanks, error) // read-only: "file:<path>?mode=ro&_pragma=busy_timeout(5000)" as ovdash.openAssetsRO does
func (t *Tanks) Close() error
// Levels returns one TankLevel per (holder, item) for the node, in the node's holder order then item order; missing rows yield Units 0 and zero CapturedAt.
func (t *Tanks) Levels(ctx context.Context, n Node) ([]TankLevel, error)
func SumUnits(levels []TankLevel, item string) float64
func Freshest(levels []TankLevel) time.Time
```

- [ ] **Step 1: Write failing tests**: `TestLevelsAgentAndFactionHolders` (craftsman-10 personal 120 water_ice at voss + CRFT faction 1455 water_ice at voss → two levels; `SumUnits(.., "water_ice") == 1575`); `TestLevelsMissingRowIsZeroWithZeroTime`; `TestLevelsUnknownFactionTagErrors`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS, lint clean.
- [ ] **Step 5: Commit** `feat(linedash): tank levels from assets.db storage snapshots`.

---

### Task 4: Events (what moved, was made, was sold)

**Files:**
- Create: `pkg/linedash/events.go`, `pkg/linedash/events_test.go` (same temp-assets.db approach as Task 3).

**Interfaces:**
- Consumes: `action_log_events(player_id, event_id, event_type, created_at, data_json)` joined to `agents`. Payloads (all values are JSON **strings**, verified live 10-24): `crafting.completed {recipe_id, runs, facility_id}`; `trading.exchange_fill {base_id, item_id, price, quantity, role: "seller"|"buyer", order_id}`; `trading.gift_sent {item_id, quantity, recipient, sender, source}` and `trading.gift_received {item_id, quantity, recipient, sender}` (credit gifts have `credits` and no `item_id` — skip them; `sender` is a display name for most agents and the bare agent id for the miningbots, whose username equals their id); `faction.withdraw {base_id, faction_id, item_id, quantity}`; `storage.deposit_items {item_id, quantity}`.
- Produces:

```go
type Event struct {
    AgentID   string    // from agents.agent_id via player_id (never from payload names)
    Type      string    // event_type
    At        time.Time
    Item      string
    Units     int       // quantity, or runs*unitsPerRun is NOT applied here (eval does it)
    Runs      int       // crafting.completed only
    Recipe    string    // crafting.completed only
    Station   string    // base_id when present
    Role      string    // exchange_fill only
    Price     int       // exchange_fill only
    Sender    string    // gift_received only: payload sender resolved to an agent_id through agents.username (or agents.agent_id); raw value when unresolved
}
type Events struct{ db *sql.DB }
func OpenEvents(ctx context.Context, assetsPath string) (*Events, error)
// Window returns the six event types above for the given agents with created_at >= since, oldest first.
func (e *Events) Window(ctx context.Context, agents []string, since time.Time) ([]Event, error)
// Cursor freshness: when each agent's action log was last walked (action_log_cursor.captured_at); zero when never.
func (e *Events) CursorAges(ctx context.Context, agents []string) (map[string]time.Time, error)
```

- [ ] **Step 1: Write failing tests**: `TestWindowParsesStringPayloads` (one row of each type → Units/Runs/Price ints, Role, Station); `TestEventsGiftSentAttributedBySenderPlayer` (a gift_sent row stored under craftsman-10's player_id whose payload `sender` is the display name → `AgentID == "craftsman-10"`); `TestWindowResolvesGiftReceivedSenderByUsername` (a gift_received row under trader-9 with `sender` `"miningbot_0000"` → `Sender == "miningbot_0000"`; with `sender` `"Harrison 'Handiwork' Hay"` and that username in `agents` → `Sender == "craftsman-10"`); `TestWindowSkipsCreditGifts`; `TestWindowFiltersAgentsAndSince`; `TestCursorAges`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** (one query with `event_type IN (...)` and `player_id IN (select player_id from agents where agent_id IN (...))`; parse `data_json` with `map[string]string` then `strconv.Atoi`, tolerating missing keys as 0/""). **Step 4: Run** → PASS, lint clean.
- [ ] **Step 5: Commit** `feat(linedash): action-log events window for line agents`.

---

### Task 5: Market (demand and our asks)

**Files:**
- Create: `pkg/linedash/market.go`, `pkg/linedash/market_test.go` (temp market.db via `market.Open(market.Config{DBPath: tmp, WAL: true})`, then raw inserts into `market_orders`; stations/items FK rows inserted first).

**Interfaces:**
- Consumes: `market_orders(station_id, item_id, side, price_each, quantity, my_quantity, source, captured_at)`; `data/agents/<agent>/shopkeep.json` = `worker.ShopState{Items map[string]ShopItemState{Price, OrderID, PostedAt, LastFillAt}}` (import `pkg/worker` for the type, or mirror it locally if that import drags in game deps — the implementer checks `go list -deps` and mirrors if it does, noting the mirror in a comment).
- Produces:

```go
type Demand struct {
    Station   string
    BidUnits  float64 // sum quantity of side='buy' rows from the LATEST capture at the station, excluding source='worker'
    BestBid   float64 // max price_each among those rows
    CapturedAt time.Time
}
type OurAsk struct {
    Station    string
    Price      float64 // from the exchange row (my_quantity > 0) — truth
    Units      float64 // my_quantity
    SeenAt     time.Time // captured_at of that row
    PostedAt   time.Time // shopkeep.json PostedAt when the OrderID matches or when no exchange row exists; else zero
    OrderID    string
}
type Market struct{ db *sql.DB; agentsDir string }
func OpenMarket(ctx context.Context, marketPath, agentsDir string) (*Market, error) // read-only
func (m *Market) Demand(ctx context.Context, station, item string) (Demand, error)
// OurAsk reads the newest sell row with my_quantity > 0 for (station,item) and overlays shopkeep.json of agent for PostedAt.
func (m *Market) OurAsk(ctx context.Context, agent, station, item string) (OurAsk, bool, error)
```

"Latest capture" = rows whose `captured_at` equals `MAX(captured_at)` for that (station, item, side); a station never captured yields `BidUnits 0` and zero time, not an error.

- [ ] **Step 1: Write failing tests**: `TestDemandExcludesWorkerSource` (bids 59,126 @1 source '' and 50,000 @1 source 'worker' → BidUnits 59,126); `TestDemandUsesLatestCaptureOnly`; `TestMarketOurAskPrefersExchangeRow` (shopkeep.json says Price 5500, exchange row my_quantity 15 @3000 → Price 3000, Units 15, PostedAt from json); `TestOurAskNoRowNoJsonIsFalse`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS, lint clean.
- [ ] **Step 5: Commit** `feat(linedash): demand excluding worker bids; our asks from exchange rows + shopkeep age`.

---

### Task 6: Evaluation — states, alerts, rates

**Files:**
- Create: `pkg/linedash/eval.go`, `pkg/linedash/eval_test.go` (pure: builds `Inputs` structs by hand, no DBs).

**Interfaces:**
- Consumes: everything above.
- Produces:

```go
type Alert string
const (
    AlertInputsStarved  Alert = "output stalled waiting on inputs"
    AlertMarketSaturated Alert = "market saturated"
    AlertFacilityBusy   Alert = "facility busy"       // reserved; never raised in v1 (no capture signal)
    AlertHaulerStarved  Alert = "hauler starved"
    AlertSourceDepleted Alert = "source depleted"
    AlertAgentBroke     Alert = "agent broke/stranded"
    AlertNoBotsLive     Alert = "no bots on node"     // all attached miners away/removed
    AlertOutputNotDrawn Alert = "output not drawn"    // produce output tank growing, no faction_pull/withdraw
    AlertDataStale      Alert = "data stale"
)

type AgentReport struct {
    AgentID, Fleet, Role, Where string // Where = "<system>/<poi>" or "docked <poi>"
    Live, Healthy, Quarantined, Removed bool
    AwayFleet, Phase string            // secondment
    Credits, FuelPct float64
    Broke, Stranded bool
    LastSeen time.Time
}
type NodeReport struct {
    ID string; Type NodeType; Name string
    State State; Alerts []Alert
    Tanks []TankLevel; Units map[string]float64 // item -> summed units
    Agents []AgentReport
    LiveAgents, AssignedAgents int               // slider figures
    Demand *Demand; Ask *OurAsk                  // sell nodes
    LastEventAt time.Time                        // the event that drove the state
    Stale bool                                   // freshest relevant source older than StaleData
    Detail []string                              // human lines shown under the node
    X, Y int                                     // filled by layout
}
type EdgeReport struct {
    From, To string; Kind EdgeKind; Item, Label string
    State State; Agents []string
    LastMovedAt time.Time; MovedUnitsWindow int // RateWindow sum of gift_sent units by edge agents for Item
}
type LineReport struct {
    ID, Name string; State State; Alerts []Alert
    ProducedPerDay, SoldPerDay, CapacityPerDay int; EffPct int
    Nodes []NodeReport; Edges []EdgeReport
    StaleSources []string; GeneratedAt time.Time
}
type Inputs struct {
    Now      time.Time
    Cfg      Thresholds
    Roster   map[string]RosterEntry
    Live     map[string]Live
    Tanks    map[string][]TankLevel   // node id -> levels
    Events   []Event                  // window = RateWindow, all line agents
    Cursors  map[string]time.Time
    Demand   map[string]Demand        // node id -> demand (sell nodes)
    Asks     map[string]*OurAsk       // node id -> ask (sell nodes)
}
func Evaluate(line Line, in Inputs) LineReport
func AgentsOf(line Line) []string // every agent on any node, edge or holder, deduped, sorted (the Events/Live query set)
```

State rules, by node type (each bullet is one named test; thresholds from `Inputs.Cfg`):

- **Agent flags** (`agentReport`): `Live` = present in Live with `LastSeen` within `StaleData`; `Broke` = `Credits < BrokeCredits`; `Stranded` = `!Docked && Fuel/MaxFuel*100 < FuelFloorPct`; either raises `AlertAgentBroke` on the node. → `TestEvalAgentBrokeAndStranded`.
- **mine**: `LiveAgents` = attached agents Live and not Removed and `AwayFleet == ""`. **Drops** = `trading.gift_received` rows logged by the node's holder agents (trader-9) whose `Sender` resolves to an attached agent, with Item in node Items. The miners' own logs are never walked (no `capture_action_log` on `pipeline_miner`), so the recipient's log is the only record of a drop. If `LiveAgents == 0` → `StateStarved` + `AlertNoBotsLive`, Detail lists each away agent as `"<id>: <phase> in <away_fleet>"`. Else if a drop within `MineWindow` → `StateOptimal`; else → `StateStarved` + `AlertSourceDepleted`. If `SumUnits(tanks, item) >= ChokeUnits` and the outgoing haul edge has not moved within `HaulWindow` → `StateChoked` (overrides). → `TestEvalMineNodeAllBotsAway`, `TestEvalMineOptimalOnRecentDrop`, `TestEvalMineChokedWhenDropTankFullAndHaulIdle`.
- **purchase**: buys = `exchange_fill` with `Role == "buyer"` and Item in Items by attached agents. Tank ≥ one hauler-lot (`ChokeUnits`) → Optimal regardless; else buy within `RateWindow` → Optimal; else Starved + Detail `"no purchase in <window>"`. → `TestEvalPurchase`.
- **store**: inflow = gift_sent by incoming-edge agents (haul/consign) or `crafting.completed` by the upstream produce node; outflow = for a store feeding a produce node, `crafting.completed` by that node's agents; for a store feeding sell edges, the outgoing edges' gift_sent. `SumUnits == 0` for every item → Starved; any item `>= ChokeUnits` and no outflow within `HaulWindow` → Choked; else Optimal. → `TestEvalStoreChokedNoOutflow`, `TestEvalStoreEmptyStarved`.
- **produce**: completed = `crafting.completed` by attached agents with `Recipe == node.Recipe`. For each input item: `need = InputsPerRun[item] * BatchRuns`; if upstream store `Units[item] < need` → Starved + `AlertInputsStarved` with Detail `"<item> <have>/<need> for one batch"`. Else completed within `CraftWindow` → Optimal; else Starved + `AlertInputsStarved`? No: inputs present but no craft = **Choked** with Detail `"inputs present, no craft in <window>"` (the still is not running; closest spec state is choked/backed-up). Output: if downstream store `Units[output] >= ChokeUnits` and no `faction.withdraw` by attached agents within `HaulWindow` → Choked + `AlertOutputNotDrawn`. → `TestEvalProduceStarvedBelowOneBatch`, `TestEvalProduceOptimal`, `TestEvalProduceChokedOutputNotDrawn`.
- **sell**: fills = `exchange_fill` `Role == "seller"`, `Station == node.Station`, Item in Items, by attached agents. `Ask == nil && Units[item] < SellLot` → Starved (Detail `"stock <n> below one lot"`). `Ask != nil && Now-Ask.PostedAt > SellStale && Ask.Price <= floor` (floor from `ShopFloor(roster[agent].Shopkeep, item)`) → Choked + `AlertMarketSaturated`. Fill within `SellWindow` → Optimal. Ask open, not stale → Optimal. Otherwise Starved. → `TestEvalSellSaturatedAtFloor`, `TestEvalSellStarvedNoStockNoAsk`, `TestEvalSellOptimalOnFill`.
- **edges**: `haul`/`consign`: `LastMovedAt` = newest gift_sent by edge Agents with Item; moved within `HaulWindow` → Optimal; else if `from` node `Units[Item] >= SellLot` → Choked + `AlertHaulerStarved` on the **from** node (the goods wait; the spec's name for it); else Starved (nothing to move). `craft` edges take the produce node's state. → `TestEvalEdgeHaulerStarved`, `TestEvalEdgeStarvedNothingToMove`.
- **stale**: a node whose relevant sources (freshest tank `CapturedAt`, every attached agent's cursor) are all older than `StaleData` gets `Stale = true`, `AlertDataStale`, and its State forced to `StateUnknown` (grey) — never a green from old data. → `TestEvalStaleSourceGreysNode`.
- **rates**: `ProducedPerDay` = Σ `Runs * UnitsPerRun` over produce nodes' `crafting.completed` within `RateWindow`; `SoldPerDay` = Σ seller fills of `OutputItem` at sell-node stations by sell agents within `RateWindow` (so a shared shopkeeper selling another line's item is not counted) ; `EffPct = ProducedPerDay*100/CapacityPerDay`. → `TestEvalRates`, `TestEvalSoldUnitsFilteredByLineItem`.
- **line state** = worst node/edge state (Choked > Starved > Unknown > Optimal); `Alerts` = deduped union, node order.

- [ ] **Step 1: Write the failing tests** named above in `eval_test.go`, with a `fixtureLine()` helper that returns the moonshine `Line` from Task 1's fixture and an `at(h float64)` helper for `Now.Add(-h*time.Hour)`.
- [ ] **Step 2: Run** `go test ./pkg/linedash/ -run TestEval -v` → FAIL.
- [ ] **Step 3: Implement `eval.go`** per the rules. Keep each node type's rule in its own function (`evalMine`, `evalPurchase`, `evalStore`, `evalProduce`, `evalSell`, `evalEdge`). Put the "facility busy" gap in the `AlertFacilityBusy` doc comment.
- [ ] **Step 4: Run** → PASS; lint clean. **Step 5: Commit** `feat(linedash): node/edge state evaluation, alerts, units per day`.

---

### Task 7: Layout, render, server

**Files:**
- Create: `pkg/linedash/layout.go`, `layout_test.go`, `pkg/linedash/render.go`, `render_test.go`, `pkg/linedash/page.gohtml` (embedded with `//go:embed`), `cmd/tools/line-dashboard/main.go`
- Modify: `docs/COLD_START.md` (dashboards section, after the overmind-status block)

**Interfaces:**
- Produces:

```go
// Layout assigns X,Y: column = longest path from any source node (nodes with no incoming edge) ; row = order within the column; grid 260x140 px with 60 px gutters, 40 px margin. Deterministic.
func Layout(r *LineReport)
func RenderBoard(reports []LineReport, refreshSec int) ([]byte, error)      // "/"
func RenderLine(r LineReport, refreshSec int) ([]byte, error)              // "/line/<id>"
```

Page contract (from the spec's screenshot grammar): `<title>Line Dashboard</title>`; dark background `#0f1420` with a 20 px CSS grid pattern; legend top-left with three dots and the texts `Optimal`, `Starved`, `Choked / Backed up`; a pause button top-right that toggles the meta auto-refresh (JS flips a `data-paused` attribute and `clearInterval`); board = one row per line: name, state dot, `<n> /day produced`, `<n> /day sold`, alert texts joined by " · ", linking to `/line/<id>`; line page = inline `<svg>` with one `<g class="node <state>">` per node (rect rx=10, a 6 px bottom bar in the state colour, a status dot, the name, then one line per tank `"<item> <units>"`, then agents `"<id>"` greyed when not live), one dashed `<path>` per edge with a quadratic curve and a label `"<Label> · <agents> · <ago>"` (ago as `"12m"`, `"3h"`, `"never"`); stats row of three figures: `Output Rate <n> /day`, `Avg Factory Eff <n>%`, `Active Alerts <n>`; one display-only `<input type=range disabled>` per mine/purchase node labelled `"<Name> <live>/<assigned> bots"`. Colours: green `#22c55e`, yellow `#eab308`, red `#ef4444`, grey `#6b7280`. Every text from data passes through `html/template` escaping (no `template.HTML` on data fields).

Server: flags `--addr :8093` (implementer confirms the port is free with `ss -ltn | grep 8093`), `--lines data/overmind/lines.yaml`, `--overmind-dir data/overmind`, `--agents-dir data/agents`, `--assets-db data/assets.db`, `--market-db data/market.db`, `--refresh 60`; routes `/`, `/line/{id}`, `/api/lines.json`, `/api/line/{id}.json`, `/healthz`; a single `build(ctx)` reloads lines.yaml and every source, evaluates all lines, lays them out, and is run on each request at most once per `refreshSec/2` (cached result under an `RWMutex`); any source error is logged and surfaced as a `StaleSources` entry — the page always renders. `ReadHeaderTimeout: 5 * time.Second` like craft-dashboard. Logs `[line-dashboard] serving N lines on :8093`.

- [ ] **Step 1: Write failing tests**: `TestLayoutColumnsFollowLongestPath` (moonshine: ice_mine & culture_buy column 0; voss_inputs 1; still 2; voss_output 3; both sells 4; rows distinct within a column); `TestRenderLineContainsNodesEdgesStatsAndSliders` (asserts the texts `Backwater Still`, `haul consigned`, `Output Rate`, `Avg Factory Eff`, `Active Alerts`, `Ice Mine 0/4 bots`, `Choked / Backed up`, and that a node name containing `<script>` is escaped); `TestRenderBoardWithMissingSources` (a report with `StaleSources: ["assets.db"]` renders the row and the text `data stale`); `TestRenderBoardRowLinksToLine`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** `layout.go`, `render.go` + `page.gohtml`, then `main.go`. **Step 4: Run** tests → PASS; `go build -o bin/line-dashboard ./cmd/tools/line-dashboard`; lint clean.
- [ ] **Step 5: Smoke against live data** (read-only): `./bin/line-dashboard --addr :8093 >> data/overmind/line-dashboard.log 2>&1 &`, then `curl -s localhost:8093/api/line/moonshine.json | head -c 1200` shows 7 nodes with states, and `curl -s localhost:8093/ | grep -c 'Pirate Moonshine'` ≥ 1. Leave it running (the operator views it). Record the pid in the report.
- [ ] **Step 6: Add the launch block to `docs/COLD_START.md`** after the overmind-status block:

```bash
go build -o bin/line-dashboard ./cmd/tools/line-dashboard
setsid nohup ./bin/line-dashboard --addr ":8093" \
  >> data/overmind/line-dashboard.log 2>&1 < /dev/null &              # :8093 contraband lines
```

- [ ] **Step 7: Commit** `git add pkg/linedash cmd/tools/line-dashboard docs/COLD_START.md` / `feat(line-dashboard): SCADA-style line view, top board, JSON api`.

---

### Task 8: Capture cadence for the line agents (data, additive)

The dashboard's event spine is the action log, walked `twice_daily` on the crafters and traders today; faction storage (the Store tanks) is captured daily. Both must be hourly on the seven line agents or every node reads "data stale".

**Files:**
- Modify: `data/agents/{craftsman-8,craftsman-10,trader-9,trader-10,marketbot_alhena,marketbot_treasure_cache}/schedule.json` — add `{ "frequency": "hourly", "command": "capture_action_log" }` where no hourly entry exists; change an existing `twice_daily` capture_action_log entry's frequency to `hourly` rather than adding a duplicate. Add `{ "frequency": "hourly", "command": "capture_faction" }` to craftsman-8 and craftsman-10. Entries are `{id, frequency, command, created_at}`; `id` = max existing id + 1; `created_at` = now in RFC3339Nano UTC (matches existing rows).
- No roles.yaml change (miners' drops are seen through trader-9's gift events; adding a daily walk to 31 bots is not worth the command budget).

- [ ] **Step 1:** Edit the six files with a short Go or `jq` script run from the scratchpad; verify with `jq '.[] | select(.command|test("capture_action_log|capture_faction")) | {frequency,command}' data/agents/<id>/schedule.json` per agent — expect hourly rows only.
- [ ] **Step 2:** Validate every edited file parses: `for a in ...; do jq empty data/agents/$a/schedule.json; done` → no output.
- [ ] **Step 3 (controller/operator step — a worker relaunch):** schedules load at worker start, so relaunch the six workers via their overminds' admin path exactly as plan 2 Task 7 did for the schedule reload (craft, haul→crft-mining, mb). Verify each is back in its `*-status.json` within 2 minutes. **Do not restart a disconnected worker** (standing rule).
- [ ] **Step 4:** After the next hour boundary, `sqlite3 -readonly data/assets.db "select a.agent_id, c.captured_at from action_log_cursor c join agents a using(player_id) where a.agent_id in (...)"` shows all six within the hour, and `faction_storage` for `voss_redoubt_station` captured within the hour. The line page shows no `data stale` badge on Voss nodes.
- [ ] **Step 5: Commit only the six schedule files** (never `git add -A`; `data/*.json` is dirty fleet-wide): `git add data/agents/{craftsman-8,craftsman-10,trader-9,trader-10,marketbot_alhena,marketbot_treasure_cache}/schedule.json` / `fleet: hourly action-log and faction-storage capture on the moonshine line agents`.

---

### Task 9: Status board and resume memory

**Files:**
- Modify: memory `project_status_board.md` (live block ≤ 15 entries: replace the "plan 3 next" entry with "line dashboard LIVE :8093; facility-busy alert has no signal; day-one metrics read from `/api/line/moonshine.json`"), `project_contraband_pipeline_resume.md` (RESUME POINT: plan 3 done, where the dashboard runs, how to add a line = a `lines.yaml` block, the trader-8 stranded ice note below).
- Operator note to carry: the 10-24 smoke config gifted miningbot_0000's ice to **trader-8**, which now holds 2,389 water_ice at `frontier_station` outside the line; one `deliver water_ice 2389 frontier_station voss_redoubt craftsman-10` by trader-8 (or a gift to trader-9) returns it to the line. Not a dashboard task; recorded so it is not lost.

- [ ] **Step 1:** Edit the two memory files; run `make memory-sync`; commit `docs/memory` explicitly (`docs(memory): line dashboard live; plan 3 complete`).

---

## Self-review

1. **Spec coverage (D7):** Go binary alongside existing dashboards, reusing the serving pattern → Task 7 (craft-dashboard pattern). Sources listed in D7 bullet 1 → Tasks 2–5 (status files, fleet yamls, assets.db storage, market.db bids excluding worker, shopkeep state; the handoff queue is read nowhere — it has no moonshine records and D5's withdraw step replaced it; `consign_stock` is superseded by personal-storage snapshots, which are the same number one hop earlier). Top board row contents → Task 7 board. Screenshot grammar: legend, pause, nodes with dot + bottom border, dashed curved links, stats row, sliders → Task 7 page contract. Line view node types: Mine, Purchase, Convert (type defined, unused), Produce, Store, Sell as nodes; Haul link as edge with hauler id + last delivery time → Tasks 1, 6, 7. States and thresholds → Task 6 with defaults in Task 1. Sliders display-only showing bot count → Task 7. Read-only, no auth → Global Constraints. Measurement cadence ("Tank levels need it hourly on line agents") → Task 8. D6 "dashboard alerts on any line agent under 500" → Task 6 `AlertAgentBroke`. Anti-goal (exclude vacuum bids) → Task 5.
2. **Step scan:** every code step names its file and signature; the two behaviours left to the implementer (SVG curve control point, column spacing) are cosmetic and bounded by the page contract.
3. **Type consistency:** `TankLevel`, `Holder`, `Node`, `Edge`, `Line`, `Thresholds` defined in Tasks 1/3 and used unchanged in Task 6's `Inputs`/reports and Task 7's render; `Demand`/`OurAsk` (Task 5) referenced by pointer in `NodeReport`; `RosterEntry.Shopkeep` feeds `ShopFloor` in Task 6.
4. **Review Focus:** all five lines have a named test in the owning task (1→6,7; 2→2,6; 3→4; 4→5; 5→1,6).
5. **Proportion:** the plan is longer than D7 because D7 delegates thresholds, windowing and layout to the builder; those decisions are the bulk of this document and would otherwise be made seven times by seven implementers.
