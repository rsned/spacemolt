package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/market"
)

// currentTick returns the best available game tick from the client state.
// In the worker package there is no global clock, so we always read from state.
func currentTick(state *game.State) int64 {
	if state == nil {
		return 0
	}
	return state.GetTick()
}

// demandHistoryBucket is the time-bucket granularity for demand-history samples.
// Captures within the same bucket upsert the same row (last observation wins).
const demandHistoryBucket = time.Hour

// demandFreshness is how recently a station's demand must have been captured for
// a new capture to be skipped.
const demandFreshness = 5 * time.Minute

// isFresh reports whether a capture at `last` is recent enough (strictly within
// `window` of `now`) that re-capturing should be skipped.
func isFresh(last, now time.Time, window time.Duration) bool {
	return now.Sub(last) < window
}

// extractConnections converts game connection info to knowledge connection structs.
func extractConnections(conns []game.ConnectionInfo) []knowledge.SystemConnection {
	result := make([]knowledge.SystemConnection, len(conns))
	for i, conn := range conns {
		result[i] = knowledge.SystemConnection{
			SystemID: conn.SystemID,
			Distance: conn.Distance,
		}
	}
	return result
}

// parseStationBuyOrders turns a compact view_market response into per-order
// MarketBuyOrderRow values across all items, carrying Source. Skips orders with
// non-positive price or qty.
func parseStationBuyOrders(raw []byte, stationID, systemID string, now time.Time) []knowledge.MarketBuyOrderRow {
	if len(raw) == 0 || stationID == "" {
		return nil
	}
	var resp struct {
		Items []serverapi.ViewMarketItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	var out []knowledge.MarketBuyOrderRow
	for _, it := range resp.Items {
		for _, o := range it.BuyOrders {
			if o.PriceEach <= 0 || o.Quantity <= 0 {
				continue
			}
			out = append(out, knowledge.MarketBuyOrderRow{
				StationID:  stationID,
				SystemID:   systemID,
				ItemID:     it.ItemID,
				ItemName:   it.ItemName,
				PriceEach:  o.PriceEach,
				Quantity:   o.Quantity,
				MyQuantity: float64(o.MyQuantity),
				Source:     o.Source,
				CapturedAt: now,
			})
		}
	}
	return out
}

// parseStationSellOrders turns a compact view_market response into per-order
// MarketSellOrderRow values across all items — the supply-side mirror of
// parseStationBuyOrders. Skips orders with non-positive price or qty.
func parseStationSellOrders(raw []byte, stationID, systemID string, now time.Time) []knowledge.MarketSellOrderRow {
	if len(raw) == 0 || stationID == "" {
		return nil
	}
	var resp struct {
		Items []serverapi.ViewMarketItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	var out []knowledge.MarketSellOrderRow
	for _, it := range resp.Items {
		for _, o := range it.SellOrders {
			if o.PriceEach <= 0 || o.Quantity <= 0 {
				continue
			}
			out = append(out, knowledge.MarketSellOrderRow{
				StationID:  stationID,
				SystemID:   systemID,
				ItemID:     it.ItemID,
				ItemName:   it.ItemName,
				PriceEach:  o.PriceEach,
				Quantity:   o.Quantity,
				MyQuantity: float64(o.MyQuantity),
				Source:     o.Source,
				CapturedAt: now,
			})
		}
	}
	return out
}

// aggregateDemandHistory collapses per-order buy demand into one
// DemandHistorySample per (station, item): best price and total quantity across
// all orders, plus the Station-Manager split (source=="station"). BucketAt is
// `now` truncated to the bucket size; CapturedAt is `now`. Output preserves
// first-seen order for deterministic rendering and tests. Orders with
// non-positive price or quantity are skipped.
func aggregateDemandHistory(orders []knowledge.MarketBuyOrderRow, now time.Time, bucket time.Duration) []knowledge.DemandHistorySample {
	type acc struct {
		stationID, systemID, itemID, itemName string
		best, total, smBest, smQty            float64
		count                                 int
	}
	key := func(s, i string) string { return s + "\x00" + i }
	order := []string{}
	m := map[string]*acc{}
	for _, o := range orders {
		if o.PriceEach <= 0 || o.Quantity <= 0 {
			continue
		}
		k := key(o.StationID, o.ItemID)
		a, ok := m[k]
		if !ok {
			a = &acc{stationID: o.StationID, systemID: o.SystemID, itemID: o.ItemID, itemName: o.ItemName}
			m[k] = a
			order = append(order, k)
		}
		a.total += o.Quantity
		a.count++
		if o.PriceEach > a.best {
			a.best = o.PriceEach
		}
		if o.Source == "station" {
			a.smQty += o.Quantity
			if o.PriceEach > a.smBest {
				a.smBest = o.PriceEach
			}
		}
	}
	bucketAt := now.UTC().Truncate(bucket)
	out := make([]knowledge.DemandHistorySample, 0, len(order))
	for _, k := range order {
		a := m[k]
		out = append(out, knowledge.DemandHistorySample{
			StationID:   a.stationID,
			SystemID:    a.systemID,
			ItemID:      a.itemID,
			ItemName:    a.itemName,
			BucketAt:    bucketAt,
			CapturedAt:  now,
			BestPrice:   a.best,
			TotalQty:    a.total,
			SMBestPrice: a.smBest,
			SMQty:       a.smQty,
			OrderCount:  a.count,
		})
	}
	return out
}

// aggregateSupplyHistory collapses per-order sell supply into one
// SupplyHistorySample per (station, item) — the mirror of aggregateDemandHistory.
// Unlike demand (where "best" is the highest price), supply BestPrice is the
// LOWEST (cheapest) sell price across all orders, and SMBestPrice is the
// cheapest Station-Manager (source=="station") price. Orders with non-positive
// price or quantity are skipped.
func aggregateSupplyHistory(orders []knowledge.MarketSellOrderRow, now time.Time, bucket time.Duration) []knowledge.SupplyHistorySample {
	type acc struct {
		stationID, systemID, itemID, itemName string
		best, total, smBest, smQty            float64
		count                                 int
	}
	key := func(s, i string) string { return s + "\x00" + i }
	order := []string{}
	m := map[string]*acc{}
	for _, o := range orders {
		if o.PriceEach <= 0 || o.Quantity <= 0 {
			continue
		}
		k := key(o.StationID, o.ItemID)
		a, ok := m[k]
		if !ok {
			a = &acc{stationID: o.StationID, systemID: o.SystemID, itemID: o.ItemID, itemName: o.ItemName}
			m[k] = a
			order = append(order, k)
		}
		a.total += o.Quantity
		a.count++
		// Cheapest wins for supply; best==0 means unset (prices are positive).
		if a.best == 0 || o.PriceEach < a.best {
			a.best = o.PriceEach
		}
		if o.Source == "station" {
			a.smQty += o.Quantity
			if a.smBest == 0 || o.PriceEach < a.smBest {
				a.smBest = o.PriceEach
			}
		}
	}
	bucketAt := now.UTC().Truncate(bucket)
	out := make([]knowledge.SupplyHistorySample, 0, len(order))
	for _, k := range order {
		a := m[k]
		out = append(out, knowledge.SupplyHistorySample{
			StationID:   a.stationID,
			SystemID:    a.systemID,
			ItemID:      a.itemID,
			ItemName:    a.itemName,
			BucketAt:    bucketAt,
			CapturedAt:  now,
			BestPrice:   a.best,
			TotalQty:    a.total,
			SMBestPrice: a.smBest,
			SMQty:       a.smQty,
			OrderCount:  a.count,
		})
	}
	return out
}

// KBUpdateSystem fetches the current system data and saves it to the knowledge
// base. detectedBy records which agent observed the data (POI provenance).
func KBUpdateSystem(ctx context.Context, client game.GameClient, kb knowledge.Base, detectedBy string) error {
	if kb == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}

	if err := client.GetSystem(ctx); err != nil {
		return fmt.Errorf("get_system failed: %w", err)
	}
	time.Sleep(game.SleepQuick)

	state := client.GetState()
	if state.System.ID == "" {
		return fmt.Errorf("no system data available")
	}

	kbSystem := knowledge.System{
		ID:              state.System.ID,
		Name:            state.System.Name,
		PoliceLevel:     state.System.PoliceLevel,
		SecurityStatus:  state.System.SecurityStatus,
		Empire:          state.System.Empire,
		IsStronghold:    state.System.IsStronghold,
		Connections:     extractConnections(state.System.Connections),
		LastUpdatedTick: currentTick(state),
		LastVisitedTick: currentTick(state),
		Position: game.Position{
			X: state.System.Position.X,
			Y: state.System.Position.Y,
		},
	}

	if err := kb.RememberSystem(ctx, kbSystem); err != nil {
		return fmt.Errorf("failed to save system: %w", err)
	}

	// Save each POI from the system data
	poiCount := 0
	for _, poi := range state.System.POIs {
		kbPOI := knowledge.POI{
			ID:       poi.ID,
			SystemID: state.System.ID,
			Name:     poi.Name,
			Type:     poi.Type,
			Class:    poi.Class,
			Position: game.Position{
				X: poi.Position.X,
				Y: poi.Position.Y,
			},
			LastUpdatedTick: currentTick(state),
			DetectedBy:      detectedBy,
		}
		if err := kb.RememberPOI(ctx, kbPOI); err != nil {
			fmt.Printf("  Warning: failed to save POI %s: %v\n", poi.Name, err)
		} else {
			poiCount++
		}
	}

	fmt.Printf("Saved system: %s (%d POIs, %d connections, tick %d)\n",
		state.System.Name, poiCount, len(state.System.Connections), kbSystem.LastUpdatedTick)
	return nil
}

// GetLocationPOI fetches the current POI via the get_location command and
// returns it as a fully-populated game.POI.
//
// The server retired get_poi (2026-06-24); resource richness/remaining is now
// only available from get_location, while structural fields (class, description,
// position, base linkage) come from get_system. This helper reads the live
// resources from get_location and enriches the static structural fields from the
// cached system data in client state (populated by a prior get_system, which the
// capture flows always run first). Fields absent from both commands — per-POI
// description (when not already cached), hidden, reveal_difficulty — are left
// zero and preserved across updates by the knowledge base's merge-UPSERT.
func GetLocationPOI(ctx context.Context, client game.GameClient) (game.POI, error) {
	if err := client.RawCommand(ctx, "get_location", nil); err != nil {
		return game.POI{}, fmt.Errorf("get_location failed: %w", err)
	}
	time.Sleep(game.SleepQuick)

	rawJSON := client.GetRawJSON("location")
	if rawJSON == nil {
		return game.POI{}, fmt.Errorf("no location data in response")
	}

	var resp struct {
		Location struct {
			POIID     string `json:"poi_id"`
			POIName   string `json:"poi_name"`
			POIType   string `json:"poi_type"`
			SystemID  string `json:"system_id"`
			Resources []struct {
				ItemID    string  `json:"item_id"`
				Richness  float64 `json:"richness"`
				Remaining float64 `json:"remaining"`
			} `json:"resources"`
		} `json:"location"`
	}
	if err := json.Unmarshal(rawJSON, &resp); err != nil {
		return game.POI{}, fmt.Errorf("failed to parse location response: %w", err)
	}
	l := resp.Location
	if l.POIID == "" {
		return game.POI{}, fmt.Errorf("location response has no current POI")
	}

	poi := game.POI{
		ID:       l.POIID,
		SystemID: l.SystemID,
		Name:     l.POIName,
		Type:     l.POIType,
	}
	for _, r := range l.Resources {
		poi.Resources = append(poi.Resources, game.POIResource{
			ResourceID: r.ItemID,
			Richness:   r.Richness,
			Remaining:  r.Remaining,
		})
	}

	// Enrich structural fields from the cached system data (get_system carries
	// class/description/position/base but no resources).
	state := client.GetState()
	if poi.SystemID == "" {
		poi.SystemID = state.System.ID
	}
	for i := range state.System.POIs {
		sp := state.System.POIs[i]
		if sp.ID != poi.ID {
			continue
		}
		poi.Class = sp.Class
		poi.Description = sp.Description
		poi.Position = sp.Position
		poi.BaseID = sp.BaseID
		poi.HasBase = sp.HasBase
		poi.BaseName = sp.BaseName
		poi.Hidden = sp.Hidden
		poi.RevealDifficulty = sp.RevealDifficulty
		poi.ExpiresAt = sp.ExpiresAt
		break
	}
	return poi, nil
}

// KBUpdatePOI fetches current POI data and saves it to the knowledge base.
// detectedBy records which agent observed the data (POI provenance).
func KBUpdatePOI(ctx context.Context, client game.GameClient, kb knowledge.Base, detectedBy string) error {
	_, err := KBUpdatePOIData(ctx, client, kb, detectedBy)
	return err
}

// KBUpdatePOIData is like KBUpdatePOI but also returns the captured POI so
// callers can reuse it (e.g. play_as's faction-intel file side effect) without
// issuing a second get_location query.
func KBUpdatePOIData(ctx context.Context, client game.GameClient, kb knowledge.Base, detectedBy string) (game.POI, error) {
	if kb == nil {
		return game.POI{}, fmt.Errorf("knowledge base not configured (use --db-path)")
	}

	poi, err := GetLocationPOI(ctx, client)
	if err != nil {
		return game.POI{}, err
	}
	state := client.GetState()

	kbPOI := knowledge.POI{
		ID:          poi.ID,
		SystemID:    poi.SystemID,
		Name:        poi.Name,
		Type:        poi.Type,
		Class:       poi.Class,
		Description: poi.Description,
		Position: game.Position{
			X: poi.Position.X,
			Y: poi.Position.Y,
		},
		Hidden:           poi.Hidden,
		RevealDifficulty: poi.RevealDifficulty,
		Resources:        poi.Resources,
		LastUpdatedTick:  currentTick(state),
		DetectedBy:       detectedBy,
	}
	if kbPOI.SystemID == "" {
		kbPOI.SystemID = state.System.ID
	}

	if err := kb.RememberPOI(ctx, kbPOI); err != nil {
		return game.POI{}, fmt.Errorf("failed to save POI: %w", err)
	}

	fmt.Printf("Saved POI: %s (%s, %d resources, tick %d)\n",
		kbPOI.Name, kbPOI.Type, len(kbPOI.Resources), kbPOI.LastUpdatedTick)
	return poi, nil
}

// KBUpdateStation fetches base details, market listings, and ship listings at the
// current station and saves them to the knowledge base. source is the tool tag
// recorded as the origin of the captured data (e.g. "play_as" or "worker").
// mc is optional: when non-nil, the market snapshot is also written to the
// market collector DB via mc.WriteSnapshot.
func KBUpdateStation(ctx context.Context, client game.GameClient, kb knowledge.Base, mc *market.Collector, source string) error {
	if kb == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}

	state := client.GetState()
	if !state.Doc {
		return fmt.Errorf("must be docked at a station")
	}

	systemID := state.System.ID
	systemName := state.System.Name
	poiID := state.CurrentPOI
	poiName := "" // Will be filled from base response.

	// --- Base details ---
	if err := client.GetBase(ctx); err != nil {
		fmt.Printf("Warning: get_base failed: %v\n", err)
	} else {
		time.Sleep(game.SleepQuick)

		rawJSON := client.GetRawJSON("base")
		if rawJSON != nil {
			base, err := knowledge.BaseDataFromRawJSON(rawJSON, source, currentTick(state))
			if err != nil {
				fmt.Printf("Warning: failed to parse base data: %v\n", err)
			} else {
				poiName = base.Name
				// Merge dock story from game state if get_base didn't include it.
				if base.Story == "" {
					if s := client.GetState(); s != nil && s.LastDockStory != "" {
						base.Story = s.LastDockStory
					}
				}
				if err := kb.RememberBase(ctx, *base); err != nil {
					fmt.Printf("Warning: failed to save base: %v\n", err)
				} else {
					fmt.Printf("Saved base: %s (%d facilities, %d services)\n",
						base.Name, len(base.Facilities), len(base.Services))
				}
			}
		}
	}

	if poiName == "" {
		poiName = poiID
	}

	// --- Market listings ---
	if err := client.GetListings(ctx); err != nil {
		fmt.Printf("Warning: get_listings failed: %v\n", err)
	} else {
		time.Sleep(game.SleepQuick)

		listings := client.GetMarketListings()
		if mc != nil {
			now := time.Now().UTC()
			snap := market.MarketSnapshot{
				StationID:   poiID,
				StationName: poiName,
				SystemID:    systemID,
				SystemName:  systemName,
				CapturedAt:  now,
				Orders:      market.OrdersFromListings(poiID, listings, source, now),
			}
			if err := mc.WriteSnapshot(ctx, snap); err != nil {
				fmt.Printf("Warning: failed to save market snapshot: %v\n", err)
			} else {
				fmt.Printf("Saved market snapshot: %d listings\n", len(listings))
			}
		}
	}

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

	return nil
}

