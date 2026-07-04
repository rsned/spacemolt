# Ship-Listings Capture Revival — Design

**Date:** 2026-07-04
**Status:** Approved in outline (user: "write it up"), spec for review
**Workstream:** precursor to resuming `feat/haul-ship-replacement` (tasks 7–9)

## Problem

Fleet-wide ship inventory intelligence is dead. Hull selection for the haul
ship-replacement feature (and any future fleet upgrade work) needs to know
which stations sell which hulls at what price — one station only stocks a
handful of distinct classes (Nyx Nexus: 152 listings but only ~7 classes), so
the buyer must compare across stations and travel.

The capture pipeline for this already exists and runs: every marketbot's
hourly `kb_update` → `KBUpdateStation` (`pkg/worker/capture.go:495`) →
`BrowseShips` → `StoreShipListings` into `ship_listings` in
`data/spacemolt-knowledge.db`. But the table's newest row is **2026-02-18**.
The mb overmind log shows 4,530 "Saved base" lines and zero "Saved ship
listings" lines — the ship-listing block silently no-ops every hour on all 34
stations.

## Root cause (verified live 2026-07-04)

The server changed the `browse_ships` response shape (evidently on
2026-02-18). Current shape, confirmed by a live craftsman-1 capture and
matching `server_docs/openapi.json` `BrowseShipsResponse`:

```json
{
  "base_id": "nyx_nexus_station",
  "base_name": "Nyx Nexus Station",
  "count": 152,
  "listings": [
    {
      "category": "Combat",
      "class_id": "eviction_notice",
      "hull": 398,                      // OPTIONAL — absent on most listings
      "listed_at": "2026-06-17T02:40:44Z",
      "listing_id": "5c57578dd76e272378929ad971eb0280",
      "max_hull": 480,
      "modules_count": 6,
      "price": 133174,
      "scale": 3,
      "seller": "[Station Manager: Nyx Nexus Station]",
      "shield": 200,
      "ship_id": "2cc4918ed504f69748ac8e7cbd20540a",
      "ship_name": "Eviction Notice",
      "tier": 3
    }
  ]
}
```

Three independent breaks stack up:

1. **Raw-store key drift.** `Client.storeRawJSON` classifies payloads by
   content shape. The old response had a `ships` array → stored under raw key
   `"ships"`. The new response has `listings` → stored under the generic
   `"listings"` key (shared with market/facility responses). The capture code
   reads `GetRawJSON("ships")` → nil → **silently skips the whole block** (no
   warning — which is why this went unnoticed for 4½ months).
2. **Extractor shape drift.** `extractShipListingsFromRaw` hand-walks the old
   shape (`ships[].{id,name,price,cargo_space,module_slots,…}`). None of
   those keys exist anymore. Note `serverapi.BrowseShipsResponse` +
   `serverapi.ShipListingDetail` (`pkg/game/serverapi/types.go:839`) already
   match the live shape exactly — the drift was fixed at the API layer but
   never in the capture layer.
3. **KB schema mismatch.** `ship_listings` columns
   (`ship_class, base_price REAL, description, cargo_space, module_slots,
   utility_slots, weapon_slots`) mirror the old catalog-style stats. The new
   listing is a per-hull market listing (`listing_id, ship_id, seller, tier,
   hull condition, listed_at`); static class stats (cargo capacity, slots)
   now belong to a `ship_classes` catalog join on `class_id`.

Also found while diagnosing: `KBUpdateStation` never sets
`ShipListings.CapturedAt`, so every historical row has
`captured_at = "0001-01-01T00:00:00Z"` — the freshness index was never
usable. Fix in passing.

## Design

### 1. Dedicated raw key for browse_ships (`pkg/game/client.go`)

In `storeRawJSON`, classify the browse_ships shape **before** the generic
`listings` check: payload containing all three of `base_id`, `listings`, and
`count` → `storeKey = "ship_listings"`. Everything else keeps current
behavior (market/facility responses continue to land under `"listings"`).

