---
name: feedback_monitor_reporting_volume
description: "When watching fleet monitors, do NOT post a message per routine event (landings, new systems, fragments, ring switches) — it floods the screen and buries what matters"
metadata:
  node_type: memory
  type: feedback
  originSessionId: 45e19edc-89f8-4358-bb3e-11324020d741
  modified: 2026-11-13T19:35:50.386Z
---

Do not reply to every routine monitor event. Per-landing / per-fragment / per-new-system
messages filled the user's screen and important items "got lost in seconds" (2026-11-13,
during the pathfinder sweep).

**Why:** the user reads the terminal live; a stream of one-line status posts pushes
losses, decisions and questions off screen.

**How to apply:** keep the monitor filter to actionable lines only (losses, quarantines,
panics, avoid refusals, stalls/loops, lap complete/heading home). Collect routine data
(landings, systems, fragments) silently into files/KB and summarise only when asked or
at a natural checkpoint. For an actionable event: one or two lines, no tables. Never
re-post the same item. See [[reference_uncharted_system_fragments]].
