package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

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
		Empire:          state.System.Empire,
		IsStronghold:    state.System.IsStronghold,
		Connections:     extractConnections(state.System.Connections),
		LastUpdatedTick: state.GetTick(),
		Position: game.Position{
			X: state.System.Position.X,
			Y: state.System.Position.Y,
		},
	}

	if err := globalKB.RememberSystem(ctx, kbSystem); err != nil {
		return fmt.Errorf("failed to save system: %w", err)
	}

	fmt.Printf("Saved system: %s (%d connections, tick %d)\n",
		state.System.Name, len(state.System.Connections), kbSystem.LastUpdatedTick)
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
		Services:        poiResp.Services,
		LastUpdatedTick: state.GetTick(),
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
			var baseResp serverapi.GetBaseResponse
			if err := json.Unmarshal(rawJSON, &baseResp); err == nil && baseResp.Base != nil {
				poiName = baseResp.Base.Name

				services := make(map[string]bool)
				for _, svc := range baseResp.Services {
					services[svc] = true
				}
				if baseResp.Base.Services != nil {
					for svc, avail := range baseResp.Base.Services {
						services[svc] = avail
					}
				}

				kbBase := knowledge.SpaceBase{
					ID:              baseResp.Base.ID,
					POIID:           poiID,
					Name:            baseResp.Base.Name,
					Description:     baseResp.Base.Description,
					Empire:          baseResp.Base.Empire,
					DefenseLevel:    baseResp.Base.DefenseLevel,
					HasDrones:       baseResp.Base.HasDrones,
					PublicAccess:    baseResp.Base.PublicAccess,
					Services:        services,
					LastUpdatedTick: state.CurrentTick,
				}

				if err := globalKB.RememberBase(ctx, kbBase); err != nil {
					fmt.Printf("Warning: failed to save base: %v\n", err)
				} else {
					fmt.Printf("Saved base: %s\n", baseResp.Base.Name)
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
		snapshot := convertMarketListings(systemID, systemName, poiID, poiName, state.CurrentTick, listings)
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
					GameTick:    state.CurrentTick,
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
	} else {
		fmt.Println("(Not docked — skipping station update)")
	}

	return nil
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
