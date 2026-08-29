---
name: reference_exploration_is_content_survey
description: "In spacemolt the system/connection map graph is fixed and fully known; \"exploration\" means surveying each system's CONTENTS (POIs, POI types, poi_resources), not discovering new systems"
metadata: 
  node_type: memory
  type: reference
  originSessionId: b9d940f1-1595-4045-bcf5-ef30a3dc8faa
---

The spacemolt galaxy's **map graph is known and fixed** — all systems and their connections are already in the KB (505 systems as of 2026-06-23). There is no topological frontier: a connection endpoint will essentially never be absent from the systems table. So in the overmind explorer's `NextExploreTarget` (pkg/worker/explore.go), the **`classFrontier` branch is effectively dead** in this game.

What is NOT fully known is each system's **contents**: POI count, POI types, and `poi_resources` (richness/depletion). On top of that, some content is **transient and moves over time**:
- **Hidden deposits** — resource deposits not visible until actively scanned/surveyed; they can appear, deplete, and relocate.
- **Wormholes that move around** — transient, mobile connections that relocate over time. The PERMANENT system/connection graph is fixed, but wormholes are a "moving frontier": a re-survey can reveal a wormhole connection that wasn't there before (and may add an endpoint/edge that didn't exist in the static `connections` table).

Because of this transient content, periodic **re-survey of already-known systems has real value** (not just bookkeeping) — staleness is a genuine signal, since a "complete" system can sprout a new hidden deposit or wormhole between visits. Real "exploration" = travelling to a system and surveying its POIs/resources/transients. The two selection classes that actually drive it:
- **unvisited** — `System.LastVisitedTick == 0` (contents never captured)
- **stale** — `nowTick - System.LastUpdatedTick > staleTicks` (contents captured long ago)

Implications for the explorer role ([[project_overmind_fleet_manager]] Phase 2a):
- The stale path is the PRIMARY driver, not a fallback. Aging `systems.last_updated_tick` in `data/spacemolt-knowledge.db` is a valid lever to force re-survey (done 2026-06-23 to test live movement: explorers parked in fully-mapped core hubs idled with "no frontier reachable" until systems were aged past the ~8640-tick / 1-day cutoff).
- **Design gap / future refinement:** system-row `last_updated_tick` is a coarse proxy for "contents known." A system can be visited+fresh yet have incomplete POI data (partial POI list, null `poi_resources`, untyped POIs). A content-aware selector (target systems whose POIs lack resource data / whose poi_count is unknown) would chase real gaps instead of time-decay. The POI-merge provenance work ([[project_poi_merge_provenance]]) tracks per-resource richness and `detected_by`, which is the substrate for such a signal.
- `explore.smolt` uses `scan` for capture; the richer `survey_system` (anomaly + deeper content, see [[project_survey_anomaly_capture]]) was deferred as a non-goal in the explorer spec — revisit if `scan` under-captures `poi_resources`.
- **Anomaly detection needs a fitted module (capability gap, 2026-06-23).** Anomalies are reported at range (e.g. Haven has a known anomaly ~5 systems away) only when the ship has a `survey_scanner_i`/`survey_scanner_ii` or `anomaly_detector` module installed. The overmind worker has no buy-module + fit-module control yet, so explorers cannot self-equip detection gear. Building that (purchase + install + verify a module before exploring) is a prerequisite for explorers to actually find anomalies/wormholes, and a natural Phase-2b mobile-role capability. See [[project_overmind_fleet_manager]].
- **BUG found live 2026-06-23 (id vs name):** `pkg/worker/explore.go` `Explore` passed `state.CurrentSystem` (display NAME, e.g. "Haven") into `NextExploreTarget`, but the KB jump graph (from `connections.from_system/to_system`) and `systems.id` are lowercase IDs (e.g. "haven"). BFS from the name reaches no graph node → every candidate "unreachable" → always "no frontier reachable; idling" despite 497/505 stale + 190 never-visited. Fix: pass `state.System.ID` (the SystemData id) as currentSystem; unit tests missed it because the fake KB used matching ids on both sides. Add an Explore-level test with name≠id.
