# Mission-Runner Canary — Provisioning & Launch Runbook

Operator steps (manual, via play_as). Worker-agent manual commands require
the supervisor-freeze + worker-stop protocol ONLY for agents in a running
fleet; these four are idle, so plain play_as sessions are fine.

## 1. Per-agent provisioning (engineer-1, engineer-2, fighter-1, fighter-2)

For each agent, `go run ./cmd/tools/play_as <agent-id>` and:

1. `get_status` — record credits, current ship, system, and EMPIRE. All four
   must be in the same empire; if one isn't, swap it for another idle
   band-1/2 account and update mission-fleet.yaml before launch.
2. Ship check: the target hull is a T2 freighter-class (guide: "a T2
   freighter with a weapon mount covers 90% of boards"; v1 skips the
   weapon). At a shipyard station, list hulls and pick the ~8,000 cr T2
   cargo hull; `buy_ship` it and `switch_ship`.
3. Funding: each agent needs ship cost + ~10,000 cr working capital for
   mission cargo buys. Fund from a treasury-holding agent via send_gift
   (note: `send_gift --source=cargo|storage` is on an unpushed local
   commit; plain credit gifts work on main).
4. `buy_insurance` on the new hull (premiums are trivial; guide
   recommendation).
5. Park docked at a station with a mission board (`get_missions` returns
   entries) so the first pass has work.

## 2. Launch

1. Build: `go build -o bin/overmind ./cmd/overmind` (binaries go in bin/).
2. Construct the launch line by mirroring the RUNNING haul fleet's exact
   flags: `cat /proc/$(pgrep -f overmind | head -1)/cmdline | tr '\0' ' '`
   — swap in `--fleet data/overmind/mission-fleet.yaml` and a distinct
   socket/log (`mission.sock`, `mission-overmind.log`). Remove any stale
   `data/overmind/mission.sock` first (`rm -f`).
3. Stagger startup (login rate limits are per-IP/minute; 4 workers is
   safe, but do not relaunch repeatedly in a tight loop).
4. Add the final launch line to the overmind launch-commands runbook
   (memory: reference_overmind_launch_commands) the SAME DAY — the
   arbitrage scanner was once lost from a relaunch because its line was
   missing there.

## 3. Verify (first hour)

- `tail -f data/overmind/mission-overmind.log` — expect "missions:" lines:
  board reads, skips with reasons, accepts, completes.
- `sqlite3 <market.db> "SELECT agent_id, outcome, expected_reward,
  credits_earned, item_cost, jumps FROM mission_results ORDER BY id DESC
  LIMIT 20;"` — rows appearing with outcome=completed and positive
  credits_earned is the success signal.
- Watch for pathologies: repeated abandon rows (selector gates too loose),
  zero board entries everywhere (parked at boardless stations), buys
  failing (underfunded).

## 4. Measure (before any scale-up)

Run for 48h+, then compare net cr/hour/worker:
`SELECT agent_id, COUNT(*), SUM(credits_earned - item_cost - fuel_cost)
 FROM mission_results WHERE outcome='completed' GROUP BY agent_id;`
against haul-fleet realized economics. Scale-up (more workers, exploration
circuits, dashboard panel) is a separate decision from this data.
