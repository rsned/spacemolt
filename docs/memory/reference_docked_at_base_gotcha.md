---
name: reference_docked_at_base_gotcha
description: "docked_at_base lives on the PLAYER object (never the ship), and dock events historically never recorded it — so State said doc=true, docked_at=\"\" and every base-id-keyed feature silently no-opped"
metadata: 
  node_type: memory
  type: reference
  originSessionId: f744e650-ff1a-4add-9401-5a3087024568
  modified: 2026-07-28T02:34:49.622Z
---

`docked_at_base` is a **player** field (`game.Player.DockedAtBase`, `serverapi` types.go:151). The ship payload has **no such key** — `game.Ship.DockedAtBase` exists in our struct but the server never fills it. Reading the ship copy compiles, never matches, and fails silently.

**The deeper trap (fixed 2026-07-27, `d7c01c3`):** the id only ever arrives on a **full player payload**. `parseActionResult`'s `dock` case set `Doc = true` but never recorded *where*, so a worker that logged in undocked reported `doc=true docked_at=""` for its entire life. Everything keyed off the base id then silently did nothing:
- `handoff_pass.go:52` returns early when `DockedAtBase == ""` → handoff passes were dead.
- `MissionDeps.HomeStation` pins could never match → a pinned worker undocked and re-docked every dry pass instead of parking.

`dock`'s own reply carries `result.base.id` (`serverapi.DockResponse.Base`), so that is now recorded there; `undock` clears it.

**STILL UNCOVERED — two other paths set `Doc = true` without a base id:** `protocol.TypeDocked` (client.go ~2422) and the `auto_dock` OK frame (~2345). A worker whose dock completes via either still reads `docked_at=""` — observed live on engineer-1 while explorer-12 (which got a real `dock` action_result) parked correctly. Do not assume a fix is universal until you see the log line for *that* worker.

**Related server quirk:** the login handler comments that the server's `docked_at_base` **persists after undocking**, which is why it is only meaningful paired with `State.Doc`. Login derives dock state from POI type instead.

**Method note:** this was found by printing `doc=%t docked_at=%q` at the decision point after two wrong static theories. Same family as the `withdraw_items` gap — [[project_freight_withdraw_silent_failure]] — where the action succeeds and the flag updates but the identity field never does. When a state-keyed feature no-ops, print the field, don't reason about it.