// publicFacilityUpserter is the subset of the KB that stores the public
// facility catalog; SQLiteKB implements it, in-memory/mocks may not.
type publicFacilityUpserter interface {
	UpsertPublicFacilities(ctx context.Context, rows []knowledge.PublicFacility) error
}

// facStr reads a string field from a decoded JSON object, returning "" if the
// key is missing or not a string.
func facStr(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// facInt reads a numeric field from a decoded JSON object. JSON numbers
// decode to float64 when unmarshaled into map[string]any/any, so this
// converts rather than type-asserting directly to int.
func facInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

// upsertPublicFromFacilityList parses a raw `facility list` response and upserts
// PUBLIC PRODUCTION facilities into the catalog. Best-effort: unknown/renamed
// fields are preserved in DetailsJSON.
//
// Public production lines are spread across three sections, and many stations
// return no public_facilities section at all: station_facilities holds NPC
// station-owned lines, faction_facilities our own faction's, and
// public_facilities other factions'. All three must be scanned.
//
// Those first two sections mix public and private lines, and a private line
// signals that by OMITTING production.public rather than setting it false (e.g.
// voss_redoubt's "The Red Room"). So public requires an explicit true.
func upsertPublicFromFacilityList(ctx context.Context, kb publicFacilityUpserter, raw []byte, tick int) (int, error) {
	var resp struct {
		BaseID            string           `json:"base_id"`
		StationFacilities []map[string]any `json:"station_facilities"`
		FactionFacilities []map[string]any `json:"faction_facilities"`
		PublicFacilities  []map[string]any `json:"public_facilities"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, err
	}
	all := make([]map[string]any, 0, len(resp.StationFacilities)+len(resp.FactionFacilities)+len(resp.PublicFacilities))
	all = append(all, resp.StationFacilities...)
	all = append(all, resp.FactionFacilities...)
	all = append(all, resp.PublicFacilities...)
	if resp.BaseID == "" || len(all) == 0 {
		return 0, nil
	}
	var rows []knowledge.PublicFacility
	for _, m := range all {
		prod, _ := m["production"].(map[string]any)
		if facStr(m, "category") != "production" {
			continue
		}
		if prod == nil {
			continue
		}
		if pub, ok := prod["public"].(bool); !ok || !pub {
			continue
		}
		name := facStr(m, "custom_name")
		if name == "" {
			name = facStr(m, "name")
		}
		recipe := facStr(m, "recipe_id")
		if recipe == "" {
			recipe = facStr(prod, "recipe")
		}
		owner := facStr(m, "faction_id")
		if owner == "" {
			owner = facStr(m, "owner_id")
		}
		// Station-owned lines carry no faction_id; owner stays empty by design.
		fee := facInt(prod, "rental_fee_per_run")
		details, _ := json.Marshal(m)
		rows = append(rows, knowledge.PublicFacility{
			StationID:       resp.BaseID,
			FacilityID:      facStr(m, "facility_id"),
			RecipeID:        recipe,
			FacilityName:    name,
			Category:        facStr(m, "category"),
			OwnerFaction:    owner,
			Level:           facInt(m, "level"),
			RentalFeePerRun: fee,
			LastSeenTick:    tick,
			DetailsJSON:     string(details),
		})
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return len(rows), kb.UpsertPublicFacilities(ctx, rows)
}

// facilityDetail matches the structure returned by the facility list command.
type facilityDetail struct {
	FacilityID           string `json:"facility_id"`
	Type                 string `json:"type"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
	Category             string `json:"category"`
	Active               bool   `json:"active"`
	MaintenanceSatisfied bool   `json:"maintenance_satisfied"`
	Service              string `json:"service,omitempty"`
	RecipeID             string `json:"recipe_id,omitempty"`
}

// KBUpdateFacilities fetches facility details via 'facility list' and saves enriched
// data (description, active, maintenance, service, recipe_id) to the knowledge base.
func KBUpdateFacilities(ctx context.Context, client game.GameClient, kb knowledge.Base) error {
	if kb == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}

	state := client.GetState()
	if !state.Doc {
		return fmt.Errorf("must be docked at a station")
	}

	// Call facility list
	if err := client.RawCommand(ctx, "facility", map[string]any{"action": "list"}); err != nil {
		return fmt.Errorf("facility list failed: %w", err)
	}
	time.Sleep(game.SleepQuick)

	rawJSON := client.GetRawJSON("_last")
	if rawJSON == nil {
		return fmt.Errorf("no facility list data in response")
	}

	if up, ok := kb.(publicFacilityUpserter); ok {
		if n, perr := upsertPublicFromFacilityList(ctx, up, rawJSON, int(currentTick(state))); perr != nil {
			fmt.Printf("public facility capture failed: %v\n", perr)
		} else if n > 0 {
			fmt.Printf("Captured %d public facilities\n", n)
		}
	}

	var resp struct {
		BaseID            string           `json:"base_id"`
		StationFacilities []facilityDetail `json:"station_facilities"`
		PlayerFacilities  []facilityDetail `json:"player_facilities"`
		FactionFacilities []facilityDetail `json:"faction_facilities"`
	}
	if err := json.Unmarshal(rawJSON, &resp); err != nil {
		return fmt.Errorf("failed to parse facility list: %w", err)
	}

	if resp.BaseID == "" {
		return fmt.Errorf("no base_id in facility list response")
	}

	// Merge all facility lists
	var allFacilities []facilityDetail
	allFacilities = append(allFacilities, resp.StationFacilities...)
	allFacilities = append(allFacilities, resp.PlayerFacilities...)
	allFacilities = append(allFacilities, resp.FactionFacilities...)

	// Load existing base from KB, update facilities
	base, err := kb.GetBase(ctx, resp.BaseID)
	if err != nil || base == nil {
		return fmt.Errorf("base %s not found in KB (run update_station first)", resp.BaseID)
	}

	// Build enriched facility list
	var facilities []knowledge.Facility
	for _, f := range allFacilities {
		facility := knowledge.Facility{
			ID:                   f.Type,
			InstanceID:           f.FacilityID,
			Name:                 f.Name,
			Description:          f.Description,
			Category:             f.Category,
			Active:               f.Active,
			MaintenanceSatisfied: f.MaintenanceSatisfied,
			Service:              f.Service,
			RecipeID:             f.RecipeID,
			LastUpdated:          currentTick(state),
		}
		facilities = append(facilities, facility)
	}

	base.Facilities = facilities
	base.LastUpdatedTick = currentTick(state)

	if err := kb.RememberBase(ctx, *base); err != nil {
		return fmt.Errorf("failed to save base with facilities: %w", err)
	}

	fmt.Printf("Saved %d facilities for %s\n", len(facilities), base.Name)
	return nil
}

// KBUpdateMissions fetches the mission board at the current station and
// upserts each keyable entry into the KB mission catalog. Procedural
// missions (no template_id) are keyed by a synthetic id (mission_id with the
// ~<hash> suffix stripped) and captured; only entries with neither a
// template_id nor a mission_id are skipped. Ported from play_as so the
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
		id, _ := knowledge.MissionCatalogID(entry)
		if id == "" {
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
					id, baseID, d.Field, d.OldValue, d.NewValue)
			}
		default:
			unchanged++
		}
	}

	fmt.Printf("update_missions: %d new, %d unchanged, %d changed, %d unkeyed skipped\n",
		inserted, unchanged, changed, skipped)
	return nil
}

