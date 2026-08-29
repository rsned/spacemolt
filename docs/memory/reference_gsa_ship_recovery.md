---
name: reference_gsa_ship_recovery
description: "Galactic Salvage Authority auto-docks ships left drifting undocked and charges a recovery fee, logged as salvage.ship_recovered under category=salvage — explains mystery dockings and offline charges"
metadata: 
  node_type: memory
  type: reference
  originSessionId: f744e650-ff1a-4add-9401-5a3087024568
  modified: 2026-08-17T16:20:46.681Z
---

**A ship left drifting undocked is eventually docked FOR you by the Galactic Salvage Authority, which charges a recovery fee** and notifies by private DM (operator, 2026-07-28).

- Log event: **`salvage.ship_recovered`**, carrying **`fee_amount`**.
- **Filed under `category: salvage`** — which most of our filters miss, so the charge looks like it came from nowhere. **If a credit charge appeared while the agent was offline, look here first.**
- **⭐ THE IDLE WINDOW IS 30 MINUTES, MEASURED 2026-08-09.** pirate-6's last
  action at `17:59:33Z`, recovery at `18:29:33Z` — exactly 30:00, with no
  session open in between. Fee **500cr**. The payload names all three facts:

  ```json
  "event_type": "salvage.ship_recovered",
  "data": {"abandoned_poi": "factory_belt_manufacturing_hub",
           "fee_amount": 500, "fee_charged": true,
           "recovery_base": "factory_belt_manufacturing_hub"}
  ```
- **It MOVES the ship, not just docks it.** pirate-6 was towed from Khambalia
  Crystal Market to Factory Belt Manufacturing Hub — **8 jumps** from where it
  was left. So the cost is the fee PLUS the round trip back to whatever the
  agent was doing.

**⚠️ Consequence for the hunt fleet** [[project_pirate_bands]]: the hunt spec
says a pass "travels to the quarry's own POI type, and **stays out there**; it
never returns to a pinned home". A belt is station-less by that design's own
rule, so a hunter that finishes a pass and idles is undocked at a belt — the
exact GSA setup. At 500cr against a `first_hunt_belt_grazers` paying 1000cr
nominal (and far less realized, [[project_empire_treasury_payout_collapse]]),
**one tow can exceed the mission's whole realized reward.**

Not yet established: whether continuous worker activity resets the timer. A
live worker acts every ~11s and probably never trips it; the measured case had
no session at all. The real exposure is an agent left undocked **across a drain
or a stop** — which is exactly what SIGUSR1 does — not one mid-pass.

**Why this matters operationally:**

1. **It retro-explains "mystery docked" agents.** Several stranded ships we investigated turned out to be sitting IN a station with no one having sent them there. GSA recovery is the likely cause, not a worker bug — check the salvage log before debugging a phantom dock. (Relevant to any future re-run of the [[project_rescue_pipeline_bugs]] investigations, and to [[reference_docked_at_base_gotcha]] where dock state came from an unexpected path.)
2. **It is a THIRD rescue option** alongside dispatching assist and manual play_as intervention — and the only one that needs no rescuer at all. When the assist fleet is too far, lacks fuel, or has no `refueling_pump` ([[project_assist_fleet_refueling_pump_gap]]), simply leaving the ship drifting gets it recovered for a fee. That is a bounded, known cost versus a rescuer that may strand itself en route — see the flat-`FuelPerJump` bug in [[project_rescue_pipeline_bugs]].
3. Worth comparing fee_amount against the fuel + time cost of an assist run before dispatching one at all.

**~~Not yet done~~ DONE 2026-08-17:** `salvage.ship_recovered` now lands in
`action_log_events` (assets.db) with its full `data` payload — `fee_amount`,
`abandoned_poi`, `recovery_base` — via [[project_action_log_capture]]. It is not
on any prune list, so tows are kept in full and the idle window / fee-vs-assist
comparison is finally computable. Live on two canaries only so far; the fan-out
to the rest of the fleet is still pending. Remember `abandoned_poi` mirrors
`recovery_base` and the true origin is only in the summary text, which this
capture deliberately does NOT store — so origin must come from the preceding
`navigation.jumped` row (72h TTL) or the battle/travel log.

