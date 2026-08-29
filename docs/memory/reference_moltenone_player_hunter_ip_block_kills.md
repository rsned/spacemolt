---
name: reference_moltenone_player_hunter_ip_block_kills
description: Every fleet loss since 2026-08-27 (10 ships) is ONE player, MoltenOne, killing ships that were stranded in the open by IP rate-limit blocks or restart churn; our ships never fire or flee — server autopilot "fire/hold" in a mining hull dies in 10 ticks
metadata:
  type: reference
---

**Diagnosed 2026-08-29.** Operator: "more pilots are getting killed traveling in
lawless" / "he's the one that killed many of our miners".

**The killer is a player, not pirates.** `action_log_events`
`combat.ship_destroyed` carries `killer_name`; every loss from 08-27 on is
`MoltenOne` (player_id `b195177bf33ce1de4d155a57d1ab149e`, no faction). 9 on
record + salvager-5 at Syrma (battle `6e3d54c4c8e4870020eea6e6221cd163`).
Earlier 15 losses (08-07..08-24) have NO killer_name. He rotates hulls:
theoria → toll (Customs Patrol) → underwriter (Raider) → **portfolio
(Freighter t2, Pulse Laser II, 180/130)**. Our logs show him SCANNING our
ships first (`[⚠ SCANNED — LAWLESS SPACE] Scanned by MoltenOne (<hull>)`) —
the parenthesised word is HIS SHIP CLASS, not a scan reason.

**Two hunting grounds, both police_level 0, no strongholds:**
- Cluster A (mining-fleet grounds, 1-2 hops apart): `bd20_2457`, `rigel`,
  `nashira`, `fumalsamakah` — 6 kills, incl. 3 in 5 minutes at nashira.
- Cluster B (haul/unlock transit corridor near sol): `hr_8832`, `ankaa`,
  `syrma` — 4 kills. Haul routes go THROUGH BOTH (salvager-5's 08-29 route:
  Ankaa → Alphecca → Scheat → Nashira → Rigel → BD+20 2457).

**⭐ Every kill happened during an IP-block window.** IP-block log lines per
hour (local): 08-27 08h-13h = 993/1320/937/633/661/203, 08-27 22h-08-28 00h =
558/508/849; zero on other hours. Kills: 08-27 08:48, 09:01, 09:03, 23:22,
23:25, 23:27, 23:40; 08-28 00:44, 00:50. Pre-death logs show either
`Your IP has been temporarily blocked` on every command, or the stall
watchdog restart churn (`terminated` → reconnect every ~95s,
[[project_overmind_stall_kill_connect_loop]]). A ship mid-route sits at a
star/gate POI in the open and cannot jump, dock, or flee. salvager-8 died,
respawned, and died again 64 min later in the same block.
"Lawless transit is safe" ([[reference_lawless_transit_vs_idle]]) assumed a
ship that keeps moving; a blocked ship is idle in the open.

**What our pilot did in the battle (from get_battle_log):** server
`auto_pilot: true`, `stance: fire`, chatter reason `hold` every tick, target
= MoltenOne, moves `pulled_closer` (dragged in by his advance). Threshold
with Mining Laser I + EM Disruptor I **fired zero shots** (all 9 shots his);
never braced, never fled. Outer → engaged in 3 ticks, dead at tick 10.
salvager-4's log shows the server echo of that stance: `Your weapons can't
reach the enemy at this range — 'advance'` once a tick. **pkg/worker has no
handler for battle_started/combat_update outside hunt.go** — the only flee
code is `hunt.go:408` (`Battle(ctx,"stance",{stance: flee})`, only valid
while `st.InBattle`). [[reference_combat_damage_pipeline]]

**Fix directions (not built, operator to choose):** (1) the root is the IP
block rate — a blocked fleet cannot react to anything; (2) a movement-layer
avoid-list of recent player-kill systems (data already in
action_log_events) — same gate the stronghold rule needs
([[reference_stronghold_guard_is_per_role]]); (3) on battle_started for
non-hunt roles send flee (or brace, 0.25 dmg) immediately — only helps when
commands are NOT blocked.
