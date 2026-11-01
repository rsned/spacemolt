# Line dashboard v2: every line, by station, worked out automatically

**Status:** draft for operator review, 2026-11-01
**Builds on:** the moonshine line dashboard (`cmd/tools/line-dashboard`, `pkg/linedash`, served on :8093; design D7 in `2026-10-24-contraband-production-pipeline-design.md`).
**Siblings, not in this design:** the Fleet tab (project 2) and the fleet role reorganisation (project 3).

## Why

The dashboard knows one line, moonshine, and it describes it wrongly:
- it still shows craftsman-8 as the Voss consigner
- it doesn't know about the Conjunction Junction sell orders or the town crier

Conjunction Junction now runs about ten lines, with more coming, and later there will be more stations. Some lines run in production facilities and some are hand-crafted by agents. Some feed other lines, some keep the station alive, and some exist to sell. Nobody can keep a hand-written line file current through that much change.

When the operator opens the dashboard, it has one question to answer: **which lines are starved or stalled, and why.** The why must name the **rule or condition that broke**, with its numbers. "Culture purchase starved" is not enough; "culture at Sirius is 70, over trader-10's cap of 50" is.

## Goals

1. **Every line, grouped by station.** The landing page lists every production, upkeep and sales line, grouped by station and, within a station, by category.
2. **Worked out automatically.** Lines come from what already drives the fleet: schedules, fleet files, the recipe catalog, captured facilities and the market. Nobody maintains structure by hand. Change a schedule, move an agent or build a facility, and the next refresh shows it. A new station appears as a new section.
3. **The why is the broken rule.** A starved or stalled line says which rule or condition broke, in plain words, with the actual numbers.
4. **Honest about both kinds of staleness:**
   - **Assignment drift:** the structure no longer matches reality. Goal 2 removes this kind.
   - **Snapshot age:** the numbers are old. Every number carries its age, and anything past its limit turns grey instead of showing a confident old verdict. This keeps the existing staleness rule; see the line dashboard reference.
5. **Shared resources read as shared.** A tank more than one line draws on is marked as shared, so "Steel plate 99,000" is never read as one line's own stock.
6. **Readable.** The base font is at least 28px and the diagram draws at 2× scale or more. Already shipped on 2026-11-01.

## Non-goals

- The Fleet tab: map, agent table, money, danger flags. That's project 2. The landing page leaves room for a **Lines | Fleet** switch, and nothing more.
- The marketbot drone mining lines. Deferred by the operator.
- Renaming profiles or redefining fleet roles. That's project 3; the dashboard shows fleet roles and never defines them.
- Acting on anything. The dashboard stays **read-only** and never calls the game.

## Anti-goals: a failure even if it technically works

- A line drawn green from stale data, or from a structure that no longer exists.
- A why invented by inference when the actual refusal or rule is on record. If the cause isn't known, say "cause unknown" plus the symptom. Don't guess.
- A setup where adding a schedule task means also editing a dashboard file for it to appear.
- A landing page that becomes a layout puzzle as lines grow. This is why it's a table, not cards.

## Decided by the operator

### Landing page (Lines tab)

- **One table, grouped by station.** Each station heading shows its name, system, owner and condition, such as "thriving 100%".
- **Category sub-rows within a station:**
  - Station upkeep
  - Refined
  - Components
  - Contraband
  - **Market/Sales**: goods not crafted at that station but brought in to sell there, such as the Voss moonshine and void dust sold at Conjunction Junction
  - other item categories as they appear
- **One row per production line, including feeder lines like steel plate.** Columns:
  - **State:** green ok, yellow starved, red stalled or choked, grey stale
  - **Kind tags:**
    - **hand** or **facility**, visibly different
    - **upkeep**, **for sale** or **feeds lines**
  - **Depended on by:** for a line other lines draw on, the count by kind, such as "3 production · 1 upkeep"
  - **Why:** the broken rule, with numbers
  - **Agents:** how many agents are allocated to the line. Moonshine has 10; a hand-craft line typically has 1.
  - **/day:** output

