---
name: reference_faction_facility_rent_mechanics
description: "Facility rent is per-station and INVISIBLE until you build; faction_deposit_credits needs faction_storage AT that station (23,562 failures); faction_build draws from faction storage, not personal; gift the parked leader instead"
metadata:
  type: reference
---

2026-10-12: Crafting Collective [CRFT] lost **11 of 14** facilities to unpaid
rent in one morning, and Data Bots [DB] had lost its Storage + Office months
earlier. Neither was an accident — three independent faults compounded.

## The three faults

1. **`faction_deposit_credits` is station-local.** It requires a Faction
   Storage facility *at the station you are docked at*. Error:
   `no_faction_storage: Your faction does not have a storage facility at this
   station.` Counted in `haul-overmind.log`: **23,562 failures vs 4 successes.**
2. **DB was deadlocked.** Losing its Lockbox meant deposits failed everywhere,
   so the treasury could never be funded, so rent could never be paid, so
   nothing could be rebuilt. Treasury sat at **43 credits** with 46 members.
3. **CRFT had no inflow at all.** `depositProfitShare` (the 5% cut,
   `TreasuryProfitCut`) is wired into **only** `pkg/worker/haul.go:1652` and
   `pkg/worker/shuttle.go:736`. CRFT is all craftsman agents — no haulers, no
   shuttles — so the cut could never fire. Treasury: **78 credits.**

## Rent is per-station and unknowable in advance

⭐🔴 **Rent dynamics differ per station and are NOT visible until you build
there.** You cannot price a station before committing. Measured:

| station | service facilities | production facilities |
|---|---:|---:|
| `grand_exchange_station` | 90/cycle | 180–181/cycle |
| `confederacy_central_command` (sol_central) | 31/cycle | 31/cycle |
| `voss_redoubt_station` (pirate stronghold) | 423/cycle (Lockbox) | **846–850/cycle** |

**Stronghold rent is ~4.7x Grand Exchange and ~27x Sol Central**, observed
2026-10-17: three contraband lines at Voss bill 850/cycle each (73,100/day),
a fourth built the same day reported `rent_per_cycle: 846`. There is no rent
arbitrage available for contraband because **contraband facilities cannot be
built in empire space** — lawless/stronghold only, so the high rate is the
price of the category, not a bad siting choice.

At 850/cycle a line needs only ~5-10% utilisation to clear its rent on
production margin (ghost_rounds ~16,377/run breaks even at 4.5 runs/day
against a 68/day ceiling; starshine ~7,107/run at 10.3 against 213;
moonshine ~13,898/run at 5.3 against 106). **Fee-avoidance alone never pays**
— renting the same line from HEXC at Zaniah costs 112-350/run, so the still
would need 653 runs/day against a 213 ceiling. Own it only if it RUNS; an
idle stronghold line is a flat 73,100/day loss.

Rent is **not** proportional to build cost — the 800,000 cr Fuel Bunker and the
200,000 cr Lockbox both bill 90 at Grand Exchange. Level does not matter either
(level-2 Office and Warehouse still bill 90). **Record observed rent at every
build; it is the only chance to learn that station's rate.**

- 1 cycle = 100 ticks ≈ 16.7 min; `est_rent_per_day = total_rent_per_cycle × 86`
- Arrears accrue the **full** bill every cycle the treasury cannot pay
- `grace_cycles` 260 ≈ **72 hours** from first miss to repossession
- Funding the treasury pays arrears down automatically (43,886 → 11,757 observed)

## Build mechanics (learned the hard way)

- **`faction_build` draws materials from faction storage / packages / cargo —
  NEVER from the builder's personal storage.**
- ⭐🔴 **Credits come from the TREASURY first, then the builder's wallet.** An
  earlier reading suggested wallet-only, but that was only because the treasury
  was empty at the time. Measured 2026-10-12 on the `recruitment_desk` upgrade
  (300,000): treasury 199,845 → **0** and the wallet covered the remaining
  ~100,775. **A build silently drains the treasury**, so a facility bought right
  after funding leaves rent unpayable — re-fund immediately after every build. Error:
  `need 100 x steel_plate, have 0 in packages + 50 in faction storage + 0 in cargo`
