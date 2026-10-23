---
name: project_banker_fleet
description: "the_banker (outerrim, sol_central, role resident) funded with 10M on 2026-10-22; 9.66M after the 10-23 sweep. A NEW agent must EARN 1000cr before it can accept or send credits. cmd/tools/banker is operator-driven; `overmind` IS a real agent; freeze overmind+worker before play_as the_banker."
metadata:
  node_type: memory
  type: project
  originSessionId: 2d7bbf54-7b88-4de5-ba93-9477eb1cb891
  modified: 2026-10-22T08:07:20.594Z
---

Single-member fleet, `data/overmind/banker-fleet.yaml`, launched 2026-10-22.

## Why

The fleet has no way to move money. A weekly tax trickle drains accounts that
cannot replenish, and **an agent at zero cannot buy fuel, so it strands, is
quarantined, and stops existing.** On 2026-10-21 that cycle had parked 17 agents
for up to 10.8 days. Four were sitting DOCKED and healthy-looking at 0 credits
(assist-krynn, assist-sol, random-3, random-5); gifting 10k–50k brought all four
back within minutes — random-3 and random-5 refuelled to 100/100 on their own.

Before this, the float lived in whichever agent happened to be rich. That is
fragile: johnny_cab is a **taxi** and roams — see
[[reference_outpost_vs_station_dockability]] for the hours it lost parked on an
outpost unable to gift.

## Setup (done)

- **Agent**: `the_banker`, empire **outerrim** — chosen for the lowest
  income/property/sales tax rates, and the float sits here.
- **Credentials**: `data/agents/the_banker/credentials.json` (mode 0600;
  `credentials.json` is covered by `.gitignore:113`).
- **Home**: `sol_central` / Confederacy Central Command. Sol is central on the
  map; the station is verified `type=station`, `wrecked=false`, satisfaction
  91%, 10 services. **Never home it on a mobile capital** — the Outer Rim
  capital drifts, and that drift caused the assist-frontier livelock.
- **Role**: `resident`. Its idle is `ensure_home → dock → refuel` — exactly a
  banker's job. It does not mine or trade.
- **Funded**: 10,000,000 gifted from johnny_cab 2026-10-22 (johnny_cab 43.2M →
  33.2M). Had to move johnny_cab off the outpost to Ironlight Crossroads first.

## ⭐ A NEW AGENT MUST EARN 1000cr BEFORE IT CAN ACCEPT OR SEND CREDITS

A freshly registered agent cannot transact. The cheap bootstrap:

```
accept_mission market_participation_buying
buy <cheapest local item> 10          # lithium_ore @1cr = 10cr total
complete_mission <mission_id>          # +1000 cr
```

Done for the_banker 2026-10-22: 100 → 1,090 credits. Any future agent needs
this before it can be gifted to.

## cmd/tools/banker

Already built, operator-driven by design: prints the full plan and sends
nothing without `--execute`. Flags: `--from`, `--floor` (default 10000),
`--max-total`, `--max-age` (refuses a status file older than 1h), `--emit-script`.
Point it at `--from the_banker`.

**Correction 2026-10-23:** `overmind` IS a real mining-fleet agent (agent_id and
username both `overmind`, Theoria hull, homes at The Anvil). `send_gift "overmind"
credits 10000` landed and its worker came up with `Credits: 10000`. The earlier
"exclude it" note was wrong.

**Driving the_banker while its worker runs (2026-10-23):** the worker does NOT park
on 4001 — it reconnects in 2 s and displaces play_as mid-script (happened 3×). SIGSTOP
just the banker WORKER, run `play_as the_banker` with `run <grants file>` (`sleep 2s`
between gifts, ≤10 gifts per session), `rm data/agents/the_banker/play_as.lease`,
SIGCONT. The supervisor kills a silent worker at 90 s (`SilenceTimeout`), and freezing
the overmind instead rolling-restarts the whole fleet on thaw — see
[[reference_clerk_token_login_and_connection_gate]]. The emitted script's `run`
works as documented; `send_gift` returns `credits_sent` per grant.

At `--floor 10000` the sweep was **46 grants / 366,649 cr** — trivial against
the 10M float.

Note: usernames are genuinely 24 chars in the ledger (`Stella 'StarStrider' Ste`
is the whole name, not a truncation) — don't "fix" them.
