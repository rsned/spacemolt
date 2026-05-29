package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// ItemsResponse represents the server response from catalog_items
type ItemsResponse struct {
	Items []CatalogItemJSON `json:"items"`
}

// CatalogItemJSON represents an item from the game catalog JSON.
// Fields are a superset of all item types; unused fields will be zero/nil.
type CatalogItemJSON struct {
	// Universal fields (present on all items)
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Size        int    `json:"size"`
	BaseValue   int    `json:"base_value"`

	// Present on non-module items
	Category  string `json:"category"`
	Rarity    string `json:"rarity"`
	Stackable bool   `json:"stackable"`
	Tradeable bool   `json:"tradeable"`

	// Module common fields (present when type/type_id are set)
	Type      string `json:"type"`
	TypeID    string `json:"type_id"`
	CPUUsage  int    `json:"cpu_usage"`
	PowerUsage int   `json:"power_usage"`
	Hidden    bool   `json:"hidden"`
	Special   string `json:"special"`

	// Weapon fields
	Damage      int    `json:"damage"`
	DamageType  string `json:"damage_type"`
	Range       *int   `json:"range"`
	Reach       *int   `json:"reach"`
	Cooldown    int    `json:"cooldown"`
	AmmoType    string `json:"ammo_type"`
	MagazineSize *int  `json:"magazine_size"`

	// Defense fields
	ArmorBonus          *int     `json:"armor_bonus"`
	HullBonus           *int     `json:"hull_bonus"`
	ShieldBonus         *int     `json:"shield_bonus"`
	ShieldRechargeBonus *int     `json:"shield_recharge_bonus"`
	ArmorRepairRate     *int     `json:"armor_repair_rate"`
	ResistanceBonus     map[string]float64 `json:"resistance_bonus"`
	DamageReduction     *float64 `json:"damage_reduction"`
	CloakStrength       *int     `json:"cloak_strength"`

	// Mining fields
	MiningPower int `json:"mining_power"`
	MiningRange int `json:"mining_range"`

	// Utility fields
	SpeedBonus      *int     `json:"speed_bonus"`
	CargoBonus      *int     `json:"cargo_bonus"`
	ScannerPower    *int     `json:"scanner_power"`
	AccuracyBonus   *int     `json:"accuracy_bonus"`
	TrackingBonus   *int     `json:"tracking_bonus"`
	SignatureBonus  *int     `json:"signature_bonus"`
	FuelEfficiency  *float64 `json:"fuel_efficiency"`
	DroneBandwidth  *int     `json:"drone_bandwidth"`
	DroneCapacity   *int     `json:"drone_capacity"`
	HarvestPower    *int     `json:"harvest_power"`
	HarvestRange    *int     `json:"harvest_range"`
	SurveyPower     *int     `json:"survey_power"`
	SurveyRange     *int     `json:"survey_range"`
	TowSpeedPenalty *int     `json:"tow_speed_penalty"`

	// Power bonus (e.g. reactors)
	PowerBonus int `json:"power_bonus"`

	// Hazardous flag
	Hazardous bool `json:"hazardous"`

	// Region lock
	RegionLock []string `json:"region_lock"`

	// Consumable effect (nested JSON object)
	Effect *EffectJSON `json:"effect"`
}

// EffectJSON represents the nested effect object on consumable items.
type EffectJSON struct {
	Type     string `json:"type"`
	Subtype  string `json:"subtype"`
	Amount   *int   `json:"amount"`
	Duration *int   `json:"duration"`
	Stat     string `json:"stat"`
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <catalog-items.json>", os.Args[0])
	}

	jsonFile := os.Args[1]

	// Read the JSON file
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	var response ItemsResponse
	if err := json.Unmarshal(data, &response); err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	if len(response.Items) == 0 {
		log.Fatalf("No items found in JSON file")
	}

	// Create knowledge base
	config := knowledge.DefaultConfig()
	if dbPath := os.Getenv("SPACEMOLT_DB"); dbPath != "" {
		config.DBPath = dbPath
	} else {
		config.DBPath = "data/spacemolt-knowledge.db"
	}
	kb, err := knowledge.NewSQLiteKB(config)
	if err != nil {
		log.Fatalf("Failed to open knowledge base: %v", err)
	}
	defer func() { _ = kb.Close() }()

	ctx := context.Background()

	// Convert JSON items to catalog items
	var items []knowledge.CatalogItem
	for _, j := range response.Items {
		item := convertItem(j)
		if item.ID == "" {
			continue
		}
		items = append(items, item)
	}

	// Store items in database
	if err := kb.StoreItems(ctx, items); err != nil {
		log.Fatalf("Failed to store items: %v", err)
	}

	// Count detail types for summary
	var modules, weapons, defenses, mining, utilities, consumables, ammo int
	for _, item := range items {
		if item.Module != nil {
			modules++
			switch {
			case item.Module.Weapon != nil:
				weapons++
			case item.Module.Defense != nil:
				defenses++
			case item.Module.Mining != nil:
				mining++
			case item.Module.Utility != nil:
				utilities++
			}
		}
		if item.ConsumableEffect != nil {
			consumables++
		}
		if item.Ammo != nil {
			ammo++
		}
	}

	fmt.Printf("Successfully imported %d items\n", len(items))
	fmt.Printf("  modules: %d (weapons: %d, defense: %d, mining: %d, utility: %d)\n",
		modules, weapons, defenses, mining, utilities)
	fmt.Printf("  consumable effects: %d, ammo: %d\n", consumables, ammo)
}

