# Sparring Testbed Design

**Date:** 2026-05-25
**Status:** Approved (design)
**Author:** Robert Snedegar (with Claude)

## Problem

The battle system is hard to learn and hard to test. NPC pirate fights are
unpredictable in difficulty, and there is no controlled place to practice the
zone/stance tactical mechanics where a pilot has a reasonable chance of
surviving or successfully retreating. A real fight this session showed a
`battle retreat` accepted yet the pilot still died, because disengaging takes
several ticks and incoming damage outpaced zone movement — server-side combat
mechanics, not a client bug.

We want a **sparring testbed**: log in two or more of our own agents and have
them fight each other in a controlled arena so the mechanics become observable
and testable. This is the prerequisite for the deferred "smart battle handler"
(`project_play_as_smart_battle_handler`) — you cannot validate proactive
combat behavior without a repeatable fight to validate it against.

## Combat model (verified from server_docs/openapi.json)

PvP is fully supported:

- `attack <target_id>` — `target_id` accepts a player ID/username; target must
  be in the same system. "Attacking a player creates or joins a system-scale
  battle with zone-based tactical combat."
- `battle <action>` — actions: `advance`, `retreat`, `stance`, `target`,
  `engage`, `help`.
  - Zones: `outer → mid → inner → engaged`. `advance` moves one closer,
    `retreat` moves one back.
  - Stances: `fire` (100% dmg dealt/taken), `evade` (0% dealt, 50% taken, costs
    fuel), `brace` (0% dealt, 25% taken, shields regen 2×), `flee` (0% dealt,
    100% taken, auto-retreats, 3 ticks from outer to escape).
- `get_battle_status` — full battle state (participants, zones, sides, your
  stats). **Free query, no tick cost** — ideal for a per-tick poll.

PvP rules established with the user:

- **Outside empire space is anything-goes** — fights are legal in non-empire
  systems. This is the simple default arena.
- **Faction war** is an alternative: two factions at war can fight even in
  empire space. Heavier to set up (factions + war declaration); noted as a
  future enablement path, **not built in this iteration**.

Real-world topology example (from the user): the station system `treasure_cache`
is one jump from `ross_128`, which is lawless. Stations typically sit one or
more jumps away from lawless zones, so equipping must happen at a safe station
**before** jumping to the arena.

## Decisions (from brainstorming)

| Question | Decision |
|----------|----------|
| Scope/priority | **Sparring testbed first**; smart handler layered on later. |
| Drive model | **Both modes** — scripted bot-vs-bot AND a "partner" slot a human can join via `play_as`. |
| Stakes | **Accept losses, use cheap ships** — fights can run to completion; rely on cheap loadouts. |
| Bot behavior | **Named policy presets now**, custom scripts later (clean extension point). |
| Pre-fight setup | **Full auto: travel + equip** — harness navigates and equips combatants. |
| Architecture | **Standalone `cmd/tools/spar` binary + reusable `pkg/spar` package**, including a `get_battle_status → state.BattleState` parse fix as a shared foundation. |

## Architecture & components

```
pkg/spar/
  arena.go       — ArenaFinder: select & validate a non-empire system w/ safe-space adjacency
  combatant.go   — Combatant: wraps *game.Client; Setup() = equip → travel → rendezvous
  policy.go      — Policy interface + presets (aggressor, skirmisher, retreater, dummy)
  match.go       — Match: setup, initiate, run per-combatant loops, end-detection, summary
  telemetry.go   — per-tick battle log line + final match summary
  *_test.go
cmd/tools/spar/
  main.go        — flags, log in N agents via game.InitializeAgent, build & run Match

pkg/game/  (foundation fix)
  client.go      — parse get_battle_status into state.BattleState (currently only nil'd)
```

`pkg/spar` holds all logic with no terminal coupling so policies and
end-detection are unit-testable against a fake client. Each unit:

- **ArenaFinder** — *what:* picks/validates an arena system. *Use:* `Find(ctx)`
  or `Validate(ctx, systemID)`. *Depends on:* a `GameClient` (`GetMap`,
  `FindRoute`) and optionally the KB.