- The **first** `faction_lockbox` is the bootstrap: it has no
  `requires_service_type`, so it builds before any faction storage exists.
  Everything else (`faction_desk`/`faction_office`, `faction_fuel_bunker`,
  `intel_terminal`) requires `faction_storage` **active**, not merely building
  (`build_failed: your faction storage facility is still under construction`).
- Move materials in with `faction_deposit_items <item> <qty> --source=storage`
  — storage-to-storage, no hull limit. Default source is **cargo**, which fails
  with `insufficient_cargo` when the goods are in storage.
- `faction_office` is level 2 and **upgrades_from `faction_desk`**, so the chain
  is Lockbox → Desk → Office. The Office **expands the per-faction member cap**
  (DB was stuck at 46/50 and could not add marketbots without it).
- Build costs: lockbox 200k/200 steel/50 circuit · desk 100k/100/25 ·
  office 500k/300/150 · warehouse 750k · fuel_bunker 800k · intel_terminal 150k

## The fix: gift the parked leader

⭐🟢 **`send_gift <user> credits <n>` crosses any distance** — the sender only
has to be docked, the recipient can be 14 jumps away and it lands directly in
their **wallet**. Verified: johnny_cab (First Step) → databot (Sol), 1,000,000
cr, instant.

So the durable pattern is **workers gift their faction's parked leader**, and
the leader — permanently docked at the faction home where the Lockbox is —
runs `faction_deposit_credits` locally. This removes the station-local
constraint instead of working around it. Both leaders are already parked
correctly: `craft_boss` at grand_exchange, `databot` at sol_central.
The same primitive funds a broke assist tanker without anyone travelling.

