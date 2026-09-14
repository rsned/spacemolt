---
name: reference_tax_bounties_and_rates
description: "⭐ A 'bounty' is UNPAID TAX, not crime — outstanding_bounty equals the unpaid shortfall exactly; weekly Sunday levy; Crimson Pact charges 10% income / 1% property, 4x the Outer Rim"
metadata:
  type: reference
---

**Answered 2026-08-18 from the action log** (`tax.income_paid` / `tax.property_paid`,
the undocumented `tax.*` category — [[project_action_log_capture]]).

## ⭐🔴 A bounty IS the unpaid tax shortfall
When an agent cannot pay a levy, the server takes what it can and the remainder
becomes an **outstanding bounty with that empire** — detention in their territory
until paid. **Nothing to do with combat**; unarmed explorers and idle marketbots
carry them. Verified exact matches (bounty == last `unpaid`):

| agent | empire | unpaid | bounty |
|---|---|---|---|
| engineer-8 | crimson | 1499 | **1499** |
| explorer-7 | crimson | 1101 | **1101** |
| explorer-11 | solarian | 1016 | **1016** |
| fighter-1 | nebula | 310 | **310** |
| miner-3 | solarian | 77 | **77** |
| random-npc | nebula | 1 | **1** |

Agents with a bounty ABOVE their last shortfall (pirate-4 3,491, explorer-8 3,362,
pirate-2 3,297) missed **multiple** levies — it accumulates. explorer-7's went
5,706 → 1,101 as partial payments were skimmed, so **bounties shrink as income is
seized but never clear on their own.** An agent at 0 credits therefore cannot
escape: [[reference_agent_bounties_not_combat]].

Payload shape: `{"empire":"crimson","owed":"2256","paid":"1155","unpaid":"1101","value":"225679"}`
— note the amount field is **`paid`/`owed`/`unpaid`, NOT `amount`** (summing
`amount` silently yields 0). `faction.station_revenue` is the one that uses
`amount`, with `reason` (e.g. `refuel`) and `payer_id`.

## ⭐ Tax rates differ 4x BY EMPIRE — Crimson Pact is the worst
Measured over 619 levies:

| empire | income tax | property tax |
|---|---|---|
| **crimson** | **9.99%** | **0.99%** |
| voidborn | 5.99% | 0.75% |
| solarian | 5.74% | 0.49% |
| nebula | 2.82% | 0.24% |
| outerrim | 1.44% | 0.24% |

Every large bounty in the fleet is **crimson**. Property tax is charged on ASSET
VALUE, so **a docked, idle agent still owes ~1% of its ship's value every week** —
which is why marketbots that "aren't spending anything" drain to 0.
**Moving assets out of Crimson Pact space is a 4x tax cut.**

## The levy is weekly, Sundays ~19:50–20:10 UTC
07-19, 07-26, 08-02, 08-09, 08-16 (72 → 88 → 181 → 133 → 145 levies).
**Next: 2026-08-23.** Ensure cash is on hand before it fires.
[[reference_empire_tax_day]]

## ⭐ Mission runners ARE taxed on PAYOUT, not promised credits (verified)
`tax.income_paid.income` **exactly equals** the sum of `mission.completed.credits`
in the preceding 7 days, for every mission runner checked — including ones with
huge expiry counts (miner-6: 9 completed / **434 expired** → income 16,100 = the
9 payouts; pirate-11: 9/428; pirate-13: 3/458). **Expired missions contribute
zero.** `mission.accepted` carries no credits field at all, so the promise is
never even recorded. Agents whose income exceeds their mission payouts
(craftsman-1, salvager-7, fighter-4) earn it from trading, which is taxed on
PROFIT — `taxable_market_income = market_sales_to_date − cost_of_goods`, with
loss carryforward and `tax_prepaid` offsets.

## Published policy: get_empire_info (bps ÷ 100 = %)
Matches the measured rates exactly. `policy_updated_at` = 2026-05-16/17 for all
five, so policy has been stable for 3 months.

