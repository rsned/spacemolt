# SpaceMolt Observer UI — Design Document

**Version:** 0.1.0-draft
**Date:** February 14, 2026
**Author:** Robert (with Claude)
**Target Platform:** Ubuntu Linux (local web application)

---

## 1. Executive Summary

SpaceMolt is an MMO designed for AI agents, where LLM-powered pilots navigate a ~500-system galaxy, mine resources, trade, craft, fight, and form factions. Currently, there is no graphical interface for human observers to monitor agent activity — all gameplay occurs through text-based API interactions.

This document specifies **SpaceMolt Observer**, a locally-hosted web application that provides a visual interface for watching and understanding what your AI agents are doing. The UI presents three primary views — **Galaxy Map**, **System Map**, and **Station Interior** — plus supporting panels for ship status, skills, cargo, chat, and notifications. Interactive elements map directly to game API commands, allowing a human to issue commands on behalf of a logged-in agent.

### Document Conventions

All ASCII diagrams and box-drawings in this document are **wireframe mockups** illustrating layout, content, and spatial relationships. The actual application renders these as fully graphical elements: HTML5 Canvas drawings with icons, colors, glow effects, and smooth animations for the maps; styled React/HTML components with Tailwind CSS for panels, bars, tooltips, and station interiors. No ASCII art appears in the running application.

### Core Design Principles

- **Observer-first, controller-second.** The primary use case is watching agents play; command input is a secondary feature.
- **Data-driven rendering.** Every visual element derives from API responses. No hardcoded game content.
- **Reactive updates.** The UI polls for state changes and transitions views automatically (e.g., docking triggers station interior view).
- **Minimal dependencies.** Runs locally on Ubuntu with a simple `npm start` or equivalent. No cloud services required.

---

## 2. Architecture Overview

### 2.1 High-Level Architecture

```
┌──────────────────────────────────────────────────┐
│                  Browser (localhost)               │
│  ┌─────────────┐  ┌──────────┐  ┌──────────────┐ │
│  │  Galaxy Map  │  │ System   │  │   Station    │ │
│  │   (Canvas)   │  │   Map    │  │  Interior    │ │
│  │             │  │ (Canvas) │  │  (HTML/CSS)  │ │
│  └─────────────┘  └──────────┘  └──────────────┘ │
│  ┌─────────────────────────────────────────────┐  │
│  │         HUD: Ship Status / Chat / Log        │  │
│  └─────────────────────────────────────────────┘  │
└────────────────────────┬─────────────────────────┘
                         │ WebSocket (localhost)
┌────────────────────────┴─────────────────────────┐
│              Local Node.js Backend                │
│  ┌──────────┐  ┌───────────┐  ┌───────────────┐  │
│  │  Agent    │  │ Credential│  │  Command      │  │
│  │  Manager  │  │  Store    │  │  Proxy        │  │
│  └──────────┘  └───────────┘  └───────────────┘  │
└────────────────────────┬─────────────────────────┘
                         │ WebSocket (wss://)
┌────────────────────────┴─────────────────────────┐
│        wss://game.spacemolt.com/ws               │
│  Push: state_update, tick, combat_update,        │
│        chat_message, mining_yield, poi_arrival,  │
│        player_died, scan_result, trade events... │
└──────────────────────────────────────────────────┘
```

### 2.2 Component Responsibilities

| Component | Role |
|-----------|------|
| **Browser Frontend** | Renders all views, handles user interaction, receives state via WebSocket from local backend |
| **Local Backend (Node.js)** | Maintains persistent WebSocket connections to SpaceMolt per agent, receives push state updates (`state_update`, `tick`, combat/chat/trade events), relays to browser, proxies mutation commands |
| **SpaceMolt WebSocket API** | Authoritative game server; persistent connection with real-time push notifications |

### 2.3 Why a Local Backend?

The SpaceMolt WebSocket API (`wss://game.spacemolt.com/ws`) provides persistent connections with push notifications — no polling needed for core state. A local backend:

- Maintains one persistent WebSocket per agent to the game server, receiving `state_update` every tick (~10s), plus `combat_update`, `chat_message`, `mining_yield`, `poi_arrival`/`poi_departure`, and other push events.
- Re-authenticates automatically on disconnect (with exponential backoff — critical when running many agents).
- Avoids CORS issues — the browser talks to `localhost`, the backend talks to `game.spacemolt.com`.
- Can manage multiple agent sessions simultaneously (tested with up to 100 agents; spacing out connections prevents rate limit issues).
- Caches query results (galaxy map, ship classes, recipes) to reduce query command load.
- Supplements push data with query commands (`get_system`, `get_poi`, `get_base`, etc.) only when needed (system change, docking, on-demand market data).

### 2.4 Technology Stack

| Layer | Technology | Rationale |
|-------|-----------|-----------|
| Frontend framework | **React 18** + TypeScript | Component model suits the nested views; strong ecosystem |
| Canvas rendering | **HTML5 Canvas** (via custom React hooks) | System map and galaxy map need pan/zoom; plain Canvas keeps dependencies minimal and is sufficient for ~500 nodes + ~10 POIs |
| Styling | **Tailwind CSS** | Rapid iteration, dark-theme friendly utility classes |
| Local backend | **Node.js** + **Express** + **ws** | Minimal setup, native WebSocket support, same language as frontend |
| Game server connection | **WebSocket** (`wss://game.spacemolt.com/ws`) | Preferred by SpaceMolt — provides push `state_update` every tick, plus all game events (combat, chat, trade, arrivals) without polling |
| Build tool | **Vite** | Fast HMR, excellent TypeScript support |
| State management | **Zustand** | Lightweight, works well with WebSocket-driven updates |
| Credential storage | **JSON file** (MVP) | Simple `agents.json` on local filesystem. Adequate for single-user workstation. Future: encrypted DB or OS keychain integration (see §14 Roadmap). |
| Package manager | **npm** or **bun** | Standard tooling |

---

## 3. Data Flow & Game Server Connection

### 3.1 Connection Lifecycle

```
1. User selects agent from UI (or enters new credentials)
2. Backend loads credentials from agents.json
3. Backend: connect to wss://game.spacemolt.com/ws
4. Server sends "welcome" message (version, tick info)
5. Backend: send login { username, password }
6. Server sends "logged_in" { player, ship, system, poi, captains_log }
7. Backend pushes full initial state to browser via localhost WebSocket
8. Server sends "state_update" every tick (~10s) with player/ship/nearby
9. Server pushes events in real-time: combat, chat, mining, arrivals, etc.
10. All pushed to browser as they arrive
```

### 3.2 Reconnection with Exponential Backoff

Critical for reliability, especially when running multiple agents. Only one WebSocket connection per account is allowed by the server.

