---
name: project_captains_log_task_resume
description: "FUTURE FEATURE — use in-game captains_log (server-persistent per-agent notes: read/write/remove) so workers re-orient after reconnect or fleet restart to what they were last doing."
metadata: 
  node_type: memory
  type: project
  originSessionId: 853398d3-bb3c-4431-bf12-2caa11653a5d
---

**Requested 2026-07-19 (user). NOT STARTED.** There's an in-game mechanic **`captains_log`** — read / write / remove **server-persistent** notes attached to an agent. Idea: workers write a breadcrumb of their current intent, so after a reconnect or fleet restart they double-check what they were last up to and resume instead of losing context.

**Why it matters (live motivation):** fighter-4 (2026-07-19) took a procurement mission, bought 295 units, stranded before delivery, and after the fleet restart had NO memory of the in-flight mission — it just spun. A captains_log breadcrumb ("mission M: bought 295 X, deliver to Z, reward R") would let it resume the delivery or trigger cut-losses liquidation instead of idling. Ties to [[project_cargo_liquidation_cut_losses]], [[reference_trading_missions_not_market_validated]], and the disconnect/restart work [[project_overmind_stall_kill_connect_loop]].

**Design sketch:**
- Verify actual command name + payload/response shape in the API before coding (do NOT assume — `captains_log` read/write/remove; check `pkg/game` / server_docs). Add to GameClient interface + client_commands if missing.
- Worker writes a compact structured breadcrumb at key commit points: mission accepted (id, item, qty, dest, reward, cost basis), haul book claimed (book, buy/sell, qty), cargo deposited-for-sweep (station, item, qty).
- On boot / after reconnect: read the log FIRST; reconcile against live ship/cargo state; resume the in-flight task or hand off to liquidation/rescue. Clear/remove the note on completion.
- Keep it idempotent and small (server-persistent, per-agent). One "current intent" note, not an append log, to avoid unbounded growth (or remove-on-complete discipline).
- Applies to ALL roles (haul, mission, mb, craft) — a shared worker helper, not per-role.

**When building:** superpowers:brainstorming first, then SDD. Confirm captains_log API shape against the live server before design lock.