| empire | income | property | fuel tax/unit | citizenship fee | min balance | min rep | auto |
|---|---|---|---|---|---|---|---|
| crimson | **10%** | **1.0%** | 4 | 10,000 | 50,000 | 50 | no (**exclusive**) |
| voidborn | 6% | 0.75% | 2 | **0** | **0** | **0** | **YES** |
| solarian | 5% | 0.5% | **6** | 5,000 | 25,000 | 40 | no |
| nebula | 3% | 0.25% | 5 | 25,000 | **1,000,000** | 0 | no |
| **outerrim** | **1.5%** | **0.25%** | **1** | **0** | **0** | **0** | no |

**Outer Rim is the cheapest on every axis** — 1.5% income (6.7x better than
crimson), 0.25% property, and fuel tax 1 vs solarian's 6 — with no fee and no
minimums. **Voidborn is the only AUTO-APPROVE empire** (instant, free).
Citizenship is the real lever; relocating assets is NOT (tax follows citizenship).

⚠ `tax_delinquency_bounty_per_credit` is 10000 for solarian and outerrim and
**0** for crimson/voidborn/nebula — yet crimson and nebula citizens still carry
bounties exactly equal to their unpaid tax. So that field is NOT what creates
them; `outstanding_bounty` appears to BE the tax debt universally, and the field
likely adds a separate player-collectable bounty. Unconfirmed.

## Scale
Captured window: **4,437,393 credits paid in tax** vs **270,768 in station/refuel
fees** — tax is **16x** the fuel bill and by far the fleet's largest expense.

At the 2026-08-16 levy alone: **owed 844,787** (income 804,973 + property 39,814),
paid 835,341, **unpaid 9,446**. **Income tax is 20x property tax** — the property
levy is small; the income levy is what matters. Biggest bills: fighter-4 380,907
(solarian, on 3.67M income), craftsman-1 208,477 (nebula, 4.49M), salvager-7
154,272 (crimson, 1.54M). Per-agent effective rates exceed the headline bps
(fighter-4 10.4% on a 5% empire), so `foreign_income_tax_deduction` /
`faction_income_tax_bps` blend in — not yet worked out.

**Capture shipped `9b32b067`:** `capture_tax` → `agent_tax` + `agent_tax_ships`,
with `Store.TaxShortfalls()` listing agents whose credits cannot cover the next
levy. NOT yet scheduled on any agent.

## ⭐🔴 Two traps when reading a tax bill (2026-09-13)

**1. Property tax is assessed on OWNED SHIPS ONLY — not stored goods.**
Confirmed by the operator and by `agent_tax_ships`, whose per-ship `value` rows
sum EXACTLY to `agent_tax.assessed_property_value` (salvager-1: 8,654 + 6,517 +
3,827 + 1,827 = 20,825). So the drone marketbots sitting on 140k units of ore
owe NOTHING on it, and "who is hoarding the most units" is the wrong query for
tax exposure. The right one is ship value vs credits.

This also means the **~170 idle hulls** are a standing, recurring cost —
property tax on ships doing nothing. See [[reference_module_wear_removed]].

**2. Do NOT derive a rate from the captured totals.** `taxable_income_to_date`
and `income_tax_total` are CUMULATIVE and net of `market_loss_carryforward` and
`tax_prepaid`, so their ratio is not the rate. Observed craftsman-1 at
314,932/6,618,648 = 4.76% and salvager-1 at 5,404/320,243 = 1.69%, while
nebula's published income rate is 3% for both. **The published bps from
`get_empire_info` (table above) is authoritative; the ratio is an artifact.**

## ⭐🔴 capture_tax is scheduled in NO role

`grep -c capture_tax data/overmind/roles.yaml` = **0**. It is a supported
dispatch command that nothing ever runs, so on tax day we hold records for
**4 of 172 agents** — and none of them are the high-income ones. The 4 rows we
do have are leftovers from manual runs.

Given tax is the fleet's largest expense (16x the fuel bill), this is the
cheapest observability gap to close: one daily query per agent. Adding it is
additive, so it takes effect at the next natural restart with no deploy of its
own — see [[reference_schedule_seeding_is_additive_only]] (removals are the
thing that does not propagate).

