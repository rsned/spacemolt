# Resident marketbot home-station enforcement

**Date:** 2026-07-14
**Status:** Approved design, pending implementation plan

## Problem

Five of the 35 overmind marketbot workers are docked at the wrong station.
They capture whichever station's market their account happens to be parked at,
so displaced bots duplicate coverage while their intended stations go
uncaptured. Confirmed 2026-07-14 from `data/overmind/mb-status.json` and
in-game `get_nearby`:

| Bot | Should cover | Actually docked at |
|---|---|---|
| `marketbot_market_prime` | `market_prime_exchange` (Market Prime) | `grand_exchange` (Haven) — with `marketbot_haven` |
| `marketbot_node_beta` | `node_beta_industrial_station` (Node Beta) | `the_core` (Nexus Prime) — with `marketbot_nexus_prime` |
| `marketbot_frontier` | Frontier | `unknown_edge_waystation` |
| `marketbot_ramens_rest` | (see below) | `unknown_edge_waystation` |
| `marketbot_the_telescope` | (no home station) | `unknown_edge_waystation` |

`market_prime_exchange` and `node_beta_industrial_station` have **zero rows**
in `market.db` — never once captured — while `unknown_edge_waystation` is
quadruple-covered.

### Root cause

`mb-fleet.yaml` sets `station: ""` for every marketbot. That field is consumed
**only** by the assist role (`pkg/worker/dispatch.go:27`,
`pkg/worker/assist.go:320`). The `resident` / `resident_gas` / `resident_ice`
roles in `data/overmind/roles.yaml` are pure sit-and-scan: an hourly
`view_market` / `update_market` / `facilities` schedule plus an `idle_market`
loop, with **no travel step anywhere**. A resident captures whatever station it
is already docked at, forever. Home exists only in the agent's *name*; nothing
enforces it, and nothing pulls a drifted bot back.

This is the same duplicate-pull problem the `mb-fleet.yaml` header documents for
the nine bots removed 2026-06-26; these five are survivors of it.

### Two structurally-broken bots

- `marketbot_the_telescope`: The Telescope has **no station** in the KB — nowhere
  to dock. Decision: **reassign** to a real uncovered station.
- `marketbot_ramens_rest`: its namesake `ramens_rest` (Last Light) is already
  home to `marketbot_last_light`. Sending it home re-creates a duplicate.
  Decision: **reassign**.

## Approach (chosen)

A data-driven `ensure_home` dispatch command, plus populating the existing
`station:` field in `mb-fleet.yaml`.

`WorkerDispatch` already carries the home POI as `d.Station`. The new command:

1. Read `d.Station` (home POI). Empty → no-op (unconfigured bots unaffected).
2. `GetState()`; if already docked at the home POI (`CurrentPOI == home && Doc`)
   → no-op. Pure-local, no server call — safe to run every idle pass.
3. Otherwise resolve the home **system** live via `Client.FindRoute(homePOI)`:
   last hop's `SystemID`, or the current system when the route is empty. This is
   the exact mechanism `assistResolveMobile` (`pkg/worker/assist.go`) already
   uses for `mobile_capital` — no hardcoded system map, no KB dependency.
4. `Autopilot(system, homePOI)` → `Dock`. Best-effort: log and return `nil` on
   any failure so the idle loop retries next pass. Reuse `assistEnsureHome`'s
   guard: only `Autopilot` when `CurrentPOI != home` (re-traveling to the POI we
   already occupy undocks us every pass and thrashes undock↔dock forever); only
   `Dock` when not already docked; tolerate "Already docked".

`ensure_home` is placed as the **first line of the resident idle script**, so a
displaced bot is nudged home each idle pass and a home-docked bot no-ops. Running
it before the market-capture commands also stops a transiting bot from capturing
the wrong station.

### Alternatives rejected

- **Go-side `StandingDeps` hook** (like `PayDebts` / `Handoffs`): less consistent
  with the data-driven role design, and a dispatch command is reusable by any
  role's idle/schedule script.
- **KB lookup for the home system** (`pois.system_id`): adds a stale-KB failure
  mode when `FindRoute` is server-authoritative and already proven for mobile
  homes.

## Config changes — `mb-fleet.yaml`

Populate `station:` per bot. 31 keep their namesake station. Notable entries:

| Bot | `station:` | Note |
|---|---|---|
| `marketbot_001` | `98eba8b1a7ad0520d6a7c8ea44b2d6aa` (Hex Star) | player station, Dheneb |
| `marketbot_frontier` | `expedition_launch` | avoids assist-frontier's `mobile_capital` |
| `marketbot_the_telescope` | `ironhearth_station` | **reassigned** — Ironhearth, Low-Sec crimson (only safe uncovered empire station) |
| `marketbot_ramens_rest` | `scout_docks` | **reassigned** — 2nd Frontier station, Max-Sec |

Both reassignment targets are **pending a jump-reachability check at relocation
time**; if unreachable, swap to the next safe uncovered candidate. Uncovered
stations were surveyed 2026-07-14: almost all sit in Lawless (police 0) or
pirate/stronghold systems and are unsafe for a stationary resident — Ironhearth
(police 55) and Frontier's spare stations are the safe exceptions.

The home POI for the 31 "namesake" bots is filled from each bot's current
at-home position / the single station in its named system (verified against the
KB `pois` table).

## Relocation as a side effect

Once shipped and the fleet is rebuilt + redeployed, `ensure_home` **moves all
five bots itself**: the three fixable ones travel home; the two reassigned ones
travel to their new stations. "Code fix" and "relocate the bots" collapse into a
single redeploy, which doubles as a live test of the feature. Standard
supervisor-freeze + worker-stop + staggered relaunch applies (see login-rate
memory).

## Testing

- Unit tests on `ensure_home` in `dispatch`, following `assist_test.go` patterns
  with a mock client:
  - already docked at home → no `FindRoute` / `Autopilot` / `Dock` calls;
  - displaced → `FindRoute` then `Autopilot(system, home)` then `Dock`;
  - empty `Station` → no-op;
  - `FindRoute` error → returns `nil` (best-effort), no `Autopilot`;
  - "Already docked" from `Dock` is tolerated.
- `roles_test`: `ensure_home` present in the `supported` command set (the test
  enforces every command named in `roles.yaml` / scripts exists in dispatch).

## Risks / edge cases

- A bot that genuinely cannot reach its home retries every pass — bounded, logs,
  never crashes (same behavior as assist today).
- `ensure_home` runs before market capture, so a transiting bot never writes the
  wrong station's snapshot.
- Player stations and capitals resolve fine through `FindRoute` (proven live for
  `mobile_capital`).

## Out of scope

- Generalizing `ensure_home` to non-resident roles (assist keeps its existing
  path).
- Dynamic fleet membership / hot add-remove of a single worker.
- The broader `market.db` retention and arbitrage-detection questions raised in
  the same session (tracked separately).
