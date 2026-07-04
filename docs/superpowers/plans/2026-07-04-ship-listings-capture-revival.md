# Ship-Listings Capture Revival Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Revive the dead-since-February ship-listings capture (browse_ships response-shape drift) and add mission-board capture to the worker's hourly `kb_update`.

**Architecture:** Classify browse_ships responses under a dedicated raw key in the game client; decode them via the already-correct `serverapi.BrowseShipsResponse`; store into a v2 `ship_listings` KB table with replace-per-station snapshot semantics; port play_as's `kbUpdateMissions` into `pkg/worker` and call both from `KBUpdateAll`'s docked branch.

**Tech Stack:** Go 1.24+, SQLite (pkg/knowledge migrations), existing pkg/game raw-JSON store.

**Spec:** `docs/superpowers/specs/2026-07-04-ship-listings-capture-revival-design.md`

## Global Constraints

- New raw-store key is exactly `"ship_listings"`; market/facility payloads must keep landing under `"listings"`.
- browse_ships payloads have NO `action` field; `facility browse_for_sale` carries the same `base_id`+`listings`+`count` shape but ALWAYS has `action` — the classifier must not steal it (guard: `action == "" || action == "browse_ships"`).
- `Hull` sentinel: `-1` = not reported (absent or zero in the payload — the wire struct uses `omitempty` so 0 and absent are indistinguishable; a for-sale hull of literal 0 does not occur).
- Migration number is exactly **47**, name `ship_listings_v2`; it DROPs and recreates the table (stale Feb data is discarded deliberately).
- `StoreShipListings` = replace-per-station: `DELETE FROM ship_listings WHERE station_id = ?` then insert, one transaction. MemoryKB mirrors this.
- Missing raw key after a successful BrowseShips must LOG A WARNING (the silent skip hid this outage for 4½ months).
- All sleeps use `pkg/game` constants (`game.SleepQuick`).
- `go build ./... && go test ./...` green before every commit (known pre-existing failure: pkg/actionspace `TestLoadFromOpenAPIContainsAllHardcoded`, salvage_wreck/server_docs drift — not ours). `golangci-lint` clean on changed packages.
- Do NOT use `git add -A` (live fleet runtime files in the working tree); stage files explicitly.

---

### Task 1: browse_ships raw-key classification

**Files:**
- Modify: `pkg/game/client.go` (inside `storeRawJSON`, immediately BEFORE the generic `hasListings` block at ~line 4335)
- Test: `pkg/game/ship_listings_store_test.go` (create)

**Interfaces:**
- Produces: browse_ships type=ok payloads retrievable via `GetRawJSON("ship_listings")`; no longer under `"listings"`.

- [ ] **Step 1: Write the failing test**

```go
package game

import (
	"io"
	"log"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestStoreRawJSONBrowseShips guards against the raw-key drift that killed
// ship-listing capture 2026-02-18..2026-07-04: browse_ships responses
// ({base_id, base_name, count, listings}, NO action field) must land under a
// dedicated "ship_listings" key, while facility sub-actions with the same
// content shape (always carrying "action") keep their existing keys.
func TestStoreRawJSONBrowseShips(t *testing.T) {
	c := &Client{
		latestRawJSON: make(map[string][]byte),
		debugLogger:   log.New(io.Discard, "", 0),
	}

	// browse_ships response (live shape 2026-07-04): no "action" field.
	c.storeRawJSON(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"base_id": "nyx_nexus_station", "base_name": "Nyx Nexus Station",
			"count": float64(1),
			"listings": []any{map[string]any{
				"listing_id": "70b2ce92", "ship_id": "720608fd",
				"class_id": "eviction_notice", "price": float64(133174),
			}},
		},
	})
	got := string(c.GetRawJSON("ship_listings"))
	if !strings.Contains(got, "eviction_notice") {
		t.Errorf("GetRawJSON(\"ship_listings\") missing browse_ships payload: %q", got)
	}
	if l := c.GetRawJSON("listings"); l != nil {
		t.Errorf("browse_ships must not land under \"listings\", got: %q", string(l))
	}

	// facility browse_for_sale: same content shape but has "action" — must
	// NOT be classified as ship_listings.
	c2 := &Client{
		latestRawJSON: make(map[string][]byte),
		debugLogger:   log.New(io.Discard, "", 0),
	}
	c2.storeRawJSON(protocol.Response{
		Type: protocol.TypeOK,
		Payload: map[string]any{
			"action": "browse_for_sale", "base_id": "grand_exchange_station",
			"base_name": "Grand Exchange Station", "count": float64(0), "listings": []any{},
		},
	})
	if s := c2.GetRawJSON("ship_listings"); s != nil {
		t.Errorf("facility browse_for_sale must not be keyed ship_listings: %q", string(s))
	}
	if l := string(c2.GetRawJSON("listings")); !strings.Contains(l, "browse_for_sale") {
		t.Errorf("facility browse_for_sale must stay under \"listings\": %q", l)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/game/ -run TestStoreRawJSONBrowseShips -count=1`
