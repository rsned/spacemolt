---
name: project_agent_empire_bands
description: Agent trailing number encodes empire band; explorer renumber done 2026-06-09
metadata: 
  node_type: memory
  type: project
  originSessionId: ff4b2c6f-1eb8-4140-99a5-0b4d4e75d3ee
---

Agent ids are `${ROLE}-${N}` and the trailing number **encodes empire band**:
1-2 nebula, 3-4 solarian, 5-6 voidborn, 7-8 crimson, 9-10 outerrim. The empire
lives in `data/agents/<id>/personality.json` `empire` field (and a matching one
in the gitignored `credentials.json`).

2026-06-09 housekeeping: every empire-range role (craftsman, engineer, fighter,
miner, salvager, trader) was already correct; **only explorer-\* was scrambled**
and got renumbered. Explorers had 4 solarian / 0 outerrim, so a clean 2-per-band
was impossible by renaming alone — decision: renumber in place, **park the 2
surplus solarian explorers at explorer-11 / explorer-12** (overflow, outside the
band scheme), and make **explorer-9 / explorer-10 empty outerrim placeholder
stubs** (`personality.json` with `"placeholder": true`, no credentials/account)
awaiting real agents.

Special categories are intentionally NOT band-aligned: prophet-1/2 = cult leaders
(independent); spark-1..5 + architect-1..5 = the two cults' acolytes
(independent); pirate-1..15 = 3 unified squads (1-5 crimson, 6-10 outerrim,
11-15 voidborn); random-1..9 = unaligned.

Gotchas:
- **Renumbering is local-only.** Game-server `username`/`player_id` are fixed
  server-side and travel with the agent; renaming never touches the account.
- Old explorer-7's server username literally contains `"Explorer-7"` and it now
  sits at slot 1 — server usernames can't be renamed, so that display name is
  permanently stale. No other explorer username embeds its number.

Tooling: `cmd/data/renumber-explorers/` is the migration tool (built TDD, two-
phase staged dir renames + DB UPDATEs + single-pass report rewrite + verify;
`--apply` flag, default dry-run; auto-discovers agent-id columns). The
`go build`-only check misses interface-mock drift — see
[[feedback_gameclient_interface_mocks]]. DB backups from the 2026-06-09 run:
`data/spacemolt-knowledge.db.bak-renumber-20260609-195025` and the
daily-summary sibling (deletable once the live server re-verifies logins).
