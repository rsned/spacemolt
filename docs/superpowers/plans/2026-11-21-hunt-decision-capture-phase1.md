# Hunt Decision Capture, Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Hunters provision ammo, stay out for one job across several grounds, return to rearm before running dry, and every provision and return choice is recorded through one decision seam.

**Architecture:** A new leaf package `pkg/huntdecide` holds the seam (`Decider`), an exploring wrapper (built, switched off), a recording wrapper, the JSON-lines record log and the per-agent sortie file. `pkg/worker` gains a pure rules decider, a compact-state builder, dock-side provisioning, a rearm leg, and a reshaped `Hunt` that opens, resumes and closes sorties. `Hunt` keeps its one-call-per-pass shape and its never-return-an-error contract; the sortie file carries state between calls.

**Tech Stack:** Go 1.24+, standard library only (no new dependencies), existing `pkg/reload`, `pkg/market`, `pkg/outcomes`, `pkg/worker` test fakes.

**Spec:** `docs/superpowers/specs/2026-11-21-hunt-decision-capture-design.md` (this plan covers its phase 1 only; phases 2 to 6 get their own plans).

## Global Constraints

- Go 1.24+: use range-over-int; `b.Loop()` in any benchmark.
- `golangci-lint run ./...` adds no new findings; run it after every task.
- `go build ./...` and `go test ./...` pass before every commit. Binaries go in `bin/`.
- No `time.Sleep` in `pkg/worker`. Use `deps.sleep` / `settle(ctx, d)` with a `game.Sleep*` constant. If none fits, stop and ask the operator.
- Check real server field names before coding against a reply (`serverapi` structs and captured frames), never assume them.
- Never edit an applied KB migration. Phase 1 needs none.
- Commit with explicit paths (`git add <files>`), never `git add -A`: `data/` is dirty by design.
- JSON Schema files declare `"$schema": "https://json-schema.org/draft/2020-12/schema"`.
- Records are built from the `huntdecide.State` projection only. Never marshal `game.State` (it carries `Password`).
- Hard gates are not decisions and are never offered to the seam or explored: predator present, hull-floor flee, worn-down flee, tick and no-progress give-ups, policed-only routes, difficulty cap, mission allowlist, species roles, default stance flee.
- `Hunt` never returns an error that kills the worker loop. Every new failure path logs and returns nil.
- Values fixed by the spec or its author's calls: ammo credit cap default **5,000** per sortie; explorer ε **0** for every kind in this phase; depot-low line below **2** sorties' worth.
- Game fact (item catalog): one ammo box or case is one cargo unit and fully loads one magazine. A Gauntlet's hold is 30. Provisioning counts **reloads** (ammo items), not rounds. Guided missiles are unverified against this rule; Task 6 checks it live before relying on it.

## Review Focus

1. `get_ship` fails or omits modules at the dock: provisioning fails open (the hunter leaves as it would today) and writes no decision record built from made-up ammo counts. Test in Task 6.
2. The record file or sortie file cannot be written (disk full, permissions): the hunt carries on; nothing panics or ends the pass. Tests in Tasks 2 and 3.
3. `hunt-sortie.json` is corrupt or half-written after a crash: treated as no open sortie, logged once, overwritten by the next sortie. Test in Task 3 and Task 8.
4. The hold is nearly full, or a Station Need has hold reserved: provisioning never withdraws or buys more than fits and never eats the need's reserved space. Test in Task 6.
5. An energy-only ship (no ammo-fed weapon): no provision decision, never `rearm`, never `blocked_unarmed`. Tests in Tasks 5 and 6.

---

## File Structure

| File | Responsibility |
|---|---|
| `pkg/huntdecide/types.go` (create) | `Kind`, `State` and its parts, `Option`, `Decision`, `Choice`, `Decider` |
| `pkg/huntdecide/explorer.go` (create) | ε-random wrapper |
| `pkg/huntdecide/log.go` (create) | record types, JSON-lines `Log`, `Recorder` wrapper |
| `pkg/huntdecide/record.schema.json` (create) | Draft 2020-12 schema for the three record types |
| `pkg/huntdecide/sortie.go` (create) | `Sortie` state file, result constants |
| `pkg/outcomes/outcomes.go` (modify) | `RecordStatus` so an `ok` outcome can carry a reason |
| `pkg/reload/plan.go` (modify) | `Loadout` / `FetchLoadout`: ammo-fed plan plus ammo-free weapon count |
| `pkg/worker/hunt_rules.go` (create) | pure rules `Decider` for `provision` and `return` |
| `pkg/worker/hunt_state.go` (create) | builds `huntdecide.State` from the client, loadout and sortie |
| `pkg/worker/hunt_provision.go` (create) | dock-side provisioning: storage, market, unarmed gate, depot-low line |
| `pkg/worker/hunt_rearm.go` (create) | rearm destination choice and the leg there |
| `pkg/worker/hunt_sortie.go` (create) | open, resume, return decision, close, outcome report |
| `pkg/worker/hunt.go` (modify) | new `HuntDeps` fields; `Hunt` reshaped around the sortie |
| `pkg/worker/dispatch.go`, `cmd/worker/main.go`, `pkg/overmind/supervisor/{config,supervisor}.go` (modify) | wiring and flags |
| `.gitignore`, `data/overmind/hunt-fleet.yaml`, `docs/ops/hunt-refit-and-depot.md` | ignore rules, stale header, ops steps |

---

### Task 1: The seam and the explorer

**Files:**
- Create: `pkg/huntdecide/types.go`, `pkg/huntdecide/explorer.go`
- Test: `pkg/huntdecide/explorer_test.go`

**Interfaces:**
- Produces (all fields carry snake_case json tags; `omitempty` only where noted):

