---
name: project_assist_fleet_refueling_pump_gap
description: "The assist (fuel-rescue) fleet could not refuel anyone because no ship had a Refueling Pump fitted; RESOLVED 2026-07-29 — all 5 fitted, spare pump banked at central_nexus — but the rescue claim election is still pump-blind"
metadata: 
  node_type: memory
  type: project
  originSessionId: 2cf71781-6ccf-44b8-b879-8971d8d06726
  modified: 2026-07-30T03:49:01.341Z
---

**⭐ SUPERSEDED for Tanker hulls — see [[reference_assist_tanker_migration]] (08-14 migration; pump is built in).** The pump-module story below applies only to NON-tanker hulls; the pump-blind election bug is still real.

**Discovered 2026-07-26 while diagnosing salvager-3.** The assist fleet exists solely to refuel stranded workers (operator's words), and **not one of its five ships had a `refueling_pump` fitted** — so every rescue ran the full claim → autopilot → refuel sequence and died on the last step:

```
refuel: no_refueling_pump: You need a Refueling Pump module fitted to
        transfer fuel to another ship. Install one in a utility slot.
```

Two logged victims in `assist-overmind.log`: **assist-krynn** (2026-07-24 — an assist agent stranded itself) and **salvager-3** (2026-07-26). Because a failed rescue record is TERMINAL and nothing retries it ([[project_rescue_pipeline_bugs]]), salvager-3 sat fuel-dead at `alpha_extraction_zone` (Node Alpha) for ~17h.

**✅ FITMENT COMPLETE 2026-07-29** — all 5 assist ships carry a pump. The last hole, **assist-nexus**, was closed when the operator hand-flew 24 jumps to Nexus Prime and gifted **2× `refueling_pump`**; I withdrew one and installed it. Verified by `get_ship`, not inferred: module `7a6d379871c69270c60a84537266a983`, `type_id: refueling_pump`, `slot: utility`, Pristine, present in `ship.modules` (threshold, 3/5 slots, cpu 11/16, power 19/30). **One spare pump is banked in storage at `central_nexus`** — use it for the next bare hull instead of buying.

| agent | hull | pump |
|---|---|---|
| assist-haven | prospect | ✓ |
| assist-sol | theoria | ✓ |
| assist-frontier | cobble | ✓ |
| assist-krynn | siphon | ✓ |
| assist-nexus | threshold | ✓ (2026-07-29) |

**🔴 TWO CLAIMS IN THE ORIGINAL VERSION OF THIS NOTE WERE WRONG — don't re-derive them:**
- **"No remote gifting — giver and recipient must be co-located" is FALSE.** A gift is deposited **at the SENDER's station** and waits in the recipient's storage; only the **sender** must be docked. `SendGiftResponse` has a required **`base_id`** plus `storage_remaining`, and there is a `StorageGift` shape. Proven twice: 14 credit gifts landed on agents scattered across Nova Terra/Sirius/Procyon/Sol while johnny_cab sat at `the_experiment`, and these 2 pumps landed at `central_nexus` for later collection. So the real constraint is only **"gift at a station the recipient can reach."** [[reference_send_gift_and_play_as_mechanics]]
- **"`play_as` cannot be used from a tool shell (interactive TTY REPL)" is FALSE.** It takes piped stdin fine — just **end the script with `quit`** or it spins on EOF. One session beats N× `server-cmd` logins.

**`central_nexus` is the BASE id for poi `the_core`** — `bases.id` ≠ `pois.id` (cf. base `grand_exchange_station` ↔ poi `grand_exchange`). A gift receipt naming `central_nexus` is NOT a different station, so don't send a ship travelling. Resolve with `select id, poi_id from bases where id=?`.

**Collect-and-fit is NOT automatic** — the assist worker has no step for it. A gifted pump needs `withdraw_items <item> <qty>` (storage→cargo) and then `install_mod <module_id>`, done by hand in the safe window. Syntax in `play_as`: `withdraw refueling_pump 1` then `install refueling_pump`.

**Market context, if a pump ever must be bought:** nothing sells one at `the_core` (`sell_orders: []`; only craftsman-1's 2-credit vacuum bid, [[reference_craftsman1_vacuum_bid_economics]]). Cheapest stocked source is `war_citadel` at **800**; `node_alpha_processing_station` 3,500; everywhere else 11,077+. `base_value` 1,100. market.db asks are a LOWER BOUND.

**🔴 THE DURABLE BUG: the claim election is pump-blind.** `claimNearestPending` → `assistElect` (`pkg/worker/assist.go:181`) ranks purely on distance with a lexicographic tiebreak and never checks fitted modules; the pump only surfaces at the `fail("refuel", err)` call in `runRescue`. Fix = require a fitted `refueling_pump` to win the election, letting the existing `assistTakeoverInterval` rank-fallback hand the rescue to the next-nearest equipped agent. That gate is what keeps a future refit or a newly-added bare hull from silently re-introducing this. Now that all 5 are fitted the gate is latent rather than active — which is exactly why it will be forgotten.

**⚠️ AGE FULLY DEFEATS THE DISTANCE ELECTION.** `assistElect` ends in `age >= rank*assistTakeoverInterval`, and `assistPendingAge` reads **`requested_at`** (not `updated_at`). A record days old therefore passes *every* assister's gate at *every* rank, so nearest-home routing is off entirely and whoever polls first wins. Observed 2026-07-29: two re-armed records with 2-day-old `requested_at` went to **assist-frontier (Starfall) for a rescue in Sol** and **assist-krynn (Krynn) for one in Nexus Prime** — each skipping the assister living in that very system. **So when you hand-re-arm a record, reset `requested_at` too, or you get worst-case routing.** An unparsable timestamp is deliberately treated as infinitely old (`1 << 62`).

**⭐ LONGER-TERM GOAL (operator, 2026-07-26): move every assist agent into a Tanker hull.** `Category: Commercial, Class: Tanker` exists for every empire plus pirate variants, with the **refuel pump built in** and huge tanks — which fixes the pump gap AND the range problem in one move. Today's assist ships carry 95–140 fuel (prospect/theoria/cobble/siphon/threshold), which is why only the 1-jump agent can realistically reach anything. Tier 2 entry points ~1,500 fuel: `Capacity` (Solarian), `Last Call` (Pirate), `Long Haul` (Outer Rim), `Morningstar` (Crimson). Tier 3 `Reserve` (Nebula) 4,000. Tier 4 tops out 10,000–12,000: `Plenum` (Voidborn) 12,000, `Endowment` (Nebula) / `Last Drop` (Outer Rim) 11,500, `Sustenance` (Solarian) 11,000, `Warbarge` (Crimson) 10,000.

**DEFERRED (operator, 2026-07-26): both the pump-blind `assistElect` gate and the Tanker migration are later tasks — do not build either without a fresh go-ahead.**

**How salvager-3 was ACTUALLY resolved (2026-07-27), and the lesson:** the rescue-queue record was **17 hours stale** — it said `alpha_extraction_zone`, but the ship was docked at `node_alpha_processing_station` with **11,799,821 credits** and a full hull. `dock` + `refuel` cost **1,080 cr** (540 market + 540 tax) and that was the whole fix. Deleting its record from `rescue-queue.json` then triggered the documented **"quarantined with no record = manual release"** path in `pollRescues`, and the haul overmind relaunched it. **Read the strandee's LIVE position (`get_status` → `player.current_poi`) before planning any rescue.** The missing pumps and pump-blind election are real and still worth fixing, but neither was this incident's blocker.

**Ops note — the safe window works and costs one restart-free reconnect.** Worker is docked so the stall watchdog can't fire; `SilenceTimeout` is 90s, so `kill -STOP <worker-pid>` → ≤60s of work → `kill -CONT`. Find the pid by scanning `/proc/*/cmdline` for `bin/worker` (NOT `pgrep -f`, which self-matches). **`play_as` with piped stdin is the better headless path** — one login for many commands — but it replaces the worker's session: the worker logs `Disconnected: ... session_replaced` and then `✓ Reconnected successfully` ~3s after `-CONT`, staying healthy with the restart counter unchanged. Two windows of 4 and 1 commands cost ~31s and ~8s. [[feedback_play_as_go_run]] [[reference_overmind_launch_commands]]

**⭐ 2026-08-15 CORRECTION (operator): Tanker-class hulls have a BUILT-IN refueling
pump** — no module needed. Proven live: assist-frontier's Capacity transferred +420
fuel to salvager-10 with no fitted pump. So the pump-module story above only applies
to non-tanker hulls, and the proposed pump-aware claim election must treat
class='Tanker' (Capacity/Reserve/etc, see knowledge `ships.class`) as always
equipped. Tanker migration state 2026-08-15: sol+frontier on Capacity, haven on
Reserve; **assist-nexus's Capacity is parked at central_nexus co-located with the
agent (bought in solarian space, hand-flown home, final switch_ship never done)**;
assist-krynn owns no tanker yet. [[project_refueler_ship_roadmap]]
