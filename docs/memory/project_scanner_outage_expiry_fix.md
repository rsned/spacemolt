---
name: project_scanner_outage_expiry_fix
description: "2026-07-16 haul outage: arbitrage-scanner died unnoticed for 10h (zero hauls 6.7h) + the stale-opportunity zombie loop found and fixed on branch fix/arbitrage-expiry-filter"
metadata: 
  node_type: memory
  type: project
  originSessionId: 2a5c2a37-a408-4c0d-9aa8-9ecda2d08824
---

**Status: fixed + deployed. Branch `fix/arbitrage-expiry-filter`, commit `6e678a4` — NOT pushed, NOT merged.**

## The outage
The arbitrage-scanner died in the 2026-07-15 ~21:21 local reboot. The ~23:51 fleet relaunch restored all 5 overminds but missed the scanner, because [[reference_overmind_launch_commands]] didn't list it (now fixed there). Consequence: 21 haulers churned a frozen pool for ~10h and completed **zero hauls for 6.7h** while looking perfectly healthy (100% hull, 0 restarts, active heartbeats).

**The scanner is unsupervised** — no overmind owns it, nothing alerts when it dies. Each scan wipes the pool (`available` → `expired`) and inserts a fresh batch, so *freshness exists only while that process lives*. First thing to check when haulers look busy but earn nothing:
```
ps -eo cmd | grep 'arbitrage-scanner watch'; tail data/overmind/arbitrage-scanner.log
```

## The zombie loop (the real bug)
Stale opportunities recirculated indefinitely and **outranked fresh ones**:
1. The scanner's sweep only touches `status='available'`, so a row held as `claimed` survives it.
2. `ReleaseOpportunity` sets a claim back to `available` — resurrecting a long-expired row.
3. `GetOpportunities` ordered by `gross_profit DESC` with **no expiry filter**, and stale rows keep their inflated fantasy profit → they sort *above* genuine fresh ones.
4. `ClaimOpportunity` accepted them despite its doc promising "false if ... expired" (its WHERE only checked `status='available'`).
5. Hauler travels, gap closed hours ago, abandons → back to step 2.

Fix: wall-clock expiry predicate (`notExpiredSQL`) in both `GetOpportunities` and `ClaimOpportunity`. Only `available` rows are filtered, so completed/expired history stays readable for the dashboard and reporting.

## The timestamp trap (do not regress this)
`expires_at` is stored RFC3339 (`2026-07-16T07:48:03Z`) but SQLite's `datetime('now')` yields a space-separated form (`2026-07-16 14:40:11`). Compared **as strings**, `'T'` (0x54) sorts above `' '` (0x20), so **every same-day expired row falsely reads as live**. Always compare via `julianday(...)`. This bit my own diagnostic queries before it was spotted in the code. Tests pin it using *today's midnight* as the expired timestamp — always same-day, so a naive string compare fails the test deterministically.

## Deploy record
Rebuilt `bin/{worker,overmind}` (the fix lives in pkg/market, which workers link — **a redeploy is required for it to take effect**). Drained the haul fleet (SIGUSR1 → 14/21 idle; poll is bounded at 60 × SleepShort ≈ 3.5 min, so the ~7 on real hour-long hauls never finish — force-TERM after the poll ends is the documented path, claims resume via `GetClaimedByAgent`). Then TERM → `rm -f data/overmind/haul.sock` → relaunch → 30s default stagger, ~10 min to all 21.

**Verified live: claimed rows went 7 fresh/14 stale → 21 fresh/0 stale**, avg 77k net. The stale-claim count is the canary — if it climbs above 0, the zombie loop is back.

Related: [[reference_haul_fleet_capacity_ceiling]] (a fat opportunity pool means the fleet is BROKEN, not that there's headroom) · [[project_arbitrage_net_of_fuel]] · [[reference_overmind_launch_commands]]
