---
name: reference_bounty_auto_pays_on_entering_territory
description: "⭐ SOLVED 2026-08-18: a tax-delinquency bounty is paid AUTOMATICALLY the moment you enter/dock in that empire's territory holding enough credits — no command exists or is needed; detention only happens when you cannot pay"
metadata:
  type: reference
---

**There is no `pay_bounty` command, and none is needed.** Proven end-to-end on
explorer-7, 2026-08-18:

| | |
|---|---|
| bounty (Crimson Pact) | **1,101** |
| credits before | **10,410** |
| credits after | **9,309** |
| difference | **exactly 1,101** |
| bounty after | **0** |

It then sat docked at `blood_forge_smelting_works` (a Crimson station) at 100%
hull, undetained, working the mission board.

## The rule
- Holding credits **outside** the empire's space does nothing — explorer-7 held
  11,085 (10x the debt) for over an hour elsewhere and the bounty was untouched.
- **Entering/docking in the creditor empire's territory with enough credits pays
  it automatically.** The debt is settled from the wallet, silently.
- **Detention is the failure path, not the payment path.** The in-game text —
  "the bounty remains until paid and you will be detained again the next time you
  dock in their territory" — describes what happens when you arrive *broke*.
- No event is emitted. 85 days x 160 agents x 50 event types contain **no**
  bounty event of any kind, so the ONLY way to observe payment is the credit step
  plus `agent_standings.outstanding_bounty` going to 0. Do not look for an event.

## What this means operationally
**Clearing the fleet's tax debt needs no new mechanism — just credits.** Seed a
broke agent and the debt settles itself on its next visit to that empire.

The real poverty trap was never the bounty:
> **0 credits -> cannot buy fuel -> stranded at a non-station POI -> cannot earn
> -> stays at 0.**

explorer-7 sat at zero for **ten days** stranded at 40_eridani. A **1,200-credit**
seed broke it out: 0 -> 11,400 within hours of normal trading, then it cleared its
own 1,101 debt. The fix is a fuel floor, not a debt programme.
[[reference_tax_bounties_and_rates]] · [[reference_agent_bounties_not_combat]]

## Probe method (reusable)
`data/overmind/<fleet>-overrides.json` `{"removed":[...]}` + `kill -HUP <overmind
pid>` pulls one worker out of a fleet without touching the yaml. Guards refuse a
reload that empties the roster or removes >half the live workers. **Back the file
up first — it carries unrelated standing removals** (mission-learn holds
craftsman-1). In the end the probe was unnecessary: explorer-7 routed into Crimson
space on its own once it had fuel money.