## ⭐🔴 v0.605.0 (2026-09-14): unpaid tax shows up as a BOUNTY, and we were blind to it

`get_tax_estimate` gained four fields, all now on `GetTaxEstimateResponse`
(absorbed `a4bd05cd`, `BuiltForAPIVersion` → v0.605.0):

| field | meaning |
|---|---|
| `outstanding_bounties` | `[{empire, bounty}]` — **current** debt. The one that matters. |
| `payment_guidance` | server prose naming `pay_bounty` as the remedy |
| `inactivity_exempt` | fully inactive characters stop being assessed |
| `latest_statement` | last completed weekly cycle; **historical, does not change when an old bounty is paid** |

**Read current debt ONLY from `outstanding_bounties`.** `latest_statement` is a
frozen record of a finished cycle and will keep showing the old unpaid figure
after you clear it.

### The finding: having money does not mean you are paid up
`explorer-8` on 2026-09-14 held **1,881,906 credits** and was carrying a
**9,267-credit crimson bounty** anyway. It had *also* paid its property
assessment (`owed 8299 / paid 0 / unpaid 0` on the current cycle). So a bounty
is not simply "this week's bill went unpaid" — and per the server's own
guidance, **paying clears that empire's FULL bounty including non-tax crimes**,
so the figure is not purely a tax debt. Do not report it as one.

Before v0.605 nothing in our tooling could have shown this: there was no field.
`play_as get_tax_estimate` now prints an "Outstanding bounties (UNPAID —
detention risk)" table ahead of the Note.

### Clearing it
`pay_bounty` with `empire` + `source=self` (wallet) or `source=faction`
(needs ManageTreasury). **Works remotely and while detained; docking is not
required.** `prepay_tax` reserves credits against the NEXT assessment and does
**not** touch existing debt — do not confuse the two.

### Inactivity exemption is not forgiveness
Gameplay and economic activity count even without a fresh login, so parked
agents are not automatically exempt. Existing debt stays due, and skipped taxes
never come back as a catch-up bill.

### ⭐ FIXED `7676685e`: bounties captured, capture_tax scheduled
- `agent_tax.outstanding_bounty_total` + `agent_tax.inactivity_exempt`
- new `agent_tax_bounties (player_id, empire, bounty)` — **replace-set**, so a
  settled bounty's row disappears rather than sending us after cleared debt.
  Per-empire because `pay_bounty` settles one empire at a time.
- `capture_tax` now `daily` in **all 13 roles** (was zero; `agent_tax` held 4
  rows for 172 agents). 7 reads per weekly cycle, 1 query/agent/day.
- `TaxShortfalls` now counts outstanding debt in what an agent owes. It
  previously read only the NEXT levy, so an indebted-but-solvent agent read as
  healthy — exactly the explorer-8 case.
- `ensureColumn` got its first real caller; verified against a `VACUUM INTO`
  copy of the live 341MB assets.db.

**Needs the new `bin/worker` + a restart to take effect** — schedule seeding is
additive at worker start ([[reference_schedule_seeding_is_additive_only]]).

### ⭐🔴 CORRECTION: explorer-8 had ~136k credits, not 1.88M
The trailing number on the `play_as` status line is the **game TICK**, not
credits. I read `| 1881906` as a balance; the `pay_bounty` reply a few minutes
later showed `tick: 1881983` and `credits: 127367` after paying 9,267 — so the
real balance was ~136,634. Never read credits off the status line; use
`get_status` or the command reply.

### `pay_bounty` takes NO arguments and clears everything
Operator-confirmed 2026-09-14. A bare `pay_bounty` settled explorer-8's full
9,267 crimson debt from the wallet. The reply is the authoritative after-state:
```json
{"amount_paid":9267,"credits":127367,"empire":"crimson","paid_from":"wallet",
 "outstanding_bounties":[],"released_from_detention":false,"reputation_after":20}
```
Note `reputation_after: 20` — exactly crimson's `rep_baseline_citizen`. **The
bounty was suppressing standing**, so clearing it restores baseline rep, not
just freedom of movement. `PayBountyResponse` already had every field.
[[reference_citizenship_mechanics]]