Expected: FAIL — `GetRawJSON("ship_listings")` empty, payload found under `"listings"`.

- [ ] **Step 3: Implement the classifier**

In `pkg/game/client.go`, inside `storeRawJSON`'s `protocol.TypeOK` branch, insert immediately BEFORE the existing block
```go
		if _, hasListings := resp.Payload["listings"]; hasListings {
```
this new block:

```go
		// browse_ships: {base_id, base_name, count, listings} with NO action
		// field (facility sub-actions carry the same shape but always include
		// "action"). Dedicated key so ship listings never collide with
		// market/facility "listings" payloads — the old "ships"-key drift
		// silently killed ship-listing capture from 2026-02-18 to 2026-07-04.
		if action, _ := resp.Payload["action"].(string); action == "" || action == "browse_ships" {
			_, hasBaseID := resp.Payload["base_id"]
			_, hasShipListings := resp.Payload["listings"]
			_, hasCount := resp.Payload["count"]
			if hasBaseID && hasShipListings && hasCount && storeKey == "" {
				storeKey = "ship_listings"
				shouldStore = true
			}
		}
```

- [ ] **Step 4: Run tests to verify pass + no regression**

Run: `go test ./pkg/game/ -run 'TestStoreRawJSON' -count=1`
Expected: PASS, including the pre-existing `TestStoreRawJSON_FacilityBrowseForSaleAlsoKeyedFacility`.

- [ ] **Step 5: Commit**

```bash
git add pkg/game/client.go pkg/game/ship_listings_store_test.go
git commit -m "fix(game): key browse_ships responses under dedicated ship_listings raw key"
```

---

### Task 2: knowledge ShipListing v2 — struct, converter, migration 47, stores

**Files:**
- Modify: `pkg/knowledge/base.go:139-148` (replace `ShipListing` struct; `ShipListings` at :151 unchanged)
- Create: `pkg/knowledge/ship_listings.go` (converter + JSON decode helper)
- Modify: `pkg/knowledge/sqlite_migrations.go` (append migration 47 after version 46 at ~line 533)
- Modify: `pkg/knowledge/sqlite.go:965-988` (`StoreShipListings`), `:1091-1126` (`getShipListingsForSnapshot`)
- Modify: `pkg/knowledge/memory.go:655-666` (`StoreShipListings` replace semantics)
- Test: `pkg/knowledge/ship_listings_test.go` (create)

**Interfaces:**
- Consumes: `serverapi.ShipListingDetail` (`pkg/game/serverapi/types.go:839`), `serverapi.BrowseShipsResponse` (`responses.go:556`).
- Produces (later tasks rely on these exact signatures):
  - `type ShipListing struct { ListingID, ShipID, ClassID, ShipName, Category string; Tier, Scale, Hull, MaxHull, Shield, ModulesCount, Price int; Seller, ListedAt string }`
  - `func ShipListingFromDetail(d serverapi.ShipListingDetail) ShipListing`
  - `func ShipListingsFromBrowseJSON(raw []byte) (string, []ShipListing, error)` — returns (base_id, listings, error)
  - `StoreShipListings(ctx, ShipListings, agentID)` unchanged signature, replace-per-station semantics.

- [ ] **Step 1: Write the failing tests**

Create `pkg/knowledge/ship_listings_test.go`:

