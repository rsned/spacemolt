# Overmind Dashboard — Unified Fleet Ops UI (v1) Design

**Date:** 2026-07-20
**Status:** Approved for planning
**Owner page:** new "Overmind" page in `frontend/`, served by new `cmd/overmind-dashboard`

## Goal

One live ops dashboard for the whole bot operation: a real-time galaxy map
showing the location and movement of every worker across all six fleets, a
top-of-page fleet accounting strip (totals, counts, earnings averages), and a
right-hand rail of stylized per-agent detail cards. Read-only in v1; the
layout and server reserve room for manual fleet control (add/remove/halt
agents) later.

## Visual direction

NavCom gold/amber terminal (per the player-dashboard reference corpus,
2026-07-20): near-black background, amber/gold accents, monospace numerals,
letter-spaced caps panel titles, thin-bordered cards. Fleet-colored map dots;
empire-territory blobs remain available as a toggleable layer.

Fixed fleet palette: haul **gold**, mission-learn **cyan**, craft **green**,
mb **violet**, assist **orange**, shuttle **pink**.

## Architecture

```
data/overmind/*-status.json ─┐  (fs poll 2s, mtime-gated)
spacemolt-knowledge.db ──────┤  (galaxy load at startup, RO)
market.db ───────────────────┘  (accounting refresh 30s, RO)
        │
  cmd/overmind-dashboard (:8091, Go stdlib)
        │  /api/systems /api/agents /api/accounting /api/stream (SSE)
        │  + static serve of frontend/dist
        ▼
  frontend/ "Overmind" page (React 19 + Vite)
   ├── GalaxyMap (reused, extended: fleet layer, badges, animation)
   ├── SystemMap (reused: click-through system panel)
   ├── useGalaxyMap (reused, initial load) + useFleetStream (new, SSE)
   ├── AccountingStrip, FleetRail/AgentCard (new)
   └── assets/empire-*.svg (reused)
```

The dashboard binary is **read-only**: it never talks to the game server or
the overmind control sockets. The future control path is POST endpoints on
this same binary that write to the existing `data/overmind/*.sock` control
protocol (drain/resume today; add/remove-worker once
`project_fleet_pool_dynamic_membership` lands). v1 ships none of that — only
a server layout (handler mux, auth-free localhost assumption) that doesn't
preclude it.

### Backend behavior

- **Startup:** load all systems (id, name, x/y, empire, security,
  stronghold, last_visited) and connections from `spacemolt-knowledge.db`
  into memory; build a name→id index (status files carry system *names*,
  e.g. "Haven"; the map keys by id).
- **Fleet snapshot loop (2s):** stat the six status files
  (`fleet`, `mission-learn`, `craft`, `mb`, `assist`, `shuttle`
  `-status.json`); re-parse on mtime change; merge into one snapshot:
  `{fleet, agent_id, role, system_id, poi, docked, credits, hull/max, fuel/max,
  cargo_used/capacity, healthy, restarts, seen, last_seen, activity}`.
- **SSE `/api/stream`:** on snapshot change, push a delta event
  (moved / vitals-changed / joined / left agents). Full-snapshot keyframe on
  connect and every 60s. Accounting event every 30s.
- **`/api/systems` and `/api/agents`:** same JSON shapes the existing
  `useGalaxyMap` hook consumes today, so the reused components work
  unmodified for initial render.
- **`/api/accounting`:** aggregates over `market.db` (see Accounting).

### Frontend behavior

- **`useFleetStream` (new hook):** EventSource subscription; maintains the
  live agent map keyed by `agent_id`; exposes `{agents, accounting,
  staleFleets, connectionState}`. Feeds both the map layer and the rail.
- Initial paint from `/api/systems` + `/api/agents`; stream takes over.

## Page layout

```
┌──┬────────────────────────────────────────────────────────────┐
│N │ FLEET ACCOUNTING strip (full width)                        │
├──┼──────────────────────────────────────────────┬─────────────┤
│A │                                              │ FLEET RAIL  │
│V │              GALAXY MAP                      │ (grouped    │
│  │                                              │  agent      │
│  │                                              │  cards)     │
└──┴──────────────────────────────────────────────┴─────────────┘
```

