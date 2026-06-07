package knowledge

import (
	"context"
	"testing"
)

// TestCatalogNewFieldsRoundTrip verifies the 2026-06 catalog stat fields
// survive a store→read round trip (recipes/ships) and are persisted to the
// item detail tables (items have no detail read path, so query directly).
func TestCatalogNewFieldsRoundTrip(t *testing.T) {
	kb := newTestSQLiteKB(t)
	defer func() { _ = kb.Close() }()
	ctx := context.Background()

	// --- Recipes: full round trip through GetRecipe. ---
	if err := kb.StoreRecipes(ctx, []RecipeDef{{
		ID: "r1", Name: "Fuel Cell", Category: "fuel",
		FacilityOnly: true, NoRecycle: true, FuelOutput: 12,
	}}); err != nil {
		t.Fatalf("StoreRecipes: %v", err)
	}
	r, err := kb.GetRecipe(ctx, "r1")
	if err != nil || r == nil {
		t.Fatalf("GetRecipe: %v (nil=%v)", err, r == nil)
	}
	if !r.FacilityOnly || !r.NoRecycle || r.FuelOutput != 12 {
		t.Errorf("recipe new fields not round-tripped: %+v", r)
	}

	// --- Ships: full round trip through GetShipClass. ---
	if err := kb.StoreShipClasses(ctx, []ShipClassDef{{
		ID: "s1", Name: "Liner", Class: "Hauler", Faction: "solarian",
		BasedOn: "prayer", NPCRole: "hauler", RequiredReputation: 60, PilotingRequired: 10,
		InherentCapabilities: []ShipCapability{{Type: "passenger_economy_berths", Value: 4}},
	}}); err != nil {
		t.Fatalf("StoreShipClasses: %v", err)
	}
	sc, err := kb.GetShipClass(ctx, "s1")
	if err != nil || sc == nil {
		t.Fatalf("GetShipClass: %v (nil=%v)", err, sc == nil)
	}
	if sc.BasedOn != "prayer" || sc.NPCRole != "hauler" || sc.RequiredReputation != 60 || sc.PilotingRequired != 10 {
		t.Errorf("ship scalar fields not round-tripped: %+v", sc)
	}
	if len(sc.InherentCapabilities) != 1 || sc.InherentCapabilities[0].Type != "passenger_economy_berths" ||
		sc.InherentCapabilities[0].Value != 4 {
		t.Errorf("ship capabilities not round-tripped: %+v", sc.InherentCapabilities)
	}

	// --- Items: persisted to detail tables (no detail read path; query directly). ---
	weaponRange := 18
	if err := kb.StoreItems(ctx, []CatalogItem{{
		ID: "i1", Name: "Railgun", Category: "weapon",
		QuestItem: true, ExtractedBy: "mining", RequiredSkills: map[string]int{"gunnery": 3},
		RegionLock: []string{"solarian"}, PassengerEconomyBerths: 2,
		Module: &ItemModule{Type: "weapon", TypeID: "i1", Slot: "weapon", Weapon: &ItemWeapon{
			Damage: 40, Range: &weaponRange, ArmorBypassBonus: f64(0.8),
		}},
	}}); err != nil {
		t.Fatalf("StoreItems: %v", err)
	}
	var quest bool
	var extractedBy, reqSkills, regionLock, slot string
	var berths int
	var armorBypass float64
	row := kb.db.QueryRowContext(ctx, `
		SELECT i.quest_item, i.extracted_by, i.required_skills, i.region_lock, i.passenger_economy_berths,
			m.slot, w.armor_bypass_bonus
		FROM items i JOIN item_modules m ON m.item_id = i.id JOIN item_weapons w ON w.item_id = i.id
		WHERE i.id = 'i1'`)
	if err := row.Scan(&quest, &extractedBy, &reqSkills, &regionLock, &berths, &slot, &armorBypass); err != nil {
		t.Fatalf("scan item detail: %v", err)
	}
	if !quest || extractedBy != "mining" || slot != "weapon" || berths != 2 || armorBypass != 0.8 {
		t.Errorf("item fields not persisted: quest=%v extractedBy=%q slot=%q berths=%d armorBypass=%v",
			quest, extractedBy, slot, berths, armorBypass)
	}
	if reqSkills != `{"gunnery":3}` || regionLock != `["solarian"]` {
		t.Errorf("item json fields not persisted: reqSkills=%q regionLock=%q", reqSkills, regionLock)
	}
}

func f64(v float64) *float64 { return &v }