// KBUpdateAll runs update_system, update_poi, and (if docked) update_station,
// update_facilities, and update_missions. detectedBy records which agent
// observed the system/POI data (provenance).
// mc is optional: when non-nil, the market snapshot is written via the collector.
func KBUpdateAll(ctx context.Context, client game.GameClient, kb knowledge.Base, mc *market.Collector, detectedBy string) error {
	if kb == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}

	if err := KBUpdateSystem(ctx, client, kb, detectedBy); err != nil {
		fmt.Printf("Warning: update_system: %v\n", err)
	}
	if err := KBUpdatePOI(ctx, client, kb, detectedBy); err != nil {
		fmt.Printf("Warning: update_poi: %v\n", err)
	}

	state := client.GetState()
	if state.Doc {
		if err := KBUpdateStation(ctx, client, kb, mc, "worker"); err != nil {
			fmt.Printf("Warning: update_station: %v\n", err)
		}
		if err := KBUpdateFacilities(ctx, client, kb); err != nil {
			fmt.Printf("Warning: update_facilities: %v\n", err)
		}
		if err := KBUpdateMissions(ctx, client, kb); err != nil {
			fmt.Printf("Warning: update_missions: %v\n", err)
		}
	} else {
		fmt.Println("(Not docked — skipping station/facilities update)")
	}

	return nil
}

