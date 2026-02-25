package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// ShipsResponse represents the server response from catalog_ships
type ShipsResponse struct {
	Items []ShipClassJSON `json:"items"`
}

// ShipClassJSON represents a ship class from the game catalog JSON
type ShipClassJSON struct {
	ID                 string              `json:"id"`
	Name               string              `json:"name"`
	Class              string              `json:"class"`
	Category           string              `json:"category"`
	Description        string              `json:"description"`
	Lore               string              `json:"lore"`
	Faction            string              `json:"faction"`
	Tier               int                 `json:"tier"`
	Scale              int                 `json:"scale"`
	Price              int                 `json:"price"`
	BaseHull           int                 `json:"base_hull"`
	BaseShield         int                 `json:"base_shield"`
	BaseShieldRecharge int                 `json:"base_shield_recharge"`
	BaseArmor          int                 `json:"base_armor"`
	BaseSpeed          int                 `json:"base_speed"`
	BaseFuel           int                 `json:"base_fuel"`
	CargoCapacity      int                 `json:"cargo_capacity"`
	CPUCapacity        int                 `json:"cpu_capacity"`
	PowerCapacity      int                 `json:"power_capacity"`
	WeaponSlots        int                 `json:"weapon_slots"`
	DefenseSlots       int                 `json:"defense_slots"`
	UtilitySlots       int                 `json:"utility_slots"`
	BuildTime          int                 `json:"build_time"`
	ShipyardTier       int                 `json:"shipyard_tier"`
	StarterShip        bool                `json:"starter_ship"`
	TowSpeedBonus      int                 `json:"tow_speed_bonus"`
	RequiredSkills     map[string]int      `json:"required_skills"`
	DefaultModules     []string            `json:"default_modules"`
	FlavorTags         []string            `json:"flavor_tags"`
	BuildMaterials     []BuildMaterialJSON `json:"build_materials"`
}

// BuildMaterialJSON represents a build material from JSON
type BuildMaterialJSON struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <catalog-ships.json>", os.Args[0])
	}

	jsonFile := os.Args[1]

	// Read the JSON file
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	var response ShipsResponse
	if err := json.Unmarshal(data, &response); err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	if len(response.Items) == 0 {
		log.Fatalf("No ship classes found in JSON file")
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

	// Convert JSON ship classes to knowledge base ship classes
	classes := make([]knowledge.ShipClassDef, len(response.Items))
	for i, sc := range response.Items {
		buildMaterials := make([]knowledge.BuildMaterial, len(sc.BuildMaterials))
		for j, bm := range sc.BuildMaterials {
			buildMaterials[j] = knowledge.BuildMaterial{
				ItemID:   bm.ItemID,
				Quantity: bm.Quantity,
			}
		}

		classes[i] = knowledge.ShipClassDef{
			ID:                 sc.ID,
			Name:               sc.Name,
			Class:              sc.Class,
			Category:           sc.Category,
			Description:        sc.Description,
			Lore:               sc.Lore,
			Faction:            sc.Faction,
			Tier:               sc.Tier,
			Scale:              sc.Scale,
			Price:              sc.Price,
			BaseHull:           sc.BaseHull,
			BaseShield:         sc.BaseShield,
			BaseShieldRecharge: sc.BaseShieldRecharge,
			BaseArmor:          sc.BaseArmor,
			BaseSpeed:          sc.BaseSpeed,
			BaseFuel:           sc.BaseFuel,
			CargoCapacity:      sc.CargoCapacity,
			CPUCapacity:        sc.CPUCapacity,
			PowerCapacity:      sc.PowerCapacity,
			WeaponSlots:        sc.WeaponSlots,
			DefenseSlots:       sc.DefenseSlots,
			UtilitySlots:       sc.UtilitySlots,
			BuildTime:          sc.BuildTime,
			ShipyardTier:       sc.ShipyardTier,
			StarterShip:        sc.StarterShip,
			TowSpeedBonus:      sc.TowSpeedBonus,
			RequiredSkills:     sc.RequiredSkills,
			DefaultModules:     sc.DefaultModules,
			FlavorTags:         sc.FlavorTags,
			BuildMaterials:     buildMaterials,
			LastUpdatedTick:    0,
		}
	}

	// Store ship classes in database
	if err := kb.StoreShipClasses(ctx, classes); err != nil {
		log.Fatalf("Failed to store ship classes: %v", err)
	}

	fmt.Printf("✓ Successfully imported %d ship classes\n", len(classes))
}
