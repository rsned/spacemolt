package game

import (
	"testing"
)

func TestBuildProgression_BasicChain(t *testing.T) {
	ships := []ShipDef{
		{ID: "shard", Name: "Shard", Class: "Mining", Price: 0, UtilitySlots: 1, WeaponSlots: 2, CargoCapacity: 40},
		{ID: "crimson_maul", Name: "Maul", Class: "Mining", Price: 2200, UtilitySlots: 1, WeaponSlots: 2, CargoCapacity: 100},
		{ID: "crimson_siege_drill", Name: "Siege Drill", Class: "Mining", Price: 7500, UtilitySlots: 2, WeaponSlots: 2, CargoCapacity: 140},
		{ID: "crimson_siege_breaker", Name: "Siege Breaker", Class: "Mining", Price: 28000, UtilitySlots: 4, WeaponSlots: 2, CargoCapacity: 300},
	}

	prog := BuildProgression(ships, "shard", "miner")
	if prog == nil {
		t.Fatal("expected non-nil progression")
	}
	if prog.CareerName != "Mining" {
		t.Errorf("career name = %q, want %q", prog.CareerName, "Mining")
	}
	if len(prog.Tiers) != 3 {
		t.Fatalf("tiers = %d, want 3", len(prog.Tiers))
	}

	// First tier: shard -> maul
	if prog.Tiers[0].FromShipClass != "shard" {
		t.Errorf("tier[0].From = %q, want %q", prog.Tiers[0].FromShipClass, "shard")
	}
	if prog.Tiers[0].ToShipClass != "crimson_maul" {
		t.Errorf("tier[0].To = %q, want %q", prog.Tiers[0].ToShipClass, "crimson_maul")
	}
	if prog.Tiers[0].Threshold != 2200 {
		t.Errorf("tier[0].Threshold = %v, want 2200", prog.Tiers[0].Threshold)
	}

	// Chain: each tier's From = previous tier's To
	if prog.Tiers[1].FromShipClass != "crimson_maul" {
		t.Errorf("tier[1].From = %q, want %q", prog.Tiers[1].FromShipClass, "crimson_maul")
	}
	if prog.Tiers[2].FromShipClass != "crimson_siege_drill" {
		t.Errorf("tier[2].From = %q, want %q", prog.Tiers[2].FromShipClass, "crimson_siege_drill")
	}
}

func TestBuildProgression_StarterShipNotInList(t *testing.T) {
	ships := []ShipDef{
		{ID: "shard", Name: "Shard", Class: "Mining", Price: 0, UtilitySlots: 1, CargoCapacity: 40},
		{ID: "crimson_maul", Name: "Maul", Class: "Mining", Price: 2200, UtilitySlots: 1, CargoCapacity: 100},
	}

	// Current ship is a generic starter not in the list
	prog := BuildProgression(ships, "starter_mining", "miner")
	if prog == nil {
		t.Fatal("expected non-nil progression")
	}
	// Should include all ships as upgrades, first tier's From = starter_mining
	if prog.Tiers[0].FromShipClass != "starter_mining" {
		t.Errorf("tier[0].From = %q, want %q", prog.Tiers[0].FromShipClass, "starter_mining")
	}
	if prog.Tiers[0].ToShipClass != "shard" {
		t.Errorf("tier[0].To = %q, want %q", prog.Tiers[0].ToShipClass, "shard")
	}
}

func TestBuildProgression_AtMaxShip(t *testing.T) {
	ships := []ShipDef{
		{ID: "shard", Name: "Shard", Class: "Mining", Price: 0, UtilitySlots: 1, CargoCapacity: 40},
		{ID: "crimson_maul", Name: "Maul", Class: "Mining", Price: 2200, UtilitySlots: 1, CargoCapacity: 100},
	}

	// Already at the best ship
	prog := BuildProgression(ships, "crimson_maul", "miner")
	if prog != nil {
		t.Errorf("expected nil progression when at max ship, got %d tiers", len(prog.Tiers))
	}
}

func TestBuildProgression_EmptyShips(t *testing.T) {
	prog := BuildProgression(nil, "starter_mining", "miner")
	if prog != nil {
		t.Error("expected nil progression for empty ship list")
	}
}

func TestBuildProgression_CombatEquipment(t *testing.T) {
	ships := []ShipDef{
		{ID: "crimson_mauler", Name: "Mauler", Class: "Fighter", Price: 0, WeaponSlots: 2, UtilitySlots: 1, CargoCapacity: 20},
		{ID: "crimson_berserker", Name: "Berserker", Class: "Heavy Fighter", Price: 5000, WeaponSlots: 5, UtilitySlots: 2, CargoCapacity: 40},
	}

	prog := BuildProgression(ships, "crimson_mauler", "fighter")
	if prog == nil {
		t.Fatal("expected non-nil progression")
	}
	if len(prog.Tiers) != 1 {
		t.Fatalf("tiers = %d, want 1", len(prog.Tiers))
	}
	// Fighters should get weapon_laser_1 and weapon slot count
	if prog.Tiers[0].ItemID != "pulse_laser_i" {
		t.Errorf("itemID = %q, want %q", prog.Tiers[0].ItemID, "pulse_laser_i")
	}
	if prog.Tiers[0].NumItems != 5 {
		t.Errorf("numItems = %d, want 5 (weapon slots)", prog.Tiers[0].NumItems)
	}
}

