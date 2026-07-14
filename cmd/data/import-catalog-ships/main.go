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
	ID                   string               `json:"id"`
	Name                 string               `json:"name"`
	Class                string               `json:"class"`
	Category             string               `json:"category"`
	Description          string               `json:"description"`
	Lore                 string               `json:"lore"`
	Faction              string               `json:"faction"`
	Tier                 int                  `json:"tier"`
	Scale                int                  `json:"scale"`
	BaseHull             int                  `json:"base_hull"`
	BaseShield           int                  `json:"base_shield"`
	BaseShieldRecharge   int                  `json:"base_shield_recharge"`
	BaseArmor            int                  `json:"base_armor"`
	BaseSpeed            int                  `json:"base_speed"`
	BaseFuel             int                  `json:"base_fuel"`
	CargoCapacity        int                  `json:"cargo_capacity"`
	CPUCapacity          int                  `json:"cpu_capacity"`
	PowerCapacity        int                  `json:"power_capacity"`
	WeaponSlots          int                  `json:"weapon_slots"`
	DefenseSlots         int                  `json:"defense_slots"`
	UtilitySlots         int                  `json:"utility_slots"`
	BuildTime            int                  `json:"build_time"`
	ShipyardTier         int                  `json:"shipyard_tier"`
	StarterShip          bool                 `json:"starter_ship"`
	TowSpeedBonus        int                  `json:"tow_speed_bonus"`
	BasedOn              string               `json:"based_on"`
	NPCRole              string               `json:"npc_role"`
	Special              string               `json:"special"`
	RequiredReputation   int                  `json:"required_reputation"`
	PilotingRequired     int                  `json:"piloting_required"`
	InherentCapabilities []ShipCapabilityJSON `json:"inherent_capabilities"`
	DefaultModules       []string             `json:"default_modules"`
	FlavorTags           []string             `json:"flavor_tags"`
	BuildMaterials       []BuildMaterialJSON  `json:"build_materials"`
	PassiveRecipes       []string             `json:"passive_recipes"`

	// Prestige / unlock gating (server v0.495.1). `price` and
	// `required_skills` were dropped by the server in the same release and are
	// deliberately absent here — they imported as 0/nil.
	RequiredAchievement        string `json:"required_achievement"`
	RequiredFactionAchievement string `json:"required_faction_achievement"`
	RequiredFactionLeader      bool   `json:"required_faction_leader"`
	PrestigeLock               string `json:"prestige_lock"`
	DefaultLoadoutVersion      int    `json:"default_loadout_version"`
}

// BuildMaterialJSON represents a build material from JSON
type BuildMaterialJSON struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

// ShipCapabilityJSON represents a built-in ship capability from JSON
type ShipCapabilityJSON struct {
	Type  string `json:"type"`
	Value int    `json:"value"`
	Flag  string `json:"flag"`
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

		capabilities := make([]knowledge.ShipCapability, len(sc.InherentCapabilities))
		for j, c := range sc.InherentCapabilities {
			capabilities[j] = knowledge.ShipCapability{
				Type:  c.Type,
				Value: c.Value,
				Flag:  c.Flag,
			}
		}

		classes[i] = knowledge.ShipClassDef{
			ID:                   sc.ID,
			Name:                 sc.Name,
			Class:                sc.Class,
			Category:             sc.Category,
			Description:          sc.Description,
			Lore:                 sc.Lore,
			Faction:              sc.Faction,
			Tier:                 sc.Tier,
			Scale:                sc.Scale,
			BaseHull:             sc.BaseHull,
			BaseShield:           sc.BaseShield,
			BaseShieldRecharge:   sc.BaseShieldRecharge,
			BaseArmor:            sc.BaseArmor,
			BaseSpeed:            sc.BaseSpeed,
			BaseFuel:             sc.BaseFuel,
			CargoCapacity:        sc.CargoCapacity,
			CPUCapacity:          sc.CPUCapacity,
			PowerCapacity:        sc.PowerCapacity,
			WeaponSlots:          sc.WeaponSlots,
			DefenseSlots:         sc.DefenseSlots,
			UtilitySlots:         sc.UtilitySlots,
			BuildTime:            sc.BuildTime,
			ShipyardTier:         sc.ShipyardTier,
			StarterShip:          sc.StarterShip,
			TowSpeedBonus:        sc.TowSpeedBonus,
			BasedOn:              sc.BasedOn,
			NPCRole:              sc.NPCRole,
			Special:              sc.Special,
			RequiredReputation:   sc.RequiredReputation,
			PilotingRequired:     sc.PilotingRequired,
			InherentCapabilities: capabilities,
			DefaultModules:       sc.DefaultModules,
			FlavorTags:           sc.FlavorTags,
			BuildMaterials:       buildMaterials,
			PassiveRecipes:       sc.PassiveRecipes,

			RequiredAchievement:        sc.RequiredAchievement,
			RequiredFactionAchievement: sc.RequiredFactionAchievement,
			RequiredFactionLeader:      sc.RequiredFactionLeader,
			PrestigeLock:               sc.PrestigeLock,
			DefaultLoadoutVersion:      sc.DefaultLoadoutVersion,

			LastUpdatedTick: 0,
		}
	}

	// Store ship classes in database
	if err := kb.StoreShipClasses(ctx, classes); err != nil {
		log.Fatalf("Failed to store ship classes: %v", err)
	}

	fmt.Printf("✓ Successfully imported %d ship classes\n", len(classes))
}
