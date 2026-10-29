# Bead 002: Mining circuits — pipeline miners rotate through an ordered POI list

**Status:** Proposed
**Date:** 2026-10-28
**Triggered by:** Cloverfield Frozen Drift and Gas Plume ran dry on 2026-10-28; nine CRFT miners logged ~75 "Deposits here are too sparse" errors each until they were repointed by hand.

---

## Request

Add a "patrol" or "circuit" route for mining agents. The agent works POI A, then B, C, D, moving on whenever the current POI runs thin, and loops back to A. Each POI regenerates while the others are being worked.

A circuit with a single POI is today's behaviour: mine the same belt non-stop. Every pipeline miner can then use one config shape.

## Where it fits today

- The fleet yaml pins each miner to one `pipeline: "item,system,poi,drop-station,recipient"` (`data/overmind/crft-mining-fleet.yaml`).
- The idle pass is refuel, fly to the POI, mine until full, fly to the drop station, deliver, repeat (`pipeline_miner` / `mine_qty_at` in `pkg/worker`).
- `mine_qty` already stops after 5 consecutive dry passes. The server refuses a lock when the richest deposit is below the array's threshold: *"The richest deposit holds 174 units; your array needs at least 175 to get a lock."*

## Design notes

- **Config:** an ordered list of stops, each with its own item (a circuit may mix iron at one belt with copper at the next). The drop station and recipient stay per-miner.
- **Advance trigger:** the sparse-deposit refusal, or the existing dry-pass limit. Also advance when a hold comes back with almost none of the target item. Kurhah deuterium at richness 16 returned 0/175 per hold because water and nitrogen crowded it out.
- **Loop:** after the last stop, return to the first. Remember the current stop across worker restarts (checkpoint).
- **Safety per hop:** keep the Lawless gate and the stronghold check on every stop and route. Reject hidden POIs (`reveal_difficulty > 0`) at config load: Beid's `deep_deuterium_ice_sheet` failed with `Unknown destination`.
- **Visibility:** log each advance (`circuit: <poi> thin (<reason>) -> <next poi>`) so the line dashboard can show where each miner is.

## Also for the hunt fleet

The same circuit shape works for hunters (user, 2026-10-28): hunt at Belt A, then Belt B, and so on, looping back. The advance trigger differs. A hunter moves on when its current belt has no targets left, or after a set time without an engagement, rather than on a sparse-deposit refusal. Build the circuit as a shared route type that both `pipeline_miner` and the hunt pass (`pkg/worker/hunt.go`) consume, each with its own "move on" test.

## Open questions

- Do other miners already working a POI count toward "thin", or only this miner's own refusal?
- Should a circuit skip a stop that was thin on the last lap until a minimum time has passed?