### Shared tanks (line diagram)

A tank that more than one line draws on is drawn with **both a dashed outline and 45° two-tone stripes** (option C), and carries a **"shared ×N"** count. A tank only one line uses is drawn solid and plain.

### Staleness has two parts

1. **Assignment drift:** a role, assignment or task changed. Solved by deriving structure (Goal 2).
2. **State snapshot:** the current numbers, each with an age:
   - tanks: inputs waiting to be hauled, outputs waiting to be hauled, sellable stock waiting to be sold
   - craft queue backlog
   - whether the agents are still doing what they're assigned

## Derivation: where lines come from

| Source | Becomes |
|---|---|
| `data/agents/<id>/schedule.json` `craft <recipe> …` | a production step at that agent's station, which comes from its fleet file pin |
| schedule `deliver <item> <qty> <from> <to> <recipient>` | a haul between stations |
| schedule `faction_deposit <item>` | a move from the agent's storage into faction storage at its station |
| schedule buy tasks that carry a price cap | a purchase step; **the cap is a rule the why can name** |
| fleet yaml `station`, `consign`, `faction_pull`, `shopkeep`, `pipeline` | pins, consign links, pulls, shop stock |
| recipe catalog (`data/game-api/latest/catalog_recipes.json`) | inputs and outputs per run; steps link where one step's output is another's input at the same station, or arrives there by a haul |
| captured faction facilities (`faction_facilities`, from the hourly `facilities` task) | **facility** vs **hand**: a facility line if a facility at that station produces the recipe, otherwise hand; level and ticks per run give capacity |
| market and sell orders (market.db, faction sell orders) | a **Market/Sales** line for each item listed at a station that isn't crafted there |
| station upkeep inputs (from the captured facility details) | marks the lines that produce them as **upkeep** |
| item catalog category | the category sub-row |

**A line** is one production step's output at one station, which is one row. Its diagram shows the steps and tanks that feed it, up to and including any shared tank, plus where its output goes. Sales lines are rooted at the sold item at the selling station.

**Overrides.** The only hand-kept file is optional overrides: a display name, a category correction, or a line hidden on purpose. It never adds or removes structure. An override that names a line which no longer exists is shown as a warning on the board, not dropped silently.

## The why: recording broken rules

Most reasons a line stops exist today only as worker log text, for example:
- `standing: "craft distill_moonshine 60": cannot_craft: Not enough materials in your station storage`
- `NOT DEPARTING: route needs ~638 fuel, 550 available`
- a buy skipped because the price is over the cap
- `no_facility`, `not_docked`, `no_fuel_cells`
- a storage item at the 100k cap

The dashboard can't read worker logs and shouldn't scrape them. So:

- **Workers record every task outcome somewhere the dashboard reads:** agent, task id, command, time, ok or refused or skipped, and the reason. The reason is the server error code and message, or the worker's own rule with its numbers, such as `price 70 > cap 50`.
- **The dashboard shows a line's why** from the latest outcomes of the tasks that make up the line, plus its tank rules. The tank rules are the existing starved and choked rules; they apply to tanks with no outflow.
- **A shared tank that is empty starves every consumer**, and each one says so, for example "steel plate 0 (shared with 3 lines)". The feeder line shows why it can't refill.

## Freshness: what is captured, how often

| Number | Source today | Gap |
|---|---|---|
| Faction storage tanks | `capture_faction`; single captor, the lowest player_id, currently miningbot_0000, hourly | none |
| Agent storage tanks | `capture_storage` on each agent | many agents capture it only daily; line agents need hourly |
| Craft outcomes, hauls, gifts | `capture_action_log` into `action_log_events` | hourly on the moonshine agents only; must be hourly on every agent in any line |
| Task outcomes and refusals (the why) | none | **new** (above) |
| Craft queue backlog | none | **new**: `craft` with no recipe_id lists the caller's queued jobs with ETAs. A facility's backlog is the sum over our agents with jobs on it. |
| Facility list, level, status | hourly `facilities` into `faction_facilities` | none |
| Asks and sell orders | market.db | none |