- **Combatant** — *what:* one logged-in agent + its assigned policy + setup.
  *Use:* `Setup(ctx, arena, rendezvous)`, then `RunPolicyLoop(ctx)`.
  *Depends on:* `*game.Client`, `pkg/game` navigation/equip helpers.
- **Policy** — *what:* decides a battle action from a battle view. *Use:*
  `Decide(View) Action`. *Depends on:* nothing (pure function over `View`).
- **Match** — *what:* orchestrates the whole fight. *Use:* `Run(ctx)`.
  *Depends on:* ArenaFinder, Combatants, Policies, telemetry.

### Reuse of existing code

- **Login:** `game.InitializeAgent(agentID, logger, ctx, debug)` → logged-in
  `*game.Client` + creds (same path `auto-fighter` uses). Called once per agent.
- **Routing:** `game.NavigateToSystem` / `client.FindRoute` (pkg/game/navigation.go).
- **Equip:** `client.Buy` + `client.InstallMod` (pkg/game/client_commands.go).
- **Combat:** `client.Attack`, `client.Battle(action, payload)`,
  `client.GetBattleStatus` (free poll).
- **Tick gating:** `game.NewGameClock` (already used by `play_as`) or the tick
  field on `get_battle_status`.

## Foundation fix: populate `state.BattleState`

`State.BattleState *BattleState` is declared (pkg/game/types.go) but **never
populated** — `get_battle_status` responses are only dumped into the generic
monitor store, and `BattleState` is only ever set to `nil`
(pkg/game/client.go:2328). The policy loop needs structured battle state.

Add parsing so the `get_battle_status` response (and ideally the
`battle_update` push) populates `state.BattleState` (`BattleID`, `SystemID`,
`IsParticipant`, `Participants[]{PlayerID,Username,ShipClass,SideID,Zone,Stance,
Hull,MaxHull,Shield,MaxShield}`). This same structured read is exactly what the
deferred smart-handler needs, so it is built once here as a shared foundation.

## Match lifecycle (data flow)

1. **Resolve arena** — `--arena <id>` or auto-discover (see Arena selection).
2. **Setup combatants** (concurrent, errgroup). Each `Combatant.Setup()`:
   a. **Equip first** — if missing weapon/shield, dock at nearest
      station-with-market, `Buy` + `InstallMod` cheap gear, refuel + repair.
      (Stations are typically one+ jumps from lawless arenas, so this precedes
      travel.)
   b. `NavigateToSystem(arena)`.
   c. Travel to the shared **rendezvous POI** (default first `asteroid_belt`,
      fallback first POI in the arena system).
3. **Verify co-location** — all combatants undocked at the same POI in arena;
   otherwise abort with a clear message.
4. **Initiate:**
   - *bot-vs-bot:* the designated aggressor calls `Attack(opponent-username)` →
     battle created.
   - *partner:* harness drives only the bot(s). It prints
     `Arena <sys>, rendezvous <poi> — run play_as <youragent>, travel there,
     and attack <bot-username>`, then polls `get_battle_status` until a battle
     appears. (The harness does **not** log in the human's agent — that would
     double-session it; the human owns their `play_as` session.)
5. **Combat loop** — one goroutine per *bot* combatant, gated on game-tick
   advance: `GetBattleStatus` → read `state.BattleState` → `Policy.Decide(view)`
   → dispatch `Battle`/`Attack`. `SleepTick` between iterations.
6. **Telemetry** — central logger prints a per-tick row:
   `tick | name | zone | hull% | shield% | stance`.
7. **End conditions** — battle gone (not a participant / `BattleState == nil`) /
   a side eliminated / `--max-ticks` reached / Ctrl-C (graceful: issue `flee`
   for bots, close clients, print partial summary).
8. **Summary** — winner, ticks elapsed, final hull/shield per combatant, damage
   taken; destroyed ships reported. With `--rebuild`, commission a cheap
   replacement for destroyed ships (consistent with "accept losses").

## Arena selection

Predicate over `SystemData`:

- **non-empire** = `Empire == ""` ∨ `SecurityStatus == "Lawless"` ∨
  `PoliceLevel == 0` ∨ `IsStronghold`.
