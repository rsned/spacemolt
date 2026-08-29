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
in.** Holding zero ("stateless") is legal and does NOT mean paying nothing.

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

⚠ The server's own `rules` text says citizenship "**will later** gate features
such as taxation" (future tense) while the command description says it decides
taxation today. **Confirm on one agent before migrating the fleet.**

## Client gotcha (fixed `add77a85`)
The command is flagged `x-is-mutation`, so `Client.Citizenship` waited for an
action frame on every action — `list` hung for the full 30s AND held the
`citizenship` action lock, so the next call failed with a lock error. `list`
now terminates on the ack. Driven from `play_as`: `citizenship [action] [empire]`.

Live test subject: **explorer-7** (crimson origin, held out of mission-learn),
petition to outerrim filed 2026-08-19T00:38Z, id `afe037f4…`, status pending.
[[reference_tax_bounties_and_rates]] · [[reference_empire_field_semantics]]
