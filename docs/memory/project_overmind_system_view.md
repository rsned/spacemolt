---
name: project_overmind_system_view
description: "Overmind zoomed system view (click system → KB-style orbital view with live agent dots) — MERGED, PUSHED, LIVE on :8091 2026-07-21 incl. click-shield fix 5e31322"
metadata: 
  node_type: memory
  type: project
  originSessionId: 86df3835-f10f-4c25-928d-7948d122ecd7
  modified: 2026-07-22T02:09:10.977Z
---

**Overmind zoomed system view — MERGED to main as `90c6bbc`, PUSHED, LIVE on :8091 (2026-07-21). Branch `feat/overmind-system-view` kept (fully merged).**

**Post-merge fix `5e31322` (pushed, live, user-verified):** FleetOverlay's filled glow circle (r=14, 15% opacity) drew over every agent-occupied system and swallowed its clicks — painted SVG circles hit-test even when nearly transparent. Fix: `pointerEvents="none"` on glow + count badge. Same commit: station glyph now matches server pages — filled cyan hex inside a docking-ring assembly (6 spokes + gray pad circles + thin outer ring). Testing lesson: dispatchEvent bypasses hit-testing, so click tests must use elementFromPoint or real clicks on AGENT-OCCUPIED systems (agent-less ones never had the bug). Also: `:8091` http.FileServer sends no Cache-Control on index.html — browsers cache it; hard refresh (Ctrl+Shift+R) needed after dist rebuilds.

Built via SDD (8 tasks, per-task reviews, final fable review = merge-ready). Head `67a3df5`, base `0a313a3` (main).

What it is: single-click a system on the overmind galaxy map → full map-slot KB-style orbital view (sun, orbit rings, class-colored planets, station hexagons, belt/ice ring scatter, gate crosshairs bearing toward neighbors) with live fleet agent dots fanned beside POIs, POI hover tooltip + FleetRail row highlighting, SSE-driven move animations (glide 1.5s / gate-sprint ghost 0.8s / arrival), wheel zoom + drag pan, Escape/✕ exit, galaxy stays mounted (pan/zoom preserved). SystemPanel retired into the view's header strip.

Backend: `pkg/ovdash` loads KB `pois` at startup (tolerates missing table); `GET /api/overmind/system/{id}/pois` (404 unknown, `[]` known-empty). Frontend: `SystemView.tsx`, `systemLayout.ts`, `useSystemPois.ts` (session cache), GalaxyMap r=14 transparent hit circle.

**Deploy gotcha:** `frontend/dist` was rebuilt during dev, so live :8091 already serves the new UI on refresh — but its Go binary lacks the pois endpoint until restarted (system view shows a retry-able "POI layout failed" line until then). Rebuild + restart :8091 binary to complete rollout. [[reference_overmind_launch_commands]]

Follow-ups (all triaged SHIP by final review): gofmt drift in pkg/ovdash (pre-existed in accounting_test.go/stream_test.go — fold `gofmt -w` into next touch); content-diff before hover Set emit if FleetRail re-renders ever matter; first-open loading indicator; hoist anims/lastPoi ref declarations above the reset effect. Spec deviations accepted: interval-based animation (not CSS transitions), galaxy hit radius fixed not zoom-scaled.

Docs: spec `docs/superpowers/specs/2026-07-21-overmind-system-view-design.md`, plan `docs/superpowers/plans/2026-07-21-overmind-system-view.md`, ledger `.superpowers/sdd/progress.md`.