```go
package huntdecide

type Kind string
const (
	KindProvision Kind = "provision"
	KindReturn    Kind = "return"
)

type Weapon struct {
	ModuleID, TypeID, DamageType string
	Reach, Cooldown              int
	AmmoType                     string // "" = ammo-free
	MagazineSize, Loaded         int
	LoadedAmmoID                 string
}
type Ammo struct { // one ammo stack aboard
	AmmoType, ItemID string
	Aboard           int // items; one item = one reload
}
type Job struct {
	Kind          string // "mission", "need", "home"
	ID, Species   string
	Target, Done  int
}
type GroundVisit struct {
	POI    string
	Quarry bool
}
type SortieSoFar struct {
	TicksOut     int64
	Grounds      []GroundVisit
	Fights, Kills, ReloadsSpent int
}
type State struct {
	AgentID, ShipClass                               string
	Hull, MaxHull, Shield, MaxShield, Fuel, MaxFuel  float64
	FreeHold                                         int
	Credits                                          float64
	System, POI                                      string
	Docked                                           bool
	PoliceLevel, JumpsToStation, JumpsToHome         int // -1 = unknown
	Weapons                                          []Weapon
	Ammo                                             []Ammo
	Job                                              Job
	Sortie                                           SortieSoFar
	Tick                                             int64
	GameVersion, WorkerCommit                        string
}
type Option struct {
	ID        string
	Features  map[string]any
	NoExplore bool `json:"no_explore,omitempty"`
}
type Decision struct {
	Kind     Kind
	SortieID string
	Seq      int
	State    State
	Options  []Option
}
const (SourceRule = "rule"; SourceExplore = "explore")
type Choice struct {
	OptionID, RuleOptionID string
	Source                 string
	Epsilon                float64
	Reason                 string
}
type Decider interface {
	Decide(ctx context.Context, d Decision) Choice
}

type Explorer struct {
	Inner   Decider
	Epsilon map[Kind]float64
	Float   func() float64  // nil -> math/rand/v2 Float64
	Intn    func(n int) int // nil -> math/rand/v2 IntN
}
func (e Explorer) Decide(ctx context.Context, d Decision) Choice
```

An inner `Decider` returns `OptionID` and `Reason`; `Explorer` fills `RuleOptionID`, `Source`, `Epsilon` on every choice.

- [ ] **Step 1: Write the failing tests** in `explorer_test.go` with a stub inner decider that always picks `"b"`:

```go
func TestExplorerZeroEpsilonReturnsRule(t *testing.T)      // Epsilon nil: OptionID "b", RuleOptionID "b", Source "rule", Epsilon 0
func TestExplorerPicksAmongExplorable(t *testing.T)        // Epsilon 1, Float->0, Intn->0, options a,b,c with c.NoExplore: OptionID "a", RuleOptionID "b", Source "explore", Epsilon 1
func TestExplorerNeverOverridesNoExploreRulePick(t *testing.T) // rule picks "b", b.NoExplore, Epsilon 1: OptionID "b", Source "rule"
func TestExplorerNeedsTwoExplorable(t *testing.T)          // only one option without NoExplore, Epsilon 1: Source "rule"
func TestExplorerAboveEpsilonReturnsRule(t *testing.T)     // Epsilon 0.15, Float->0.15: Source "rule" (explores only when Float() < ε)
```

- [ ] **Step 2: Run** `go test ./pkg/huntdecide/` and confirm it fails to compile.
- [ ] **Step 3: Implement** `types.go` and `Explorer.Decide`. Explore only when ε > 0, `Float() < ε`, the rule's pick is not `NoExplore`, and at least two options are explorable; pick uniformly among explorable options (the rule's own pick may be drawn).
- [ ] **Step 4: Run** `go test ./pkg/huntdecide/` (PASS) and `golangci-lint run ./pkg/huntdecide/`.
- [ ] **Step 5: Commit** `huntdecide: decision seam and exploring wrapper`.

---

### Task 2: Record log, recorder and schema

**Files:**
- Create: `pkg/huntdecide/log.go`, `pkg/huntdecide/record.schema.json`
- Test: `pkg/huntdecide/log_test.go`

**Interfaces:**
- Consumes: Task 1 types.
- Produces:

```go
const DecisionsDir = "hunt-decisions"

type DecisionRecord struct {
	Type string    `json:"type"` // "decision"
	At   time.Time `json:"at"`
	Decision       // embedded: kind, sortie_id, seq, state, options
	Choice         // embedded: option_id, rule_option_id, source, epsilon, reason
}
type OutcomeRecord struct {
	Type     string         `json:"type"` // "outcome"
	At       time.Time      `json:"at"`
	SortieID string         `json:"sortie_id"`
	Seq      int            `json:"seq"`   // decision it answers; 0 = none
	Phase    string         `json:"phase"` // phase 1 writes only "provision"
	Data     map[string]any `json:"data"`
}
type SortieRecord struct {
	Type     string    `json:"type"` // "sortie_start" or "sortie_end"
	At       time.Time `json:"at"`
	SortieID string    `json:"sortie_id"`
	State    State     `json:"state"`
	Result   string    `json:"result,omitempty"` // sortie_end only
}

type Log struct{ /* agentDir, mutex */ }
func NewLog(agentDir string) *Log
func (l *Log) Append(now time.Time, rec any) error // nil receiver: no-op, nil error

type Recorder struct {
	Inner Decider
	Log   *Log
	Now   func() time.Time // nil -> time.Now
}
func (r Recorder) Decide(ctx context.Context, d Decision) Choice
```

`Append` writes one JSON line to `<agentDir>/hunt-decisions/<UTC date of now>.jsonl`, creating the directory, opening with `O_APPEND|O_CREATE|O_WRONLY`, mode 0o644.

