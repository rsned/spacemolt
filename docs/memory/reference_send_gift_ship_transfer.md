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
