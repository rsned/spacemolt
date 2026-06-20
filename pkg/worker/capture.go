package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
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

// convertMarketListings converts game market listings to a knowledge snapshot.
func convertMarketListings(systemID, systemName, stationID, stationName string, gameTick int64, gameListings []game.MarketListing) knowledge.MarketSnapshot {
	listings := make([]knowledge.MarketListing, len(gameListings))
	for i, l := range gameListings {
		listings[i] = knowledge.MarketListing{
			ItemID:       l.ItemID,
			ItemType:     l.ItemType,
			Quantity:     l.Quantity,
			PricePerUnit: l.PricePerUnit,
			TotalPrice:   l.TotalPrice,
			Type:         l.Type,
			ListedBy:     l.ListedBy,
		}
	}

	return knowledge.MarketSnapshot{
		SystemID:    systemID,
		SystemName:  systemName,
		StationID:   stationID,
		StationName: stationName,
		GameTick:    gameTick,
		Listings:    listings,
	}
}

// extractShipListingsFromRaw parses a raw JSON response into ship listings.
func extractShipListingsFromRaw(serverData map[string]any) []knowledge.ShipListing {
	var ships []knowledge.ShipListing

	shipsData, ok := serverData["ships"]
	if !ok {
		return ships
	}

	shipsArray, ok := shipsData.([]any)
	if !ok {
		return ships
	}

	for _, shipData := range shipsArray {
		shipMap, ok := shipData.(map[string]any)
		if !ok {
			continue
		}

		ship := knowledge.ShipListing{}

		if id, ok := shipMap["id"].(string); ok {
			ship.ShipClass = id
		}
		if name, ok := shipMap["name"].(string); ok {
			ship.ShipName = name
		}
		if price, ok := shipMap["price"].(float64); ok {
			ship.BasePrice = price
		}
		if desc, ok := shipMap["description"].(string); ok {
			ship.Description = desc
		}
		if cargo, ok := shipMap["cargo_space"].(float64); ok {
			ship.CargoSpace = int(cargo)
		}
		if modules, ok := shipMap["module_slots"].(float64); ok {
			ship.ModuleSlots = int(modules)
		}
		if utility, ok := shipMap["utility_slots"].(float64); ok {
			ship.UtilitySlots = int(utility)
		}
		if weapons, ok := shipMap["weapon_slots"].(float64); ok {
			ship.WeaponSlots = int(weapons)
		}

		ships = append(ships, ship)
	}

	return ships
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

// KBUpdatePOI fetches current POI data and saves it to the knowledge base.
// detectedBy records which agent observed the data (POI provenance).
func KBUpdatePOI(ctx context.Context, client game.GameClient, kb knowledge.Base, detectedBy string) error {
	if kb == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}

	if err := client.GetPOI(ctx); err != nil {
		return fmt.Errorf("get_poi failed: %w", err)
	}
	time.Sleep(game.SleepQuick)

	state := client.GetState()

	rawJSON := client.GetRawJSON("poi")
	if rawJSON == nil {
		return fmt.Errorf("no POI data in response")
	}
	var poiResp serverapi.GetPOIResponse
	if err := json.Unmarshal(rawJSON, &poiResp); err != nil {
		return fmt.Errorf("failed to parse POI response: %w", err)
	}

	kbPOI := knowledge.POI{
		ID:          poiResp.POI.ID,
		SystemID:    state.System.ID,
		Name:        poiResp.POI.Name,
		Type:        poiResp.POI.Type,
		Class:       poiResp.POI.Class,
		Description: poiResp.POI.Description,
		Position: game.Position{
			X: poiResp.POI.Position.X,
			Y: poiResp.POI.Position.Y,
		},
		Services:         poiResp.Services,
		Hidden:           poiResp.POI.Hidden,
		RevealDifficulty: poiResp.POI.RevealDifficulty,
		LastUpdatedTick:  currentTick(state),
		DetectedBy:       detectedBy,
	}

	// Extract resources if present.
	for _, r := range poiResp.Resources {
		kbPOI.Resources = append(kbPOI.Resources, game.POIResource{
			ResourceID: r.ResourceID,
			Richness:   r.Richness,
			Remaining:  r.Remaining,
		})
	}

	if err := kb.RememberPOI(ctx, kbPOI); err != nil {
		return fmt.Errorf("failed to save POI: %w", err)
	}

	fmt.Printf("Saved POI: %s (%s, %d resources, tick %d)\n",
		kbPOI.Name, kbPOI.Type, len(kbPOI.Resources), kbPOI.LastUpdatedTick)
	return nil
}

// KBUpdateStation fetches base details, market listings, and ship listings at the
// current station and saves them to the knowledge base.
func KBUpdateStation(ctx context.Context, client game.GameClient, kb knowledge.Base) error {
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
			base, err := knowledge.BaseDataFromRawJSON(rawJSON, "worker", currentTick(state))
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
		snapshot := convertMarketListings(systemID, systemName, poiID, poiName, currentTick(state), listings)
		if err := kb.StoreMarketSnapshot(ctx, snapshot, "worker"); err != nil {
			fmt.Printf("Warning: failed to save market snapshot: %v\n", err)
		} else {
			fmt.Printf("Saved market snapshot: %d listings\n", len(listings))
		}
	}

	// --- Ship listings ---
	if err := client.BrowseShips(ctx, nil); err != nil {
		fmt.Printf("Warning: browse_ships failed: %v\n", err)
	} else {
		time.Sleep(game.SleepQuick)

		rawJSON := client.GetRawJSON("ships")
		if rawJSON != nil {
			var serverData map[string]any
			if err := json.Unmarshal(rawJSON, &serverData); err == nil {
				ships := extractShipListingsFromRaw(serverData)
				shipListings := knowledge.ShipListings{
					SystemID:    systemID,
					SystemName:  systemName,
					StationID:   poiID,
					StationName: poiName,
					GameTick:    currentTick(state),
					Listings:    ships,
				}
				if err := kb.StoreShipListings(ctx, shipListings, "worker"); err != nil {
					fmt.Printf("Warning: failed to save ship listings: %v\n", err)
				} else {
					fmt.Printf("Saved ship listings: %d ships\n", len(ships))
				}
			}
		}
	}

	return nil
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

// KBUpdateAll runs update_system, update_poi, and (if docked) update_station and
// update_facilities. It does NOT run update_missions (that is play_as-specific).
// detectedBy records which agent observed the system/POI data (provenance).
func KBUpdateAll(ctx context.Context, client game.GameClient, kb knowledge.Base, detectedBy string) error {
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
		if err := KBUpdateStation(ctx, client, kb); err != nil {
			fmt.Printf("Warning: update_station: %v\n", err)
		}
		if err := KBUpdateFacilities(ctx, client, kb); err != nil {
			fmt.Printf("Warning: update_facilities: %v\n", err)
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
