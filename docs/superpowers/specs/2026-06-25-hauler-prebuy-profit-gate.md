# Hauler Pre-Buy Profit Gate — Design

**Date:** 2026-06-25
**Status:** approved (scope: gate only; selection scoring deferred)
**Builds on:** `2026-06-24-overmind-hauler-role-design.md` (Market Intelligence Phase 5)

## Problem

The first live hauler runs (2026-06-24) executed a **losing trade**: trader-3 bought
21× Processing Core @ 2654 (55,734cr) and sold @ 2631 (55,251cr) — a ~483cr loss on
an opportunity the scanner rated profitable. Root causes:

1. **No live re-pricing.** `runClaimedHaul` sized and bought against the snapshot
   `opp.BuyPrice` baked into the arbitrage row at scan time, and never re-checked
   whether the spread still cleared by the time the hauler arrived. (The spec's
   documented v1 simplification — "live re-pricing is a deferred refinement.")
2. **Stale market data.** Market captures were on the MVP hourly cadence (and the
   marketbots were not all logged in during the test), so the scanned spread was old.
3. **Distance amplifies drift.** An 8-jump reposition gives prices minutes-to-an-hour
   to move adversely between scan and execution.

## Decision

Add a **pre-buy profitability gate** in `runClaimedHaul`, executed after the hauler
arrives at the buy station and before it buys:

1. **Dock, then live buy-side recapture (cadence-independent).** Autopilot leaves the
   ship at the station POI but *undocked*; `view_market` — unlike buy/sell — does not
   auto-dock, so the hauler `Dock()`s first (a station whose POI has no base leaves the
   row claimed). Docked, it does `view_market` + `CaptureFromClient` to write a
   real-time snapshot for that station — independent of marketbot cadence. The recapture
   is injected as a nil-safe `HaulDeps.RecaptureBuyMarket` func (tests pass nil and seed
   prices). Without the explicit dock the recapture silently fails and the gate falls
   back to stale prices (observed live 2026-06-25: passed phantom opportunities for
   items no longer sold, then the real buy failed `item_not_available`).
2. **Re-price both legs from freshest data.** Read `GetItemStationPrices(itemID)` and
   take the buy station's live `BestAsk` and the sell station's latest `BestBid`.
   (Sell side still relies on the latest capture — the hauler isn't there yet — which
   freshens as the capture dial tightens from hourly toward 15 min.)
3. **Dual threshold.** Size the buy on the live ask, then require the live spread to
   clear **BOTH** a percentage margin and an absolute net floor:
   - `margin = (sellBid − liveAsk) / liveAsk ≥ 0.03`
   - `net    = (sellBid − liveAsk) × qty   ≥ 1000`
   If either fails (or a price is missing, or the buy is unaffordable), **skip the buy
   and leave the row claimed** — same swallow-and-idle contract as the other
   leave-claimed paths. Reclaiming stale/locked rows is the deferred Phase-5b sweeper's
   job (market team), unchanged here.

The gate is the **safety net** that stops stale-data losses regardless of cadence; it
is not a substitute for the freshness dial.

## Out of scope (deferred to a later spec)

- **Selection scoring:** net $/jump (so several short hauls can beat one long one) and
  a decaying-freshness weight on opportunities. Selection stays today's
  profit-dominant + proximity tiebreak.
- **On-demand marketbot refresh:** overmind→`marketbot_<system>` targeted
  `update_market` before committing (Phase-2b assign_task is the hook).
- **Scanner-side scoring:** baking jump/fuel/freshness into the opportunity score.

## Constants

- `haulMinMargin = 0.03`
- `haulMinNetProfit = 1000.0`

## Components

- `haulGate(opp, prices, cargoFree, credits) (qty, liveAsk, sellBid, ok, reason)` —
  pure, unit-tested decision function.
- `HaulDeps.RecaptureBuyMarket func(ctx) error` — nil-safe live buy-side capture.
- `OpportunityStore.GetItemStationPrices` — added to the interface (already on
  `*market.Collector`).
- dispatch `case "haul"` wires `RecaptureBuyMarket` to `ViewMarket` + `CaptureFromClient`.
