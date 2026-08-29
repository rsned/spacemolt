---
name: reference_settled_mission_livelock
description: The server lists a mission ACTIVE that it also refuses to complete (mission_already_settled) — workers loop forever; abandon_mission is the cure
metadata:
  node_type: memory
  type: reference
---

**A mission can be simultaneously ACTIVE in `get_missions` and already settled
server-side.** `complete_mission` on it returns:

    mission_already_settled: This faction mission has already been fulfilled or cancelled.

The worker resumes it every pass because it is still in the active list, so it
loops at tick cadence forever. **`abandon_mission <id>` clears it** — verified on
miner-5, miner-6 and prophet-1 (2026-08-23), each freeing instantly and
departing on its route within ~60s.

**Signature to grep** (this exact cycle, ~10s apart, forever):

    missions: resuming exploration <id> (<title>): 0 leg(s) remaining
    Finding route to <sys>... -> You are already at the target system
    Dock action pending -> Error: Already docked
    missions: explore return dock failed: Already docked; held for next pass

Count it with `grep -c 'held for next pass'` on a fleet log tail: 1,481 in
mission-learn and 1,468 in unlock in ONE tail on 2026-08-23.

**Scale when found: 22 agents (14% of the fleet)** — 15 mission-learn, 7 unlock —
all on the same mission, `HEXC recon: Hex Star`. It is the same defect that
burned 47 hours / 14,244 iterations on johnny_cab
([[reference_livelock_invisible_to_health_checks]]), and every one of them reads
**healthy** the whole time.

**Two independent faults, both real:**
1. *Client* — `missionRunExplore` aborted on the "Already docked" refusal before
   `missionComplete`, so even a genuinely completable mission could not retire.
   FIXED `6635aa73` (`dockIdempotent`, applied to the return-dock AND the
   leg-dock). **Needs a worker rebuild + fleet restart to take effect.**
2. *Server/state* — the settled-but-active listing above. The client fix does not
   remove it; `abandon_mission` is the only clearing action found.

**How to apply:** after any incident, grep `held for next pass` per fleet before
assuming the fleet is working. To clear an agent by hand: `play_as <agent>` ->
`abandon_mission <id>`, taking the id from the `resuming exploration <id>` log
line. SIGSTOP the worker first ([[reference_sigstop_preserves_game_sessions]]).

Related: [[reference_sell_leg_dock_gap]] (same already-there-meets-fresh-dock
family) · [[project_pirate_reputation_unlock_campaign]] (this blocked 3 of 6
wave-1 agents from departing)
