# Rescue-Fee Reimbursement — Design

**Date:** 2026-07-05
**Status:** Approved, pending implementation plan
**Related:** [2026-07-05 dynamic rescue-fuel sizing](2026-07-05-rescue-fuel-sizing-design.md) — this covers the *credit* side of the same rescue loop.

## Problem

Assist-fleet rescuers give stranded workers fuel via ship-to-ship transfer
(`refuel target=<player>`, drawn from the rescuer's own tank). The rescuer then
flies home and **re-tanks at its station for credits** (~1 cr/fuel). Nothing
reimburses that spend, so rescuers bleed credits and go broke — live right now,
`assist-nexus` sits at **3 credits** and `assist-frontier` at ~1,700 — while the
haulers they rescue hold **1–2.5M credits each**. The economic loop is
one-directional and unsustainable.

## Goal

After a real fuel rescue, the rescued worker reimburses its rescuer with a flat
credit gift, once it is next docked. Credits flow hauler → assister, building the
assisters a buffer.

## Mechanism constraints (verified against the server API)

- `send_gift` with `credits` requires the **sender** to be **docked at a base
  with storage service**. The **recipient needs neither co-location nor to be
  online** — delivery is async to their wallet.
- The assister's in-game username is `shipside_assist_<home>`
  (haven/sol/krynn/frontier/nexus), readable via
  `rescue.ResolveUsername(agentsDir, agentID)`.
- `SendGift(ctx, payload)` is already on the `game.GameClient` interface
  (`pkg/game/interface.go:193`); payload `{recipient, credits, message}`.
- The rescue record already carries `ClaimedBy` (the rescuing assist agent id)
  and `RescueFuel` (the amount transferred).

## Design

### 1. Debt record — overmind side

The rescued worker is offline (quarantined) during the rescue, so it cannot pay
at transfer time. The overmind — which gates the strandee's relaunch — records
the debt so the relaunched worker can find and pay it.

In `cmd/overmind/rescueops.go` `pollRescues`, when a record transitions to
`done`, **before** archiving it, apply the debt gate and write:

- **Gate:** only a real assister rescue owes a fee — `rec.ClaimedBy != ""`
  **and** `rec.RescueFuel > 0`. This excludes server-tow recoveries,
  operator-manual done-flips, and skip-and-release (qty 0) — none of which spent
  an assister's fuel.
- **Recipient:** `rescue.ResolveUsername(agentsDir, rec.ClaimedBy)` →
  `shipside_assist_<home>`. If resolution fails, log and skip the debt (never
  block the rejoin).
- **Amount:** a new overmind flag `--rescue-fee` (default **1000**), stamped
  into the debt so the fee is fixed at rescue time.
- **Write:** append `{recipient, credits}` to
  `data/agents/<rec.AgentID>/rescue-debts.json` via a new pure helper
  `rescue.AppendDebt(agentsDir, strandeeID, Debt)`.

`pollRescues` gains two parameters: `agentsDir` (default `"data/agents"`, matching
`makeOnQuarantine`) and `fee int`. `main.go` passes the flag value.

### 2. Debt payment — worker side

A per-pass, best-effort hook mirroring the treasury deposit pattern
(`pkg/worker/treasury.go`):

```
payRescueDebt(ctx, c giftClient, out io.Writer, agentDir string)
```

- `giftClient` is a narrow interface — `GetState() *game.State` and
  `SendGift(ctx, payload) error` — for unit-testability.
- Load `data/agents/<id>/rescue-debts.json`. If empty → no-op.
- If the worker is **not docked** (`state.Doc == false`) → skip this pass and
  retry next (haulers dock frequently; no urgency).
- Otherwise take the **head** debt, `SendGift({recipient, credits, message:
  "rescue fuel reimbursement"})`. On success, `rescue.RemoveHead` and rewrite the
  file. One debt per pass respects the 1-gift-per-tick rate limit and handles the
  rare multi-rescue case without losing debts. On error, log and leave the debt
  in place for next pass.

Wired into the shared per-pass path in `pkg/worker/dispatch.go` (where treasury
and shuttle state are already threaded), so **any** rescued mobile worker pays —
not only haulers (an assister rescued by another assister owes a fee too).

### 3. Debt file

`data/agents/<id>/rescue-debts.json` — a JSON array of `{recipient, credits}`.
Overmind appends; worker pops the head on each docked pass. Missing file = no
debts. New `pkg/rescue` helpers: `AppendDebt(agentsDir, strandeeID, Debt)`,
`LoadDebts(agentsDir, strandeeID) ([]Debt, error)`, and `RemoveHead(agentsDir,
strandeeID) error` (drops the first entry and rewrites, removing the file when it
empties).

## Decisions

- Flat fee, default **1000**, configurable via `--rescue-fee`.
- **Always pay** — no solvency gate (haulers hold 0.5–2.5M in practice).
- Only real assister rescues create debts (the `ClaimedBy != "" && RescueFuel >
  0` gate).
- Sender must be docked (gated on `state.Doc`); recipient async.
- Multiple outstanding debts are a list, paid one-per-pass.

## Out of scope

- Reconciling the fee to the *exact* re-tank cost — a flat fee that over-covers
  is intentional (builds the assisters a buffer).
- Faction-treasury funding of assisters (a separate, existing mechanism;
  assisters are only partly factioned).
- Any change to the fuel-sizing logic shipped separately today.

## Testing

- **`pkg/rescue`** table tests for `AppendDebt` / `LoadDebts` / `RemoveHead`:
  append accumulates; head removal pops the first; missing file → empty; the
  file round-trips.
- **`cmd/overmind/rescueops_test.go`:** `pollRescues` writes a debt when
  `ClaimedBy != "" && RescueFuel > 0`, and writes **none** when either is
  absent (tow / operator / skip-release). Assert the debt file contents and the
  resolved recipient.
- **`pkg/worker`** (`payRescueDebt`): docked + one debt → `SendGift` called with
  `{recipient, credits}` and the debt removed; not docked → no `SendGift`, debt
  retained; empty file → no-op; `SendGift` error → debt retained. Fake
  `giftClient`.

## Files touched

- `pkg/rescue/` — new `debt.go` (`Debt`, `AppendDebt`, `LoadDebts`, `RemoveHead`)
  + test.
- `cmd/overmind/rescueops.go` — `pollRescues` gains `agentsDir`, `fee`; writes
  the debt on the real-rescue gate before archiving.
- `cmd/overmind/main.go` — `--rescue-fee` flag (default 1000); pass it +
  agentsDir into `pollRescues`.
- `cmd/overmind/rescueops_test.go` — update `pollRescues` call sites; add
  debt-gate tests.
- `pkg/worker/rescue_fee.go` (new) — `giftClient`, `payRescueDebt` + test.
- `pkg/worker/dispatch.go` — call `payRescueDebt` on the shared per-pass path.