## GSA vs the rescue pipeline (2026-08-13)

**A quarantined agent cannot be towed back into service, because it is the
quarantine — not the stranding — that keeps it out.** salvager-3 and salvager-9
sat out of every fleet for FOUR DAYS after their rescue records went
`status: failed` (5 attempts, refused by all five assisters). The supervisor
stops a quarantined worker and never relaunches it until `ClearQuarantine`, and
`restoreQuarantine` re-applies that at every boot, so it survives restarts.

GSA had already fixed the actual problem while they sat there:

| agent | rescue record said | where GSA actually left it |
|---|---|---|
| salvager-3 | `first_step/mobile_capital`, 94/270 fuel | `first_step_memorial_station` — a station WITH a fuel desk |
| salvager-9 | `first_step/mobile_capital`, 0/120 fuel | **Valor**, towed clean out of the system |

So all five rescuers were dispatched to a four-day-old POI and found empty
space, while salvager-3 stood next to a fuel desk with 15M credits. Released, it
refuelled itself to 270/270 unprompted. Tow fees were 2,028 and 1,415 credits —
irrelevant against those balances.

**How to release a quarantined agent:** delete its record from
`data/overmind/rescue-queue.json` (take the flock on
`rescue-queue.json.lock`). `pollRescues` sees a quarantined worker with no
record, logs `rescue: no record for quarantined <id>; releasing`, and calls
`ReleaseQuarantine` on the next status tick — the worker relaunches within
~60s. There is no CLI tool for this.

**How to apply:** when a rescue fails repeatedly with location errors
(`different_location`, or "insufficient fuel" on jump 1 of a long route), do
NOT send more rescuers. Stop the worker, let GSA's 30-minute window tow it,
then delete the record and let it report its own position. Trust the LIVE
position over the record every time — the ALERT text says so, and nothing in
the pipeline enforces it. See [[project_rescue_pipeline_bugs]].

## GSA tows OUT of pirate strongholds — cargo intact (2026-08-15)

**"Stronghold-stranded = unrescuable" is wrong: GSA is the rescuer.** Both
haul agents stranded in stronghold systems during the 08-13/14 incident were
towed to safety by GSA for 500cr each, cargo untouched:

| agent | stranded at | GSA left it at | fee | cargo preserved |
|---|---|---|---|---|
| trader-10 | Dross Citadel (Algol) | Ramen's Rest (last_light, empire) | 500 | 37 plasma_gas |
| explorer-1 | Xamidimura | Ramen's Rest (last_light, empire) | 500 | 288 trade_authenticator (~2.9M face) |

trader-10's tow fired at `2026-08-14T02:04:53Z` — the whole subsequent rescue
effort (assist-sol's Algol approach the user aborted as suicidal) was chasing
a ship that was ALREADY SAFE. Check the salvage log before planning any
stronghold rescue; the answer is almost always "do nothing for 30 minutes."

**Payload quirk:** in the tow event, `data.abandoned_poi` MIRRORS
`recovery_base` (both said `ramens_rest`) — the true origin appears only in
the `summary` text ("Ship recovered from Dross Citadel..."). Do not trust
`abandoned_poi` as the stranding location when capturing these events.

**⚠ Do NOT conclude "the scanner sent a rep-less agent into a stronghold."**
Checked against `agent_capability`: explorer-1 and trader-10 BOTH hold
`stronghold_access` eligible=1, and explorer-1 had *completed* hauls sourcing
from Dross Citadel and Mera Sanctum hours earlier. Their stronghold routes
were legitimate; the strandings were fuel/route failures, not rep gates. Only
salvager-3 lacks access ("best pirate_crix baseline -30, needs 10"). A
stronghold destination is a FACT about a route, not evidence of a planner bug
— check the claiming agent's capability row before blaming allocation.
[[project_pirate_reputation_unlock_campaign]]