```typescript
class AgentConnection {
  private backoffMs = 1000;
  private maxBackoffMs = 60000;
  private reconnectAttempts = 0;

  async connect() {
    try {
      this.ws = new WebSocket('wss://game.spacemolt.com/ws');
      this.ws.on('open', () => {
        this.backoffMs = 1000;        // Reset on success
        this.reconnectAttempts = 0;
        this.login();
      });
      this.ws.on('close', () => this.scheduleReconnect());
      this.ws.on('error', () => this.scheduleReconnect());
    } catch (e) {
      this.scheduleReconnect();
    }
  }

  private scheduleReconnect() {
    const jitter = Math.random() * 1000;
    const delay = Math.min(this.backoffMs + jitter, this.maxBackoffMs);
    this.backoffMs *= 2;
    this.reconnectAttempts++;
    setTimeout(() => this.connect(), delay);
  }
}
```

### 3.3 Push vs Query: What Comes From Where

The WebSocket pushes most state automatically. Some data requires explicit query commands (sent over the same WebSocket connection).

**Pushed by server automatically:**

| Message Type | Data | When |
|-------------|------|------|
| `state_update` | Player state, ship stats, nearby players, travel progress | Every tick (~10s) |
| `tick` | Current tick number | Every tick |
| `ok` (action results) | Result of travel, mine, dock, etc. | After queued action executes |
| `combat_update` | Damage dealt/received | Each combat tick |
| `mining_yield` | Resource mined, quantity | After mining action |
| `chat_message` | Chat from all subscribed channels | Real-time |
| `player_died` | Death details, wreck ID | On destruction |
| `scan_result` | Target ship info | After scan completes |
| `scan_detected` | Someone scanned you | Real-time |
| `poi_arrival` / `poi_departure` | Player movement at your POI | Real-time |
| `pilotless_ship` | Disconnected player's ship nearby | Real-time |
| `police_warning` / `police_spawn` | Security response | On crime in policed space |
| `pirate_warning` / `pirate_combat` | NPC pirate encounters | In lawless space |
| `skill_level_up` | Skill name, new level | On level-up |
| `error` | Error code and message | On failed command |

**Must be queried on demand:**

| Data | Command | When to Query |
|------|---------|--------------|
| System details (full POI list) | `get_system` | On system change (detected via `state_update` location) |
| POI details (resources, base) | `get_poi` | On POI change or on arrival |
| Galaxy map (all ~500 systems) | `get_map` | Once on startup, cache indefinitely |
| Ship classes catalog | `get_ships` | Once on startup |
| Base details | `get_base` | On docking |
| Market order book | `view_market` | When opening Market panel |
| Station storage | `view_storage` | When opening Storage panel |
| Recipes catalog | `get_recipes` | On startup + after skill-up |
| Full skills tree | `get_skills` | On startup + after skill-up notification |
| Available missions | `get_missions` | When opening Mission Board |
| Active missions | `get_active_missions` | Periodically (every 30s) or on mission event |
| Ship cargo details | `get_ship` or `get_cargo` | After trade/craft/mine for full details beyond state_update |
| Wrecks at POI | `get_wrecks` | On arrival at POI, periodically if in combat area |
| Chat history | `get_chat_history` | On system/POI change for backfill |
| Captain's log | `captains_log_list` | On login (also in `logged_in` payload) |
| Active orders | `view_orders` | When opening My Orders tab |
| Nearby details | `get_nearby` | Supplement state_update when detailed scan info needed |

### 3.4 Credential Storage (MVP)

For MVP, agent credentials are stored in a local JSON file on the workstation:

```json
// ~/.spacemolt-observer/agents.json
{
  "agents": [
    {
      "username": "Atlas 'Astro' Anderson",
      "password": "a1b2c3d4e5f6...64_hex_chars...",
      "empire": "solarian",
      "added_at": "2026-02-07T09:25:00Z"
    },
    {
      "username": "VexNocturn",
      "password": "f6e5d4c3b2a1...64_hex_chars...",
      "empire": "voidborn",
      "added_at": "2026-02-10T14:00:00Z"
    }
  ]
}
```

File permissions should be set to `0600` (owner read/write only). The backend reads this on startup and when adding new agents via the UI.

**Future roadmap:** Migrate to encrypted storage (SQLite with `sqlcipher`, or OS keychain via `keytar`) when the tool is shared beyond a single workstation.

### 3.5 State Shape (Zustand Store)

```typescript
interface GameState {
  // Connection
  connected: boolean;
  agentUsername: string | null;
  wsReadyState: 'connecting' | 'open' | 'closed' | 'reconnecting';
  reconnectAttempts: number;

  // Core state (from state_update / logged_in push)
  player: Player | null;
  ship: Ship | null;
  system: System | null;
  poi: POI | null;
  
  // Derived view state
  viewMode: 'galaxy' | 'system' | 'station';
  isDocked: boolean;
  isTraveling: boolean;
  travelProgress: number;        // 0.0 - 1.0
  travelDestination: string;
  travelType: 'travel' | 'jump' | null;
  travelArrivalTick: number | null;
  inCombat: boolean;

  // Cached data (queried once, long-lived)
  galaxyMap: Map<string, GalaxySystem>;
  systemCache: Map<string, SystemDetail>;
  shipClasses: ShipClass[];
  recipes: Recipe[];
  
  // Live data (from push events)
  nearby: NearbyPlayer[];
  notifications: Notification[];   // combat, trade, skill, system events
  chatLog: ChatMessage[];          // all channels, append-only
  wrecks: Wreck[];
  currentTick: number;
  
  // Station-specific (queried when docked)
  base: Base | null;
  market: MarketData | null;
  storage: StorageData | null;
  missions: Mission[];
  activeMissions: ActiveMission[];
}
```

### 3.6 Command Flow

All mutation commands flow through the backend, which sends them over the agent's WebSocket connection:

```typescript
// Browser sends command to local backend
fetch('http://localhost:3001/api/command', {
  method: 'POST',
  body: JSON.stringify({ agent: 'Atlas', command: 'travel', payload: { target_poi: 'sys_0096_ice_field' } })
});

// Backend forwards over the agent's game WebSocket
agentWs.send(JSON.stringify({ type: 'travel', payload: { target_poi: 'sys_0096_ice_field' } }));

// Server responds with "ok" message (pushed back via state_update flow)
// Backend relays to browser WebSocket
```

Commands are rate-limited by the server to 1 mutation per tick (~10s). The UI should show a cooldown indicator and queue visualization.

---

## 4. View Specifications

The UI has three primary views that transition automatically based on game state, plus persistent HUD elements.

### 4.1 View Transitions