```go
package knowledge

import (
	"context"
	"testing"
	"time"
)

// Live browse_ships fixture (trimmed from a 2026-07-04 Nyx Nexus capture):
// one listing with hull absent, one damaged (hull 22), one at full (hull 200).
const browseShipsFixture = `{
  "base_id": "nyx_nexus_station",
  "base_name": "Nyx Nexus Station",
  "count": 3,
  "listings": [
    {"category":"Combat","class_id":"eviction_notice","listed_at":"2026-05-23T20:58:34Z","listing_id":"70b2ce927871dc69f45996d517f33636","max_hull":480,"modules_count":6,"price":133174,"scale":3,"seller":"[Station Manager: Nyx Nexus Station]","shield":200,"ship_id":"720608fde6e20c73af4552bc90e9b382","ship_name":"Eviction Notice","tier":3},
    {"category":"Combat","class_id":"close_enough","hull":22,"listed_at":"2026-05-23T20:58:34Z","listing_id":"3f106b29d47eef035b42db3801cd859b","max_hull":200,"modules_count":5,"price":110975,"scale":2,"seller":"[Station Manager: Nyx Nexus Station]","shield":140,"ship_id":"adebe3be73391e60968e2879be328e94","ship_name":"Close Enough","tier":2},
    {"category":"Industrial","class_id":"losers_weepers","hull":70,"listed_at":"2026-06-18T17:32:17Z","listing_id":"0c5a1ea1f539a789885ab56a8494cb23","max_hull":70,"modules_count":2,"price":13688,"scale":1,"seller":"[Station Manager: Nyx Nexus Station]","shield":25,"ship_id":"66c0373c1002e3cf6ef776bc878bb74b","ship_name":"Losers Weepers","tier":1}
  ]
}`

func TestShipListingsFromBrowseJSON(t *testing.T) {
	baseID, ships, err := ShipListingsFromBrowseJSON([]byte(browseShipsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if baseID != "nyx_nexus_station" {
		t.Errorf("baseID = %q, want nyx_nexus_station", baseID)
	}
	if len(ships) != 3 {
		t.Fatalf("got %d listings, want 3", len(ships))
	}
	first := ships[0]
	if first.ListingID != "70b2ce927871dc69f45996d517f33636" || first.ClassID != "eviction_notice" ||
		first.Price != 133174 || first.Tier != 3 || first.MaxHull != 480 ||
		first.Seller != "[Station Manager: Nyx Nexus Station]" || first.ListedAt != "2026-05-23T20:58:34Z" {
		t.Errorf("first listing mapped wrong: %+v", first)
	}
	if first.Hull != -1 {
		t.Errorf("absent hull must map to -1, got %d", first.Hull)
	}
	if ships[1].Hull != 22 {
		t.Errorf("damaged hull must pass through, got %d", ships[1].Hull)
	}
}

func TestStoreShipListingsReplacesPerStation(t *testing.T) {
	ctx := context.Background()
	kb, err := NewSQLiteKB(Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = kb.Close() }()

	snap := func(station string, n int) ShipListings {
		ls := make([]ShipListing, n)
		for i := range ls {
			ls[i] = ShipListing{ListingID: station + string(rune('a'+i)), ShipID: "s", ClassID: "lemming", Price: 9738, Hull: -1}
		}
		return ShipListings{
			SystemID: "nyx", SystemName: "Nyx", StationID: station, StationName: station,
			GameTick: 100, CapturedAt: time.Now().UTC(), Listings: ls,
		}
	}

	if err := kb.StoreShipListings(ctx, snap("station_a", 3), "test"); err != nil {
		t.Fatal(err)
	}
	if err := kb.StoreShipListings(ctx, snap("station_b", 2), "test"); err != nil {
		t.Fatal(err)
	}
	// Re-capture station_a with fewer listings: must REPLACE, not append.
	if err := kb.StoreShipListings(ctx, snap("station_a", 1), "test"); err != nil {
		t.Fatal(err)
	}

	got, err := kb.GetLatestShipListings(ctx, "nyx", "station_a")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got.Listings) != 1 {
		t.Fatalf("station_a must hold exactly the last snapshot (1 listing), got %+v", got)
	}
	if got.Listings[0].ClassID != "lemming" || got.Listings[0].Price != 9738 {
		t.Errorf("scanned listing fields wrong: %+v", got.Listings[0])
	}
	other, err := kb.GetLatestShipListings(ctx, "nyx", "station_b")
	if err != nil {
		t.Fatal(err)
	}
	if other == nil || len(other.Listings) != 2 {
		t.Fatalf("station_b snapshot must be untouched, got %+v", other)
	}
}

func TestMemoryKBStoreShipListingsReplaces(t *testing.T) {
	ctx := context.Background()
	kb := NewMemoryKB()
	mk := func(station string, n int) ShipListings {
		return ShipListings{SystemID: "nyx", StationID: station, Listings: make([]ShipListing, n)}
	}
	_ = kb.StoreShipListings(ctx, mk("a", 3), "t")
	_ = kb.StoreShipListings(ctx, mk("a", 1), "t")
	got, _ := kb.GetLatestShipListings(ctx, "nyx", "a")
	if got == nil || len(got.Listings) != 1 {
		t.Fatalf("MemoryKB must replace per station, got %+v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/knowledge/ -run 'ShipListings' -count=1`
Expected: FAIL — `ShipListingsFromBrowseJSON` undefined; struct fields undefined.

- [ ] **Step 3: Replace the ShipListing struct**

In `pkg/knowledge/base.go`, replace the existing `ShipListing` (lines 139-148) with:

