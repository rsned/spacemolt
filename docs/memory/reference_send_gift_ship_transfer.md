---
name: reference-send-gift-ship-transfer
description: send_gift ship_id transfers a hull free and remotely, but the ship STAYS PARKED where it is — the recipient must travel to it
metadata:
  type: reference
---

`send_gift(recipient, ship_id=...)` transfers a ship to another player for
**free** — no sale, no spread, no 2% sales tax. Verified in the openapi
`/send_gift` description 2026-09-18.

**Sender side is unconstrained:** "Gifting a ship works remotely: the ship can
be parked at any station and you don't need to be docked or travel to it (you
can even send it mid-flight) — it just must not be your active ship."

**Recipient side is NOT:** "The transferred ship stays parked where it is and
the recipient finds it (and your pink-slip note) at that station." The hull
does not follow the recipient. They must travel to that station and
`switch_ship` there. Recipient need not be online; delivery is async and
shows on their next storage view.

`credits`, `item_id`+`quantity`, and `ship_id` are **mutually exclusive** —
one per call. Player item/credit gifts require the SENDER docked at a base
with storage service; ship gifts do not. `send_gift` is a **mutation, 1 per
tick**. Normal gift unlock (1000 lifetime credits earned) applies.

Response is `GiftShipResponse`: `action:"gift_ship"`, `ship_id`, `class_id`,
`class_name`, `base_id`, `recipient`.

🔴 **THE GIFT UNLOCK BLOCKS PARKED AGENTS.** Sending ANY gift requires
**1,000 lifetime credits EARNED** — not balance. Parked marketbot residents
never trade, so they have never earned it and CANNOT gift at all:

```
send_gift "Arthur 'Artificer' Artis" aluminum_ore 1089 --source=storage
❌ gift_not_unlocked: You must earn at least 1000 credits before sending gifts.
```

marketbot_last_light failed this on 2026-09-18 while HOLDING 1,217 credits —
balance is irrelevant. This strands the sensor net's materials: 1,550
aluminum_ore across five Outer Rim marketbots that cannot hand any of it
over. Workarounds: sell to the station market and have the buyer re-buy it
(which also earns the marketbot its first credits toward the unlock), or
skip the marketbots entirely and fund a trading agent to buy on the open
market.

**Gifted items land at the SENDER's station**, same as ships — confirmed by
the operator 2026-09-18. So a gift never relocates anything; it only changes
owner.

**Planning consequence:** combined with
[[reference_ship_commissioning_is_credits_only]] — `commission_ship` only
works at the yard you are docked at, and a finished commission lands in
storage THERE. So a builder agent can only stock one yard, and gifting moves
ownership but never location. Pre-building helps only agents already routed
to that station; everyone else is better off commissioning at their own
nearest yard, which also parallelises (builds serialise PER YARD).

Credit gifts do NOT require co-location — proven 2026-09-18, 50,000 cr sent
from grand_exchange_station (Haven) to an agent at First Step. See
[[project_assist_frontier_mobile_capital_livelock]].
