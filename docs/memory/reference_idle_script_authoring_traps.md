---
name: reference_idle_script_authoring_traps
description: "Four traps when authoring worker idle scripts (.smolt): dead idle_params, stale POI cache after jump, first-belt selection, per-agent override path"
metadata:
  type: reference
---

Learned building the mining fleet 2026-08-21 ([[project_mining_fleet]]). All four
were caught live, three of them only because the fleet was watched after launch.

**1. `idle_params` IS DEAD CONFIG.** `Role.IdleParams` is declared in
`pkg/worker/roles.go:21` and consumed NOWHERE — `resolveIdle` (standing.go:178)
returns script lines without calling `SubstituteParams`, which is wired only for
assigned-task params (standing.go:294). So any `$FOO$` a role tries to inject
stays literal. `mine_local.smolt`'s `loop -f $COUNT$ mine` and
`mining_run.smolt`'s `$TARGET_SYSTEM$` are both unusable from a role for this
reason. The `resident` role's `idle_params: N: "20"` is vestigial — harmless only
because `resident_market.smolt` contains no `$N$`. **Use `idle_mine.smolt`
(hardcoded 25) or a per-agent script; never a param-bearing one.**

**2. `$ASTEROID_BELT$` / `$STATION$` RESOLVE AGAINST A STALE POI CACHE AFTER A
JUMP.** They resolve from the client's cached system POI list, which still holds
the PREVIOUS system right after `jump`. miner-10 jumped to frostfeld and then
tried to travel to "Ironhearth Fields", the belt it had just left; the pass died
there. **Put `get_system` after EVERY jump** — it is an info query, no tick cost.

**3. `FindMiningLocation` RETURNS THE FIRST BELT, WITH NO RICHNESS RANKING**
(`pkg/game/helpers.go:47`). In a multi-belt system `$ASTEROID_BELT$` is a coin
flip. Live: `overmind` sat on `hd_20794_belt` (~24k across all resources) while
`hd_20794_forge_vein` in the same system held 62,283 copper + 12,875 iron — a 50%
depleted rate against 100% for every other miner. **In any multi-belt system,
name the POI literally** (`travel hd_20794_forge_vein`). Single-belt systems are
the only place `$ASTEROID_BELT$` is safe.

**4. PER-AGENT SCRIPTS OVERRIDE SHARED ONES**, and this is the way to vary
behaviour per agent when params can't. `ScriptSearchPaths` (scripts.go:17) tries
`data/agents/<agentID>/scripts/<name>.smolt` BEFORE `data/scripts/<name>.smolt`.
So three agents can run a custom `idle_mine` while the rest keep the shared one,
with no role change. Paths are relative to the REPO ROOT (workers run with cwd
`/home/robert/spacemolt/spacemolt`) — a `go test` in `pkg/worker` cannot resolve
them, so don't "verify" the override with a unit test; check the file path.

**5. The idle script is read ONCE at worker start** (standing.go:129), so editing
a .smolt needs a worker restart. Killing a single worker is enough — the
supervisor respawns it (costs 1 restart on the counter).

**`loop -f` semantics (operator, 2026-08-21):** `-f` tolerates errors so a mined-out
belt is FINE — resources respawn and an idle agent has nothing better to do.
`mine` + `cargo_full`/`no_cargo_space` raises `GoalReachedError`
(`server_errors.go:56`), which the loop treats as a POSITIVE early exit
(`🎯 goal reached`), falling through to deposit. So `loop -f 100 mine` means
"mine until the hold is full", not "mine 100 times". **Do NOT report
"Resources depleted" as a fault.**
