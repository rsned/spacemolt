# Voss contraband line: void dust and nerve burn

**Status:** proposed, awaiting operator approval (2026-10-31)
**Why now:** server v0.656.0 replaced demand-priced rent with a flat 3% of
build cost per day, doubled at pirate stations. An L1 contraband lab at Voss
Redoubt drops from roughly 400,000 cr/day to 6,000–8,500 cr/day, so a
production line there can pay for itself again.

## Goal

Turn stock we already hold (chlorine gas, nebula gas, plasma gas) into the two
contraband goods with the deepest standing demand, void dust and nerve burn,
and sell them slowly enough not to repeat the moonshine collapse.

## Market (bids captured 2026-10-30/31)

| Output | Best bid | Bids within 50% of best | Standing value |
|---|---|---|---|
| void_dust | 7,970 | 1,018 units at 27 stations | ~7.9M |
| nerve_burn | 8,135 | 1,056 units at 24 stations | ~8.3M |
| gutter_flux (intermediate) | 2,023 | 9 units | not a product |
| red_mist / ghost_rounds | 2,000 / 947 | 157 / 491 | 0.3M / 0.4M, later |
| starshine | 2,322 | 887 | needs energy crystals (RESERVED), excluded |

The bids are a one-time depth; the refill rate is unknown. That is the main risk.

## The chain

| Stage | Recipe | Facility (L1) | Where | Rent/day |
|---|---|---|---|---|
| 1 | 8 chlorine_gas + 1 flex_polymer → 3 chlorine_compound | Chlorine Neutralization Vat | Conjunction Junction (not pirate-only; CRFT pays 0) | 0 |
| 2 | 8 chlorine_compound + 12 chlorine_gas → 8 gutter_flux | Backstreet Chem Lab | Voss Redoubt (`pirate_base_only`) | ~7,440 |
| 3a | 2 gutter_flux + 3 nebula_gas → 1 void_dust | Sealed Dust Lab | Voss Redoubt | ~6,960 |
| 3b | 2 gutter_flux + 3 plasma_gas → 1 nerve_burn | Stim Synthesis Lab | Voss Redoubt | ~7,020 |

Voss rent ~21,400/day plus labour (15–20 cr/run). One void dust or nerve burn
costs ~2 compound + ~3 chlorine gas + 3 nebula/plasma gas.

## Inputs on hand

| Item | Owned | Notes |
|---|---|---|
| chlorine_gas | 94,768 | enough for thousands of units |
| chlorine_compound | 496 | enough to start Phase 1 without the Vat (→ 496 flux → ~248 product) |
| nebula_gas | 128,868 | Maia miners (0020–0022) deliver to Conjunction Junction |
| plasma_gas | 125,336 | miningbot_0023 mines it |
| flex_polymer | 13,264 | Grand Exchange: CRFT 1,940 in faction storage |

## Build materials

| Item | Need (3 labs + Vat) | Owned | Where it is |
|---|---|---|---|
| steel_plate | 8,250 | 353,525 | CJ 11,038 (the fuel tank expansion also needs 8,000); fighter-2 112k at Cargo Lanes |
| copper_piping | 4,150 | 101,997 | **fighter-2 44,916 at Cargo Lanes**; CJ 0, Voss 50 |
| coolant_loop | 750 | 1,438 | craftsman-1 706 at Grand Exchange |
| air_recycler | 650 | **359** | **short 291**; LHS 1140 sells 10,000 at 32 (~9,600 for 300) |
| sensor_array | 250 | 1,633 | craftsman-1 500 at Sol |
| heat_sink | 300 | 1,420 | craftsman-1 500 at Sol |
| control_node | 300 (Vat) | 1,142 | CJ 613 |

Lab materials must be in CRFT faction storage at Voss (the lockbox holds
100,000). The builder is a CRFT member docked there with Officer rank or above.

## Phases (each needs operator approval)

**Phase 0: gather materials.**
- Move 3,150 copper piping, ~5,700 steel, 750 coolant loops, 650 air recyclers,
  250 sensor arrays and 300 heat sinks into CRFT storage at Voss.
- miner-2 buys ~300 air recyclers at LHS 1140 (~9,600).
- Move the 496 chlorine compound to Voss.
- Cost: ~10k plus hauling.

**Phase 1: Voss labs on existing compound.**
- Build the Chem Lab, Dust Lab and Stim Lab: 357,000 from the treasury.
- craftsman-8 and craftsman-10 (Voss crafters, already unlocked) run
  `craft … prefer_own` from faction storage.
- The output of 496 compound (~248 void dust or nerve burn, worth about
  1.5–2M at current bids) proves the line before we spend more.

**Phase 2: compound at home.**
- Build the Chlorine Neutralization Vat at Conjunction Junction: 116,000 plus
  materials.
- miningbot_0000 runs it hourly.
- A hauler carries compound, nebula gas and plasma gas to Voss. The trader-10 /
  trader-9 Voss routes are the natural fit.

**Phase 3: selling.**
- A shopkeep at Voss (marketbot_alhena already runs one) lists at a floor near
  50% of the best bid.
- Haulers sell into the higher bids elsewhere in small lots.
- Watch the realised price per day. Pause production if it falls toward the
  production cost, as moonshine did.

## Treasury

- About 1.0M after the 10-31 top-up, against a 1M floor.
- Phase 1 plus Phase 2 is ~473k of builds, so it needs a the_banker top-up
  first, sized at approval time.
- Running cost is ~21,400/day of rent plus labour, set against Haven's
  ~72,756/day.

## Success measures

- Realised sale price stays above ~50% of today's best bid.
- Daily revenue is at least 3× the line's rent.
- No stockpile beyond about two weeks of observed sales.

## Open questions for the operator

1. Fund Phases 1 and 2 together, or Phase 1 alone first?
2. Split the line between void dust and nerve burn, or lead with one?
3. Is the Voss shopkeep floor (50% of best bid) right?