- **safe-adjacent** = has a `Connection` to a policed system (a place to
  retreat / a nearby station to equip and rebuild).

Auto-discovery prefers a qualifying system that (a) is reachable by all
combatants (`FindRoute` succeeds), (b) has a rendezvous POI, (c) minimizes total
jumps across combatants. Data source: KB `get_map` when `--db` is given,
otherwise one combatant's live `get_map`. If none found, abort and suggest
`--arena`. `ross_128` (lawless, one jump from `treasure_cache`) is the canonical
example arena.

## Policy presets (extension point for `.smolt` scripts later)

```go
type View struct {
    Self     game.BattleParticipant
    Enemies  []game.BattleParticipant
    Allies   []game.BattleParticipant
    Tick     int64
    BattleID string
}
type Action struct {
    Kind         string         // "battle" | "attack" | "noop"
    BattleAction string         // advance | retreat | stance | target | engage
    Payload      map[string]any // stance, target_id
}
type Policy interface {
    Name() string
    Decide(View) Action
}
```

Presets:

- **aggressor** — advance until `engaged`, then `target` nearest enemy +
  `stance fire`.
- **skirmisher** — hold `mid`, `stance fire`; `retreat` one zone if own hull <
  threshold (e.g. 40%).
- **retreater** — `stance flee` immediately (exercises the multi-tick escape so
  we can measure how long escape takes vs incoming damage).
- **dummy** — `stance brace`, never advance/fire (low-risk practice partner).

The `Policy` interface is the seam where custom `.smolt`-style battle scripts
plug in later (reusing the existing token/script system).

## CLI

```
spar [flags] <agent-1> <agent-2> [agent-3 ...]
  --mode botvbot|partner     default botvbot
  --arena <system_id>        default: auto-discover
  --policy a=aggressor,b=dummy   default: first=aggressor, rest=skirmisher
  --aggressor <agent>        which bot initiates (botvbot)
  --rendezvous <poi-type>    default asteroid_belt → first POI
  --max-ticks <n>            safety cap, default 60
  --no-equip                 skip auto-equip (verify-only)
  --weapon <id> --shield <id>   cheap gear, defaults pulse_laser_i / shield_booster_i
  --rebuild                  commission cheap replacement for destroyed ships
  --db <path>                KB for arena auto-discovery (optional)
  --debug
```

Binary builds to `bin/spar` (project rule: compiled binaries live in `bin/`).

## Error handling

- Login failure for any agent → abort naming the agent.
- No arena found → abort, suggest `--arena`.
- Combatant can't reach arena (no route / out of fuel) → drop that combatant;
  if fewer than 2 remain, abort the match.
- Equip failure (no market / insufficient credits) → warn and continue if the
  combatant is already armed; otherwise drop it.
- Battle never starts (attack rejected) → timeout with the server's reason.
- Ctrl-C → issue `flee` for bots, close clients, print partial summary.

## Testing

- **policy** — table tests: synthetic `View` → expected `Action`, no network.
  Cover each preset and the skirmisher hull-threshold branch.
- **arena** — table tests over synthetic `SystemData` lists for the non-empire
  predicate and safe-adjacency.
- **BattleState parse** — feed a representative `get_battle_status` payload to
  the client response handler → assert `state.BattleState` is populated with the
  right participants/zones/hull.
- **match end-detection** — fake `GameClient` (implements `GetBattleStatus`,
  `Battle`, `GetState`) → assert the loop dispatches policy actions each tick and
  stops when a side is eliminated.

Per CLAUDE.md: run `go build ./...`, `go test ./...`, and `golangci-lint`
before committing.

## Out of scope (this iteration)

- The smart battle handler in `play_as` (auto-retreat/warnings) — separate,
  follow-on work that builds on the `BattleState` foundation laid here.
- Faction-war-in-empire-space arenas.
- Custom `.smolt` battle scripts (the `Policy` interface leaves the seam).
- Multi-side (3+ team) battles — initial presets assume two sides; the data
  model (`SideID`) supports more, but presets target 1v1/2v2.