Rejected alternative: have the capture read the shared `"listings"` key and
sniff the shape. The kb_update flow calls `GetListings` (market) immediately
before `BrowseShips`, both writing the same key — a timeout or reordering
would silently capture market rows as ships. A dedicated key removes the
class of bug that caused this outage.

### 2. Decode via serverapi (`pkg/worker/capture.go`)

Replace `extractShipListingsFromRaw`'s map-walking with a straight
`json.Unmarshal` into `serverapi.BrowseShipsResponse`, then map
`ShipListingDetail` → `knowledge.ShipListing`. Read `GetRawJSON("ship_listings")`.

**No more silent skip:** when BrowseShips succeeded but the raw key is
missing or decodes to zero listings, print a warning naming the raw key —
the exact failure mode that hid this for months.

Set `ShipListings.CapturedAt = time.Now().UTC()` (fixes the zero-time bug).

`Hull` is optional in the payload (absent = station-managed/undamaged
listing). Sentinel: `Hull = -1` when absent, so 0 remains meaningful and we
avoid nullable plumbing through the knowledge API. `MaxHull` is always
present.

### 3. KB schema v2 (`pkg/knowledge/`, migration 47 `ship_listings_v2`)

Drop and recreate `ship_listings` (the existing 4,817 rows are Feb-stale,
old-shape, and have broken `captured_at` — nothing consumes them on main):

```sql
DROP TABLE IF EXISTS ship_listings;
CREATE TABLE ship_listings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    system_id TEXT NOT NULL,
    system_name TEXT NOT NULL,
    station_id TEXT NOT NULL,
    station_name TEXT NOT NULL,
    listing_id TEXT NOT NULL,
    ship_id TEXT NOT NULL,
    class_id TEXT NOT NULL,
    ship_name TEXT,
    category TEXT,
    tier INTEGER,
    scale INTEGER,
    hull INTEGER,               -- -1 = not reported
    max_hull INTEGER,
    shield INTEGER,
    modules_count INTEGER,
    price INTEGER NOT NULL,
    seller TEXT,
    listed_at TEXT,
    game_tick INTEGER NOT NULL,
    captured_at TEXT NOT NULL,
    agent_id TEXT,
    FOREIGN KEY (station_id) REFERENCES pois(id) ON DELETE CASCADE
);
CREATE INDEX idx_ship_listings_station ON ship_listings(station_id, captured_at DESC);
CREATE INDEX idx_ship_listings_class ON ship_listings(class_id, captured_at DESC);
```

`knowledge.ShipListing` is rewritten to match (ListingID, ShipID, ClassID,
ShipName, Category, Tier, Scale, Hull, MaxHull, Shield, ModulesCount, Price
int, Seller, ListedAt). The old fields (`BasePrice`, `Description`,
`CargoSpace`, `ModuleSlots`, `UtilitySlots`, `WeaponSlots`) are removed —
static class stats come from joining `ship_classes` on `class_id` at query
time. Known consumers to update: `pkg/worker/capture.go`,
`pkg/knowledge/{base,sqlite,memory}.go`, `cmd/auto-explorer/main.go`.

### 4. Replace-per-station snapshot semantics

`StoreShipListings` today is append-only INSERT. Reviving it as-is would add
~34 stations × ~150 listings × 24 captures/day ≈ **120k rows/day** of mostly
unchanged data to knowledge.db (the listing set barely moves — March listings
are still on the board). No consumer needs history.

Change `StoreShipListings` to, in one transaction: `DELETE FROM ship_listings
WHERE station_id = ?` then insert the new snapshot. The table stays bounded
at ~5k rows (latest snapshot per station), and "latest" queries become plain
selects — no MAX(captured_at) windowing. `GetShipListings`/
`GetLatestShipListings` keep their signatures but effectively return the
single retained snapshot; their godoc is updated to say so.

