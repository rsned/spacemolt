---
name: reference_take_agent_out_for_play_as
description: "To fly an overmind-managed agent by hand for more than a minute, add it to <fleet>-overrides.json removed-set + SIGHUP the overmind; SIGSTOP and play_as alone do NOT hold the ship (11-23)"
metadata:
  type: reference
---

Live 2026-11-23 (engineer-1 package recovery, mission-learn fleet):
- **play_as does not displace the worker.** Both sessions stay connected and both send actions; they collide with "Another action is already pending (jump/dock/switch_ship)" and the worker keeps flying its own plan.
- **SIGSTOP does not hold.** The overmind replaced the stopped worker with a fresh one in ~90s.
- **What works:** `jq '.removed += ["<agent>"]' data/overmind/<fleet>-overrides.json` then `kill -HUP <overmind pid>`. Log shows `membership: removing "<agent>" — drain sent, force-stop after 4m0s`; worker finishes its pass and exits (≤4 min). Undo with `.removed -= [...]` + SIGHUP; a new worker starts within seconds.
- A worker mid-route keeps issuing jumps until it exits; wait for the process to be gone before scripting play_as.
- Script play_as one step at a time when the ship might still be moving; a failed autopilot leaves every later command running against the wrong place.

Related: [[reference_pgrep_self_match_and_sole_member]], [[reference_secondment_overrides_are_removed_sets]].