```go
// ShipListing is one hull for sale at a station, matching the browse_ships
// listing shape (server response since ~2026-02-18). Static class stats
// (cargo space, slots) are NOT here — join ship_classes on ClassID.
type ShipListing struct {
	ListingID    string
	ShipID       string
	ClassID      string
	ShipName     string
	Category     string
	Tier         int
	Scale        int
	Hull         int // current hull; -1 = not reported by the server
	MaxHull      int
	Shield       int
	ModulesCount int
	Price        int
	Seller       string
	ListedAt     string
}
```

- [ ] **Step 4: Create the converter + decode helper**

Create `pkg/knowledge/ship_listings.go`:

```go
package knowledge

import (
	"encoding/json"
	"fmt"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// ShipListingFromDetail maps a browse_ships wire listing to the KB row shape.
// The wire Hull field is int/omitempty, so absent and 0 are indistinguishable;
// a for-sale hull of literal 0 does not occur, so both map to -1 (not
// reported).
func ShipListingFromDetail(d serverapi.ShipListingDetail) ShipListing {
	hull := d.Hull
	if hull == 0 {
		hull = -1
	}
	return ShipListing{
		ListingID:    d.ListingID,
		ShipID:       d.ShipID,
		ClassID:      d.ClassID,
		ShipName:     d.ShipName,
		Category:     d.Category,
		Tier:         d.Tier,
		Scale:        d.Scale,
		Hull:         hull,
		MaxHull:      d.MaxHull,
		Shield:       d.Shield,
		ModulesCount: d.ModulesCount,
		Price:        d.Price,
		Seller:       d.Seller,
		ListedAt:     d.ListedAt,
	}
}

// ShipListingsFromBrowseJSON decodes a raw browse_ships response payload
// (raw-store key "ship_listings") into KB listing rows. Returns the base id
// the server reported alongside the rows.
func ShipListingsFromBrowseJSON(raw []byte) (string, []ShipListing, error) {
	var resp serverapi.BrowseShipsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", nil, fmt.Errorf("parse browse_ships response: %w", err)
	}
	ships := make([]ShipListing, 0, len(resp.Listings))
	for _, d := range resp.Listings {
		ships = append(ships, ShipListingFromDetail(d))
	}
	return resp.BaseID, ships, nil
}
```

- [ ] **Step 5: Add migration 47**

In `pkg/knowledge/sqlite_migrations.go`, append after the `version: 46` entry (keep the same struct-literal style):

```go
		{
			version: 47,
			name:    "ship_listings_v2",
			sql: `
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
					hull INTEGER,
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
			`,
		},
```

Note: the pre-existing indexes `idx_ship_listings_system_station` / `idx_ship_listings_captured_at` are dropped together with the old table; the two new indexes replace them.

- [ ] **Step 6: Rewrite SQLiteKB.StoreShipListings (replace-per-station) and the snapshot scanner**

Replace `StoreShipListings` in `pkg/knowledge/sqlite.go` (lines 965-988):

