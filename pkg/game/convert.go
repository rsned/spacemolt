package game

import (
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// PositionFromAPI converts a serverapi Position to an internal Position.
func PositionFromAPI(ext serverapi.Position) Position {
	return Position{
		X: ext.X,
		Y: ext.Y,
		Z: ext.Z,
	}
}

// POIResourceFromAPI converts a serverapi POIResource to an internal POIResource.
func POIResourceFromAPI(ext serverapi.POIResource) POIResource {
	return POIResource{
		ResourceID: ext.ResourceID,
		Richness:   ext.Richness,
		Remaining:  ext.Remaining,
	}
}

// CargoItemFromAPI converts a serverapi CargoItem to an internal CargoItem.
func CargoItemFromAPI(ext serverapi.CargoItem) CargoItem {
	return CargoItem{
		ItemID:   ext.ItemID,
		Quantity: ext.Quantity,
	}
}

// ConnectionInfoFromAPI converts a serverapi ConnectionInfo to an internal ConnectionInfo.
func ConnectionInfoFromAPI(ext serverapi.ConnectionInfo) ConnectionInfo {
	return ConnectionInfo{
		SystemID: ext.SystemID,
		Name:     ext.Name,
		Distance: ext.Distance,
	}
}

// SkillFromAPI converts a serverapi Skill to an internal Skill.
func SkillFromAPI(ext serverapi.Skill) Skill {
	return Skill{
		Level: ext.Level,
		XP:    ext.XP,
	}
}

// PlayerStatsFromAPI converts a serverapi PlayerStats to an internal PlayerStats.
func PlayerStatsFromAPI(ext serverapi.PlayerStats) PlayerStats {
	return PlayerStats{
		ShipsDestroyed:    ext.ShipsDestroyed,
		TimesDestroyed:    ext.TimesDestroyed,
		OreMined:          ext.OreMined,
		CreditsEarned:     ext.CreditsEarned,
		CreditsSpent:      ext.CreditsSpent,
		TradesCompleted:   ext.TradesCompleted,
		SystemsDiscovered: ext.SystemsDiscovered,
		ItemsCrafted:      ext.ItemsCrafted,
		MissionsCompleted: ext.MissionsCompleted,
		BasesDestroyed:    ext.BasesDestroyed,
		DistanceTraveled:  ext.DistanceTraveled,
		PiratesDestroyed:  ext.PiratesDestroyed,
		ShipsLost:         ext.ShipsLost,
		TimePlayed:        ext.TimePlayed,
	}
}

// ModuleDefinitionFromAPI converts a serverapi ModuleDefinition to an internal ModuleDefinition.
func ModuleDefinitionFromAPI(ext serverapi.ModuleDefinition) ModuleDefinition {
	return ModuleDefinition{
		ID:          ext.ID,
		Name:        ext.Name,
		Type:        ext.Type,
		Description: ext.Description,
	}
}

// SkillDefinitionFromAPI converts a serverapi SkillDefinition to an internal SkillDefinition.
func SkillDefinitionFromAPI(ext serverapi.SkillDefinition) SkillDefinition {
	xpPerLevel := make([]float64, len(ext.XpPerLevel))
	copy(xpPerLevel, ext.XpPerLevel)

	var bonusPerLevel map[string]float64
	if len(ext.BonusPerLevel) > 0 {
		bonusPerLevel = make(map[string]float64, len(ext.BonusPerLevel))
		for k, v := range ext.BonusPerLevel {
			bonusPerLevel[k] = v
		}
	}

	return SkillDefinition{
		ID:            ext.ID,
		Name:          ext.Name,
		Description:   ext.Description,
		Category:      ext.Category,
		MaxLevel:      ext.MaxLevel,
		XpPerLevel:    xpPerLevel,
		BonusPerLevel: bonusPerLevel,
	}
}

// POIFromAPI converts a serverapi POI to an internal POI.
func POIFromAPI(ext serverapi.POI) POI {
	resources := make([]POIResource, len(ext.Resources))
	for i, r := range ext.Resources {
		resources[i] = POIResourceFromAPI(r)
	}
	return POI{
		ID:          ext.ID,
		SystemID:    ext.SystemID,
		Type:        ext.Type,
		Class:       ext.Class,
		Name:        ext.Name,
		Description: ext.Description,
		Position:    PositionFromAPI(ext.Position),
		Resources:   resources,
		BaseID:      ext.BaseID,
		HasBase:     ext.HasBase,
		BaseName:    ext.BaseName,
		Online:      ext.Online,
		Hidden:      ext.Hidden,
	}
}

// SystemDataFromAPI converts a serverapi SystemData to an internal SystemData.
func SystemDataFromAPI(ext serverapi.SystemData) SystemData {
	pois := make([]POI, len(ext.POIs))
	for i, p := range ext.POIs {
		pois[i] = POIFromAPI(p)
	}
	connections := make([]ConnectionInfo, len(ext.Connections))
	for i, c := range ext.Connections {
		connections[i] = ConnectionInfoFromAPI(c)
	}
	return SystemData{
		ID:             ext.ID,
		Name:           ext.Name,
		Description:    ext.Description,
		Empire:         ext.Empire,
		PoliceLevel:    ext.PoliceLevel,
		SecurityStatus: ext.SecurityStatus,
		IsStronghold:   ext.IsStronghold,
		Online:         ext.Online,
		POIs:           pois,
		Connections:    connections,
		Discovered:     ext.Discovered,
		Position:       PositionFromAPI(ext.Position),
		DiscoveredBy:   ext.DiscoveredBy,
	}
}

// NearbyPlayerFromAPI converts a serverapi NearbyPlayer to an internal NearbyPlayer.
func NearbyPlayerFromAPI(ext serverapi.NearbyPlayer) NearbyPlayer {
	return NearbyPlayer{
		PlayerID:       ext.PlayerID,
		Username:       ext.Username,
		ShipClass:      ext.ShipClass,
		PrimaryColor:   ext.PrimaryColor,
		SecondaryColor: ext.SecondaryColor,
		Anonymous:      ext.Anonymous,
		InCombat:       ext.InCombat,
		FactionID:      ext.FactionID,
		FactionTag:     ext.FactionTag,
		StatusMessage:  ext.StatusMessage,
		ClanTag:        ext.ClanTag,
	}
}

// ShipFromAPI converts a serverapi Ship to an internal Ship.
func ShipFromAPI(ext serverapi.Ship) Ship {
	modules := make([]string, len(ext.Modules))
	copy(modules, ext.Modules)

	cargo := make([]CargoItem, len(ext.Cargo))
	for i, c := range ext.Cargo {
		cargo[i] = CargoItemFromAPI(c)
	}

	var activeBuffs []map[string]any
	if len(ext.ActiveBuffs) > 0 {
		activeBuffs = make([]map[string]any, len(ext.ActiveBuffs))
		for i, b := range ext.ActiveBuffs {
			m := map[string]any{}
			if b.ItemID != "" {
				m["item_id"] = b.ItemID
			}
			if b.Stat != "" {
				m["stat"] = b.Stat
			}
			if b.Amount != 0 {
				m["amount"] = b.Amount
			}
			if b.TicksLeft != 0 {
				m["ticks_left"] = b.TicksLeft
			}
			if b.ExpiresAt != 0 {
				m["expires_at"] = b.ExpiresAt
			}
			activeBuffs[i] = m
		}
	}

	return Ship{
		ID:                       ext.ID,
		OwnerID:                  ext.OwnerID,
		ClassID:                  ext.ClassID,
		Name:                     ext.Name,
		Hull:                     ext.Hull,
		MaxHull:                  ext.MaxHull,
		Shield:                   ext.Shield,
		MaxShield:                ext.MaxShield,
		ShieldRecharge:           ext.ShieldRecharge,
		Armor:                    ext.Armor,
		Speed:                    ext.Speed,
		Fuel:                     ext.Fuel,
		MaxFuel:                  ext.MaxFuel,
		CargoUsed:                ext.CargoUsed,
		CargoCapacity:            ext.CargoCapacity,
		CPUUsed:                  ext.CPUUsed,
		CPUCapacity:              ext.CPUCapacity,
		PowerUsed:                ext.PowerUsed,
		PowerCapacity:            ext.PowerCapacity,
		WeaponSlots:              ext.WeaponSlots,
		DefenseSlots:             ext.DefenseSlots,
		UtilitySlots:             ext.UtilitySlots,
		Modules:                  modules,
		Cargo:                    cargo,
		ActiveBuffs:              activeBuffs,
		DamagePenalty:            ext.DamagePenalty,
		SpeedPenalty:             ext.SpeedPenalty,
		DisruptionTicksRemaining: ext.DisruptionTicksRemaining,
		DockedAtBase:             ext.DockedAtBase,
		LastProcessTick:          ext.LastProcessTick,
		CreatedAt:                ext.CreatedAt,
	}
}

// PlayerFromAPI converts a serverapi Player to an internal Player.
func PlayerFromAPI(ext serverapi.Player) Player {
	skills := make(map[string]Skill, len(ext.Skills))
	for k, v := range ext.Skills {
		skills[k] = SkillFromAPI(v)
	}

	var skillXP map[string]float64
	if len(ext.SkillXP) > 0 {
		skillXP = make(map[string]float64, len(ext.SkillXP))
		for k, v := range ext.SkillXP {
			skillXP[k] = v
		}
	}

	return Player{
		ID:             ext.ID,
		Username:       ext.Username,
		Empire:         ext.Empire,
		Credits:        ext.Credits,
		CurrentSystem:  ext.CurrentSystem,
		CurrentPOI:     ext.CurrentPOI,
		CurrentShipID:  ext.CurrentShipID,
		HomeBase:       ext.HomeBase,
		DockedAtBase:   ext.DockedAtBase,
		FactionID:      ext.FactionID,
		FactionRank:    ext.FactionRank,
		StatusMessage:  ext.StatusMessage,
		ClanTag:        ext.ClanTag,
		PrimaryColor:   ext.PrimaryColor,
		SecondaryColor: ext.SecondaryColor,
		Anonymous:      ext.Anonymous,
		IsCloaked:      ext.IsCloaked,
		Skills:         skills,
		SkillXP:        skillXP,
		Stats:             PlayerStatsFromAPI(ext.Stats),
		TowingWreckID:     ext.TowingWreckID,
		Experience:        ext.Experience,
		DiscoveredSystems: ext.DiscoveredSystems,
		LastActiveAt:      ext.LastActiveAt,
		LastLoginAt:       ext.LastLoginAt,
		CreatedAt:         ext.CreatedAt,
	}
}

// MarketListingFromAPI converts a serverapi MarketListing to an internal MarketListing.
// Handles server field name variations (price_per_unit/price_each, total_price/total, listed_by/seller).
func MarketListingFromAPI(ext serverapi.MarketListing) MarketListing {
	pricePerUnit := ext.PricePerUnit
	if pricePerUnit == 0 {
		pricePerUnit = ext.PriceEach
	}

	totalPrice := ext.TotalPrice
	if totalPrice == 0 {
		totalPrice = ext.Total
	}

	listedBy := ext.ListedBy
	if listedBy == "" {
		listedBy = ext.Seller
	}

	return MarketListing{
		ItemID:       ext.ItemID,
		ItemType:     ext.ItemType,
		Quantity:     ext.Quantity,
		PricePerUnit: pricePerUnit,
		TotalPrice:   totalPrice,
		Type:         ext.Type,
		ListedBy:     listedBy,
	}
}