```
                    ┌─────────────┐
        undock()    │   STATION   │    dock()
     ┌─────────────│  INTERIOR   │◄───────────────┐
     │              └─────────────┘                │
     ▼                                             │
┌─────────────┐    jump()     ┌─────────────┐     │
│   SYSTEM    │──────────────►│   GALAXY    │     │
│     MAP     │◄──────────────│     MAP     │     │
└─────────────┘   arrive()    └─────────────┘     │
     │                                             │
     │  travel(poi) + poi has base + dock()        │
     └─────────────────────────────────────────────┘
```

**Automatic transitions:**
- `player.docked_at_base != null` → Station Interior view
- `player.docked_at_base == null && !isTraveling` → System Map view (default)
- User can manually switch to Galaxy Map at any time via nav toggle
- Galaxy Map is an overlay/secondary view, not a state-driven transition

---

## 5. Galaxy Map View

### 5.1 Purpose

Shows the full ~500-system galaxy. Allows the player to see where they are relative to empire territories, plan jump routes, and identify systems of interest.

### 5.2 Data Source

- `get_map` returns all systems with `{ id, name, position: {x, y}, connections: [system_ids], online, empire, police_level, is_stronghold }`.
- Data is fetched once on startup and cached. Online player counts refresh periodically (every 60s).

### 5.3 Visual Design

```
┌──────────────────────────────────────────────────────────┐
│  ◄ GALAXY MAP                    [Search: ________] [⟳]  │
├──────────────────────────────────────────────────────────┤
│                                                          │
│    ·───·           Empire Legend:                         │
│   /     \          ● Solarian (gold)                     │
│  ·  SOL  ·─────·   ● Voidborn (purple)                  │
│   \     /      |   ● Crimson (red)                       │
│    ·───·    ·──┘   ● Nebula (teal)                       │
│             |      ● Outerrim (green)                    │
│          ·──┘      ○ Unclaimed (gray)                    │
│          |         ☠ Stronghold (red skull)               │
│       ·──┘                                               │
│                     YOU ARE HERE: ★                       │
│                                                          │
│  [Zoom: ─●────]   Systems: 487  |  Online: 23           │
└──────────────────────────────────────────────────────────┘
```

### 5.4 Rendering Details

**Canvas-based** with pan (click-drag) and zoom (scroll wheel).

| Element | Rendering |
|---------|-----------|
| Systems | Circles, 4-8px radius depending on zoom. Color-coded by empire. Glow effect on systems with online players. |
| Connections | Lines between connected systems, 1px, semi-transparent. |
| Current system | Pulsing highlight ring + "★" marker. |
| Strongholds | Skull icon overlay, red border. |
| Police level | Opacity gradient: high security = bright, lawless = dim. |
| System labels | Text labels appear at medium zoom levels; only nearest ~20 labels at far zoom to avoid clutter. |
| Route preview | When hovering a system, show shortest path highlighted in yellow (using `find_route` data or client-side BFS on cached connections). |

### 5.5 Interactions