**Conformance:** "is the agent doing what it's assigned" means its scheduled tasks are firing and their outcomes aren't all refusals. It also means the agent is at its pinned station, or travelling to it. An agent looping on a route it can't fly shows as non-conforming, with the route refusal as the why.

Every number shows its age. The existing rule holds: the oldest source a verdict depends on decides grey, and a grey node is fixed by fixing the capture cadence, not the rule.

## Constraints

- Read-only. The dashboard never calls the game and never writes game state.
- `energy_crystal` and starshine never appear on a line. Keep the existing load-time rejection.
- Schedule seeding is additive only. Any new capture command added to schedules fires forever once seeded, so prefer extending a read already on the wire, as `upsertFactionFromFacilityList` does, over adding commands.
- Workers use the Sleep constants and the `settle` seam; no `time.Sleep` in pkg/worker.
- Code is golangci-lint clean; `go build ./...` and `go test ./...` must pass.

## Settled after the builder check

- **The why covers more than schedule tasks.** The operator asked for any broken rule or condition to be visible. So outcomes are also recorded for:
  - the fleet-yaml behaviours: pipeline mining, consign, faction_pull, shopkeep
  - the travel home to the pinned station, the source of `NOT DEPARTING`

  A behaviour record uses the behaviour name in place of a task id, for example `consign:pirate_moonshine` or `ensure_home`.
- **Derivation follows the sources; stale sources are fixed in the data.** If the fleet file and schedule say craftsman-8 consigns moonshine at Voss, the dashboard shows it. Its conformance then shows every run failing, which is the honest picture. Correcting the fleet file is a data edit, outside this project.
- **Only pinned agents make lines.** An agent with `station: ""` contributes no production line, because it has no station to group under. Its hauls still link lines when they name both ends, as `deliver` does.
- **The drone marketbots' ore-conversion crafts are part of the deferred drone lines.** Pinned marketbots with active mining drones hand-craft `refine_steel 500`, `process_copper_wiring 600` and `draw_copper_piping 200` to turn accumulated ore into common high-volume inputs. That frees storage so the drones can keep collecting. These crafts belong to the drone mining lines the operator deferred, so v2 does not draw them as production lines. When the drone lines are designed, they are the storage-relief step of each drone line.
- **"Builds" is dropped** from "Depended on by" until a source of build demand exists. The labels are production, upkeep and sales.
- **A Market/Sales line with no supply on record** still gets a verdict from its stock and asks. Its diagram shows the supply side as "supply not on record" instead of guessing a source. An example is moonshine at Conjunction Junction, hauled by the operator.
- **Rollout:** the plan ends with a staged worker roll using the established recipe: TERM the overmind, wait for its socket to show zero workers, relaunch it with the captured argv, one fleet at a time. The plan **stops for the operator's go-ahead** before the roll. Never restart a disconnected worker. Commit data files by path, never with `git add -A`.

## Left to the builder

- The storage and format of task-outcome records (assets.db table, file per agent, etc.).
- How a recurring refusal is summarised: the latest one, or a count since the last success.
- The exact category mapping from item catalog categories, plus Upkeep and Market/Sales.
- Whether the moonshine line's `lines.yaml` is retired at once, or kept as an override fixture during the transition.
- The /day calculation for facility lines: observed rate vs capacity. Bead 003 computes cost per unit at capacity, so show capacity alongside the observed rate.

## Calls made by Claude, not the operator

- The "Depended on by" breakdown labels: production, upkeep, sales. "Builds" was dropped for lack of a source.
- Only pinned agents make lines.
- Behaviour outcomes use the behaviour name in place of a task id.
- The rollout stops for the operator's go-ahead before the staged roll.
- A shared tank's emptiness is reported on every consumer line, not only on the feeder.
- Overrides cannot change structure.
- "Cause unknown + symptom" when no refusal is on record, rather than an inferred cause.
