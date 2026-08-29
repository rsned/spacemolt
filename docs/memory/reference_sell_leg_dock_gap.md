---
name: reference_sell_leg_dock_gap
description: "FIXED 2fea237a 2026-08-15: haulSellLeg never docked — autopilot leaves the ship at the POI undocked and the already-there early return skips travel, so a resumed haul looped 'not_docked' forever; standing AT a POI is not being DOCKED"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 9acefb51-d097-42a4-bd75-27e0bb652f32
  modified: 2026-08-16T00:21:48.462Z
---

**"Already at the station" is NOT "docked at the station."** Two correct
changes combined into an infinite loop:

1. `autopilotTravelToPOI` returns early when the ship already stands at the
   target POI — the 2026-08-13 fix for the priced-before-measured fuel loop
   ([[reference_travel_priced_before_measured]]).
2. `haulSellLeg` had **no `Dock` call**, relying on `Sell` to auto-dock.

A *resumed* haul (worker restart → `GetClaimedByAgent` → destination already
reached) therefore never travelled and never docked. Where the station does not
auto-dock, `Sell` fails `not_docked` every pass forever. The buy leg had
carried the explicit `Dock` — and a comment saying autopilot leaves the ship
undocked — since it was written; only the sell leg was missing it.

**Live cost 2026-08-15:** craftsman-1 sat at Korr Fortress (Gliese 581) holding
**1,100 liquid hydrogen worth ~300,300 cr**, retrying every 10s and killed by
the stall watchdog every 15 minutes (six restarts, counter to 25) until an
operator SIGSTOP'd the worker and docked it by hand. The sale then filled
instantly for 300,300.

**Fixed `2fea237a`** — dock before selling, guarded on `!st.IsDocked()` because
a docked ship answers "Already docked" and that must not abort a sale we are
standing on. **Regression tests must construct `game.State{Doc: false}` and
assert `dock` appears BEFORE `sell:` in `fakeClient.calls`** — proven red by
neutering the block (`calls=[find_route travel sell]`, no dock).

**Where else to audit:** any path that reaches a market action after
`haulAutopilot` without an explicit dock. Watch for this shape whenever a
worker "arrives" via the resume path rather than by travelling.

**Operator note:** the manual unstick is the standard safe window — SIGSTOP the
worker, `bin/play_as <agent>` piped with `dock` / `sell <item> <qty>` / `quit`,
then SIGCONT. Expect the overmind to kill the stopped worker on its 90s silence
timeout and respawn it; that is harmless, the sale is already banked.