// CaptureMarket persists the full source-classified market data from the
// client's most recent (full, no-item_id) view_market response: buy-order demand
// into the demand tables and sell-order supply into the supply tables, each
// replacing the station's entire order set. Best-effort: silently no-ops when
// the KB is absent, there is no market data, or the player is not at a station.
// A single freshness gate (keyed on the demand ledger) guards both sides, since
// they come from the same read.
func CaptureMarket(ctx context.Context, client game.GameClient, kb knowledge.Base) {
	if kb == nil {
		return
	}
	sqlite, ok := kb.(*knowledge.SQLiteKB)
	if !ok {
		return
	}
	state := client.GetState()
	if state == nil {
		return
	}
	now := time.Now()
	raw := client.GetRawJSON("market")
	buyOrders := parseStationBuyOrders(raw, state.CurrentPOI, state.CurrentSystem, now)
	sellOrders := parseStationSellOrders(raw, state.CurrentPOI, state.CurrentSystem, now)
	if len(buyOrders) == 0 && len(sellOrders) == 0 {
		return
	}
	// Skip all writes if this station's market was captured recently (possibly by
	// another agent) — avoids redundant work across many agents sharing a station.
	if last, ok, err := sqlite.LatestDemandCapture(ctx, state.CurrentPOI); err == nil && ok && isFresh(last, now, demandFreshness) {
		return
	}
	_ = sqlite.ReplaceStationBuyOrders(ctx, state.CurrentPOI, buyOrders)
	_ = sqlite.RecordDemandHistory(ctx, aggregateDemandHistory(buyOrders, now, demandHistoryBucket))
	_ = sqlite.ReplaceStationSellOrders(ctx, state.CurrentPOI, sellOrders)
	_ = sqlite.RecordSupplyHistory(ctx, aggregateSupplyHistory(sellOrders, now, demandHistoryBucket))
}
