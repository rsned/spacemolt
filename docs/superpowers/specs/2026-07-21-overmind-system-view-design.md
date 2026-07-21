# Overmind Zoomed System View — Design

**Date:** 2026-07-21
**Status:** Approved (pending final spec review)

## Problem

On the overmind galaxy map, systems with many fleet agents (e.g. 9 agents in
Alpha Centauri) render as an unreadable cluster of tiny dots — too small to
click, hover, or distinguish. We want a zoomed-in, per-system view in the
style of the KB system pages (https://rsned.github.io/spacemolt-kb/systems/…)
— dark orbital layout with sun, orbit rings, planets, stations, belt chunk
scatter, and jump-gate crosshairs on the periphery — but live: real agent
dots at POIs, movement animation, and hover interactions tied to the fleet
rail.

## Decisions (locked with user)

- **Full map-slot takeover**: the system view replaces the galaxy map in the
  same slot. The FleetRail on the right stays visible (needed for
  hover-highlight). Galaxy pan/zoom state is preserved on return.
- **Single click on a system zooms in.** The small `SystemPanel` info card is
  retired; its content (empire, police, jump lanes, stronghold warning)
  becomes a header strip inside the system view.
- **Exit** via Escape key OR an ✕ button in the top-right corner of the
  system view. Clicking empty space never exits.
- **Inert empty space**: in both views, a click that hits neither a system
  node nor an agent dot does nothing (no zoom, no deselect, no close).
- **Forgiving hit radius**: clicks/hovers resolve to the nearest eligible
  target within a generous radius (scaled with zoom, per the existing
  `findNearest` pattern in `frontend/src/components/system/SystemMap.tsx`).
  Agent dots win over the system node / POI when both are in range.
- **Scroll-to-zoom + drag-to-pan** inside the system view.

## Approach

New purpose-built `SystemView` component under
`frontend/src/components/overmind/`, borrowing the wheel-zoom/drag-pan math
from `SystemMap.tsx` but NOT reusing that component — it is ~1000 lines and
coupled to a live `Player` (travel commands, tick interpolation). Alternatives
rejected: generalizing `SystemMap.tsx` (regression risk on the per-agent UI,
wrong aesthetic) and reusing KB static SVGs (static sibling repo, no live
data).

## Backend (pkg/ovdash — read-only, like everything in the package)

- Load the KB `pois` table at startup alongside systems (a few thousand rows
  total). Fields per POI: `id, name, type, class, position_x, position_y`.
  Skip `hidden` POIs.
- New endpoint `GET /api/overmind/system/{id}/pois` →
  `[{id, name, type, class, x, y}]`. 404 on unknown system id; empty array
  for a known system with no POIs.
- Jump gates need no backend support: gate angle = bearing from the viewed
  system to each connected system, computed client-side from galaxy
  positions already loaded (matches KB page placement, e.g. Sol gate at the
  bottom of Alpha Centauri).

## Frontend

### View state & shell (`OvermindPage.tsx`)

- View state: `'galaxy' | { systemId: string }`. Clicking a system switches
  to the system view; Escape / ✕ returns.
- `GalaxyMap` stays mounted but hidden while the system view is shown, so
  its pan/zoom and fetched data survive the round trip.
- New lifted state `highlightedAgentIds: string[]` (from POI/dot hover)
  passed to `FleetRail`.
- `SystemPanel.tsx` deleted; its rows move into the system view header.

### `SystemView.tsx` (new)

- SVG orbital render, KB aesthetic on the overmind palette (`#0a0a08`
  background family): sun at center with glow, dashed orbit ring per POI
  orbital radius, planets colored by `class`, station as hexagon, ice
  field / asteroid belt as chunk scatter along its ring, gate crosshairs on
  the periphery labeled with the destination system name, POI labels.
- Header strip: system name, empire, police level, jump lane count,
  stronghold warning, agent count, ✕ button.
- Wheel zoom + drag pan (logic borrowed from `SystemMap.tsx`; markers scale
  sub-linearly with zoom).
- Data: fetch `/api/overmind/system/{id}/pois` on open; cache per systemId
  in a module-level Map for the session.

### Agent dots

- Agents with `system_id === viewed system` are matched to a POI by name
  (case-insensitive), falling back to POI id match. Unmatched agents render
  in an "unplaced" tray at the bottom of the view so nobody silently
  disappears.
- Dots are fleet-colored (existing `FLEETS` palette), arranged in a small
  arc beside their POI so multiple agents at one POI stay individually
  hoverable. Docked agents get an anchor ring.
- Click a dot → select agent (existing `selectedAgent` flow).

### Hover interactions

- Hover a POI (forgiving radius) → tooltip card listing our agents there
  (agent id, role, activity, docked ⚓) AND all their ids lift to
  `highlightedAgentIds` → matching `FleetRail` rows get a highlight ring.
- Hover a single agent dot → highlight just that row.
- (Reverse direction — rail hover highlighting the dot — is out of scope
  for this iteration.)

### Movement animation (driven by existing SSE deltas)

- Same-system POI change (`updated` delta): dot glides to the new POI
  (~1.5s CSS transition).
- Departure (`moved` delta with `from_system_id` = viewed system): dot
  sprints at fast speed (~0.8s) to the jump gate pointing at the
  destination system, then fades out.
- Arrival (`moved` delta into the viewed system): dot fades in at the
  origin-facing gate and glides to its POI.
- No server-side travel-destination field exists in the stream; all
  animation is inferred from deltas. Dots ease via CSS transitions on
  transform; no requestAnimationFrame loop needed.

## Error handling

- POI fetch failure → header strip still renders (galaxy data), body shows
  a retry-able error line; agent dots fall back to the unplaced tray.
- Unknown/missing POI names from the stream → unplaced tray (never drop an
  agent).
- SSE disconnect handling is unchanged (existing `connected` flag).

## Testing

- Go: unit tests for the new ovdash endpoint (known system with POIs,
  known system without POIs, unknown system 404, hidden POIs excluded).
- `go build ./... && go test ./...`.
- Frontend: `tsc`/vite production build passes.
- Manual: live dashboard, Alpha Centauri (9 agents) — zoom in, hover
  station, confirm rail highlight, watch a departure animation, Escape out,
  confirm galaxy pan/zoom preserved.
