---
name: reference_handoff_gifts_from_storage
description: "send_gift source=storage gifts straight from station storage (no cargo, no size limits). The handoff pass withdrew cargo-sized batches and misread a size-2 cargo overflow as 'holder storage empty' — fixed 10-22 (uncommitted)"
metadata:
  type: reference
---

**`send_gift ... source=storage` moves goods straight from the sender's
station storage to the recipient's storage there — no withdraw, no cargo
hold, no item-size limits.** Proven live: alhena's action log shows
`gift_sent ... "source":"storage"` (375 adamantite, 2026-10-18), and the user
confirmed the flag 10-22. The old `deliver.go` note "gifts are cargo-only
(live-verified 2026-07-10)" was wrong by 10-22; Deliver still carries goods
only because a gift lands at the SENDER's station.

**The bug it exposed (2026-10-22):** `HandoffPass` withdrew `min(remaining,
free cargo)` UNITS, ignoring item size. With a 100 hold (90 free) and size-2
adamantite it asked for 90 = 180 space; the server's refusal matched
`isShortSupplyErr` ("not enough"/"insufficient") and the record failed as
**"holder storage empty, replan needed"** while 5,675 sat in storage. A
45-unit probe moved 41 and closed "done" at 41/45 — a cargo-limited partial
also reads as source exhausted.

**Fix (uncommitted, needs a worker rollout):** `moveHandoffStock` =
view_storage -> one storage gift of min(remaining, held). Until alhena runs
it, queue handoffs in pieces that fit the hold (40 x size 2 into 82 free).

Related: [[project_consignment_haul_relay]] [[reference_send_gift_and_play_as_mechanics]]