- [ ] **Step 1: Write the failing tests:**

```go
func TestLogAppendsOneLinePerRecordInDatedFile(t *testing.T) // two Appends at 2026-11-21T23:59Z and 2026-11-22T00:01Z -> files 2026-11-21.jsonl and 2026-11-22.jsonl, one line each, each line valid JSON with "type"
func TestNilLogAppendIsNoOp(t *testing.T)                    // (*Log)(nil).Append(...) == nil
func TestLogAppendErrorWhenDirUnwritable(t *testing.T)       // agentDir is a regular file: Append returns an error, does not panic
func TestRecorderWritesDecisionAndReturnsInnerChoice(t *testing.T) // line decodes to DecisionRecord with type "decision", kind, seq, state.agent_id, options, option_id, rule_option_id, source
func TestRecorderSurvivesLogFailure(t *testing.T)            // unwritable dir: Decide still returns the inner choice
func TestSchemaIsDraft202012AndCoversRecordFields(t *testing.T) // record.schema.json parses; $schema is the 2020-12 URI; has oneOf of three record shapes; every json tag of DecisionRecord, OutcomeRecord, SortieRecord and State (via reflect) appears under that shape's "properties"
```

- [ ] **Step 2: Run** and confirm failure.
- [ ] **Step 3: Implement** `log.go` and write `record.schema.json` (embed it with `//go:embed` for the test). The schema test uses `reflect` to walk json tags; no validator library.
- [ ] **Step 4: Run** tests and lint.
- [ ] **Step 5: Commit** `huntdecide: JSON-lines record log, recorder and schema`.

---

### Task 3: Sortie state file

**Files:**
- Create: `pkg/huntdecide/sortie.go`
- Test: `pkg/huntdecide/sortie_test.go`

**Interfaces:**
- Produces:

```go
const SortieFile = "hunt-sortie.json"

const (
	ResultCompleted       = "completed"
	ResultReturnedDry     = "returned_dry"
	ResultReturnedDamaged = "returned_damaged"
	ResultNoQuarry        = "no_quarry"
	ResultBlockedUnarmed  = "blocked_unarmed"
	ResultStranded        = "stranded"
	ResultDestroyed       = "destroyed"
	ResultInterrupted     = "interrupted"
)
func ResultFailed(result string) bool // true for blocked_unarmed, stranded, destroyed

type Sortie struct {
	ID            string
	Seq           int
	StartedAt     time.Time
	StartTick     int64
	JobKind, JobID string
	Grounds       []GroundVisit
	Fights, Kills, ReloadsSpent int
	PendingResult string // set when heading home; the result to close with on docking
}
func NewSortie(agentID string, now time.Time) *Sortie // ID = agentID + "-" + now.UTC().Format("20060102T150405Z")
func (s *Sortie) NextSeq() int                        // increments and returns; first call returns 1
func (s *Sortie) Visited() []string                   // POI ids in Grounds
func LoadSortie(agentDir string) (*Sortie, error)     // missing file: nil, nil; undecodable: nil, error
func (s *Sortie) Save(agentDir string) error          // tmp file + rename
func ClearSortie(agentDir string) error               // remove; missing file is not an error
```

- [ ] **Step 1: Write the failing tests:**