func TestBuildProgression_MinerEquipment(t *testing.T) {
	ships := []ShipDef{
		{ID: "solarian_theoria", Name: "Theoria", Class: "Mining", Price: 0, WeaponSlots: 1, UtilitySlots: 3, CargoCapacity: 50},
		{ID: "solarian_archimedes", Name: "Archimedes", Class: "Mining", Price: 2200, WeaponSlots: 1, UtilitySlots: 3, CargoCapacity: 105},
	}

	prog := BuildProgression(ships, "solarian_theoria", "miner")
	if prog == nil {
		t.Fatal("expected non-nil progression")
	}
	// Miners should get mining_laser_1 and utility slot count
	if prog.Tiers[0].ItemID != "mining_laser_i" {
		t.Errorf("itemID = %q, want %q", prog.Tiers[0].ItemID, "mining_laser_i")
	}
	if prog.Tiers[0].NumItems != 3 {
		t.Errorf("numItems = %d, want 3 (utility slots)", prog.Tiers[0].NumItems)
	}
}

func TestFilterShipsByEmpire(t *testing.T) {
	ships := []ShipDef{
		{ID: "shard", Name: "Shard"},
		{ID: "solarian_theoria", Name: "Theoria"},
		{ID: "crimson_maul", Name: "Maul"},
		{ID: "nebula_prospect", Name: "Prospect"},
	}

	result := FilterShipsByEmpire(ships, "crimson")
	if len(result) != 1 {
		t.Fatalf("filtered count = %d, want 1", len(result))
	}
	if result[0].ID != "crimson_maul" {
		t.Errorf("result[0] = %q, want %q", result[0].ID, "crimson_maul")
	}
}

func TestShipMaxSlots_Combat(t *testing.T) {
	ship := ShipDef{Class: "Fighter", WeaponSlots: 4, UtilitySlots: 2}
	if got := ShipMaxSlots(ship); got != 4 {
		t.Errorf("ShipMaxSlots(Fighter) = %d, want 4", got)
	}
}

func TestShipMaxSlots_NonCombat(t *testing.T) {
	ship := ShipDef{Class: "Mining", WeaponSlots: 1, UtilitySlots: 3}
	if got := ShipMaxSlots(ship); got != 3 {
		t.Errorf("ShipMaxSlots(Mining) = %d, want 3", got)
	}
}

func TestBuildProgression_DeduplicatesSamePrice(t *testing.T) {
	ships := []ShipDef{
		{ID: "ship_a", Name: "Ship A", Class: "Mining", Price: 1000, UtilitySlots: 2, CargoCapacity: 50},
		{ID: "ship_b", Name: "Ship B", Class: "Mining", Price: 1000, UtilitySlots: 3, CargoCapacity: 60},
		{ID: "ship_c", Name: "Ship C", Class: "Mining", Price: 5000, UtilitySlots: 4, CargoCapacity: 100},
	}

	prog := BuildProgression(ships, "starter", "miner")
	if prog == nil {
		t.Fatal("expected non-nil progression")
	}
	// Should deduplicate: two ships at 1000 become one (ship_b has more utility slots)
	if len(prog.Tiers) != 2 {
		t.Fatalf("tiers = %d, want 2 (deduped)", len(prog.Tiers))
	}
	if prog.Tiers[0].ToShipClass != "ship_b" {
		t.Errorf("tier[0].To = %q, want %q (higher utility slots)", prog.Tiers[0].ToShipClass, "ship_b")
	}
}

func TestDefaultEquipment(t *testing.T) {
	tests := []struct {
		role     string
		class    string
		wSlots   int
		uSlots   int
		wantItem string
		wantN    int
	}{
		{"miner", "Mining", 1, 3, "mining_laser_i", 3},
		{"fighter", "Fighter", 4, 2, "pulse_laser_i", 4},
		{"salvager", "Salvager", 0, 5, "basic_tow_rig", 5},
		{"explorer", "Explorer", 1, 4, "ship_scanner_i", 4},
	}
	for _, tt := range tests {
		ship := ShipDef{Class: tt.class, WeaponSlots: tt.wSlots, UtilitySlots: tt.uSlots}
		itemID, count := DefaultEquipment(ship, tt.role)
		if itemID != tt.wantItem {
			t.Errorf("DefaultEquipment(%s) item = %q, want %q", tt.role, itemID, tt.wantItem)
		}
		if count != tt.wantN {
			t.Errorf("DefaultEquipment(%s) count = %d, want %d", tt.role, count, tt.wantN)
		}
	}
}

func TestRoleCategories_AllRolesHaveEntries(t *testing.T) {
	expectedRoles := []string{"miner", "fighter", "trader", "explorer", "pirate", "salvager", "engineer", "craftsman"}
	for _, role := range expectedRoles {
		cats, ok := RoleCategories[role]
		if !ok || len(cats) == 0 {
			t.Errorf("RoleCategories missing or empty for role %q", role)
		}
	}
}
