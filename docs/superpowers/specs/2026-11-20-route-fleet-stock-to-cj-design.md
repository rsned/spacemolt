# Route fleet: move our stored stock to Conjunction Junction (short design)

**Approved by operator 2026-11-20** ("go ahead with design").

## Intent
Our agents hold millions of units in personal storage at about 31 stations outside CJ. That
includes 1.23M carbon_ore, while CJ faction storage holds 2. Ten new accounts (trucker-100..109:
CRFT, gift unlocked, starter Cobbles with 75 cargo) get pinned to explicit station routes. They
move that stock into CJ faction storage, and 1–2 of them supply Beid. They move to hauling hulls
once those are found or built.

## Constraint that shapes it
Only the owner can move personal storage, and only while docked at that station. A trucker
can't pick it up alone. The owner can hand it over in one call with no cargo trip:
`send_gift {recipient, item_id, quantity, source:"storage"}`.

## Approaches
1. **Release + haul (chosen).** The owner gifts storage to the route's trucker when docked
   there; the trucker hauls its own storage to CJ with the existing `deliver`. Small new
   code: one verb.
2. Owners haul their own stock. Rejected: owners are miners and marketbots with other jobs,
   and the operator wants dedicated haulers.
3. Owners deposit into CRFT faction storage at the station, and truckers withdraw it. Rejected:
   most owners are not in CRFT, and CRFT faction storage away from our stations is unproven.

## Design
- New worker verb `release ITEM STATION RECIPIENT [KEEP]`:
  - It is a no-op with no API calls unless the agent is docked at STATION (base id or POI id).
  - Otherwise it reads its own storage and gifts `held - KEEP` of ITEM to RECIPIENT with
    `source:"storage"`.
  - RECIPIENT resolves like `deliver` (an agent id goes through credentials.json).
  - A refusal (e.g. gift not unlocked) is an error, so it shows in task outcomes.
- Owners run it `ten_minutely` (cheap while undocked), so a roaming miner releases on its
  next dock there.
- Truckers run `hourly deliver ITEM 100000 <station> <CJ base> faction`, which is existing code:
  withdraw own storage at the station, carry to CJ, faction-deposit, repeat until the source is
  short.
- First routes (my calls):
  - trucker-100: CJ -> beid, `daily deliver liquid_hydrogen 1000 faction:<CJ> <beid> faction`.
    It takes over from craftsman-2.
  - trucker-101 + 102: Treasure Cache carbon. Owners: prophet-1, miner-1 -> 101;
    marketbot_treasure_cache (keep 50,000 = its shopkeep floor), miner-4 -> 102.
  - trucker-103: Gold Run carbon. Owner: miner-6. marketbot_gold_run is excluded: it already
    consigns carbon to trader-9 for CJ. craftsman-1 is excluded: operator account.
  - trucker-104: Ironhearth carbon. Owners: marketbot_015 (keep 50,000), prophet-2.
  - trucker-105..109: held for the next piles.
- Non-goals: hull upgrades and auto-discovery of piles. Routes are hand-set in schedules for now.

## Left to the builder
Log wording, test shape, where the verb lives (pkg/worker/release.go).
