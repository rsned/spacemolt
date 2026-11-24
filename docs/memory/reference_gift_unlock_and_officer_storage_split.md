---
name: reference_gift_unlock_and_officer_storage_split
description: "send_gift needs 1,000 EARNED credits (gift_not_unlocked); player-station marketbots cannot self-unlock (no mission board): sell to a visiting hauler; gifts to an officer land in its own storage and a craft uses ONE storage, so collection points need faction_deposit sweeps"
metadata:
  node_type: memory
  type: reference
  originSessionId: 90ad50cf-9c6a-4f6f-a1d4-f3dc05c8ebcb
  modified: 2026-10-30T20:29:31.185Z
---

**gift_not_unlocked (2026-10-30):** an account cannot send gifts until it has *earned* 1,000
credits itself; gifted credits do not count. marketbot_015 (10,000 gifted credits) failed every
consign gift for a day.
**Easiest unlock (operator recipe):** at the agent's station, accept the
`market_participation_buying` mission, buy 10 units of the cheapest item on the market (some
ore at 1 cr), then `complete_mission` -> +1,000 cr reward, which counts as earned. Works with
no sellable stock. (Selling also works: 6 rhodium_ore at Ironhearth, 190 each, unlocked
marketbot_015 on 10-30.)
New accounts (marketbot_021-033 etc.) will hit this before any consign rule works. Consign now
logs refused gifts (06b8dcd2); before that it dropped them silently.

**Marketbots at PLAYER stations can't self-unlock (operator, 2026-11-23):** player stations have
no mission board, or don't offer `market_participation_buying`, so the hourly `unlock_gifting`
verb never works there. It logs "no mission board" or misleadingly "not offered here and not
active — already completed" (seen for ~30 marketbots). Two ways out:
1. Fly the bot to an EMPIRE station that has a mission board and run the recipe there.
2. If the local station has a market, a visiting hauler buys from the bot: the bot posts
   `create_sell_order <item> 1000 2` and the hauler `buy <item> 1000` = 2,000 earned. NPC bids are
   usually absent there (`sell` answered "No buyers"). Done 11-23: marketbot_018 (Winterhold)
   via trucker-109. A gift-locked `release` fails `gift_not_unlocked` every 10 min.

**Officer collection points:** a gift to miningbot_0000 at Conjunction Junction lands in its
PERSONAL storage, and a craft takes all inputs from one storage (own OR faction), so split inputs
refuse with `cannot_craft: Not enough materials`. Fix: the worker verb `faction_deposit ITEM`
(f939e280) scheduled half-hourly per item. One explicit item per entry, never a wildcard
(reserved goods share the storage). See [[project_cloverfield_home_station]].

**Treasury top-ups (operator, 2026-10-30):** `send_gift` needs no faction lockbox and works from
any station; `faction_deposit_credits` only works docked where the faction has storage. So a
non-member funder (the_banker) gifts credits to a CRFT member docked at Conjunction Junction
(miningbot_0000), which deposits them. Done 10-30 for 1M after the treasury hit 0 (rent at
Haven/Voss drains it ~4M/day; it emptied three times 10-26..10-30).

**v0.669.0 (2026-11-08): credit-only send_gift works ANYWHERE, including in space**, and
`faction_deposit_credits` / `faction_withdraw_credits` work docked or in space (withdraw still
needs manage-treasury). ITEM gifts still need a dock at a station with storage. (Before v0.669
a credit gift needed the sender docked: 14 of 18 haulers failed `not_docked` on 10-31.) The
rescue-fee DebtPayer's dock gate was removed for this. Treasury top-ups no longer need
miningbot_0000 at CJ: any CRFT member can receive and deposit.

**Credit gifts go to the WALLET (verified 2026-11-08):** the_banker gifted 100k to each trucker
(at Sol); trucker-00's heartbeat showed ~98.5k within 2 minutes while it was away in Iron Reach.
Only ITEM gifts land in station storage. Note the recipient is the in-game USERNAME, not the
agent id (trucker-00 = u_mother_trucker_000; `agents` table in assets.db maps them).
