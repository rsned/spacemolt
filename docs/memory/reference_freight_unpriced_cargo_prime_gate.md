---
name: reference_freight_unpriced_cargo_prime_gate
description: Freight cargo the market cannot appraise posts as risk_band=unpriced and ONLY prime-tier carriers can see or accept it
metadata: 
  node_type: memory
  type: reference
  originSessionId: db74e972-dd15-4cd6-9408-f974d4fa7975
  modified: 2026-08-09T11:03:15.224Z
---

Thin-market goods post as **`risk_band: "unpriced"`**, and unpriced freight is
visible only to **prime-tier carriers**. Everyone else gets an empty board with
`empty_reason_code: "no_eligible_shipments"`.

**The causal chain, confirmed live 2026-08-09** with a `shield_recharger_ii`
contract at `grand_exchange_station` (Haven → `frontier_station`, 11 hops):

1. the item has no completed-fill VWAP → the shipping house cannot appraise it
2. → `insurable: false`, with the reason stated verbatim on the contract:
   `uninsurable_reason: "shield_recharger_ii cannot be appraised from useful completed-fill VWAP"`
3. → `risk_band: "unpriced"`
4. → prime-only

**A/B proof, same contract, same station:**

| agent | tier | result |
|---|---|---|
| explorer-8 | probationary | `shipments: []`, `total: 0`, *"You cannot accept any freight posted here right now: unpriced cargo requires prime carrier standing."* |
| fighter-4 | prime | the contract, with **`"eligible": true`** |

This is a property of the MARKET, not of the goods — anything you craft that
does not trade will post this way. So a crafted-goods freight route needs a
prime carrier or it cannot move at all.

`reserved_exposure` (1,000,000 on this contract) is NOT the operative gate, and
guessing that it was is a mistake I actually made. Prime carries
`liability_unlimited: true` and `active_contracts_unlimited: true`, so exposure
never binds for a prime carrier — the tier check is what the server reports and
what the A/B isolates. Read `empty_reason`; it names the real rule.

**Accept does NOT load your hold.** `ShippingAccept`'s own doc comment is the
authority: *"The package lands in the carrier's storage at origin."* Flying off
straight after accepting carries nothing and arrives at a `deliver` that cannot
settle — `withdraw_items` at the origin station first.

## ⭐ Freight pays IN FULL — it is immune to the treasury collapse

Run completed 2026-08-09, same day the hunt chain paid **266cr** on a mission:
`base_reward` 10,000 → **`carrier_payout` 10,000**, measured as an exact
+10,000 credit delta on fighter-4 (6,272,637 → 6,282,637).

**Freight rewards come from the SHIPPER'S ESCROW, not the empire treasury.**
`reward_escrow: 10000` is posted up front and drops to `0` at settlement. So
the ~37% realized-ratio collapse in [[project_empire_treasury_payout_collapse]]
does **not** apply to freight — mission credits are broken, freight credits are
not. This makes freight the reliable credit path while the treasury is broke,
and is a concrete argument for [[project_fleet_role_interchangeability]]: a
fleet that can rotate into freight can earn full value when missions cannot.

Settlement shape on success: `status: "delivered"`,
`terminal_reason: "delivered_intact"`, `settled_at` set, `reward_escrow: 0`,
and the beacon fingerprint flips to
`player_storage:<dest_base>:player:<recipient_id>` — i.e. **the goods land in
the RECIPIENT's storage at the destination**, not the carrier's. Delivered in
101 ticks against a 1290-tick target and a 2520-tick deadline.
`max_speed_bonus: 0` here, so beating the target paid nothing extra.

See [[reference_shipping_no_active_contracts_listing]] and
[[project_agent_capability_ledger_storage_faction]].
