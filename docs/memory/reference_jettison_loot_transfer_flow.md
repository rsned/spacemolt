---
name: reference_jettison_loot_transfer_flow
description: "How to hand cargo to a ship that cannot dock: jettison creates a cannister, any co-located agent (including the jettisoner) loots it. Verified working; the not_found storm was a server-cmd parseValue bug, now fixed"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 2cf71781-6ccf-44b8-b879-8971d8d06726
  modified: 2026-07-27T02:29:36.615Z
---

**The problem this solves:** `send_gift` only works *"to another player or to an empire **at this station**"*, so it cannot reach a ship stranded away from a base. `refuel target=<ship>` (tank-to-tank) needs a `refueling_pump` fitted. This path needs neither, and is the operator's intended mechanism for resupplying a ship that cannot dock.

## The flow (operator-verified 2026-07-27)

1. Carrier loads up: `withdraw_items item_id=<x> quantity=<n>`.
2. Carrier flies to the strandee's **exact POI**: `jump target_system=<sys>` then `travel target_poi=<poi>`.
3. Carrier **`jettison item_id=<x> quantity=<n>`** → reply carries **`container_id`** (the field is NOT called `wreck_id`), and the flavour text calls it a *cannister*.
4. Strandee loots it: `loot_wreck wreck_id=<container_id> item_id=<x> quantity=<n>`. play_as syntax is positional: `loot_wreck <wreck_id> <item_id> <qty>`.
5. Strandee consumes: `use_item item_id=fuel_cell quantity=<n>` — `fuel_cell` is `category=consumable`, **"restores 20 fuel"**, base_value 43, size 1. 9 cells = 180 fuel.

**Both directions verified working:** a *different* co-located agent can loot it, AND **the jettisoning agent can loot its own cannister back** (*"Looted 1 aluminum_ore from cannister. There are still more items in the cannister."*). There is no self-retrieval restriction — an earlier note here claiming otherwise was wrong.

Other confirmed details:
- The **undashed 32-char hex** `container_id` is correct as-is; openapi calls `wreck_id` a *"UUID"*, but do NOT dash it.
- A cannister holds **multiple item types** and is looted one `item_id` at a time; the reply tells you when more remains.
- Cannisters live **10 minutes** (`expires_at`) — the whole transfer must land inside that window.
- `jettison`/`loot_wreck` **auto-undock** you (`"Automatically undocked (required for loot_wreck)"`). This bites: that auto-undock is what knocked salvager-3 off its dock mid-diagnosis.
- **`wreck_id` omitted → `invalid_payload: wreck_id is required`**, contradicting openapi's "omit when towing to default to your towed wreck" — that path is legal only while actually towing.
- `tow_wreck` checks modules FIRST (`no_tow_rig`), so it never validates the id — useless as an id-validity probe.

## ✅ RESOLVED: the `not_found` storm was a server-cmd bug, not a game rule

**Root cause: `cmd/tools/server-cmd`'s `parseValue` mangled the wreck id.** Its float branch used `fmt.Sscanf(s, "%f", &f)`, which parses a **prefix** and returns no error — so `wreck_id=158222749cdf759839e4cacd4f943691` went on the wire as the NUMBER `158222749`. Every id starting with a digit was truncated at the first non-digit (`720dfdc8...`→720, `9b61991d...`→9, `041453ac...`→41453), and the server correctly reported `not_found` for a wreck that never existed. The integer branch was already safe because it had a round-trip guard (`fmt.Sprintf("%d", i) == s`); the float branch had none.

Fixed 2026-07-27 with `strconv.ParseInt`/`ParseFloat`, which reject trailing garbage. **Verified end-to-end**: payload went out as `"wreck_id":"158222749cdf759839e4cacd4f943691"` and algol's cargo went 1 → 2 carbon_ore. Residual sharp edge: an id composed ENTIRELY of digits is still coerced to a number — nothing in a `key=value` CLI says which keys are ids. [[project_server_cmd_spec_driven_payloads]]

**Lesson worth keeping: `--debug` on server-cmd prints the exact wire payload, and it settled in one run what six behavioural experiments could not.** Reach for the wire dump before theorising about game rules. The false trail cost several rounds of eliminating POI, expiry, self-vs-cross-agent, id encoding, docked state, and races — all of which were red herrings.

### Historical: the eliminated hypotheses (do not re-investigate)

Symptom was: driving `marketbot_algol` via `server-cmd`, EVERY `loot_wreck` returned `not_found`, while `marketbot_xamidimura` via play_as succeeded against the very same cannisters, seconds apart, at the same POI. These were all ruled out before the wire dump found the real cause:
- **Not POI mismatch**: algol's own `get_wrecks` listed the exact id at its own POI.
- **Not expiry**: attempts were minutes inside the 10-minute window.
- **Not self-vs-cross-agent**: algol failed on both its own and xamidimura's cannister; xamidimura succeeded on both.
- **Not id encoding**: dashed-UUID form fails identically.
- **Not the item/quantity params**: `wreck_id`-only "loot everything" fails too.
- **`quantity` typing was fine** — it really did go out as a JSON int. This is the hypothesis that came closest: the bug WAS payload typing, but on `wreck_id`, and comparing the two clients' payload *shapes* by eye missed it because only the wire dump showed the id had become a bare number.

- **Not docked state**: `undock` on algol returned `not_docked` — it was already in space, same as the succeeding agent. (The successful path shows `auto_undocked: true` in its `action_result`, so auto-undock is normal and harmless.)
- **Not a race, except once**: the FINAL algol attempt (02:22:35Z) was a real race — the operator's loot one second earlier returned `wreck_empty: true` and the cannister despawned. But the earlier failures were not: cannister `720dfdc8` still held its carbon_ore when algol failed at ~02:12–02:14, and it looted cleanly at 02:16:30.

A successful loot returns `xp_gained: {"salvaging": 10}` and `wreck_empty: true` once the last item is taken.

Related: [[project_assist_fleet_refueling_pump_gap]] (why this path was needed), [[project_rescue_pipeline_bugs]], [[feedback_play_as_go_run]].