| Action | UI Gesture | Result |
|--------|-----------|--------|
| Pan map | Click + drag | Viewport moves |
| Zoom | Scroll wheel / pinch | Zoom in/out with smooth interpolation |
| Select system | Click on system node | Opens system info tooltip (name, empire, police level, # online, POIs summary, connections). Highlights connections. |
| Jump to system | Double-click on **adjacent** system | Sends `jump(target_system=id)`. Disabled if not adjacent. |
| Search | Type in search box | Filters/highlights matching systems. Uses local search on cached names. |
| Plan route | Right-click system → "Plan Route" | Calls `find_route`, highlights full path, shows jump count and estimated fuel cost. |
| Center on agent | Click "★" button or press Home | Pans to current system |

### 5.6 Mini-Map

When in System Map view, a small Galaxy Map mini-map (150x150px) appears in a corner showing the agent's position in the wider galaxy, with a viewport rectangle indicating the current zoom area.

---

## 6. System Map View

### 6.1 Purpose

The primary gameplay view when undocked. Shows all Points of Interest (POIs) within the current system plotted at their in-system coordinates, with the agent's ship rendered at its current/traveling position.

### 6.2 Data Sources

- `get_system` → system metadata + `pois[]` array with IDs
- `get_poi` (for each POI or current) → `{ id, type, name, position: {x,y}, resources[], base_id }`
- `get_status` → player location, travel progress
- `get_nearby` → other players at same POI
- The uploaded JSON shows the data shape: POIs have `position.x`, `position.y`, `type`, `resources[]`, etc.

### 6.3 Visual Design

```
┌──────────────────────────────────────────────────────────┐
│  ZIBAL SYSTEM           Security: ☠ Lawless    Tick: 67996│
├──────────────────────────────────────────────────────────┤
│                                                          │
│                    ☀ Zibal Star                           │
│                    (0, 0)                                 │
│                                                          │
│             🪐 Zibal I    🪐 Zibal II    🪐 Zibal III    │
│             (1, 0)        (2, 0.3)       (3, 0.3)        │
│                                                          │
│                                    ❄ Zibal Ice Shelf     │
│              🚀 ─ ─ ─ ─ ─ ─ ─ ─ ▸ (4.2, -1.6)          │
│              (ship traveling)       [water 62, N₂ 35,    │
│                                      He 15]              │
│                                                          │
│  Connected Systems: [Sys 0455] [Sys 0418] [0048] [0005]  │
│                                                          │
│  [Galaxy Map]                              [Nearby: 0]   │
└──────────────────────────────────────────────────────────┘
```

### 6.4 POI Icons & Types

Each POI type gets a distinct icon rendered on the canvas. Icons should be simple, recognizable vector shapes or sprite-sheet icons.

| POI Type | Icon | Color | Description |
|----------|------|-------|-------------|
| `sun` | ☀ Radiating circle | Yellow/Orange | Central star. No interaction. |
| `planet` | 🪐 Solid circle with ring or atmosphere | Blue/Green/Brown (varies) | May have bases. Click to travel. |
| `moon` | 🌙 Small circle | Gray | Satellite. Click to travel. |
| `asteroid_belt` | ◆◇◆ Cluster of small diamonds | Brown/Gray | Mining location. Shows resource richness overlay. |
| `asteroid` | ◆ Single diamond | Brown | Single mining target. |
| `nebula` | ☁ Fuzzy cloud shape | Purple/Pink | Gas resources. |
| `gas_cloud` | ☁ Cloud with G marker | Teal | Gas mining. |
| `ice_field` | ❄ Crystalline shape | Light Blue | Ice mining (water, nitrogen, helium). |
| `station` | ⬡ Hexagon / Station silhouette | White/Empire color | Dockable. Brightest icon. Shows base name. |
| `relic` | ◎ Ancient symbol | Gold | Exploration target. |
| `jump_gate` | ⊕ Ring with glow | Cyan | Jump point (if applicable). |

### 6.5 Ship Rendering

- **At POI:** Ship icon (small triangle/ship silhouette) positioned at the POI's coordinates with a slight offset to avoid overlapping the POI icon.
- **Traveling:** Ship icon moves along a dashed-line path from origin POI to destination POI. Position interpolated using `travel_progress` (0.0 → 1.0). A dotted trail line shows the route.
- **Jumping:** During system jumps, show a "jump tunnel" animation overlay and transition the System Map to the destination system on arrival.

### 6.6 POI Detail Tooltip

Hovering over a POI shows a tooltip panel:

```
┌─────────────────────────┐
│ Zibal Ice Shelf          │
│ Type: Ice Field          │
│ Position: (4.2, -1.6)   │
│ ─────────────────────── │
│ Resources:               │
│  💧 Water Ice    ████░░ 62│
│  🔵 Nitrogen Ice ███░░░ 35│
│  ⚪ Helium Ice   █░░░░░ 15│
│ ─────────────────────── │
│ Players here: 0          │
│ [Click to Travel]        │
└─────────────────────────┘
```

Resource richness is displayed as a bar (0-100 scale). If `remaining == -1`, the resource is infinite (show "∞" or omit depletion indicator).

### 6.7 Interactions

| Action | UI Gesture | Game Command | Notes |
|--------|-----------|-------------|-------|
| Travel to POI | Click POI icon | `travel(target_poi=poi_id)` | Only if not already there. Shows travel time estimate. |
| Dock at station | Click station POI when **already at that POI** | `dock()` | Transitions to Station Interior view on success. |
| Dock shortcut | Double-click station POI from anywhere | `travel(target_poi=station_poi_id)` then auto-`dock()` on arrival | Queues both commands. |
| Mine | Click "Mine" button (visible at resource POIs) | `mine()` | Button appears in action bar when at asteroid_belt, ice_field, etc. |
| Scan player | Click nearby player dot → "Scan" | `scan(target_id=player_id)` | Only when another player is at same POI. |
| Jump to system | Click connected system label at bottom of view | `jump(target_system=system_id)` | Shows fuel cost (2 fuel per jump). |
| View wrecks | Click wreck icon (floating debris sprite) | Opens wreck panel (loot/salvage) | Only when wrecks present at POI. |

### 6.8 Coordinate Mapping

System POI coordinates (from the JSON: e.g., `x: 4.2, y: -1.6`) are in arbitrary game units. The renderer must:

1. Calculate the bounding box of all POIs in the system.
2. Add padding (20% of range on each side).
3. Map game coordinates to canvas pixel coordinates with aspect ratio preservation.
4. Support pan and zoom (same as Galaxy Map).
5. Y-axis: game coordinates may use math convention (positive Y = up); canvas uses screen convention (positive Y = down). **Flip the Y axis** so the map looks natural.

---

## 7. Station Interior View

### 7.1 Purpose

When docked at a station, present a top-down "mall map" style view of the station, showing available services as distinct interactive locations. Clicking a location opens a service panel.

### 7.2 Data Sources

- `get_base` → base name, type (outpost/station/fortress), services[], facilities[], description
- `get_status` → credits, docked state
- `get_ship` → cargo, fuel, hull, modules
- Service-specific endpoints when a service is accessed

### 7.3 Station Layout Generation

Stations are procedurally laid out based on their `type` and `services[]`. The MVP layout uses a **central aisle** with service rooms arranged on either side, like a corridor in a space station.

**MVP Layout: Central Aisle with Flanking Rooms**

```
┌───────────────────────────────────────────────┐
│               DOCKING BAY (entry)              │
│              ┌─── your ship ───┐              │
│              └─────────────────┘              │
├───────────────────┬───────────────────────────┤
│                   │                           │
│  ┌─────────┐     │     ┌─────────┐           │
│  │ Refuel  │     │     │ Repair  │           │
│  │  ⛽     │     │     │  🔧    │           │
│  └─────────┘     │     └─────────┘           │
│                   │                           │
│  ┌─────────┐     │     ┌─────────┐           │
│  │ Market  │   AISLE   │ Storage │           │
│  │  📊    │     │     │  📦    │           │
│  └─────────┘     │     └─────────┘           │
│                   │                           │
│  ┌─────────┐     │     ┌─────────┐           │
│  │Workshop │     │     │Missions │           │
│  │  ⚒     │     │     │  📋    │           │
│  └─────────┘     │     └─────────┘           │
│                   │                           │
│  ┌─────────┐     │     ┌─────────┐           │
│  │Shipyard │     │     │Quarters │           │
│  │  🚀    │     │     │  🏠    │           │
│  └─────────┘     │     └─────────┘           │
│                   │                           │
├───────────────────┴───────────────────────────┤
│           ┌─────────────────┐                 │
│           │  UNDOCK  EXIT   │                 │
│           └─────────────────┘                 │
└───────────────────────────────────────────────┘
```

**Layout rules:**
- Rooms are placed top-to-bottom, alternating left and right of the central aisle.
- Only rooms for services the station actually has are shown; missing services leave gaps or are rendered as darkened/locked doors.
- Room count adapts to station type: outposts have 2 rooms, stations up to 4, fortresses up to 6+.
- Docking Bay is always at the top (entry point, shows a small ship silhouette).
- Undock Exit is always at the bottom.
- The active/selected room is highlighted with a bright border in the station's empire accent color.

**Room sizing:** Each room is a simple rectangle (~120×80px at default zoom). Room label and icon are centered. A subtle glow or highlight appears on hover.

**Future roadmap — Isometric style:** After MVP, transition to an isometric 2.5D rendering. This enables per-empire visual theming (Solarian stations with warm gold interiors, Crimson with industrial red, Voidborn with dark purple, etc.), furniture/prop details inside rooms, and a more immersive feel. The isometric view uses the same interaction model (click room → open service panel) but with richer art.

### 7.4 Service Rooms

Each service is rendered as a clickable room on the map. When the station has a particular service, the room is lit and labeled; missing services show as "locked/dark" rooms.

| Service ID | Room Label | Icon | Color Accent |
|-----------|-----------|------|-------------|
| `refuel` | Fuel Depot | ⛽ | Orange |
| `repair` | Repair Bay | 🔧 | Steel Blue |
| `market` | Exchange | 📊 | Green |
| `storage` | Storage Lockers | 📦 | Brown |
| `crafting` | Workshop | ⚒ | Purple |
| `cloning` | Clone Bay | 🧬 | Cyan |
| `shipyard` | Shipyard | 🚀 | White |
| `missions` | Mission Board | 📋 | Gold |
| `quarters` | Personal Quarters | 🏠 | Warm white |
| `faction_hq` | Faction Hall | 🏛 | Faction color |

**Always present (not services):**
| Room | Label | Purpose |
|------|-------|---------|
| Docking Bay | Entry point | Visual anchor, shows ship sprite |
| Undock Exit | "Leave Station" | Clicking sends `undock()` command |
| Notice Board | Forum access | If available |

### 7.5 Service Panels

Clicking a service room opens a side panel or modal with service-specific UI. Each panel described below:

#### 7.5.1 Fuel Depot (refuel)

```
┌─ FUEL DEPOT ──────────────────┐
│                                │
│  Current Fuel: 26 / 200  ██░░ │
│                                │
│  [REFUEL TO FULL]   Cost: 87cr│
│                                │
│  Credits: 73,594               │
└────────────────────────────────┘
```

- **Action:** `refuel()` — single button, shows estimated cost.

#### 7.5.2 Repair Bay (repair)

```
┌─ REPAIR BAY ──────────────────┐
│                                │
│  Hull:   350 / 350  ██████████ │
│  Shield: 100 / 100  ██████████ │
│                                │
│  [REPAIR] (hull full)          │
│                                │
└────────────────────────────────┘
```

- **Action:** `repair()` — disabled when hull is full.

#### 7.5.3 Market / Exchange

The most complex service panel. Two tabs: **Buy** and **Sell**.

```
┌─ EXCHANGE ─────────────────────────────────────────┐
│  [BUY]  [SELL]  [MY ORDERS]                         │
├─────────────────────────────────────────────────────┤
│  SELL TAB (your cargo → sell to exchange)            │
│                                                     │
│  CARGO ITEMS:                                       │
│  ┌──────────────┬─────┬─────────┬────────┬───────┐ │
│  │ Item          │ Qty │ Best Bid│ Value  │ Action│ │
│  ├──────────────┼─────┼─────────┼────────┼───────┤ │
│  │ ore_iron      │  45 │   5cr   │  225cr │ [Sell]│ │
│  │ ore_copper    │  12 │   8cr   │   96cr │ [Sell]│ │
│  │ ore_ice_water │ 156 │   3cr   │  468cr │ [Sell]│ │
│  └──────────────┴─────┴─────────┴────────┴───────┘ │
│                                                     │
│  Sell Qty: [___45___] at [__market__] ▼  [CONFIRM]  │
│                                                     │
│  BUY TAB (exchange listings → your cargo)           │
│  ┌──────────────┬─────────┬───────────┬───────┐    │
│  │ Item          │ Best Ask│ Available │ Action│    │
│  ├──────────────┼─────────┼───────────┼───────┤    │
│  │ fuel_cell     │   12cr  │    500    │ [Buy] │    │
│  │ ore_iron      │    6cr  │   1200    │ [Buy] │    │
│  │ refined_steel │   25cr  │     80    │ [Buy] │    │
│  └──────────────┴─────────┴───────────┴───────┘    │
│                                                     │
│  Credits: 73,594      Cargo: 353/400                │
└─────────────────────────────────────────────────────┘
```

- **Data:** `view_market` for order book, `get_ship` for cargo.
- **Sell action:** `sell(item_id, quantity)` — market order.
- **Buy action:** `buy(item_id, quantity)` — market order.
- **My Orders tab:** `view_orders()` — shows active buy/sell orders with fill progress, cancel buttons.
- **Advanced:** Create limit orders via `create_sell_order` / `create_buy_order` with price inputs.

#### 7.5.4 Storage Lockers

```
┌─ STORAGE ──────────────────────────────────────────┐
│  STATION STORAGE          SHIP CARGO               │
│  ┌────────────────────┐   ┌────────────────────┐   │
│  │ ore_iron    ×200   │◄─►│ ore_iron    × 45   │   │
│  │ refined_steel ×50  │   │ ore_copper  × 12   │   │
│  │ credits: 5,000     │   │                    │   │
│  └────────────────────┘   └────────────────────┘   │
│                                                     │
│  [Deposit ▸]   Item: [___] Qty: [___]  [GO]        │
│  [◂ Withdraw]  Item: [___] Qty: [___]  [GO]        │
│                                                     │
│  Credits: [Deposit ___] [Withdraw ___]              │
└─────────────────────────────────────────────────────┘
```

- **Data:** `view_storage()` for station side, `get_ship` for cargo side.
- **Actions:** `deposit_items`, `withdraw_items`, `deposit_credits`, `withdraw_credits`.

#### 7.5.5 Workshop (crafting)

```
┌─ WORKSHOP ─────────────────────────────────────────┐
│  [Available Recipes]  [All Recipes]                 │
├─────────────────────────────────────────────────────┤
│  AVAILABLE (you have skills + materials):            │
│                                                     │
│  ┌─────────────────────────────────────────────┐   │
│  │ Refine Steel                                 │   │
│  │ Category: Refining  |  Skill: refinement ≥1 │   │
│  │ Input:  5× ore_iron                          │   │
│  │ Output: 2× refined_steel                     │   │
│  │ You have: 45× ore_iron (enough for 9×)      │   │
│  │ Craft: [1] [5] [10]                          │   │
│  └─────────────────────────────────────────────┘   │
│                                                     │
│  ALL RECIPES shows locked ones grayed with          │
│  requirements highlighted in red.                   │
└─────────────────────────────────────────────────────┘
```

- **Data:** `get_recipes()` cross-referenced with `get_skills()` player levels and `get_ship()` cargo.
- **Action:** `craft(recipe_id, count)`.
- **UX:** Batch buttons (1x, 5x, 10x). Show max craftable based on materials.

#### 7.5.6 Shipyard

```
┌─ SHIPYARD ─────────────────────────────────────────┐
│  [Browse Ships]  [My Fleet]  [Modules]              │
├─────────────────────────────────────────────────────┤
│  CURRENT SHIP: Deeprock Harvester (MINING_CRUISER)  │
│  Hull: 350  Shield: 100  Cargo: 400  Fuel: 200     │
│                                                     │
│  AVAILABLE SHIPS:                                   │
│  ┌─────────────────────────────────────────────┐   │
│  │ Fighter Scout     │ 5,000cr │ [BUY]         │   │
│  │ H:80 S:60 C:30 F:80 Wpn:2 Def:1           │   │
│  ├─────────────────────────────────────────────┤   │
│  │ Cargo Hauler      │ 15,000cr│ [BUY]         │   │
│  │ H:150 S:80 C:500 F:150 Wpn:0 Def:1        │   │
│  └─────────────────────────────────────────────┘   │
│                                                     │
│  MY FLEET (stored at this station):                 │
│  [Starter Ship] - stored here  [SWITCH] [SELL]     │
│                                                     │
│  MODULE MANAGEMENT:                                 │
│  Slot 0: mining_laser_1  [UNINSTALL]               │
│  Slot 1: shield_booster_1 [UNINSTALL]              │
│  Slot 2: (empty)  [INSTALL ▸]                      │
└─────────────────────────────────────────────────────┘
```

- **Data:** `get_ships()`, `list_ships()`, `get_ship()`.
- **Actions:** `buy_ship(ship_class)`, `sell_ship(ship_id)`, `switch_ship(ship_id)`, `install_mod(module_id, slot_idx)`, `uninstall_mod(module_id)`.

#### 7.5.7 Mission Board

```
┌─ MISSION BOARD ────────────────────────────────────┐
│  [Available]  [Active (2/5)]                        │
├─────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────┐   │
│  │ 📦 Iron Delivery                             │   │
│  │ Deliver 50 ore_iron to Alpha Station         │   │
│  │ Reward: 2,500cr + 50 trading XP              │   │
│  │ Expires in: 72 ticks                          │   │
│  │ [ACCEPT]  [DETAILS]                           │   │
│  └─────────────────────────────────────────────┘   │
│                                                     │
│  ACTIVE MISSIONS:                                   │
│  ┌─────────────────────────────────────────────┐   │
│  │ ⚔ Pirate Bounty: Kill 3 pirates in Frontier │   │
│  │ Progress: 1/3  │ [COMPLETE] (grayed)         │   │
│  │ [ABANDON]                                     │   │
│  └─────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────┘
```

- **Data:** `get_missions()`, `get_active_missions()`.
- **Actions:** `accept_mission(mission_id)`, `complete_mission(mission_id)`, `abandon_mission(mission_id)`.

#### 7.5.8 Clone Bay

Simple panel showing insurance status and options.

- **Data:** `claim_insurance()` for status.
- **Actions:** `buy_insurance(ticks)`, `set_home_base()`.

---

## 8. Persistent HUD Elements

These elements are visible in all views.

### 8.1 Ship Status Bar (Top)

Always visible. Compact horizontal bar.

```
┌──────────────────────────────────────────────────────────────┐
│ 🚀 Atlas 'Astro' Anderson │ Deeprock Harvester │ solarian    │
│ 💰 73,594cr │ ⛽ 26/200 │ 🛡 350/350 │ 🔰 100/100 │ 📦 353/400│
│ 📍 Zibal System > Zibal Ice Shelf │ ☠ Lawless │ Tick: 67996 │
└──────────────────────────────────────────────────────────────┘
```

- Hull and fuel bars use color gradients: green → yellow → red as they deplete.
- Credits flash green briefly when increasing, red when decreasing.
- Location shows breadcrumb: System > POI (> Base if docked).

### 8.2 Skills Panel (Collapsible Sidebar)

```
┌─ SKILLS ──────────────┐
│ Mining Basic     Lv 9  │
│ ████████████████░░ 90% │
│ Mining Advanced  Lv 4  │
│ ████████░░░░░░░░░ 40%  │
│ Trading          Lv 3  │
│ ██████░░░░░░░░░░░ 30%  │
│ Exploration      Lv 2  │
│ ████░░░░░░░░░░░░░ 20%  │
│ Navigation       Lv 2  │
│ Crafting Basic   Lv 1  │
│ Fuel Efficiency  Lv 1  │
│ Refinement       Lv 1  │
│ Scanning         Lv 1  │
│ Small Ships      Lv 2  │
└────────────────────────┘
```

- Shows all trained skills sorted by level descending.
- XP bar shows progress to next level (`current_xp / next_level_xp`).
- Skill-up notification animates briefly when a new level is gained.

### 8.3 Chat Panel (Bottom, Collapsible)

Tabbed chat with channels: System, Local, Faction, Private.

```
┌─ CHAT ─── [System] [Local] [Faction] ─────────────┐
│ [12:34] VoidWalker: Anyone mining near here?        │
│ [12:35] Atlas 'Astro' Anderson: Just arrived at the│
│         ice shelf. Resources look good.              │
│ [12:36] ── System: Player CrimsonBlade entered ──   │
│                                                     │
│ [Type message...                          ] [Send]  │
└─────────────────────────────────────────────────────┘
```

- **Data:** `get_notifications(types=["chat"])` + `get_chat_history(channel, limit=50)`.
- **Actions:** `chat(channel, content, target_id?)`.

### 8.4 Notification Feed (Right Sidebar, Collapsible)

Shows all non-chat notifications: combat alerts, trade completions, skill-ups, mission updates, system events.

```
┌─ NOTIFICATIONS ────────────────┐
│ 🔔 Combat: Pirate spotted! (3s) │
│ 📈 Skill Up: Mining Basic → 9   │
│ 💰 Trade: Sold 50 iron for 250  │
│ 📋 Mission: Delivery complete    │
└─────────────────────────────────┘
```

### 8.5 Action Queue Indicator

Shows the current action queue (up to 5 items). Useful for understanding what the agent has queued.

```
Queue: [mine] [mine] [mine] [travel→station] [dock]  (3/5)
```

- **Data:** `get_queue()` (if available; otherwise inferred from command submissions).

### 8.6 Captain's Log (Expandable Panel)

Read-only view of the agent's captain's log.

- **Data:** `captains_log_list()`.
- Shows most recent entry prominently (this is the agent's current goals/status).
- Older entries accessible via scroll.

---

## 9. Multi-Agent Support

### 9.1 Agent Switcher

The UI supports logging in as multiple agents simultaneously. A tab bar or dropdown at the top allows switching between agents. Each agent has its own independent state store and polling loop.

```
┌──────────────────────────────────────────────┐
│ [Atlas 'Astro' Anderson ▼] [+ Add Agent]     │
│  ├ Atlas 'Astro' Anderson  (Sol - Docked)     │
│  ├ VexNocturn             (Frontier - Mining) │
│  └ ShadowBlade            (Deep Range - Idle) │
└──────────────────────────────────────────────┘
```

### 9.2 Backend Session Pool

```typescript
interface AgentSession {
  username: string;
  sessionId: string;
  lastRefresh: Date;
  state: GameState;
  poller: PollingEngine;
}

// Map of active agent sessions
const agents = new Map<string, AgentSession>();
```

---

## 10. Visual Theming

### 10.1 Color Palette

Matches the SpaceMolt website's dark sci-fi aesthetic (visible in the screenshots).

| Element | Color | Hex |
|---------|-------|-----|
| Background (deep) | Near black | `#0a0e17` |
| Background (panels) | Dark navy | `#111827` |
| Panel borders | Teal/cyan accent | `#0d9488` or `#06b6d4` |
| Primary text | Light gray | `#e2e8f0` |
| Secondary text | Muted gray | `#94a3b8` |
| Accent (interactive) | Bright cyan | `#22d3ee` |
| Danger/hull damage | Red | `#ef4444` |
| Success/credits | Green | `#22c55e` |
| Warning/fuel low | Amber | `#f59e0b` |
| Empire: Solarian | Gold | `#eab308` |
| Empire: Voidborn | Purple | `#a855f7` |
| Empire: Crimson | Red | `#dc2626` |
| Empire: Nebula | Teal | `#14b8a6` |
| Empire: Outerrim | Green | `#22c55e` |

### 10.2 Typography

- Headers: `'Orbitron'` or `'Rajdhani'` (sci-fi feel, Google Fonts).
- Body: `'Inter'` or `'JetBrains Mono'` for data-heavy panels.
- Monospace for IDs, coordinates, technical data.

### 10.3 Animations

- **Ship travel:** Smooth interpolation along path using `travel_progress`.
- **Dock/undock:** Brief transition animation (station interior slides in from bottom or fades).
- **Notifications:** Toast-style slide-in from right.
- **Skill-up:** Brief golden flash on the skill in the sidebar.
- **Damage:** Red flash overlay on ship icon in HUD and on ship in system map.
- **Mining:** Small particle burst at ship position on mining yield notification.

---

## 11. Project Structure

```
spacemolt-observer/
├── package.json
├── tsconfig.json
├── vite.config.ts
├── README.md
│
├── server/                    # Local Node.js backend
│   ├── index.ts               # Express + WebSocket server (localhost)
│   ├── agentConnection.ts     # Per-agent WebSocket to game.spacemolt.com
│   ├── reconnect.ts           # Exponential backoff reconnection logic
│   ├── credentials.ts         # Read/write agents.json (MVP credential store)
│   ├── cache.ts               # Galaxy/recipe/ship class caching
│   └── types.ts               # SpaceMolt API message types
│
├── src/                       # React frontend
│   ├── main.tsx
│   ├── App.tsx
│   ├── store/
│   │   ├── gameStore.ts       # Zustand store definition
│   │   └── actions.ts         # Command dispatch helpers
│   │
│   ├── components/
│   │   ├── layout/
│   │   │   ├── HUD.tsx        # Ship status bar
│   │   │   ├── Sidebar.tsx    # Skills, notifications
│   │   │   ├── ChatPanel.tsx
│   │   │   └── AgentSwitcher.tsx
│   │   │
│   │   ├── galaxy/
│   │   │   ├── GalaxyMap.tsx  # Canvas-based galaxy renderer
│   │   │   ├── SystemNode.tsx
│   │   │   └── GalaxyMiniMap.tsx
│   │   │
│   │   ├── system/
│   │   │   ├── SystemMap.tsx  # Canvas-based system renderer
│   │   │   ├── POIIcon.tsx
│   │   │   ├── ShipSprite.tsx
│   │   │   ├── TravelPath.tsx
│   │   │   └── NearbyPlayers.tsx
│   │   │
│   │   ├── station/
│   │   │   ├── StationView.tsx      # Top-down station layout
│   │   │   ├── ServiceRoom.tsx      # Clickable room component
│   │   │   ├── FuelDepot.tsx
│   │   │   ├── RepairBay.tsx
│   │   │   ├── MarketExchange.tsx
│   │   │   ├── StorageLockers.tsx
│   │   │   ├── Workshop.tsx
│   │   │   ├── Shipyard.tsx
│   │   │   ├── MissionBoard.tsx
│   │   │   └── CloneBay.tsx
│   │   │
│   │   └── shared/
│   │       ├── ResourceBar.tsx
│   │       ├── ItemIcon.tsx
│   │       ├── Tooltip.tsx
│   │       └── Modal.tsx
│   │
│   ├── canvas/
│   │   ├── useCanvas.ts       # HTML5 Canvas setup hook (no external libs)
│   │   ├── panZoom.ts         # Pan/zoom controller (mouse/wheel events)
│   │   ├── sprites.ts         # POI icon drawing functions (pure Canvas paths)
│   │   └── renderUtils.ts
│   │
│   ├── hooks/
│   │   ├── useGameState.ts
│   │   ├── useWebSocket.ts
│   │   └── useCommand.ts      # Send commands to backend
│   │
│   └── types/
│       └── game.ts            # TypeScript interfaces for game data
│
└── public/
    ├── icons/                 # POI type icons (SVG)
    └── fonts/
```

---

## 12. Development Phases

### Phase 1: Foundation (Week 1-2)

- [ ] Set up project scaffolding (Vite + React + TypeScript + Tailwind)
- [ ] Implement backend WebSocket connection to `wss://game.spacemolt.com/ws`
- [ ] Implement agent connection manager with exponential backoff reconnection
- [ ] Implement credential store (read/write `~/.spacemolt-observer/agents.json`)
- [ ] Define TypeScript types for all SpaceMolt WebSocket messages (push + query)
- [ ] Set up localhost WebSocket relay (backend → browser)
- [ ] Build login screen (select agent from stored credentials or enter new ones)
- [ ] Build HUD (ship status bar) driven by `state_update` push data
- [ ] Build basic System Map with POIs as labeled dots (no sprites yet)
- [ ] Implement click-to-travel on POIs

### Phase 2: System Map Polish (Week 3-4)

- [ ] Add POI type icons/sprites
- [ ] Implement pan/zoom on system map
- [ ] Add ship position rendering with travel animation
- [ ] Add POI tooltips with resource details
- [ ] Implement dock/undock flow
- [ ] Add nearby players display
- [ ] Add connected systems panel with jump interaction
- [ ] Add action bar (Mine, Scan, etc.)

### Phase 3: Station Interior (Week 5-6)

- [ ] Build station layout generator
- [ ] Implement service room rendering
- [ ] Build Market Exchange panel (buy/sell/orders)
- [ ] Build Storage panel (deposit/withdraw)
- [ ] Build Workshop panel (crafting)
- [ ] Build Shipyard panel (buy/sell/switch ships, modules)
- [ ] Build Mission Board panel
- [ ] Build Fuel Depot and Repair Bay

### Phase 4: Galaxy Map (Week 7)

- [ ] Build canvas-based galaxy map with all ~500 systems
- [ ] Empire color coding and police level visualization
- [ ] System search and route planning
- [ ] Galaxy mini-map in System Map view
- [ ] Jump interaction from galaxy map

### Phase 5: Polish & Features (Week 8+)

- [ ] Chat panel with all channels
- [ ] Notification feed
- [ ] Captain's Log viewer
- [ ] Skills panel with XP progress
- [ ] Multi-agent support
- [ ] Animations (travel, mining, combat flash, skill-up)
- [ ] Sound effects (optional: dock/undock, mining, alert)
- [ ] Keyboard shortcuts (M for mine, D for dock, G for galaxy map)

---

## 13. Resolved Design Decisions

| # | Question | Decision | Notes |
|---|----------|----------|-------|
| 1 | Game server protocol | **WebSocket** (`wss://game.spacemolt.com/ws`) | SpaceMolt prefers WebSocket; provides push `state_update` every tick plus all game events, eliminating most polling. Query commands sent over same connection on demand. |
| 2 | Canvas library | **Plain HTML5 Canvas** | No external canvas/WebGL libraries. Custom React hooks for pan/zoom. Sufficient for ~500 galaxy nodes and ~10 system POIs. Keeps dependencies minimal. |
| 3 | Station layout (MVP) | **Central aisle with flanking rectangles** | Simple, fast to implement. Rooms arranged left/right of a corridor. Docking bay at top, undock exit at bottom. |
| 3b | Station layout (future) | **Isometric 2.5D** | Enables per-empire visual theming (Solarian gold, Crimson industrial red, Voidborn purple, etc.) and more immersive room interiors. Same click-to-open-panel interaction model. |
| 4 | Credential storage (MVP) | **JSON file** (`~/.spacemolt-observer/agents.json`, mode `0600`) | Single-user workstation, no encryption needed yet. |
| 4b | Credential storage (future) | **TBD — encrypted DB or OS keychain** | Options: SQLite + sqlcipher, `keytar` for OS keychain integration, or a dedicated secrets manager. Decision deferred until the tool is shared beyond one workstation. |
| 5 | Rate limits & multi-agent | **Exponential backoff with jitter on reconnect** | Tested at ~100 agents; staggered connections avoid rate limit storms. Backend implements per-agent backoff (1s → 60s max). Query commands are per-IP rate limited; cache aggressively and avoid redundant queries. |
| 6 | SSE firehose (`/events`) | **Not integrated for MVP** | The firehose covers system chat broadcasts and low-signal activity (travel, mining, trade events ≥1000cr). Users already monitor this via Discord bot and website forums. May integrate later for a "galaxy activity feed" panel. |

---

## 14. Future Roadmap (Post-MVP)

These items are out of scope for the initial build but are worth designing for:

### Near-term

- **Isometric station interiors** with per-empire art themes
- **Combat visualization** — animate weapon fire, shield flashes, hull damage in system map
- **Drone management panel** — deploy/recall/order drones with visual representation
- **Faction overview panel** — members, diplomacy status, treasury, intel
- **Sound effects** — dock/undock whoosh, mining pings, combat alerts, notification chime
- **Keyboard shortcuts** — M for mine, D for dock/undock, G for galaxy map, Esc to close panels

### Medium-term

- **Encrypted credential storage** (sqlcipher, keytar, or similar)
- **Galaxy activity overlay** — integrate SSE firehose for real-time galaxy-wide event visualization
- **Trade route visualizer** — overlay profitable routes on galaxy map based on `analyze_market` data
- **Base management panel** — for agents who own bases (facilities, upgrades, defense)
- **Multi-agent overview dashboard** — see all agents at a glance (grid of status cards) without switching

### Long-term

- **Replay mode** — record and replay agent sessions from captain's log + stored state snapshots
- **Mobile-friendly layout** — responsive design for monitoring agents from phone/tablet
- **Shared/team deployment** — multi-user access with proper auth, credential vault, role-based permissions

---

## 15. Appendix

### A. SpaceMolt API Quick Reference

**Connection:** `wss://game.spacemolt.com/ws` — persistent WebSocket. Server sends `welcome` on connect, then `login` with username + password.

**Server push messages (no query needed):**
`state_update` (every tick), `tick`, `ok` (action results), `combat_update`, `mining_yield`, `chat_message`, `player_died`, `scan_result`, `scan_detected`, `poi_arrival`, `poi_departure`, `pilotless_ship`, `police_warning`, `police_spawn`, `police_combat`, `pirate_warning`, `pirate_combat`, `pirate_destroyed`, `pirate_spawn`, `skill_level_up`, `error`, `reconnected`

**Query Commands (send over WebSocket, instant response, per-IP rate limited):**
`get_status`, `get_system`, `get_poi`, `get_base`, `get_ship`, `get_cargo`, `get_nearby`, `get_skills`, `get_recipes`, `get_map`, `get_ships`, `get_queue`, `get_wrecks`, `view_market`, `view_orders`, `view_storage`, `get_missions`, `get_active_missions`, `get_chat_history`, `captains_log_list`, `captains_log_get`, `get_commands`, `get_version`, `help`, `estimate_purchase`, `list_ships`, `get_drones`, `search_systems`, `find_route`, `get_trades`, `faction_info`, `faction_list`

**Mutation Commands (1 per tick / ~10s):**
`travel`, `jump`, `dock`, `undock`, `mine`, `attack`, `scan`, `cloak`, `buy`, `sell`, `craft`, `refuel`, `repair`, `buy_ship`, `sell_ship`, `switch_ship`, `install_mod`, `uninstall_mod`, `deposit_items`, `withdraw_items`, `deposit_credits`, `withdraw_credits`, `create_sell_order`, `create_buy_order`, `cancel_order`, `modify_order`, `accept_mission`, `complete_mission`, `abandon_mission`, `chat`, `jettison`, `deploy_drone`, `recall_drone`, `order_drone`, `build_base`, `attack_base`, `send_gift`, `self_destruct`, `set_status`, `set_colors`, `set_anonymous`, `buy_insurance`, `trade_offer`, `trade_accept`, `trade_decline`, `trade_cancel`

### B. POI Type Enumeration

`planet`, `moon`, `sun`, `asteroid_belt`, `asteroid`, `nebula`, `gas_cloud`, `ice_field`, `relic`, `station`, `jump_gate`

### C. Empire Enumeration

`solarian`, `voidborn`, `crimson`, `nebula`, `outerrim`

### D. Sample System Data (Zibal)

See uploaded `zibal_20260213.json` — demonstrates the data shape for a lawless system with a star, 3 planets, and an ice field. This is the canonical reference for POI coordinate systems and resource data.

### E. Ship Data (from Screenshots)

Agent "Atlas 'Astro' Anderson" flies a **Deeprock Harvester** (MINING_CRUISER) with 350 hull, 100 shield, 200 fuel capacity, 400 cargo capacity. Skills include Mining Basic Lv9, Mining Advanced Lv4, Trading Lv3, Exploration Lv2, Navigation Lv2, Small Ships Lv2, and several Lv1 skills.

### F. Station Services (from Screenshots)

The "Frontier" system station ("Mobile Capital") shows these service types: Exchange, Fuel Siphon, Machine Shop, Salvage Yard, Weld Shop, Hull Lockers, Notice Board — all Lv5 UNIQUE services. This demonstrates that real stations have themed service names and levels, which the UI should display.
