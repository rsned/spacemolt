---
name: reference_worker_heartbeat_credits_stale
description: A worker's heartbeat/status credits can read 0 while the agent actually holds credits — gifts look like they failed when they landed
metadata:
  node_type: memory
  type: reference
---

**`<fleet>-status.json` credits (and the `heartbeat ... credits=` log line) can be
badly stale.** 2026-08-23: seven unlock agents were gifted 100,000 cr each. The
server confirmed every send (`credits_sent: 100000`, correct `recipient`, and
hauler-0's wallet decremented 19,497,979 -> 18,797,979 exactly). Six of the seven
then reported `credits=0` for **20+ minutes**, through many heartbeats, while
actively issuing commands (`Route found`, `missions: no acceptable missions`).

`play_as miner-3` proved the wallet was right and the heartbeat wrong:
`Ready! Credits: 100000.00`, `Gifts received: 1`.

**Do not conclude a gift failed from the status file.** The authoritative check is
`play_as <agent>` -> `status`. The one agent that DID show its new balance
(pirate-14) had spent credits in the meantime (a 592 cr refuel), which is what
refreshed its local state.

**Consequence: an agent can be broke-by-belief.** A worker whose cached credits
say 0 will not necessarily act on money it actually has, so a funded agent can
sit still. The reliable nudge is to make it spend once — a `refuel` via play_as
was enough for all five remaining agents.

Related: [[reference_send_gift_and_play_as_mechanics]] ·
[[reference_client_cargo_used_drifts_upward]] (same family: worker-local state
drifting from server truth) · [[reference_livelock_invisible_to_health_checks]]