### 5. Mission-board capture in worker kb_update (folded in 2026-07-04)

The worker's `kb_update` deliberately skipped missions ("play_as-specific").
Consequence: mission_templates coverage is incidental — 96 templates total,
only the station where a play_as session happens to sit gets refreshed (1
station fresh on 2026-07-04). Fold it in: fleet-wide hourly mission boards at
all 29 marketbot stations, riding the same restart.

Port `kbUpdateMissions` (`cmd/tools/play_as/kb_update.go:175`) to
`pkg/worker/capture.go` as:

```go
func KBUpdateMissions(ctx context.Context, client game.GameClient, kb knowledge.Base) error
```

Same logic: require docked; `client.GetMissions(ctx)` (free query) →
`GetRawJSON("missions")` → `serverapi.GetMissionsResponse` →
`kb.UpsertMissionTemplate(ctx, entry, baseID, systemID, tick)` per
hand-authored entry (empty `template_id` = procedural, skipped), summary line
`update_missions: N new, N unchanged, N changed, N procedural skipped`.
Changed-field diffs log plainly (`field: old -> new`); the play_as-only
pretty formatter (`formatMissionDiffValue`) is not ported.

Call it from `KBUpdateAll`'s docked branch after `KBUpdateFacilities`,
warn-and-continue like its siblings. play_as's `kbUpdateMissions` delegates
to the worker version (same pattern as its other kb wrappers);
`formatMissionDiffValue` becomes unused and is deleted.

The intel-file side effect stays play_as-only by design (faction workflow;
marketbots are factionless and cannot submit intel).

### 6. Out of scope (next steps, separate plan)

- Rebase `feat/haul-ship-replacement` onto main; rework its task-4
  `GetAllLatestShipListings` and task-2 `SelectHaulerHull` against the v2
  columns + `ship_classes` join (cargo capacity now comes from the catalog,
  price/tier/hull condition from the listing); then finish tasks 7–9.
- Miner/other-role hull selection reusing the same table.
- No schedule changes: hourly `kb_update` on all 34 marketbots already calls
  this path. Data starts flowing on the next mb fleet restart with a rebuilt
  `bin/worker` — batch with the pending quarantine+rescue deploy.

## Testing

- `storeRawJSON` classification: browse_ships payload → `"ship_listings"` key;
  market get_listings payload still → `"listings"`. Table-driven in pkg/game.
- Capture decode: fixture built from the live 2026-07-04 Nyx Nexus response
  (trimmed to a few listings, including one with `hull` present, one without,
  and one damaged `hull:22`) → asserts field mapping incl. Hull=-1 sentinel.
- Missing-raw-key path emits the warning (no silent skip).
- `StoreShipListings` replace semantics: two stores for the same station keep
  only the second; different stations coexist.
- Migration 47 applies on a DB at version 46 and on a fresh DB.
- `KBUpdateMissions`: docked + missions raw JSON → upserts hand-authored
  entries, skips procedural (empty template_id); not-docked errors; missing
  raw data errors.

## Deployment

1. Merge to main, rebuild `bin/worker` (+`bin/overmind` already pending).
2. knowledge.db migration auto-applies on first open by a new binary.
3. Restart mb fleet (drain SIGUSR1 → relaunch, per
   [reference_overmind_launch_commands]) — same restart window as the
   quarantine+rescue deploy.
4. Verify within an hour: `SELECT station_id, COUNT(*), MAX(captured_at) FROM
   ship_listings GROUP BY station_id` shows ~29 stations with current
   timestamps, mb-overmind.log shows "Saved ship listings: N ships" and
   "update_missions:" summaries, and `SELECT COUNT(*) FROM mission_templates
   WHERE last_seen_at > <deploy time>` jumps well past the pre-deploy single
   digits (the table is a global template catalog keyed by template_id, so
   the signal is many templates re-seen, not per-station rows).
