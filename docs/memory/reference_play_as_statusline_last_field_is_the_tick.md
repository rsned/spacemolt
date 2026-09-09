---
name: reference_play_as_statusline_last_field_is_the_tick
description: "The trailing number in the play_as statusline is the GAME TICK, not credits — misread it 2026-09-09 and planned two ship purchases on 1.83M of imaginary money"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 2d7bbf54-7b88-4de5-ba93-9477eb1cb891
  modified: 2026-09-09T06:14:08.595Z
---

The `play_as` statusline looks like this:

```
assist-sol | Sol | sol_central (⚓Docked) | theoria | Fuel:[██░░░] Cargo:[░░░░░] | 1834683
```

**That trailing number is the GAME TICK, not credits.** It ticks up by 1 every
few seconds and is identical (±2) across every agent you inspect at the same
moment — which is the tell. `ship_listings.game_tick` carries the same value.

**2026-09-09: I read it as credits** and reported assist-sol and assist-krynn as
holding "1,834,595 / 1,834,597 credits" — actually just the tick, twice. Real
balances were **0** and **23,315**. Two tanker purchases were planned and
launched on that basis; both failed at the server with `insufficient_funds`.

**Read credits from one of these instead:**

- the login banner: `Ready! Credits: 5425941.00 | Ship: Arbitrage | Cargo: 0/345`
- the `get_status` panel row: `│ Credits  23,315 cr │`
- `agent_profile.credits` in `assets.db` — but **check `captured_at`**: it is a
  periodic capture, and craftsman-1 read 130,671 there while actually holding
  **5,425,941** (a day-stale row). A stale ledger row will silently rule out the
  right funding agent.

**How to apply:** never source a spend decision from the statusline. Confirm the
balance from the login banner in the same session that will do the spending, and
treat `agent_profile.credits` as a hint whose age must be checked.

See [[reference_worker_heartbeat_credits_stale]] — the same class of error from
a different surface.
