---
name: reference_citizenship_mechanics
description: "Citizenship is separate from origin and multi-valued; crimson is EXCLUSIVE and outerrim is NOT, so migrating needs an explicit renounce or you get taxed by both"
metadata:
  type: reference
---

`player.empire` (and `agent_profile.empire`) is **ORIGIN**, not citizenship.
Origin is fixed at character creation, gates origin-restricted skills and ship
classes, and **cannot be changed by anything**. Citizenship is a *separate,
mutable, multi-valued* membership. Counting `agent_profile.empire` tells you
birthrights, NOT who taxes whom.

**Income and property tax are assessed by every empire you hold citizenship
in.**

## ⭐🔴 CORRECTION 2026-09-14: STATELESS OWES NO PERSONAL INCOME OR PROPERTY TAX

This file previously said holding zero citizenships "does NOT mean paying
nothing". **That was wrong.** The official guide
(https://spacemolt.com/docs/guides/taxes) states plainly:

> "Stateless characters owe no personal income or property tax."

Which is also the plain reading of the live `citizenship list` rules text
already quoted below — no empires hold you, so no empire assesses you. Only
**sales tax** still applies to the stateless, at each empire's third (worst)
rate, paid per transaction rather than weekly.

**Strategic consequence:** renouncing is not a step on the way to another
citizenship, it is a destination. An agent that mostly holds hulls and rarely
sells pays *less* stateless than under any empire. We carry ~170 idle hulls
being property-taxed weekly for producing nothing.

**Property tax has NO foreign-tax credit** — "each citizenship empire assesses
the full fleet value independently". Income tax does get credits between
assessments; property does not. So a second citizenship *doubles* the property
bill outright. This makes the exclusivity asymmetry below worse than recorded:
`apply outerrim` while holding crimson means paying BOTH fleets' worth of
property tax until the renounce lands.

## The command
`citizenship` with `action` = list | apply | renounce | withdraw.
- **`list` is a QUERY** — free, no tick, no empire_id. It returns origin,
  citizenships, pending_petitions, recent_decisions, `rules`, and a per-empire
  policy summary (open / exclusive / auto_approve / fee / min_balance /
  min_reputation / your_reputation / eligible / ineligible_reason).
  The reply carries **no `action` field**.
- `apply` debits the fee to escrow immediately; needs `credits >= min_balance +
  fee` AND `reputation >= min_reputation` AND `open`. auto_approve grants on the
  spot; otherwise it queues for manual empire review. Refunded on reject/withdraw,
  kept on grant. One pending application per empire.
- `renounce` is permanent, refunds nothing, leaves origin unchanged.
- `petition` is a DIFFERENT command — free-text mail to empire leadership,
  1/empire/hour. It grants nothing. Do not confuse the two.

## ⭐🔴 The exclusivity asymmetry is the whole migration plan
A grant in an **exclusive** empire auto-renounces every other citizenship you
hold (checked at grant time only).

| empire | fee | min_balance | min_rep | auto | exclusive |
|---|---|---|---|---|---|
| solarian | 5,000 | 25,000 | 40 | no | no |
| voidborn | **0** | **0** | **0** | **YES** | no |
| crimson | 10,000 | 50,000 | 50 | no | **YES** |
| nebula | 25,000 | 1,000,000 | 0 | no | no |
| **outerrim** | **0** | **0** | **0** | no | **no** |

- **Outer Rim gates on NOTHING** — a 0-credit agent can apply. There is no
  "fund them before they can petition" dependency.
- **Outer Rim is NOT exclusive**, so an apply alone leaves crimson in place and
  BOTH empires assess you — strictly worse. The migration is
  `apply outerrim` → wait for manual review → `renounce crimson`.
  Ordering matters: renounce-first means stateless for an unknown review period.
- **Crimson IS exclusive** — a crimson citizen holds only crimson.
- **voidborn is the only auto-approve empire, free and unrestricted** — the one
  instant, zero-gate citizenship available. Worth pricing against outerrim.

## ⭐🟢 RESOLVED 2026-09-13: the rules text no longer hedges

It previously said citizenship "**will later** gate features such as taxation",
contradicting the command description. Live `citizenship list` now reads:

> "Citizenship decides taxation today: an empire charges its own citizens one
> sales-tax rate, citizens of other empires another, and the stateless a third.
> Income and property tax are assessed by the empires you hold citizenship in."

The "confirm before migrating the fleet" caveat is discharged — **citizenship is
the tax lever, today.** (The later-gating language now applies only to listing
fees, facility eligibility and ship/goods access.)

## Client gotcha (fixed `add77a85`)
The command is flagged `x-is-mutation`, so `Client.Citizenship` waited for an
action frame on every action — `list` hung for the full 30s AND held the
`citizenship` action lock, so the next call failed with a lock error. `list`
now terminates on the ack. Driven from `play_as`: `citizenship [action] [empire]`.

## Live test subject: explorer-7 — STILL PENDING AFTER 25 DAYS

`Nova 'Navigator' Nash`, crimson origin, back in mission-learn (GSC-0019).
Petition `afe037f4ca853808bea8bfcb86ff988f` to **outerrim**, filed
2026-08-19T00:38:00Z. Checked 2026-09-13: **status still `pending`,
`recent_decisions: null`, fee_paid 0, reputation 10.**

Outer Rim is not auto-approve and its queue appears unattended — 25 days, no
decisions of any kind. **Operator filed a bug 2026-09-13 to see if review gets
restarted; we are WAITING, not switching.** Nothing is bleeding: the fee is 0,
so a stalled petition costs only the opportunity.

Meanwhile explorer-7 remains crimson-only, i.e. on the most expensive rates in
the game (10% income, 1.0% property).

**Reconsider the whole plan given the stateless correction above.** Three
options, not two:
1. wait for outerrim → then renounce crimson (1.5% / 0.25%)
2. voidborn (auto-approve, instant) → then renounce crimson
3. **`renounce crimson` and stop there** — zero personal income and property
   tax, only per-transaction sales tax at the stateless rate

For an agent that flies and holds hulls rather than trading, (3) is cheapest
and needs no one's approval. Price the stateless sales-tax rate against the
citizen rate before committing for market-facing agents — crimson citizens pay
**0.00%** sales tax at crimson stations (seen live on explorer-8), so a heavy
seller inside its own empire can be better off a citizen.

⭐ Check it with `citizenship action=list` — a free query, no tick. On a live
fleet worker, SIGSTOP the worker first or the session is replaced mid-command.
[[reference_tax_bounties_and_rates]] · [[reference_empire_field_semantics]]
