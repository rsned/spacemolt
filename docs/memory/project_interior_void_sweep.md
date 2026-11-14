---
name: project-interior-void-sweep
description: NEXT after the round-2 full-lap sweep finishes — explorers sweep the empty void areas INSIDE the galaxy with ~100 LY pass spacing
metadata:
  node_type: memory
  type: project
  originSessionId: 45e19edc-89f8-4358-bb3e-11324020d741
  modified: 2026-11-14T20:39:39.724Z
---

User request 2026-11-14: once the explorers finish the current round (round 2,
full 360° laps on rings 7200-8550, see [[project-pathfinder-frontier-sweep]]),
the next job is the void areas inside the galaxy, with passes ~100 LY apart
as the ships cross them.

**Why:** the frontier sweeps found hidden systems at the edge; voids inside the
charted galaxy may hide more (pathfinder drift captures within ~50-90 LY, so
100 LY spacing leaves no gaps).

**How to apply:** don't start until every sweep_state.json reaches phase done.
Design still open — void detection from KB system positions, path shape
(straight chords vs. rings), and how the lap code is reused. Brainstorm with
the user before building.
