package knowledge

import (
	"encoding/json"
	"fmt"
)

// BaseDataFromRawJSON extracts base information from raw get_base response JSON
// Returns a SpaceBase that can be saved to the knowledge base
//
// Example usage in an agent:
//
//	rawJSON := client.GetRawJSON("base")
//	if rawJSON != nil {
//	    base, err := knowledge.BaseDataFromRawJSON(rawJSON, agentID, state.GetTick())
//	    if err != nil {
//	        logger.Printf("Failed to parse base data: %v", err)
//	        return err
//	    }
//	    if err := kb.RememberBase(ctx, *base); err != nil {
//	        logger.Printf("Failed to save base to knowledge base: %v", err)
//	        return err
//	    }
//	}
func BaseDataFromRawJSON(rawJSON []byte, discoveredBy string, lastUpdatedTick int64) (*SpaceBase, error) {
	var response struct {
		Base struct {
			ID           string            `json:"id"`
			POIID        string            `json:"poi_id"`
			Name         string            `json:"name"`
			Description  string            `json:"description"`
			Empire       string            `json:"empire"`
			DefenseLevel int               `json:"defense_level"`
			HasDrones    bool              `json:"has_drones"`
			PublicAccess bool              `json:"public_access"`
			Services     map[string]bool   `json:"services"`
			Facilities   []string          `json:"facilities"`
			Market       []json.RawMessage `json:"market"`
		} `json:"base"`
	}

	if err := json.Unmarshal(rawJSON, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal base data: %w", err)
	}

	// Parse market items
	var marketItems []BaseMarketItem
	for _, rawItem := range response.Base.Market {
		var item struct {
			ID        string  `json:"id"`
			ItemID    string  `json:"item_id"`
			PriceEach float64 `json:"price_each"`
			Quantity  int     `json:"quantity"`
			IsNPC     bool    `json:"is_npc"`
		}
		if err := json.Unmarshal(rawItem, &item); err != nil {
			// Skip invalid items
			continue
		}
		marketItems = append(marketItems, BaseMarketItem{
			ID:        item.ID,
			ItemID:    item.ItemID,
			PriceEach: item.PriceEach,
			Quantity:  item.Quantity,
			IsNPC:     item.IsNPC,
		})
	}

	base := &SpaceBase{
		ID:              response.Base.ID,
		POIID:           response.Base.POIID,
		Name:            response.Base.Name,
		Description:     response.Base.Description,
		Empire:          response.Base.Empire,
		DefenseLevel:    response.Base.DefenseLevel,
		HasDrones:       response.Base.HasDrones,
		PublicAccess:    response.Base.PublicAccess,
		Services:        response.Base.Services,
		Facilities:      response.Base.Facilities,
		Market:          marketItems,
		DiscoveredBy:    discoveredBy,
		LastUpdatedTick: lastUpdatedTick,
	}

	return base, nil
}