Beware `treasuryRescue`: any member may pull 5,000/hr (`treasuryRescueAmount`,
cooldown 1h, floor 1,000). Across 46 members that is up to 230k/hr against a
116k/**day** bill — rent money needs a ring-fenced floor that member rescues
cannot touch. Not yet built.

Related: [[reference_v0618_visibility_overhaul]] ·
[[project_haul_fleet_rehull_2026_10]] · [[reference_faction_withdraw_pulls_personal_storage]]

## ⭐ `facility faction_owned` is the telemetry — it already exists

Do not build a rent dashboard before running this. It aggregates **every
station** (`faction_list` only covers the one you are docked at) and is the
single call that answers "what do we owe":

```
CRFT 2026-10-12: 14 facilities, 2768/cycle, ~238,048/day
  Grand Exchange (haven)   1351/cycle  116,186/day  10 facilities  labor 41/run
  Voss Redoubt  (alhena)   1417/cycle  121,862/day   4 facilities  labor 40/run
```

**Voss costs more with 4 facilities than Grand Exchange does with 10.** Its
production units bill **405/cycle** against 180 at Grand Exchange, and its
Lockbox 202 against 90. Station rates vary by more than 2x and there is no way
to know before building — a pre-build estimate scaled from Grand Exchange was
less than half the real figure.

## ⭐🔴 Labor is a second, usage-driven treasury charge

> "Production facilities also bill their labor cost to the treasury per run."

`faction_owned` reports it as `labor N/run` per station (CRFT: 81/run total).
This is **not** in any rent aggregate and scales with how much we craft. It
accounted for roughly 29,000 of a 45,437 treasury drop over 28 minutes that
rent and arrears could not explain. Any runway calculation from rent alone is
optimistic. `refuel_ally` in the storage activity log is *not* a credit cost —
the Fuel Bunker holds a fuel reserve in units (`faction_fuel_reserve`).

## ⭐ Repossession at a FOREIGN station transfers the facility

> "at another faction's station a production facility passes to that faction and
> any other facility is removed, with its build materials returned to your
> faction storage there, after the owning faction takes enough of them to cover
> the unpaid rent"

So defaulting at someone else's station **hands them a working factory**, and
they also take build materials to cover arrears. Non-production facilities come
back as materials, minus their cut.

## ⭐🟢 The strategic exit: own the station

> "Facilities at your faction's own station or outpost pay no rent."

Founding a faction station/outpost removes this cost class entirely. Both our
factions report `Bases: 0`. That is the only fix that does not need perpetual
funding, and it is why the faction mobile-base commands exist (currently
declared unused in `command_coverage_test.go`).

## 2026-10-12 outcome

| faction | treasury before | after | rent/day | runway |
|---|---:|---:|---:|---:|
| CRFT | 51 | **10,302,978** | 238,048 | ~43 d |
| Data Bots | 43 | **200,000** | 5,332 | ~37 d |

DB chain rebuilt Lockbox -> Desk -> Office (upgrade needs
`facility upgrade --facility_id=<id> --facility_type=faction_office`, *not*
`faction_build`, which answers `this is an upgrade — use 'upgrade' action`).
Funded by johnny_cab gifting the parked leaders: 1M to databot, 10M to
craft_boss. Watch for `session_replaced` when play_as fights a live worker for
the same credentials — verify a large gift landed before retrying it.

## Member cap is `faction_recruitment`, NOT the Office

⭐ **`faction_cap` means two different things depending on service type.** On
`faction_storage` it is storage units (lockbox 100,000, warehouse 200,000); on
`faction_recruitment` it is the **member cap**. Conflating them led to a wrong
conclusion on 2026-10-12 that no facility raises the cap. It does — there is a
five-tier ladder and **every upgrade raises the limit**:

| L | id | build cost | member cap | materials |
|---|---|---:|---:|---|
| 1 | `hiring_board` | 75,000 | **50** | 75 steel, 25 circuit |
| 2 | `recruitment_desk` | 300,000 | **100** | 200, 100 |
| 3 | `recruitment_center` | 2,000,000 | **200** | 1000, 500, 300 flex_polymer |
| 4 | `guild_hall_recruiting` | 8,000,000 | **400** | 3000, 1500, 800 titanium_alloy |
| 5 | `grand_recruitment_bureau` | 20,000,000 | **1000** | 8000, 5000, 2500 titanium_alloy, 1000 durasteel_plate |

Each upgrades_from the tier below; all require `faction_storage`. This is the
gate on marketbot fleet growth — Data Bots sat at 46/50.

`faction_office` is a separate thing: the **top** of exactly two `faction_admin`
tiers (`faction_desk` L1 100k → `faction_office` L2 500k). It restores the
admin service that invites and role management need, and it did **not** move the
displayed cap (46/50 before and after). Both are needed: admin to invite,
recruitment to have somewhere to put them.

Verified 2026-10-12 after the upgrade completed: arrears cleared on the first
funded cycle (93 → 0), both facilities active, 62/cycle. Note sol_central
reported **grace: 0 cycles** against 260 at Grand Exchange — eviction tolerance
is per-station too, so a single missed cycle there may be terminal.

## ⭐🔴 UNRESOLVED: members_limit does not follow the Recruitment Desk

Built Data Bots' recruitment chain 2026-10-12: `hiring_board` (75k) upgraded to
`recruitment_desk` (300k). The upgrade reply itself returned `"capacity": 100`,
and `facility faction_list` shows it **active, level 2, Capacity 100**.

But `faction_info` still reports **`members_limit: 50`**, re-checked 15 minutes
after it went active. `member_count` 46.

So the effective cap is **unverified**. It may be a lazy server-side cache, or
`members_limit` may be computed from something we have not found. There is no
way to distinguish by experiment until the faction actually reaches 50 members —
below that an invite succeeds either way. Data Bots can certainly add **4** more
right now; whether it can pass 50 is unknown and would be discovered at member
51. Worth a DevTeam question. `members_limit` is a real server field
(`pkg/game/serverapi/responses.go:1503`), not a client constant.