- **Left nav rail:** icon-only. "Map" is live; grayed placeholders for the
  agreed future pages: Ship, Storage, Facilities, Missions, Economy.
- **Right rail:** collapsible fleet groups, colored to match map dots;
  filter box on top; collapsed group shows a one-line roll-up
  (n agents · ₡ total · worst health).

## Galaxy map

Reuse `GalaxyMap.tsx` (SVG, zoom/pan, empire territory blobs) extended with:

- **Fleet layer:** one dot per worker in fleet color, orbit-offset around
  the system dot so co-located agents don't stack; docked = ring outline,
  in-space = filled.
- **Count badges:** occupied systems get a numbered chip and a soft glow
  tinted by the dominant fleet present (live-traffic reference).
- **Lane highlight:** a lane brightens in fleet color while an agent
  traverses it.
- **Movement animation:** on a system-change delta, animate the dot along
  the connecting lane over ~2s with a short fading trail; non-adjacent jumps
  (missed intermediate hops between polls) animate on a straight line.
- **Layer toggles:** legend chip row toggles per-fleet visibility; empire
  territories and unexplored-dimming are toggles too.
- **Interactions:** hover tooltip (agent or system summary); click system →
  side panel (reused `SystemMap` orbital view + system facts + agents
  present); click agent dot → highlight + scroll/expand its rail card.

## Agent cards

NavCom session-card style:

```
┌ fighter-4 ────────────────── ◉ ┐   name · health dot (green/amber/red)
│ Nova Terra / nova_terra_central ⚓ │  location · dock glyph
│ ₡ 7,228  hull ██████ 350/350  fuel ██▌ 92/240 │
│ cargo ▏110/800                  │
│ ► Freight 4753adb… → nova_terra_central │  activity line
│ restarts 0 · seen 3s            │
└─────────────────────────────────┘
```

- Unhealthy (`healthy:false`) or unseen agents float to the top of their
  group with a red edge.
- Clicking a card centers the map on that agent.
- Card footer is the reserved slot for future halt/override controls
  (duke-arb2 reference).

## Accounting strip

- **Total credits:** sum of live worker wallets (no storage valuation v1).
- **Earnings/hr, trailing 24h,** per source and combined, plus
  per-active-agent average:
  - `haul_results` net
  - `freight_results` `carrier_payout − fuel_cost`
  - `mission_results` net
- **Health:** healthy/total counts, summed restarts, oldest `captured_at`.
  A stale status file (>60s) shows an amber "fleet stale" chip rather than
  silently frozen numbers.
- Numbers animate on change; a tick/last-update indicator sits at the right
  edge.

## Error handling

- Missing/corrupt status file → that fleet greys out in rail and map with a
  "stale since …" banner; other fleets unaffected.
- Unknown system name (name→id miss) → agent shown in an "off-map" tray at
  the map edge, never silently dropped.
- SSE disconnect → client auto-reconnects (EventSource default) and repaints
  from the next keyframe; connection state shown in the accounting strip.
- market.db locked/busy → accounting keeps last-good values with the stale
  chip; RO connections with busy_timeout.

## Testing

- Go: table-driven tests for snapshot merge, name→id resolution, delta
  computation, and accounting SQL against fixture DBs/status files; SSE
  handler via `httptest` (connect, keyframe, delta, reconnect).
- Frontend: `npm run build` must pass (tsc); component logic that computes
  (badges, offsets, animation paths) lives in pure functions with vitest
  tests only if vitest already exists — otherwise keep logic in the hook
  and rely on tsc + manual verification (no new test framework in v1).

## Future pages (nav placeholders only in v1)

- **Ship view** — trails-style wireframe with shields/armor/module detail
  (needs full-page space).
- **Storage explorer** — per-agent, per-station; must paginate/search
  (craftsman-1: ~2.6M items across 15 stations).
- **Facilities tracker** — cross-system facility status.
- **Mission details** — popup panels or full detail pages.
- **Economy** — fold market-dashboard (:8090) views in natively.
- **Manual control** — halt/override per agent card; add/remove workers via
  overmind sockets once dynamic membership exists.

## Out of scope (v1)

Auth, remote exposure (localhost tool), historical position playback,
per-POI-level positioning on the galaxy map (system granularity only;
POI shows in card/tooltip text), writing to any socket or DB.
