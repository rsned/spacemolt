package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// currentTick returns the best available game tick: from the global clock if
// running, otherwise from the client state's last server-reported value.
func currentTick(state *game.State) int64 {
	if globalClock != nil {
		return globalClock.Tick()
	}
	return currentTick(state)
}

// kbUpdateSystem fetches the current system data and saves it to the knowledge base.
func kbUpdateSystem(client game.GameClient, ctx context.Context) error {
	if globalKB == nil {
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
		Position: game.Position{
			X: state.System.Position.X,
			Y: state.System.Position.Y,
		},
	}

	if err := globalKB.RememberSystem(ctx, kbSystem); err != nil {
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
		}
		if err := globalKB.RememberPOI(ctx, kbPOI); err != nil {
			fmt.Printf("  Warning: failed to save POI %s: %v\n", poi.Name, err)
		} else {
			poiCount++
		}
	}

	fmt.Printf("Saved system: %s (%d POIs, %d connections, tick %d)\n",
		state.System.Name, poiCount, len(state.System.Connections), kbSystem.LastUpdatedTick)
	return nil
}

// kbUpdatePOI fetches current POI data and saves it to the knowledge base.
func kbUpdatePOI(client game.GameClient, ctx context.Context) error {
	if globalKB == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}

	if err := client.GetPOI(ctx); err != nil {
		return fmt.Errorf("get_poi failed: %w", err)
	}
	time.Sleep(game.SleepQuick)

	state := client.GetState()

	// Build POI from the current location's data in state.
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
	}

	// Extract resources if present.
	for _, r := range poiResp.Resources {
		kbPOI.Resources = append(kbPOI.Resources, game.POIResource{
			ResourceID: r.ResourceID,
			Richness:   r.Richness,
			Remaining:  r.Remaining,
		})
	}

	if err := globalKB.RememberPOI(ctx, kbPOI); err != nil {
		return fmt.Errorf("failed to save POI: %w", err)
	}

	fmt.Printf("Saved POI: %s (%s, %d resources, tick %d)\n",
		kbPOI.Name, kbPOI.Type, len(kbPOI.Resources), kbPOI.LastUpdatedTick)
	return nil
}

// kbUpdateStation fetches base details, market listings, and ship listings at the
// current station and saves them to the knowledge base.
func kbUpdateStation(client game.GameClient, ctx context.Context) error {
	if globalKB == nil {
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
			base, err := knowledge.BaseDataFromRawJSON(rawJSON, "play_as", currentTick(state))
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
				if err := globalKB.RememberBase(ctx, *base); err != nil {
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
		if err := globalKB.StoreMarketSnapshot(ctx, snapshot, "play_as"); err != nil {
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
				if err := globalKB.StoreShipListings(ctx, shipListings, "play_as"); err != nil {
					fmt.Printf("Warning: failed to save ship listings: %v\n", err)
				} else {
					fmt.Printf("Saved ship listings: %d ships\n", len(ships))
				}
			}
		}
	}

	return nil
}

// kbUpdateAll runs update_system, update_poi, and (if docked) update_station.
func kbUpdateAll(client game.GameClient, ctx context.Context) error {
	if globalKB == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}

	if err := kbUpdateSystem(client, ctx); err != nil {
		fmt.Printf("Warning: update_system: %v\n", err)
	}
	if err := kbUpdatePOI(client, ctx); err != nil {
		fmt.Printf("Warning: update_poi: %v\n", err)
	}

	state := client.GetState()
	if state.Doc {
		if err := kbUpdateStation(client, ctx); err != nil {
			fmt.Printf("Warning: update_station: %v\n", err)
		}
		if err := kbUpdateFacilities(client, ctx); err != nil {
			fmt.Printf("Warning: update_facilities: %v\n", err)
		}
		if err := kbUpdateMissions(client, ctx); err != nil {
			fmt.Printf("Warning: update_missions: %v\n", err)
		}
	} else {
		fmt.Println("(Not docked — skipping station/facilities update)")
	}

	return nil
}

// kbUpdateFacilities fetches facility details via 'facility list' and saves enriched
// data (description, active, maintenance, service, recipe_id) to the knowledge base.
func kbUpdateFacilities(client game.GameClient, ctx context.Context) error {
	if globalKB == nil {
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
	base, err := globalKB.GetBase(ctx, resp.BaseID)
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

	if err := globalKB.RememberBase(ctx, *base); err != nil {
		return fmt.Errorf("failed to save base with facilities: %w", err)
	}

	fmt.Printf("Saved %d facilities for %s\n", len(facilities), base.Name)
	return nil
}

// kbUpdateMissions fetches the mission board at the current station and upserts
// each hand-authored (non-procedural) entry into the knowledge-base mission
// catalog. Procedural missions (empty template_id) are counted and skipped.
func kbUpdateMissions(client game.GameClient, ctx context.Context) error {
	if globalKB == nil {
		return fmt.Errorf("knowledge base not configured (use --db-path)")
	}

	state := client.GetState()
	if !state.Doc {
		fmt.Println("(Not docked — skipping missions update)")
		return nil
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
		res, err := globalKB.UpsertMissionTemplate(ctx, entry, baseID, systemID, tick)
		if err != nil {
			fmt.Printf("Warning: upsert %s: %v\n", entry.MissionID, err)
			continue
		}
		switch {
		case res.Inserted:
			inserted++
		case len(res.Diffs) > 0:
			changed++
			fmt.Printf("Mission template %q changed at %s:\n", entry.TemplateID, baseID)
			for _, d := range res.Diffs {
				fmt.Printf("  %s: %q -> %q\n", d.Field, d.OldValue, d.NewValue)
				fmt.Fprintf(os.Stderr, "mission template %s changed at base %s: field=%s old=%q new=%q\n",
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

// --- Helpers ---

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
