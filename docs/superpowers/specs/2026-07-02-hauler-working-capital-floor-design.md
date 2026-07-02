# Hauler Working-Capital Floor — Design Spec

**Date:** 2026-07-02
**Status:** Draft
**Related:** `pkg/worker/treasury.go`, `pkg/worker/haul.go`, [pre-buy profit gate](2026-06-25-hauler-prebuy-profit-gate.md), per-death loss capture (planned)

## Problem

A hauler's productivity is gated by **working capital**: `sizeBuy` (`pkg/worker/haul.go:328`) caps each buy at `floor(credits / askEach)`, so a hauler can only fill as much of its hold as its wallet affords. A 325-unit hold needs roughly **100k+ credits** to fill on typical routes; below that it fills a fraction per trip, and `haulGate` (`haul.go:98`) rejects most opportunities as `unaffordable/no cargo`.

The existing treasury rescue (`pkg/worker/treasury.go`) does **not** cover this:

- `treasuryRescueFloor = 1000`, `treasuryRescueAmount = 5000` — it only fires when a worker is essentially broke (< 1k), and 5k is nowhere near a viable fill. A worker sitting at 6k–90k is above the floor (no rescue) yet far below viable (can't trade). It churns buy→travel→buy indefinitely without completing a haul.
- Withdrawals require the `manage_treasury` permission **and** presence at a `faction_storage` facility. **We currently own no faction facility**, so the treasury deposit/withdraw path is entirely non-functional. The 5% profit cut (`TreasuryProfitCut`, `depositProfitShare` at `haul.go:1009`) also silently no-ops.

**Observed instance (trader-8, 2026-07-01):** a ship-loss wiped its capital to 6,838 — above the 1k floor, so no rescue. It completed **2 hauls in 5 days** (last 06-27), churning otherwise. A manual `send_gift` of 100k restored it; within <1 day it completed **6 hauls / +161k profit**, filling its full 325 hold on bulk runs and affording high-margin loads. The 100k was the entire difference between dead and productive.

The dead zone is structural: **5k rescue floor ≪ ~100k working capital**, and the only funding source (treasury) is unreachable. Recovery today requires a manual operator gift.

## Goals

1. Detect an **under-capitalized** hauler (below viable working capital), not just a broke one.
2. **Self-heal** the capital shortfall automatically, without an operator gift and without depending on a faction facility we don't have.
3. Preserve existing guardrails: don't recapitalize mid-haul (in-flight capital), rate-limit, cap total injection per worker, and escalate to a human when a worker is a black hole.
4. Produce an audit trail (donor, recipient, amount, reason) that feeds per-death loss capture.

## Non-Goals

- Building faction facilities or fixing the treasury withdraw path (separate work; this spec routes *around* it).
- Changing the pre-buy profit gate or route ranking.
- Recapitalizing non-hauler roles (miners/shuttles have different capital profiles; out of scope for v1).

## Design Overview

Two layers, because the funding source lives at a different level than the detection:

- **Worker layer** — measures its own capital adequacy and, when it can self-fund (treasury reachable), tops up to a working-capital target instead of a flat 5k. When it *cannot* self-fund, it publishes a structured "under-capitalized, need N" signal and stops burning cycles.
- **Overmind layer (new)** — a fleet **recapitalization coordinator**. The overmind already holds every worker's balance in `fleet-status.json`; it watches for under-capitalized workers and orchestrates a **peer top-up** — instructing a surplus-rich faction member to `send_gift` the shortfall via a one-shot assigned task. This is what makes recovery work with no facility.

### Working-capital floor (detection)

Replace the single absolute `treasuryRescueFloor` with a per-role **working-capital floor**:

```
workingCapitalFloor(role, cargoCap) = max(absoluteFloor[role], k * cargoCap * refAsk)
```

- `absoluteFloor[hauler]` — configurable, default **100_000** (matches the observed viable threshold).
- `k * cargoCap * refAsk` — a dynamic term so bigger holds and pricier regimes scale up. `refAsk` = a rolling median ask across recently-claimed opportunities (available from the arbitrage/market layer); `k` ≈ 1.0 (enough to fill the hold once). If `refAsk` is unavailable, the absolute floor governs.

A worker is **under-capitalized** when `credits < workingCapitalFloor` **and** it is idle/dockable with empty cargo (mirrors the current `maybe()` idle gate at `treasury.go:85` — never trigger on capital that is legitimately tied up in in-flight cargo).

Corroborating signal (optional, stronger): track the last K `haulGate` outcomes; if the buy was affordability-capped (`sizeBuy` limited by credits, not cargo) to < 50% of free hold on a majority, flag under-capitalized regardless of the absolute number. This adapts to price regimes the static floor misses.

### Recapitalization amount

Top up **to** the floor, not by a fixed amount:

```
topUp = clamp(workingCapitalFloor - credits, minTopUp, maxTopUpPerEvent)
```

- `minTopUp` (default 25_000) avoids trickle injections.
- `maxTopUpPerEvent` (default 150_000) bounds a single transfer.

### Funding sources (priority order)

1. **Faction treasury** — `FactionWithdrawCredits(topUp)` when a `faction_storage` facility exists and the worker holds `manage_treasury`. Currently always fails; kept as the preferred path for when facilities land.
2. **Peer transfer (new, overmind-orchestrated)** — the overmind picks a **donor**: a same-faction fleet member whose `credits - topUp >= donorSafetyBuffer` (default 300_000, so donors keep their own working capital plus margin), preferring the richest. It assigns the donor a one-shot task `send_gift <recipient> credits <topUp>`. This is the working path today.

If neither source is available (no facility, no eligible donor), the worker stays flagged and the overmind logs an escalation for the operator.

## Components

### 1. Worker-side (`pkg/worker`)

- `treasury.go`: replace `treasuryRescueFloor`/`treasuryRescueAmount` constants with a `WorkingCapital` config struct (`absoluteFloor`, `k`, `donorSafetyBuffer`, `minTopUp`, `maxTopUpPerEvent`, `cooldown`). Generalize `treasuryRescue.maybe()` to compute `workingCapitalFloor` and top-up amount; attempt treasury withdraw first.
- New: when treasury withdraw fails/unavailable, emit an `under_capitalized` heartbeat/status field: `{ shortfall, floor, cargo_cap }`. Publish through the existing worker→overmind control channel (same path as heartbeats).
- `haul.go`: expose the affordability-capped signal from `sizeBuy`/`haulGate` so the corroborating detector can consume it. No change to gate math.

### 2. Overmind-side (`pkg/overmind`)

- New `recap` coordinator invoked from the supervisor tick (alongside `AssignPending`, `cmd/overmind/main.go:156`). Inputs: the fleet snapshot (per-worker `credits`, `role`, `faction`, `under_capitalized`), donor eligibility, and a per-recipient recapitalization ledger.
- Emits a one-shot assigned task (`control.Assign`) to the donor with script `send_gift <recipient> credits <amount>` (a small parameterized smolt script, e.g. `gift_credits.smolt` with `RECIPIENT`/`AMOUNT` params).
- Ledger (persisted, e.g. `data/overmind/recap-ledger.jsonl`): one row per event `{ts, recipient, donor, amount, source, reason}`. Enforces caps and feeds audit / loss-capture.

### 3. Config

Per-role block in `roles.yaml` (or overmind flags), e.g.:

```yaml
recap:
  hauler:
    working_capital_floor: 100000
    fill_multiplier_k: 1.0
    donor_safety_buffer: 300000
    min_top_up: 25000
    max_top_up_per_event: 150000
    cooldown: 1h
    max_events_per_day: 3       # then escalate to operator instead of re-funding
```

## Guardrails

- **In-flight capital**: only trigger when idle + empty cargo (never mid-haul). Reuses the current idle gate.
- **Cooldown + daily cap** per recipient; after `max_events_per_day` recapitalizations that don't stick, stop funding and log an operator escalation (a chronically-draining worker is a bug or a bad ship, not a funding problem — ties to per-death loss capture).
- **Donor protection**: a donor never drops below `donor_safety_buffer`; the coordinator picks the largest surplus and skips if none qualifies.
- **Same-faction only**: gifts stay within the faction (matches treasury semantics).
- **Idempotency**: one outstanding recap task per recipient at a time; the ledger prevents double-funding across ticks.

## Testing

- `treasury_test.go`: floor computation (absolute vs dynamic term), top-up clamp, idle/in-flight gating, cooldown.
- New `recap_test.go` (overmind): donor selection honoring the safety buffer, richest-first, no eligible donor → escalation, daily cap → escalation, ledger idempotency (no double-fund).
- Regression: with a facility present, treasury withdraw path still used first (mock `FactionWithdrawCredits` success).

## Rollout

1. Land the worker-side floor + `under_capitalized` signal (behavior-neutral until the coordinator exists; treasury withdraw still just fails).
2. Land the overmind `recap` coordinator + `gift_credits.smolt` + ledger, defaulted **off** via config.
3. Enable for the haul fleet with conservative caps; watch the ledger and `haul_results` (a funded worker should resume completing hauls — the trader-8 signature: hold-filling bulk runs + high-margin loads within a day).

## Open Questions

- **`refAsk` source**: is a rolling median ask readily available to the worker at gate time, or should v1 ship the absolute floor only and add the dynamic term later? (Lean: absolute-only v1.)
- **Donor selection fairness**: always-richest concentrates giving on one hauler. Round-robin among eligible donors, or is richest-first fine? (Lean: richest-first; simple, and the safety buffer prevents harm.)
- **Escalation channel**: operator escalation via log only, or also an mbox/notification? (Lean: log + fleet-status flag for v1.)
- Should the same coordinator eventually **replace** the treasury profit-cut entirely, given the facility path may never materialize? (Defer.)