// convertItem converts a JSON item to a CatalogItem with module/effect detail.
func convertItem(j CatalogItemJSON) knowledge.CatalogItem {
	// Modules (anything with type_id set) omit the tradeable field in the
	// server catalog — the zero-value `false` would mark them
	// not-tradeable in the DB even though the live market clearly accepts
	// orders for them (the server only rejects quest_item / artifact
	// shapes). Assume tradeable for modules unless the catalog explicitly
	// said otherwise.
	tradeable := j.Tradeable
	if j.TypeID != "" {
		tradeable = true
	}
	item := knowledge.CatalogItem{
		Name:        j.Name,
		Description: j.Description,
		Category:    j.Category,
		Rarity:      j.Rarity,
		Size:        j.Size,
		BaseValue:   j.BaseValue,
		Stackable:   j.Stackable,
		Tradeable:   tradeable,
		Hazardous:   j.Hazardous,
		PowerBonus:  j.PowerBonus,
	}

	// Modules have type/type_id set; use type_id as the canonical ID.
	if j.TypeID != "" {
		item.ID = j.TypeID
		item.Module = convertModule(j)
	} else {
		item.ID = j.ID
	}

	// Consumable effects
	if j.Effect != nil {
		if j.Effect.Type == "ammo" {
			item.Ammo = &knowledge.ItemAmmo{
				AmmoType: j.Effect.Subtype,
			}
		} else {
			item.ConsumableEffect = &knowledge.ItemConsumableEffect{
				EffectType: j.Effect.Type,
				Subtype:    j.Effect.Subtype,
				Amount:     j.Effect.Amount,
				Duration:   j.Effect.Duration,
				Stat:       j.Effect.Stat,
			}
		}
	}

	return item
}

func convertModule(j CatalogItemJSON) *knowledge.ItemModule {
	m := &knowledge.ItemModule{
		Type:       j.Type,
		TypeID:     j.TypeID,
		CPUUsage:   j.CPUUsage,
		PowerUsage: j.PowerUsage,
		Hidden:     j.Hidden,
		Special:    j.Special,
	}

	switch j.Type {
	case "weapon":
		m.Weapon = &knowledge.ItemWeapon{
			Damage:       j.Damage,
			DamageType:   j.DamageType,
			Range:        j.Range,
			Reach:        j.Reach,
			Cooldown:     j.Cooldown,
			AmmoType:     j.AmmoType,
			MagazineSize: j.MagazineSize,
		}
	case "defense":
		m.Defense = &knowledge.ItemDefense{
			ArmorBonus:          j.ArmorBonus,
			HullBonus:           j.HullBonus,
			ShieldBonus:         j.ShieldBonus,
			ShieldRechargeBonus: j.ShieldRechargeBonus,
			ArmorRepairRate:     j.ArmorRepairRate,
			ResistanceBonus:     j.ResistanceBonus,
			DamageReduction:     j.DamageReduction,
			CloakStrength:       j.CloakStrength,
			Cooldown:            intPtrIfNonZero(j.Cooldown),
			Damage:              intPtrIfNonZero(j.Damage),
			DamageType:          j.DamageType,
			Range:               j.Range,
		}
	case "mining":
		m.Mining = &knowledge.ItemMining{
			MiningPower: j.MiningPower,
			MiningRange: j.MiningRange,
		}
	case "utility":
		m.Utility = &knowledge.ItemUtility{
			SpeedBonus:      j.SpeedBonus,
			CargoBonus:      j.CargoBonus,
			CloakStrength:   j.CloakStrength,
			ScannerPower:    j.ScannerPower,
			AccuracyBonus:   j.AccuracyBonus,
			TrackingBonus:   j.TrackingBonus,
			SignatureBonus:  j.SignatureBonus,
			FuelEfficiency:  j.FuelEfficiency,
			DroneBandwidth:  j.DroneBandwidth,
			DroneCapacity:   j.DroneCapacity,
			HarvestPower:    j.HarvestPower,
			HarvestRange:    j.HarvestRange,
			SurveyPower:     j.SurveyPower,
			SurveyRange:     j.SurveyRange,
			TowSpeedPenalty: j.TowSpeedPenalty,
			Cooldown:        intPtrIfNonZero(j.Cooldown),
		}
	}

	return m
}

func intPtrIfNonZero(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}
