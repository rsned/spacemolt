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
			HasDrones         bool              `json:"has_drones"`
			PublicAccess      bool              `json:"public_access"`
			PirateRepRequired int               `json:"pirate_rep_required"`
			Services     map[string]bool   `json:"services"`
			Facilities   []string          `json:"facilities"`
			Market       []json.RawMessage `json:"market"`
		} `json:"base"`
		Condition *struct {
			Condition         string `json:"condition"`
			ConditionText     string `json:"condition_text"`
			SatisfactionPct   int    `json:"satisfaction_pct"`
			SatisfiedCount    int    `json:"satisfied_count"`
			TotalServiceInfra int    `json:"total_service_infra"`
		} `json:"condition"`
		Services []string `json:"services"` // Top-level services array
		Story    string   `json:"story"`
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

	// Parse facilities with category information
	// Generate unique instance IDs for facilities from the basic ID list.
	// The get_base response only provides type IDs; if a station has duplicates
	// of the same type, append a counter suffix to keep instance IDs unique.
	instanceCount := make(map[string]int)
	var facilities []Facility
	for _, facilityID := range response.Base.Facilities {
		instanceCount[facilityID]++
		instanceID := facilityID
		if instanceCount[facilityID] > 1 {
			instanceID = fmt.Sprintf("%s_%d", facilityID, instanceCount[facilityID])
		}

		if facilityInfo, ok := FacilityCategoryMapping[facilityID]; ok {
			facilities = append(facilities, Facility{
				ID:          facilityInfo.ID,
				InstanceID:  instanceID,
				Name:        facilityInfo.Name,
				Category:    facilityInfo.Category,
				Level:       facilityInfo.Level,
				LastUpdated: lastUpdatedTick,
			})
		} else {
			facilities = append(facilities, Facility{
				ID:          facilityID,
				InstanceID:  instanceID,
				Name:        facilityID,
				Category:    "unknown",
				Level:       0,
				LastUpdated: lastUpdatedTick,
			})
		}
	}

	// Merge services from both the base object map and the top-level array.
	services := make(map[string]bool)
	for _, svc := range response.Services {
		services[svc] = true
	}
	for svc, avail := range response.Base.Services {
		services[svc] = avail
	}

	base := &SpaceBase{
		ID:              response.Base.ID,
		POIID:           response.Base.POIID,
		Name:            response.Base.Name,
		Description:     response.Base.Description,
		Story:           response.Story,
		Empire:          response.Base.Empire,
		DefenseLevel:    response.Base.DefenseLevel,
		HasDrones:         response.Base.HasDrones,
		PublicAccess:      response.Base.PublicAccess,
		PirateRepRequired: response.Base.PirateRepRequired,
		Services:          services,
		Facilities:        facilities,
		Market:            marketItems,
		LastUpdatedTick:   lastUpdatedTick,
	}

	if response.Condition != nil {
		base.Condition = response.Condition.Condition
		base.ConditionText = response.Condition.ConditionText
		base.SatisfactionPct = response.Condition.SatisfactionPct
		base.SatisfiedCount = response.Condition.SatisfiedCount
		base.TotalServiceInfra = response.Condition.TotalServiceInfra
	}

	return base, nil
}
