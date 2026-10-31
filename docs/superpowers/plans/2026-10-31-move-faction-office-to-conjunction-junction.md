# Move the Faction Office (and Hiring Board) from Haven to Conjunction Junction

**Status:** approved by the operator 2026-10-31; the operator carries out the
dismantle and rebuild.

## Why

- **Speed.** A faction admin facility speeds up the faction's own production
  facilities at its station by +15% per tier. There are only two tiers:
  Faction Desk (T1) and Faction Office (T2), so +30% at most.
- **Haven today.** Haven has CRFT's Faction Office (T2). Its combustor runs at
  0.46 ticks/run (78,000 fuel/hr); Conjunction Junction's identical L1
  combustor runs at 0.60 ticks/run (60,000/hr). That gap is exactly 1.30×.
- **Where the production is.** Conjunction Junction now holds almost all of
  CRFT's production. With the office there, every faction production facility
  at Conjunction Junction runs +30% faster:

  | Line | Now | With the office |
  |---|---|---|
  | Steel | ~160,000/day | ~208,000/day |
  | Copper piping | ~96,000/day | ~125,000/day |
  | Fuel | 60,000/hr | 78,000/hr |
  | Platinum control nodes | 200/hr | 260/hr |

- **Rent.** Haven rent drops by 14,964 cr/day (the Office). Conjunction
  Junction charges CRFT no rent.
- **Customization.** It is unlocked galaxy-wide by a single admin facility, so
  it carries over to the new one.

**Optional:** move the Hiring Board too, for +5% output per recruitment level
and 2,236 cr/day of Haven rent. How its output bonus applies is still to be
confirmed.

## Steps

1. At Grand Exchange (Haven):
   - `facility faction_dismantle d5b180bb72022c2e496fa27b20f865b6` (Faction
     Office, L2)
   - optionally `facility faction_dismantle 8ead4fd3ed92090c61501ee854de519e`
     (Hiring Board)
   - Dismantling returns 100% of build materials as packages.
2. Haul the packages to Conjunction Junction. Record their ids, as for the
   iron refinery (`data/facility-packages/<type>.json`).
3. At Conjunction Junction:
   - `facility faction_build faction_office --package_ids @data/facility-packages/faction_office.json`
     (and the same for `hiring_board`)
   - An L2 facility may need its L1 base built first and then upgraded. If the
     L2 build from packages is refused, check that.
4. Verify on `facility faction_list` at Conjunction Junction: the Copper Drawing
   Mill should show 0.28 ticks/run (was 0.36) and the Iron Refinery about
   0.083 (was 0.11).

## Effect on Haven

Haven's remaining production (its combustor) loses the bonus. Haven is now
mostly storage (Fuel Bunker, Warehouse, Lockbox), so the loss is small.
