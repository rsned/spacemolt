---
name: project_consignment_haul_relay
description: "Gift-sourced hauling: ConsignRule (send side, SHIPPED) + consigned arbitrage routes (engine, SHIPPED, inert). The generator is the open piece; claim must precede the gift."
metadata:
  type: project
---

Gift-sourced hauling, started 2026-10-17. **Two halves shipped, the generator
is the remaining piece.**

## The mechanic that makes it work

`send_gift` lands in the RECIPIENT's storage at the **sending** station and
waits there for them. Only the sender must be docked where the goods are, so a
pinned collector can consign to a hauler days away — no rendezvous.
See [[reference_faction_capture_remote_vs_docked]].

## SHIPPED: send side — `ConsignRule` (`1b655b42`)

`pkg/worker/consign.go`, run from the standing **idle pass** (beside PayDebts
and Handoffs), not the schedule — schedule seeding is ADDITIVE ONLY so a
seeded command fires forever, while an idle-pass step is switched by the fleet
yaml and is reversible. Fleet yaml: `consign: ["item:recipient:min[:keep]"]`,
forwarded as `--consign`.

Explicit ALLOW-LIST, never patterns: gifting is irreversible and the drone
project's reserved energy crystals and a ship's fuel cells share that storage.
`keep` holds a reserve; `min` batches (min<1 rejected). Recipient is an AGENT
ID resolved via `UsernameFor` — ids are stable, usernames rot. Reads personal
storage at the docked station only, never cargo.

**Still useful and kept** even though claim-driven is better for path 1,
because path 2's destination agent IS fixed.

## SHIPPED: engine side — consigned routes (`08439d32`), INERT

`market.ActionWithdrawThenSell` + `ArbitrageOpportunity.IsConsigned()`.
haulGate routes past the ask gate; `sizeConsigned` caps by the hold alone, so
a BROKE hauler can run one. Load branches to `WithdrawItems` instead of `Buy`.
Everything downstream unchanged.

**Why not just buy_price = 0:** `sizeBuy` returns 0 for any ask <= 0 then
divides credits by it, and haulGate reports "no live ask at buy station" — a
reason the run treats as proof the source book is gone, so it calls
`abandonCollapsed` and expires the route AND its sibling destinations. A
synthetic zero-price row poisons the pool rather than failing.

A failed withdraw is `releaseBookAnd`, not `abandonCollapsed`, so the arrival
race costs a wasted pass and not the route.

## OPEN: the generator, and the claim-before-gift ordering

The recipient cannot be known until a hauler claims, and the goods cannot be
gifted until the recipient is known. The existing claim machinery resolves it:

```
1. collector creates a consigned opportunity, goods still in ITS OWN storage,
   from_station = where it sits, status = available
2. a hauler claims it            -> ClaimedBy = <agent>
3. collector's next idle pass reads ClaimedBy on its own offers, send_gifts
4. hauler arrives, withdraws, flies, sells, settles
```

Claim-to-gift is one idle pass (seconds); gift-to-arrival is a journey
(minutes), so the gift normally lands first. The exception is a hauler already
docked at the collector when it claims.

**Unverified before building:** opportunity rows are currently produced by the
scanner only. Check the claim/expiry machinery does not assume scanner
provenance before having workers inject rows.

## Path 2, not started: haul + re-gift relay

Agent K (System W) -> hauler H -> Agent D (System Z), for crafting logistics.
Needs a PERSISTED record (a hauler mid-relay must survive restart), must claim
the hull against arbitrage, and delivery is not confirmable from our side —
there is no shipping-style active-contracts listing and the post-gift state
read is unreliable. Likely home is the existing handoff queue
(`deps.Handoffs` / `HandoffPass`), which already has queue semantics for
"fulfill pending stock-handoff records" — but read what it carries before
overloading it.

## First real use case

marketbot_alhena is pinned at `voss_redoubt` with drones mining
adamantite_ore. Factory Belt is **5 jumps** from Alhena and bids **1,600**
(2x base 800) — the same price every buyer offers, so proximity is the only
discriminator. A 1,900-cargo congregation carries 950 units (size 2) =
~1.52M/run.

Related: [[project_haul_fleet_rehull_2026_10]] ·
[[reference_book_depth_is_the_real_haul_ceiling]]