```go
// StoreShipListings stores the latest ship-listing snapshot for a station,
// replacing any prior snapshot for that station (no history is kept — an
// hourly append across the fleet would grow ~120k rows/day of near-identical
// data).
func (kb *SQLiteKB) StoreShipListings(ctx context.Context, listings ShipListings, agentID string) error {
	if listings.CapturedAt.IsZero() {
		listings.CapturedAt = time.Now().UTC()
	}

	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM ship_listings WHERE station_id = ?`, listings.StationID); err != nil {
		return fmt.Errorf("failed to clear prior snapshot: %w", err)
	}

	for _, ship := range listings.Listings {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO ship_listings (system_id, system_name, station_id, station_name,
				listing_id, ship_id, class_id, ship_name, category, tier, scale,
				hull, max_hull, shield, modules_count, price, seller, listed_at,
				game_tick, captured_at, agent_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, listings.SystemID, listings.SystemName, listings.StationID, listings.StationName,
			ship.ListingID, ship.ShipID, ship.ClassID, ship.ShipName, ship.Category,
			ship.Tier, ship.Scale, ship.Hull, ship.MaxHull, ship.Shield, ship.ModulesCount,
			ship.Price, ship.Seller, ship.ListedAt,
			listings.GameTick, listings.CapturedAt.Format(time.RFC3339), agentID)
		if err != nil {
			return fmt.Errorf("failed to insert ship listing: %w", err)
		}
	}

	return tx.Commit()
}
```

Replace `getShipListingsForSnapshot` (lines 1091-1126):

```go
func (kb *SQLiteKB) getShipListingsForSnapshot(ctx context.Context, systemID, stationID string, capturedAt time.Time) ([]ShipListing, error) {
	query := `
		SELECT listing_id, ship_id, class_id, ship_name, category, tier, scale,
			hull, max_hull, shield, modules_count, price, seller, listed_at
		FROM ship_listings
		WHERE system_id = ? AND station_id = ? AND captured_at = ?
		ORDER BY class_id, price
	`

	rows, err := kb.db.QueryContext(ctx, query, systemID, stationID, capturedAt.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("failed to query ship listings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var listings []ShipListing
	for rows.Next() {
		var ship ShipListing
		if err := rows.Scan(&ship.ListingID, &ship.ShipID, &ship.ClassID, &ship.ShipName,
			&ship.Category, &ship.Tier, &ship.Scale, &ship.Hull, &ship.MaxHull,
			&ship.Shield, &ship.ModulesCount, &ship.Price, &ship.Seller, &ship.ListedAt); err != nil {
			return nil, fmt.Errorf("failed to scan ship listing: %w", err)
		}
		listings = append(listings, ship)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating ship listings: %w", err)
	}

	return listings, nil
}
```

`GetShipListings` / `GetLatestShipListings` / `HasShipListingsToday` select only snapshot-header columns and need no changes beyond a godoc note on `GetShipListings` that only the latest snapshot per station is retained. If any scanned column in them referenced removed columns it would fail Step 8 — they don't.

- [ ] **Step 7: MemoryKB replace semantics**

Replace `MemoryKB.StoreShipListings` (memory.go lines 655-666):

```go
// StoreShipListings stores the latest ship-listing snapshot for a station,
// replacing any prior snapshot for that station (mirrors SQLiteKB).
func (kb *MemoryKB) StoreShipListings(ctx context.Context, listings ShipListings, agentID string) error {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	if listings.CapturedAt.IsZero() {
		listings.CapturedAt = time.Now()
	}

	kept := kb.shipListings[:0]
	for _, l := range kb.shipListings {
		if l.StationID != listings.StationID {
			kept = append(kept, l)
		}
	}
	kb.shipListings = append(kept, listings)
	return nil
}
```

- [ ] **Step 8: Run tests**

Run: `go test ./pkg/knowledge/ -count=1`
Expected: PASS (all, not just the new ones — migration 47 runs on every fresh test DB).

Note: `go build ./...` will FAIL at this point — `pkg/worker/capture.go` and `cmd/auto-explorer` still reference the removed struct fields. That is expected; they are Tasks 3-4. Run only the pkg/knowledge and pkg/game tests here.

- [ ] **Step 9: Commit**

```bash
git add pkg/knowledge/base.go pkg/knowledge/ship_listings.go pkg/knowledge/ship_listings_test.go pkg/knowledge/sqlite_migrations.go pkg/knowledge/sqlite.go pkg/knowledge/memory.go
git commit -m "feat(knowledge): ship_listings v2 schema + replace-per-station store"
```

---

### Task 3: worker capture rewrite (KBUpdateStation ship-listings block)

**Files:**
- Modify: `pkg/worker/capture.go` — the `// --- Ship listings ---` block inside `KBUpdateStation` (~lines 568-594) and delete `extractShipListingsFromRaw` (~lines 51-100)
- Test: `pkg/worker/capture_ships_test.go` (create)

**Interfaces:**
- Consumes: `knowledge.ShipListingsFromBrowseJSON(raw []byte) (string, []ShipListing, error)` from Task 2; `GetRawJSON("ship_listings")` from Task 1.

- [ ] **Step 1: Write the failing test**

Create `pkg/worker/capture_ships_test.go`. The package's `fakeClient` (dispatch_test.go) has a `raw map[string][]byte` backing `GetRawJSON` and a `state *game.State` backing `GetState`; add a `BrowseShips` recorder method if it lacks one:

```go
package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

const browseShipsRaw = `{"base_id":"nyx_nexus_station","base_name":"Nyx Nexus Station","count":2,"listings":[
 {"category":"Combat","class_id":"eviction_notice","listed_at":"2026-05-23T20:58:34Z","listing_id":"l1","max_hull":480,"modules_count":6,"price":133174,"scale":3,"seller":"[Station Manager]","shield":200,"ship_id":"s1","ship_name":"Eviction Notice","tier":3},
 {"category":"Exploration","class_id":"lemming","hull":80,"listed_at":"2026-06-18T17:32:17Z","listing_id":"l2","max_hull":80,"modules_count":2,"price":9738,"scale":1,"seller":"[Station Manager]","shield":40,"ship_id":"s2","ship_name":"Lemming","tier":1}
]}`

// KBUpdateStation must decode browse_ships from the "ship_listings" raw key
// and store a snapshot with CapturedAt set (the old path read a dead "ships"
// key and silently skipped for months).
func TestKBUpdateStationStoresShipListings(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	st := &game.State{Doc: true}
	st.System.ID = "nyx"
	st.System.Name = "Nyx"
	st.CurrentPOI = "nyx_nexus_station"
	client := &fakeClient{
		state: st,
		raw:   map[string][]byte{"ship_listings": []byte(browseShipsRaw)},
	}

	if err := KBUpdateStation(context.Background(), client, kb, nil, "test"); err != nil {
		t.Fatal(err)
	}

	got, err := kb.GetLatestShipListings(context.Background(), "nyx", "nyx_nexus_station")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got.Listings) != 2 {
		t.Fatalf("want 2 stored listings, got %+v", got)
	}
	if got.CapturedAt.IsZero() {
		t.Error("CapturedAt must be set (was zero-time for all historical rows)")
	}
	if got.Listings[0].Hull != -1 && got.Listings[1].Hull != -1 {
		t.Error("absent hull must be stored as -1")
	}
}
```

`fakeClient` must implement every `game.GameClient` method KBUpdateStation touches (`GetBase`, `GetListings`, `GetMarketListings`, `BrowseShips`); the embedded interface panics on missing ones — add no-op recorder methods as the panics surface, following the file's existing one-liner style.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/worker/ -run TestKBUpdateStationStoresShipListings -count=1`
Expected: FAIL (compile error — Task 2 removed the struct fields `extractShipListingsFromRaw` uses; or, once compiling, 0 stored listings).

- [ ] **Step 3: Rewrite the ship-listings block**

In `KBUpdateStation` (`pkg/worker/capture.go`), replace the entire `// --- Ship listings ---` block with:

```go
	// --- Ship listings ---
	if err := client.BrowseShips(ctx, nil); err != nil {
		fmt.Printf("Warning: browse_ships failed: %v\n", err)
	} else {
		time.Sleep(game.SleepQuick)

		rawJSON := client.GetRawJSON("ship_listings")
		if rawJSON == nil {
			// Never skip silently: the old "ships" raw-key drift no-opped this
			// block from 2026-02-18 to 2026-07-04 without a single log line.
			fmt.Println("Warning: browse_ships succeeded but no ship_listings payload was stored (response-shape drift?)")
		} else if _, ships, err := knowledge.ShipListingsFromBrowseJSON(rawJSON); err != nil {
			fmt.Printf("Warning: failed to parse ship listings: %v\n", err)
		} else {
			shipListings := knowledge.ShipListings{
				SystemID:    systemID,
				SystemName:  systemName,
				StationID:   poiID,
				StationName: poiName,
				GameTick:    currentTick(state),
				CapturedAt:  time.Now().UTC(),
				Listings:    ships,
			}
			if err := kb.StoreShipListings(ctx, shipListings, source); err != nil {
				fmt.Printf("Warning: failed to save ship listings: %v\n", err)
			} else {
				fmt.Printf("Saved ship listings: %d ships\n", len(ships))
			}
		}
	}
```

Delete `extractShipListingsFromRaw` entirely (Task 2's helper replaces it). Remove any imports that become unused.

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/worker/ -count=1` (the suite takes ~20s)
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/worker/capture.go pkg/worker/capture_ships_test.go pkg/worker/dispatch_test.go
git commit -m "fix(worker): decode browse_ships via serverapi; warn instead of silent skip"
```

---

### Task 4: auto-explorer uses the shared decode path

**Files:**
- Modify: `cmd/auto-explorer/main.go:425-460` (ship-listings block) and delete its local `extractShipListings` + `convertShipListingsToKnowledge` (~lines 463-540)

**Interfaces:**
- Consumes: `knowledge.ShipListingsFromBrowseJSON`, `GetRawJSON("ship_listings")`.

- [ ] **Step 1: Rewrite the block**

auto-explorer reads `GetRawJSON("listings")` — dead after Task 1 rekeys browse_ships. Replace the inner capture (inside the existing `if !hasShipsToday { if wsClient, ok := ... }` scaffolding, keeping the `BrowseShips` call and logger lines) with:

```go
			if err := wsClient.BrowseShips(ctx, nil); err != nil {
				logger.Printf("Failed to get ship listings: %v", err)
			} else {
				rawJSON := client.GetRawJSON("ship_listings")
				if rawJSON == nil {
					logger.Printf("⚠️  browse_ships succeeded but no ship_listings payload was stored")
				} else if _, ships, err := knowledge.ShipListingsFromBrowseJSON(rawJSON); err != nil {
					logger.Printf("⚠️  Failed to parse ship listings: %v", err)
				} else {
					shipListings := knowledge.ShipListings{
						SystemID:    state.System.ID,
						SystemName:  systemName,
						StationID:   poiID,
						StationName: poiName,
						GameTick:    state.CurrentTick,
						CapturedAt:  time.Now().UTC(),
						Listings:    ships,
					}
					if err := kb.StoreShipListings(ctx, shipListings, agentID); err != nil {
						logger.Printf("⚠️  Failed to save ship listings to knowledge base: %v", err)
					} else {
						logger.Printf("💾 Saved ship listings to knowledge base (%d ships)", len(ships))
					}
				}
			}
```

Delete `convertShipListingsToKnowledge` and `extractShipListings`; remove imports that become unused. (Check `state.System.ID` vs the variable the surrounding code actually uses — mirror the existing block's variables exactly.)

- [ ] **Step 2: Build and run package tests**

Run: `go build ./cmd/auto-explorer/ && go test ./cmd/auto-explorer/ -count=1`
Expected: build OK; tests PASS (or "no test files").

- [ ] **Step 3: Commit**

```bash
git add cmd/auto-explorer/main.go
git commit -m "fix(auto-explorer): share the browse_ships decode path"
```

---

### Task 5: KBUpdateMissions in worker kb_update + play_as delegation

**Files:**
- Modify: `pkg/worker/capture.go` — add `KBUpdateMissions`, call it from `KBUpdateAll`'s docked branch (after `KBUpdateFacilities`, ~line 713); update `KBUpdateAll`'s godoc (it currently says missions are play_as-specific)
- Modify: `cmd/tools/play_as/kb_update.go` — `kbUpdateMissions` delegates; delete `formatMissionDiffValue` (~lines 31-98) and imports that become unused
- Test: `pkg/worker/capture_missions_test.go` (create)

**Interfaces:**
- Consumes: `knowledge.Base.UpsertMissionTemplate(ctx, entry serverapi.MissionBoardEntry, baseID, systemID string, tick int64) (*MissionUpsertResult, error)`; `serverapi.GetMissionsResponse{Missions []MissionBoardEntry; BaseID string}`; `client.GetMissions(ctx)`; `GetRawJSON("missions")`.
- Produces: `func KBUpdateMissions(ctx context.Context, client game.GameClient, kb knowledge.Base) error`.

- [ ] **Step 1: Write the failing test**

Create `pkg/worker/capture_missions_test.go`:

```go
package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// recordingMissionKB counts template upserts (MemoryKB has no template getter).
type recordingMissionKB struct {
	knowledge.Base
	upserts []serverapi.MissionBoardEntry
}

func (r *recordingMissionKB) UpsertMissionTemplate(ctx context.Context, entry serverapi.MissionBoardEntry, baseID, systemID string, tick int64) (*knowledge.MissionUpsertResult, error) {
	r.upserts = append(r.upserts, entry)
	return &knowledge.MissionUpsertResult{Inserted: true}, nil
}

const missionsRaw = `{"base_id":"nyx_nexus_station","base_name":"Nyx Nexus Station","missions":[
 {"mission_id":"m1","template_id":"tpl_courier_1","title":"Courier Run"},
 {"mission_id":"m2","template_id":"","title":"Procedural Haul"}
]}`

func TestKBUpdateMissionsUpsertsHandAuthoredOnly(t *testing.T) {
	st := &game.State{Doc: true}
	st.System.ID = "nyx"
	st.CurrentPOI = "nyx_nexus_station"
	client := &fakeClient{state: st, raw: map[string][]byte{"missions": []byte(missionsRaw)}}
	kb := &recordingMissionKB{Base: knowledge.NewMemoryKB()}

	if err := KBUpdateMissions(context.Background(), client, kb); err != nil {
		t.Fatal(err)
	}
	if len(kb.upserts) != 1 || kb.upserts[0].TemplateID != "tpl_courier_1" {
		t.Fatalf("want exactly the hand-authored template upserted, got %+v", kb.upserts)
	}
}

func TestKBUpdateMissionsRequiresDock(t *testing.T) {
	client := &fakeClient{state: &game.State{}}
	if err := KBUpdateMissions(context.Background(), client, knowledge.NewMemoryKB()); err == nil {
		t.Fatal("undocked KBUpdateMissions must error")
	}
}
```

`fakeClient` needs a `GetMissions(ctx context.Context) error` recorder method — add it to dispatch_test.go in the file's one-liner style. Verify the `MissionBoardEntry` field names (`MissionID`, `TemplateID`, `Title`) against `pkg/game/serverapi` before coding — do NOT assume.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/worker/ -run KBUpdateMissions -count=1`
Expected: FAIL — `KBUpdateMissions` undefined.

- [ ] **Step 3: Implement KBUpdateMissions**

Add to `pkg/worker/capture.go` (after `KBUpdateFacilities`):

```go
// KBUpdateMissions fetches the mission board at the current station and
// upserts each hand-authored entry into the KB mission catalog. Procedural
// missions (empty template_id) are skipped. Ported from play_as so the
// worker fleet's hourly kb_update captures mission boards fleet-wide.
func KBUpdateMissions(ctx context.Context, client game.GameClient, kb knowledge.Base) error {
	if kb == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}

	state := client.GetState()
	if !state.Doc {
		return fmt.Errorf("must be docked at a station")
	}

	if err := client.GetMissions(ctx); err != nil {
		return fmt.Errorf("get_missions: %w", err)
	}
	time.Sleep(game.SleepQuick)

	raw := client.GetRawJSON("missions")
	if len(raw) == 0 {
		return fmt.Errorf("get_missions returned no data")
	}

	var resp serverapi.GetMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parse get_missions response: %w", err)
	}

	baseID := resp.BaseID
	if baseID == "" {
		baseID = state.CurrentPOI
	}
	systemID := state.System.ID
	tick := currentTick(state)

	var inserted, unchanged, changed, skipped int
	for _, entry := range resp.Missions {
		if entry.TemplateID == "" {
			skipped++
			continue
		}
		res, err := kb.UpsertMissionTemplate(ctx, entry, baseID, systemID, tick)
		if err != nil {
			fmt.Printf("Warning: upsert %s: %v\n", entry.MissionID, err)
			continue
		}
		switch {
		case res.Inserted:
			inserted++
		case len(res.Diffs) > 0:
			changed++
			for _, d := range res.Diffs {
				fmt.Printf("mission template %s changed at %s: %s: %q -> %q\n",
					entry.TemplateID, baseID, d.Field, d.OldValue, d.NewValue)
			}
		default:
			unchanged++
		}
	}

	fmt.Printf("update_missions: %d new, %d unchanged, %d changed, %d procedural skipped\n",
		inserted, unchanged, changed, skipped)
	return nil
}
```

(Add the `serverapi` import if capture.go lacks it.)

In `KBUpdateAll`'s docked branch, after the `KBUpdateFacilities` call, add:

```go
		if err := KBUpdateMissions(ctx, client, kb); err != nil {
			fmt.Printf("Warning: update_missions: %v\n", err)
		}
```

and update `KBUpdateAll`'s godoc: it now DOES run update_missions when docked.

- [ ] **Step 4: Delegate play_as**

In `cmd/tools/play_as/kb_update.go`, replace `kbUpdateMissions`'s body:

```go
// kbUpdateMissions fetches the mission board at the current station and upserts
// each hand-authored (non-procedural) entry into the knowledge-base mission
// catalog. Delegates to the shared worker implementation.
func kbUpdateMissions(client game.GameClient, ctx context.Context) error {
	if state := client.GetState(); state != nil && !state.Doc {
		fmt.Println("(Not docked — skipping missions update)")
		return nil
	}
	return worker.KBUpdateMissions(ctx, client, globalKB)
}
```

Delete `formatMissionDiffValue` (now unused) and any imports that become unused (`os`, `serverapi`, `time` — whichever the compiler flags).

- [ ] **Step 5: Run tests + build**

Run: `go test ./pkg/worker/ -count=1 && go build ./cmd/tools/play_as/`
Expected: PASS / builds.

- [ ] **Step 6: Commit**

```bash
git add pkg/worker/capture.go pkg/worker/capture_missions_test.go pkg/worker/dispatch_test.go cmd/tools/play_as/kb_update.go
git commit -m "feat(worker): capture mission boards in kb_update; play_as delegates"
```

---

### Task 6: full verification

- [ ] **Step 1: Build everything**

Run: `go build ./...`
Expected: OK (this is the first task where the whole tree must compile).

- [ ] **Step 2: Full test suite**

Run: `go test ./... -count=1` (allow ~5 min; if the shell times out, run `pkg/...` and `cmd/...` separately)
Expected: PASS everywhere except the known pre-existing pkg/actionspace `TestLoadFromOpenAPIContainsAllHardcoded` failure (salvage_wreck / server_docs drift — not this branch).

- [ ] **Step 3: Lint**

Run: `golangci-lint run ./pkg/game/... ./pkg/knowledge/... ./pkg/worker/... ./cmd/auto-explorer/... ./cmd/tools/play_as/...`
Expected: 0 issues.

- [ ] **Step 4: Commit any stragglers, then done**

Deployment happens separately per the spec: rebuild `bin/worker`, restart the mb fleet (batched with the pending quarantine+rescue deploy), verify ship_listings + mission_templates fill within an hour.
