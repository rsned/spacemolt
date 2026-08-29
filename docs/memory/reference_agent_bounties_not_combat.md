---
name: reference_agent_bounties_not_combat
description: Fleet bounties are NOT from combat — 15 agents carry them, mostly unarmed roles, across 4 factions; every 0-credit agent in the fleet has one, and bounties seize income so they cannot climb out
metadata:
  type: reference
---

**Unarmed agents carry bounties.** explorer-7 (a missionrunner with no weapons)
was detained by the **Crimson Pact** for **5,706 credits** on 2026-08-05, decayed
to **1,101** by 08-17. Bounties **decay over time** but are never cleared until paid.

## ⭐🔴 The 51-credit bounty was the RESCUER's, not the strandee's
The overmind's UNRESCUABLE alert prints the **rescuer's** last error as if it were
the strandee's state:
> `rescue: ALERT explorer-7 is UNRESCUABLE ... last error: travel: jump 1/13 to
> Sirius failed: You are detained by the Solarian Confederacy. Pay 51 credits`

That 51cr Solarian bounty belongs to **assist-sol** (184 log lines, only assist-sol;
`jailed_until 2026-08-17T23:27:50Z` in `agent_standings`). explorer-7 was never
detained by the Solarians. **A rescuer that is itself jailed reports failure in a
way that reads as the strandee's fault** — and all 4 assist agents were blamed for
5 failed attempts. Check `agent_standings.jailed_until` for the RESCUER before
diagnosing a strandee.

## It is systematic, and it correlates with being broke
15 agents carry outstanding bounties across **4 factions** (crimson, solarian,
outerrim, nebula) — roles include engineer, explorer, random, miner, marketbot and
assist. Largest: pirate-4 3,491 · explorer-8 3,362 · pirate-2 3,297 · engineer-8
1,499 · explorer-7 1,101.

**11 of the 15 have exactly 0 credits — and there are only 11 zero-credit agents
in the entire 161-agent fleet.** The overlap is essentially total.

Direction of causation is NOT established. The plausible mechanism is that a
bounty **seizes income**, so a bountied agent can never accumulate the credits
needed to clear it — explorer-7 has been at 0 credits and stuck for ~10 days, and
its bounty fell 5,706 → 1,101 in the same period, consistent with earnings being
skimmed. **The triggering event is still unknown** (no combat; candidates are
customs/contraband, unpaid tax/rent [[reference_empire_tax_day]], or mission
failure).

**This is answerable now:** the action log carries the undocumented `tax.*`
category plus `other.rent_paid`, and it backfills ~85 days — which covers
2026-08-05. Capture was fanned out to all 161 agents on 2026-08-18
[[project_action_log_capture]]. Query the days before a bounty appears.