```go
func TestSortieSaveLoadRoundTrip(t *testing.T)
func TestLoadSortieMissingFileIsNil(t *testing.T)
func TestLoadSortieCorruptFileIsError(t *testing.T)    // file contains `{"id":` -> nil sortie, non-nil error
func TestNextSeqStartsAtOne(t *testing.T)
func TestNewSortieID(t *testing.T)                     // ("pirate-6", 2026-11-21T09:47:10Z) -> "pirate-6-20261121T094710Z"
func TestResultFailed(t *testing.T)                    // the three failed results true, the other five false
func TestClearSortieMissingIsNil(t *testing.T)
```

- [ ] **Step 2: Run** and confirm failure. **Step 3: Implement.** **Step 4: Run** tests and lint.
- [ ] **Step 5: Commit** `huntdecide: per-agent sortie state file`.

---

### Task 4: Outcomes can carry a reason on `ok`; ignore rules

**Files:**
- Modify: `pkg/outcomes/outcomes.go`, `.gitignore`
- Test: `pkg/outcomes/outcomes_test.go`

**Interfaces:**
- Produces: `func (l *Log) RecordStatus(key, command, status, reason string, at time.Time)`. `Record` becomes a call to it. `reason` is capped at `reasonMax` runes.

- [ ] **Step 1: Write the failing test** `TestRecordStatusKeepsReasonOnOK`: `RecordStatus("behaviour:hunt_sortie", "hunt", outcomes.StatusOK, "no_quarry", at)` then `Load` returns status `ok`, reason `no_quarry`. Keep the existing `Record` tests green.
- [ ] **Step 2: Run**, confirm failure. **Step 3: Implement.**
- [ ] **Step 4:** Add to `.gitignore`, beside the `hunt-chain.json` line: `data/agents/*/hunt-decisions/` and `data/agents/*/hunt-sortie.json`. Verify with `git check-ignore -q data/agents/pirate-6/hunt-decisions/2026-11-21.jsonl && git check-ignore -q data/agents/pirate-6/hunt-sortie.json` (exit 0).
- [ ] **Step 5: Run** `go test ./pkg/outcomes/`, lint, **commit** `outcomes: ok outcomes may carry a reason; ignore hunt records`.

---

### Task 5: Rules decider, loadout read and compact state

**Files:**
- Modify: `pkg/reload/plan.go`, `pkg/worker/hunt.go` (constants and `HuntDeps` fields only)
- Create: `pkg/worker/hunt_rules.go`, `pkg/worker/hunt_state.go`
- Test: `pkg/reload/plan_test.go`, `pkg/worker/hunt_rules_test.go`, `pkg/worker/hunt_state_test.go`

**Interfaces:**
- Consumes: Tasks 1 to 3.
- Produces:

```go
// pkg/reload
type FreeWeapon struct{ ModuleID, TypeID string }
type Loadout struct {
	Plan []Entry      // ammo-fed weapons, as BuildPlan returns
	Free []FreeWeapon // weapon modules with no ammo type
}
func BuildLoadout(modules []serverapi.ShipModule, cargo []serverapi.CargoItem, lookup Lookup) Loadout
func FetchLoadout(ctx context.Context, client game.GameClient, kb knowledge.Base) (Loadout, error)
// Fetch stays, implemented through FetchLoadout.

// pkg/worker/hunt.go, new HuntDeps fields
Decider       huntdecide.Decider // nil -> huntRules{}
Decisions     *huntdecide.Log    // nil -> no records
AmmoCreditCap float64            // 0 -> huntDefaultAmmoCreditCap
Sellers       func(ctx context.Context, itemID string, minQty float64) ([]market.ItemSeller, error) // nil -> no market destinations
ReportSortie  func(result string) // nil -> not reported
Version, Commit string

// pkg/worker/hunt.go, new constants
const (
	huntDefaultAmmoCreditCap = 5000.0
	huntCreditFloor          = 1000.0 // never spent on ammo: fuel and repair
	huntProvisionDefault     = 2      // reloads per ammo-fed weapon
	huntLootReserve          = 10     // hold units provisioning leaves free
	huntDepotLowSorties      = 2
	huntReturnRepairHull     = 0.5
	huntMaxSortieGrounds     = 8
)
var huntProvisionBuckets = []int{1, 2, 4}

// pkg/worker/hunt_rules.go
type huntRules struct{}
func (huntRules) Decide(ctx context.Context, d huntdecide.Decision) huntdecide.Choice
func huntDecider(deps HuntDeps) huntdecide.Decider // deps.Decider or huntRules{}
func huntCanFire(s huntdecide.State) bool          // an ammo-free weapon, or an ammo-fed one with Loaded > 0 or a matching Ammo stack aboard
func huntHasReserve(s huntdecide.State) bool       // an ammo-free weapon, or any Ammo stack with Aboard >= 1 matching a fitted weapon

// option ids
const (
	huntOptContinue = "continue"
	huntOptRearm    = "rearm"
	huntOptRepair   = "repair"
)
func huntProvisionOptionID(reloads int) string // "reloads_1", "reloads_2", "reloads_4"

// pkg/worker/hunt_state.go
func huntDecisionState(ctx context.Context, deps HuntDeps, job huntJob, s *huntdecide.Sortie, lo reload.Loadout) huntdecide.State
func huntJobKind(job huntJob) (kind, id string) // "home"/"home grounds", "need"/need mission id, "mission"/active or board id
```

`huntRules` reads only `d.State` and `d.Options`, never the client or KB. That is the point of the seam: a model later gets exactly the same inputs.

Rules:
- `provision`: pick `reloads_2` if offered; else the largest offered below it; else the smallest offered. Reason names the count.
- `return`, first match wins among the options offered:
  1. `repair` when `MaxHull > 0` and `Hull/MaxHull < huntReturnRepairHull`.
  2. `rearm` when `!huntHasReserve(state)`.
  3. `continue`.

`huntDecisionState` fills weapon damage type, reach and cooldown from `kb.GetItem(typeID).Module.Weapon`; `JumpsToStation` and `JumpsToHome` from the policed graph helpers the hunt already uses (`huntPolicedGraph`, `navigation.BFSJumps`), `-1` when unknown or when `deps.KB` is nil; `Sortie` from `s` with `TicksOut = state.CurrentTick - s.StartTick` (zero value when `s` is nil).

- [ ] **Step 1: Write the failing tests:**

```go
// pkg/reload
func TestBuildLoadoutSplitsAmmoFedFromAmmoFree(t *testing.T) // modules: autocannon_ii (ammo_type autocannon), pulse_laser_i (none), a shield (type defense) -> Plan len 1, Free len 1

// pkg/worker/hunt_rules_test.go (pure table tests, no fake client)
func TestHuntRulesProvisionPrefersTwoReloads(t *testing.T)      // options 1,2,4 -> reloads_2; options 1 only -> reloads_1; options 4 only -> reloads_4
func TestHuntRulesReturnRepairBelowHalfHull(t *testing.T)       // hull 109/220, ammo plentiful -> repair
func TestHuntRulesReturnRearmWithoutReserve(t *testing.T)       // one autocannon Loaded 300, no Ammo aboard, no free weapon -> rearm
func TestHuntRulesReturnContinueWithFreeWeapon(t *testing.T)    // same but a pulse laser fitted -> continue
func TestHuntRulesReturnContinueWithReserve(t *testing.T)       // autocannon, Ammo{autocannon, Aboard 1} -> continue
func TestHuntRulesEnergyOnlyNeverRearms(t *testing.T)           // only ammo-free weapons, hull full -> continue (Review Focus 5)
func TestHuntRulesReturnPicksOnlyOfferedOptions(t *testing.T)   // hull low but repair not offered -> falls through to rearm/continue

// pkg/worker/hunt_state_test.go
func TestHuntDecisionStateProjectsShipAmmoAndSortie(t *testing.T) // from a huntFake + a Loadout + a Sortie with 2 grounds: weapons, ammo aboard, free hold, job kind, ticks out
func TestHuntDecisionStateHasNoPassword(t *testing.T)             // json.Marshal(state) contains neither "assword" nor the fake's password value
```

- [ ] **Step 2: Run** `go test ./pkg/reload/ ./pkg/worker/ -run 'Loadout|HuntRules|HuntDecisionState'`, confirm failure.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the same tests, then `go build ./...`, lint.
- [ ] **Step 5: Commit** `hunt: rules decider, loadout read and compact decision state`.

---

### Task 6: Provisioning at the dock

**Files:**
- Create: `pkg/worker/hunt_provision.go`
- Test: `pkg/worker/hunt_provision_test.go`

**Interfaces:**
- Consumes: Task 5; existing `consignHeld(raw []byte) (map[string]int, bool)`, `deps.Client.ViewStorage`, `WithdrawItems`, `ViewMarket`, `GetMarketListings`, `Buy`, `huntReload`.
- Produces:

```go
// huntProvision runs docked, with a job in hand, before the hunter leaves.
// armed=false means no weapon can fire and the hunter must not leave.
// reserve is hold space the job needs kept free (a Station Need's quantity; 0 otherwise).
func huntProvision(ctx context.Context, deps HuntDeps, out io.Writer, who string, job huntJob, s *huntdecide.Sortie, reserve int) (armed bool)

func huntProvisionOptions(lo reload.Loadout, freeHold, reserve int) []huntdecide.Option
// per ammo type: which item to bring and how many are missing for `reloads` per weapon
type huntAmmoNeed struct{ AmmoType, ItemID string; Missing int }
func huntAmmoNeeds(lo reload.Loadout, reloads int, storage map[string]int, lookup reload.Lookup) []huntAmmoNeed
```

Behaviour:
1. `reload.FetchLoadout` fails: log, return `true` (fail open), record nothing.
2. No ammo-fed weapon: return `true` with no decision.
3. Options: each bucket in `huntProvisionBuckets` whose extra cargo (missing items across all ammo types) fits in `freeHold - reserve - huntLootReserve`. Features: `reloads_per_weapon`, `cargo_units`. If none fits, no decision; go to step 6 with what is aboard.
4. Decide through `huntDecider(deps)` with `Kind: KindProvision`, `Seq: s.NextSeq()`.
5. For each ammo type, choose the item: the one loaded in a weapon of that type; else the fitting item with most in this station's storage; else the cheapest fitting sell listing here. Withdraw `min(missing, held)` from storage. For what is still missing, buy from this station's sell listings cheapest first, stopping when the sortie's spend would pass `cap` (`deps.AmmoCreditCap` or the default) or credits would drop below `huntCreditFloor`. A withdraw or buy error is logged and that ammo type is skipped.
6. `huntReload`, then `FetchLoadout` again; `armed` is whether an ammo-free weapon is fitted or any ammo-fed weapon has `CurrentAmmo > 0`.
7. Append an `OutcomeRecord{Phase: "provision", Seq: <decision seq>}` with `Data`: `wanted`, `from_storage`, `from_market`, `credits_paid`, `depot_left` (each a map by item id, `credits_paid` a number), `armed`.
8. Only when docked at `deps.HomeStation`: if storage left for a provisioned item is below `huntDepotLowSorties × (weapons of that type × chosen reloads)`, log once: `hunt[<who>]: DEPOT LOW: <item> <n> left at <station> (under 2 sorties)`.
9. `armed == false`: log `hunt[<who>]: BLOCKED UNARMED at <station>: no weapon can fire and no <ammo types> ammo in storage or on the market within the cap`.

- [ ] **Step 0: Check the missile rule live** (read-only): `go run ./cmd/tools/play_as` as a hunter or any agent holding `standard_guided_missiles`, read the item's catalog text and one `reload` reply if a launcher is fitted. If one missile item is not one full magazine, stop and report; the reload-count unit needs a per-ammo-type factor before this task continues.

- [ ] **Step 1: Write the failing tests.** Extend the test fake (a `huntProvisionFake` wrapping `huntFake`, in the style of `huntReloadFake`) with: ship modules for `get_ship`, a `storage` frame, sell listings, credits, and recorded `WithdrawItems` / `Buy` calls that move items into cargo.

```go
func TestHuntProvisionWithdrawsToTwoReloadsPerWeapon(t *testing.T) // 3x autocannon_ii, 0 aboard, storage 50 standard_rounds_box -> withdraw 6; armed; decision record kind provision option reloads_2; outcome from_storage {standard_rounds_box: 6}
func TestHuntProvisionCountsAmmoAlreadyAboard(t *testing.T)        // 4 aboard -> withdraw 2
func TestHuntProvisionBuysShortfallUnderCap(t *testing.T)          // storage 1, listings 100 @ 6cr, cap 5000 -> withdraw 1, buy 5, credits_paid 30
func TestHuntProvisionStopsAtCreditCap(t *testing.T)               // listings @ 2000cr, cap 5000 -> buys 2, not 6
func TestHuntProvisionKeepsCreditFloor(t *testing.T)               // credits 1010, price 6 -> buys 1 (1010-6 >= 1000), not 2
func TestHuntProvisionClampsToHold(t *testing.T)                   // free hold 13, reserve 0 -> only reloads_1 offered (3 items fit in 13-10); free hold 10 -> no decision, no withdraw (Review Focus 4)
func TestHuntProvisionRespectsNeedReserve(t *testing.T)            // free hold 30, reserve 16 -> extra cargo budget 4 -> reloads_1 only (Review Focus 4)
func TestHuntProvisionEnergyOnlyIsSilent(t *testing.T)             // 3x pulse_laser_ii -> armed, no decision record, no storage or market call (Review Focus 5)
func TestHuntProvisionFailsOpenWhenShipUnreadable(t *testing.T)    // get_ship error -> armed true, zero records written (Review Focus 1)
func TestHuntProvisionBlockedUnarmed(t *testing.T)                 // dry autocannons, empty storage, no listings -> armed false, output has "BLOCKED UNARMED"
func TestHuntProvisionMixedLoadoutArmedByLaser(t *testing.T)       // dry autocannon + pulse laser, nothing available -> armed true
func TestHuntProvisionDepotLowLineOnlyAtHome(t *testing.T)         // storage 8 after withdrawing 6 for 3 weapons x 2 (needs 12 for 2 sorties) -> "DEPOT LOW" at HomeStation, absent elsewhere
```

- [ ] **Step 2: Run** `go test ./pkg/worker/ -run HuntProvision`, confirm failure.
- [ ] **Step 3: Implement** `hunt_provision.go`. Confirm against `serverapi` and a captured frame which fields `view_storage`, `view_market` and `buy` replies use before reading them; reuse `consignHeld` for storage.
- [ ] **Step 4: Run** the tests, `go build ./...`, lint.
- [ ] **Step 5: Commit** `hunt: provision ammo from the depot and the market before leaving`.

---

### Task 7: Rearm destination and leg

**Files:**
- Create: `pkg/worker/hunt_rearm.go`
- Test: `pkg/worker/hunt_rearm_test.go`

**Interfaces:**
- Consumes: Task 5 (`deps.Sellers`, `deps.HomeStation`), existing `huntFlyTo(ctx, deps, out, home bool, systemID, poiID string) error`, `huntPolicedGraph`, `navigation.BFSJumps`, `market.ItemSeller` (`StationID`, `SystemID`).
- Produces:

```go
// huntRearmDestination picks where a hunter that needs ammoItems goes.
// Candidates: the home station, and every policed station deps.Sellers reports
// selling one of ammoItems. Nearest by policed jumps wins; home wins a tie.
// With no candidate reachable it returns home. ok=false only when home is unknown too.
func huntRearmDestination(ctx context.Context, deps HuntDeps, fromSystem string, ammoItems []string) (systemID, poiID, why string, ok bool)

// huntGoRearm flies there and docks. The next pass provisions.
func huntGoRearm(ctx context.Context, deps HuntDeps, out io.Writer, who string, lo reload.Loadout) error
```

`huntGoRearm` derives `ammoItems` from the loadout: each ammo-fed weapon's `LoadedAmmoID`, or when none is loaded, every catalog ammo item of that ammo type the KB lists.

- [ ] **Step 1: Write the failing tests** (KB fixture with home 3 jumps away and a market station 1 jump away):

```go
func TestHuntRearmPrefersNearerMarketOverHome(t *testing.T)   // seller 1 jump, home 3 -> the seller's station
func TestHuntRearmPrefersHomeOnTie(t *testing.T)
func TestHuntRearmSkipsUnpolicedSellers(t *testing.T)         // nearer seller in a system under police level 30 -> home
func TestHuntRearmFallsBackHomeWithNoSellers(t *testing.T)    // deps.Sellers nil -> home
func TestHuntRearmNoHomeNoSellers(t *testing.T)               // HomeStation "" and no sellers -> ok false
```

- [ ] **Step 2: Run**, confirm failure. **Step 3: Implement.** **Step 4: Run** tests, build, lint.
- [ ] **Step 5: Commit** `hunt: choose the nearer of home depot and a selling station to rearm`.

---

### Task 8: Sortie lifecycle and the reshaped Hunt

**Files:**
- Create: `pkg/worker/hunt_sortie.go`
- Modify: `pkg/worker/hunt.go` (`Hunt` body, around lines 291 to 659)
- Test: `pkg/worker/hunt_sortie_test.go`; existing `pkg/worker/hunt*_test.go` must stay green

**Interfaces:**
- Consumes: Tasks 1 to 7.
- Produces:

```go
func huntAgentDir(deps HuntDeps) string // <AgentsDir or DefaultAgentsDir>/<AgentID>; "" when AgentID is empty

// huntLoadSortie returns the open sortie, or nil. A corrupt file logs once and counts as none.
func huntLoadSortie(deps HuntDeps, out io.Writer) *huntdecide.Sortie

// huntOpenSortie starts a sortie for job, writes the sortie_start record and saves the file.
func huntOpenSortie(ctx context.Context, deps HuntDeps, out io.Writer, job huntJob, lo reload.Loadout) *huntdecide.Sortie

// huntCloseSortie writes sortie_end, calls deps.ReportSortie(result), and clears the file. Nil sortie: no-op.
func huntCloseSortie(ctx context.Context, deps HuntDeps, out io.Writer, s *huntdecide.Sortie, job huntJob, result string)

// huntReturnDecision asks the seam after a fight. Returns huntOptContinue, huntOptRearm or huntOptRepair.
func huntReturnDecision(ctx context.Context, deps HuntDeps, out io.Writer, job huntJob, s *huntdecide.Sortie) string

// huntHeadHome sets s.PendingResult, saves, and flies to the rearm or repair station and docks.
func huntHeadHome(ctx context.Context, deps HuntDeps, out io.Writer, who string, s *huntdecide.Sortie, choice string)
```

With `deps.AgentID == ""` (several existing tests), sortie functions are no-ops that still return a usable in-memory `*Sortie`, so nothing is written and no existing test needs an agents directory.

**Return decision options.** `continue` is offered only when `huntCanFire`. `rearm` is offered when the ship has an ammo-fed weapon; `NoExplore` when `s.Fights < 1`. `repair` is always offered and always `NoExplore`. When nothing can fire, no decision is made: the return is forced with result `returned_dry`.

**New shape of `Hunt`** (each numbered item is a behaviour the tests below pin; everything not listed stays as it is today):

1. **Pass start, sortie open, ship undocked away from a station.** Skip the dock, the station recovery, the dock repair and the board read. Rebuild the job: `home` from the sortie file; `mission` or `need` through the existing `huntResumeJob` / `huntResumeNeed`. If the job cannot be rebuilt (mission expired or completed elsewhere), set `PendingResult = interrupted` and fall through to today's station recovery. Otherwise continue at item 5 with `tried` seeded from `s.Visited()`.
2. **Pass start, sortie open, ship docked.** Close it with `PendingResult`, or `interrupted` when that is empty, then run the pass as today.
3. **Docked, job selected, about to leave.** After `huntBankAtDock` and before `huntReadyToLeave`: `huntProvision`. Not armed: report `blocked_unarmed` through `deps.ReportSortie`, set activity `Hunt · unarmed`, return. Armed and `huntReadyToLeave` passes: `huntOpenSortie`.
4. A need whose parts are already aboard still goes straight to `huntDeliverNeed`, with no provisioning and no sortie.
5. **Ground loop.** Wrap the existing find-quarry and engagement code in a loop bounded by `huntMaxSortieGrounds` across the whole sortie (`len(s.Grounds)`). `huntFindQuarry` gains a `skip []string` parameter that seeds its `tried` list and returns the POI it settled on; each ground the ship reaches is appended to `s.Grounds` with `Quarry` set, and the file saved.
6. **After each fight** that did not flee or error, and with the job still unmet: update `s.Fights`, `s.Kills`; call `huntReturnDecision`. `rearm` or `repair`: `huntHeadHome` with `PendingResult` `returned_dry` or `returned_damaged`, then return. `continue`: carry on.
7. **Pool empty at a ground, job unmet**: go round the ground loop (today: return and re-dock).
8. **Forced returns** (no decision): nothing can fire at the reload gate → `huntHeadHome` with `returned_dry`; hull below the flee threshold between engagements or `huntFled` → `PendingResult = returned_damaged`, return as today (the next pass's recovery docks and item 2 closes it); `huntMaxSortieGrounds` reached or `huntFindQuarry` finds nothing → `PendingResult = no_quarry`, return.
9. **Job done**: kill mission completed, or need delivered → close `completed` (for a need, after `huntDeliverNeed` docks; pass the sortie through `huntJob` or close at item 2 with `PendingResult = completed`). Home hunt: its engagement cap ends the pass as today with `PendingResult = completed`.
10. `s.ReloadsSpent` increases by the drop in total ammo items aboard across each `huntReload`.
11. `Hunt` still returns nil on every path.

`deps.ReportSortie` is called exactly once per sortie, from `huntCloseSortie`, plus once per `blocked_unarmed` pass.

- [ ] **Step 1: Write the failing tests** in `hunt_sortie_test.go`, using `huntFake` with an agents dir and a recording `ReportSortie`:

```go
func TestHuntOpensSortieWhenLeavingWithAJob(t *testing.T)          // sortie_start record written; hunt-sortie.json exists while out
func TestHuntClosesCompletedOnMissionComplete(t *testing.T)        // kill mission met -> sortie_end result completed; ReportSortie("completed"); file cleared (after the docked pass that follows, if completion happens undocked)
func TestHuntStaysOutForNextGroundWhenPoolRunsDry(t *testing.T)    // ground 1 has 1 grazer, target 2, ground 2 has grazers -> no Dock call between them; 2 kills in one sortie id
func TestHuntResumesOpenSortieUndocked(t *testing.T)               // pre-seeded sortie file, ship at a belt, mission held -> no relocation to a station; hunts; same sortie id in records; visited ground not revisited
func TestHuntClosesInterruptedWhenFoundDocked(t *testing.T)        // pre-seeded open sortie, ship docked, PendingResult "" -> sortie_end interrupted
func TestHuntInterruptedWhenJobGoneWhileOut(t *testing.T)          // open sortie, undocked, no held mission -> recovers to a station; next pass closes interrupted
func TestHuntReturnsToRearmWhenReserveGone(t *testing.T)           // autocannon only, last box spent in fight 1 -> return decision record with option rearm; flies to HomeStation and docks; result returned_dry
func TestHuntContinuesWithReserve(t *testing.T)                    // return decision record option continue; second engagement happens
func TestHuntReturnsToRepairBelowHalfHull(t *testing.T)            // hull 45% after a fight (above the 35% flee gate) -> repair; result returned_damaged
func TestHuntForcedDryReturnMakesNoDecision(t *testing.T)          // nothing can fire at the reload gate -> no return decision record; result returned_dry
func TestHuntBlockedUnarmedNeverLeavesDock(t *testing.T)           // no Undock call; ReportSortie("blocked_unarmed"); no sortie file
func TestHuntSortieGroundCap(t *testing.T)                         // 8 empty grounds across passes -> result no_quarry
func TestHuntCorruptSortieFileIsIgnored(t *testing.T)              // file `{"id":` -> pass runs as a fresh docked pass, logs "sortie file unreadable" once (Review Focus 3)
func TestHuntRunsWhenRecordDirUnwritable(t *testing.T)             // hunt-decisions is a regular file -> the mission still completes (Review Focus 2)
func TestHuntReturnOptionsExploreFlags(t *testing.T)               // before any fight rearm.NoExplore; repair always NoExplore; continue absent when nothing can fire
func TestHuntAlwaysReturnsNil(t *testing.T)                        // each path above returns a nil error
```

- [ ] **Step 2: Run** `go test ./pkg/worker/ -run 'HuntOpens|HuntCloses|HuntStays|HuntResumes|HuntInterrupted|HuntReturns|HuntContinues|HuntForced|HuntBlocked|HuntSortie|HuntCorrupt|HuntRuns|HuntReturnOptions|HuntAlways'`, confirm failure.
- [ ] **Step 3: Implement** `hunt_sortie.go` and reshape `Hunt`. Keep each exit's existing log line and activity label; add the sortie bookkeeping beside it.
- [ ] **Step 4: Run the whole worker suite** `go test ./pkg/worker/`. Existing hunt tests that asserted "held for next pass" after a dry pool now see a second ground attempt; update those assertions to the new behaviour only where item 7 changes it, and say which in the commit body.
- [ ] **Step 5: Run** `go build ./... && go test ./... && golangci-lint run ./...`.
- [ ] **Step 6: Commit** `hunt: one job per sortie across grounds, with return-to-rearm`.

---

### Task 9: Wiring, flags and ops notes

**Files:**
- Modify: `pkg/worker/dispatch.go` (the `case "hunt"` block near line 568 and the `WorkerDispatch` struct), `cmd/worker/main.go` (flags near line 101, dispatch fields near line 512), `pkg/overmind/supervisor/config.go` (near line 81), `pkg/overmind/supervisor/supervisor.go` (near line 124), `data/overmind/hunt-fleet.yaml`
- Create: `docs/ops/hunt-refit-and-depot.md`
- Test: `pkg/worker/hunt_dispatch_test.go`, `pkg/overmind/supervisor/supervisor_test.go`, `cmd/worker` flag-parse test if one exists for `--gift-to`

**Interfaces:**
- Consumes: everything above.
- Produces:
  - Worker flags: `--hunt-ammo-cap` (float, default 5000) and `--hunt-explore` (string, default empty; comma-separated `kind=ε`, for example `provision=0.15,return=0.05`).
  - `func ParseHuntExplore(spec string) (map[huntdecide.Kind]float64, error)` in `pkg/worker`: empty spec gives an empty map; an unknown kind, a missing `=`, or ε outside `[0, 1]` is an error.
  - Fleet yaml agent fields `hunt_ammo_cap` (float) and `hunt_explore` (string), forwarded as the two flags only when set.
  - `WorkerDispatch` fields `HuntAmmoCap float64`, `HuntExplore map[huntdecide.Kind]float64`.
  - Dispatch builds the decider as `Recorder{Inner: Explorer{Inner: huntRules{}, Epsilon: d.HuntExplore}, Log: log}` with `log = huntdecide.NewLog(<agents dir>/<agent id>)`; sets `Decisions: log`, `AmmoCreditCap`, `Sellers: d.Market.FindItemSellers` when `d.Market != nil`, `Version`/`Commit` from the worker's build info (the values the status file already reports), and `ReportSortie` writing `d.Outcomes.RecordStatus("behaviour:hunt_sortie", "hunt", status, result, time.Now())` where status is `failed` when `huntdecide.ResultFailed(result)`, else `ok`.

- [ ] **Step 1: Write the failing tests:**

```go
func TestParseHuntExplore(t *testing.T)                          // "" -> empty; "provision=0.15,return=0.05" -> two entries; "ground=0.1", "return", "return=2" -> errors
func TestHuntDispatchWiresRecorderAndDefaults(t *testing.T)      // a dispatched hunt pass with an ammo-fed ship writes a decision record under <agents>/<id>/hunt-decisions; source "rule"
func TestHuntDispatchReportsSortieOutcome(t *testing.T)          // blocked_unarmed -> task-outcomes key behaviour:hunt_sortie status failed reason blocked_unarmed; no_quarry -> status ok
func TestSupervisorForwardsHuntFlags(t *testing.T)               // spec with hunt_ammo_cap 2500 and hunt_explore "return=0.05" -> args contain both flags; unset -> neither
```

- [ ] **Step 2: Run**, confirm failure. **Step 3: Implement.** A bad `--hunt-explore` is a startup error printed to stderr and the worker log (the overmind log does not carry worker `log.Fatalf` output).
- [ ] **Step 4:** Rewrite the header of `data/overmind/hunt-fleet.yaml`: remove "NOT LAUNCHED", "two agents" and "`station` ... the hunt role does not read it"; state that `station` is the ammo depot and rearm destination, and document `hunt_ammo_cap` and `hunt_explore` (leave both unset: defaults 5000 and off).
- [ ] **Step 5:** Write `docs/ops/hunt-refit-and-depot.md` (check `.gitignore` for a broad `docs/` pattern and add a negation if the file is ignored): the spec's loadout table; the live checks before fitting (each hull's fitted modules, CPU 22 and power 50 budget on a Gauntlet); the depot fill (transfer from craftsman-1's storage at `frontier_station` into each hunter's own storage there: at least 4 sorties' worth, which is `weapons × 2 reloads × 4`); funding or gifting for pirate-6 (3 credits, 3 fuel); the rollout order (build `bin/worker`, restart one hunter, watch one sortie's records, then the rest); and how to read a day's records (`jq -c 'select(.type=="sortie_end")' data/agents/pirate-9/hunt-decisions/2026-11-21.jsonl`).
- [ ] **Step 6: Run** `go build ./... && go test ./... && golangci-lint run ./...`; build the worker with `go build -o bin/worker ./cmd/worker`.
- [ ] **Step 7: Commit** `hunt: wire the decision seam, ammo cap and explore flags; ops notes`.

---

## Not in this plan

Phases 2 to 6 of the spec: search, fight and loot outcome records; raw battle-log capture; the `wildlife_kills` and `wildlife_attacks` fixes; `ground`, `target`, `range` and `job` behind the seam; switching exploration on; the dataset tool. The refit, depot fill and fleet restart are operator steps described in Task 9's ops note, not code.
