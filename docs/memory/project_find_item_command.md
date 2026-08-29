---
name: project_find_item_command
description: "BUILT 2026-07-04 (commit e1c73ff) — `find_item` command: pkg/finditem shared core + play_as wrapper; ranks selling stations by jump hops then price"
metadata: 
  node_type: memory
  type: project
  originSessionId: 364c6370-661d-49b2-8613-76f294f500c9
---

**find_item** — queued by user 2026-07-04 ("i want to buy something but the local station doesnt have it").

Requirements:
- Search market.db for stations SELLING a given item id.
- Optional quantity arg: only count stations whose sell-side depth covers the quantity.
- Order results by distance in JUMPS (hops) from the caller's current system.
- Default output: the 5 closest stations with the item (price + qty + system + hops).

Building blocks that already exist:
- market.db order queries: `OpportunityStore.GetStationOrders` / `FindBestPrices` (pkg/market); compact view_market rows are source-tagged.
- Jump distances: `navigation.BFSJumps(graph, from, targets)` + `JumpGraphFromConnections(kb.GetConnections)` — same pattern as `haulFindReroute` (pkg/worker/haul.go) and assistElect (pkg/worker/assist.go).
- Surface (user clarified 2026-07-04): core logic in a SHARED package (e.g. pkg/market or pkg/navigation-adjacent, `FindItemStations(ctx, store, kb, itemID, qty, from, limit)`) so play_as AND other callers (worker roles, tools) can use it; play_as gets the user-facing `find_item` command wrapper.

Watch: market.db station_id values are POI ids (e.g. `grand_exchange`); map station→system via KB pois table for the BFS.

Status: **BUILT & MERGED 2026-07-04** (commit e1c73ff on main).
- `market.FindItemSellers(ctx, itemID, minQty)` — per-station cheapest ask + total sell depth from latest capture (pkg/market/query.go).
- `finditem.Find(ctx, col, kb, itemID, minQty, fromSystem, limit)` in NEW pkg/finditem — composes sellers + `navigation.BFSJumps`; sorts hops→price→station; kb nil → price-only with Jumps=-1 (JumpsUnknown).
- play_as: `find_item <item> [qty] [--limit=N]` (cmd/tools/play_as/find_item.go), in help + completer.
- KEY QUIRK found in live smoke test: 27 market.db `stations` rows store system DISPLAY NAMES ("Sol", "Trader's Rest") as system_id instead of canonical ids; finditem resolves via kb.GetSystems name→id map.
- **Root cause FIXED 2026-07-12 (`4983fbe`):** `market.CaptureFromClient` wrote `state.CurrentPOI` as station_name and `state.CurrentSystem` (display-name-polluted by get_system merges, client.go mergeSystemDataLocked) as system_id; every resident's hourly update_market clobbered good rows via the upsert. Now maps from `System.ID`/`System.Name` + POI-list name lookup (BaseName>Name>id), extracted as testable `snapshotFromState`. Rows SELF-HEAL as fixed-binary fleets recapture hourly (mb fleet redeployed on it 2026-07-12; craft fleet gets it at its next restart). finditem's name→id workaround can retire once all rows show canonical ids.
