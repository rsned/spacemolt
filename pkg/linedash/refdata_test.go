package linedash

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

const (
	cjBase   = "c976fa7df53c87f9616165d7d411b41a"
	cjPOI    = "862e07fa76571d0a76cab3cbaf4046e7"
	vossBase = "voss_redoubt_station"
	vossPOI  = "voss_redoubt"
	crftID   = "e727c0e918d994c72db2978fe5b18edc"
)

// seedKB builds a knowledge DB holding two stations (Conjunction Junction in
// Cloverfield, Voss Redoubt in Alhena), a few recipes and items, and faction
// facilities for CRFT and one other faction.
func seedKB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kb.db")
	kb, err := knowledge.NewSQLiteKB(knowledge.Config{DBPath: path})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	stmts := []string{
		`INSERT INTO systems(id, name, position_x, position_y) VALUES ('cloverfield', 'Cloverfield', 0, 0), ('alhena', 'Alhena', 1, 1)`,
		`INSERT INTO pois(id, system_id, name, type, position_x, position_y, base_id) VALUES
			('` + cjPOI + `', 'cloverfield', 'Conjunction Junction', 'station', 0, 0, '` + cjBase + `'),
			('` + vossPOI + `', 'alhena', 'Voss Redoubt', 'station', 0, 0, '` + vossBase + `')`,
		`INSERT INTO bases(id, poi_id, name, condition, satisfaction_pct) VALUES
			('` + cjBase + `', '` + cjPOI + `', 'Conjunction Junction', 'thriving', 100),
			('` + vossBase + `', '` + vossPOI + `', 'Voss Redoubt Station', 'operational', 80)`,
		`INSERT INTO recipes(id, name, category, crafting_time) VALUES
			('grow_sunspindle', 'Grow Sunspindle', 'Hydroponics', 360),
			('refine_steel', 'Refine Steel', 'Refining', 30)`,
		`INSERT INTO recipe_inputs(recipe_id, item_id, quantity) VALUES
			('grow_sunspindle', 'purified_water', 300), ('grow_sunspindle', 'liquid_nitrogen', 30),
			('refine_steel', 'iron_ore', 5)`,
		`INSERT INTO recipe_outputs(recipe_id, item_id, quantity) VALUES
			('grow_sunspindle', 'sunspindle', 200), ('refine_steel', 'steel_plate', 2)`,
		`INSERT INTO items(id, name, category) VALUES
			('steel_plate', 'Steel Plate', 'refined'), ('sunspindle', 'Sunspindle', 'consumable'),
			('pulse_laser_i', 'Pulse Laser I', '')`,
		`INSERT INTO item_modules(item_id, type, type_id) VALUES ('pulse_laser_i', 'weapon', 'pulse_laser')`,
		`INSERT INTO faction_facilities(faction_id, base_id, facility_id, facility_type, category, level, status, recipe_id, captured_utc) VALUES
			('` + crftID + `', '` + cjBase + `', 'f1', 'iron_refinery', 'production', 1, 'active', 'refine_steel', '2026-11-01T22:05:18Z'),
			('` + crftID + `', '` + cjBase + `', 'f2', 'atmospheric_recycler', 'infrastructure', 1, 'active', '', '2026-11-01T22:05:18Z'),
			('otherfaction', '` + cjBase + `', 'f9', 'iron_refinery', 'production', 1, 'active', 'refine_steel', '2026-11-01T22:05:18Z')`,
	}
	for _, s := range stmts {
		if _, err := kb.DB().Exec(s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
	if err := kb.Close(); err != nil {
		t.Fatal(err)
	}

	return path
}

func writeFacilityCatalog(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "catalog_facilities.json")
	body := `{"items":[{"id":"atmospheric_recycler","maintenance_inputs":[{"item_id":"sunspindle","quantity":1},{"item_id":"water_ice","quantity":3},{"item_id":"purified_water","quantity":1},{"item_id":"oxygen_gas","quantity":2},{"item_id":"catalytic_scrubber_cartridge","quantity":1}]},{"id":"iron_refinery"}]}`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	return p
}

func loadSeededRef(t *testing.T) RefData {
	t.Helper()
	r, err := LoadRefData(context.Background(), seedKB(t), writeFacilityCatalog(t), crftID)
	if err != nil {
		t.Fatalf("LoadRefData: %v", err)
	}

	return r
}

func TestStationIndexResolvesPOIAndBaseToOneStation(t *testing.T) {
	r := loadSeededRef(t)
	byPOI, ok1 := r.Station(vossPOI)
	byBase, ok2 := r.Station(vossBase)
	if !ok1 || !ok2 || byPOI.BaseID != vossBase || byBase.BaseID != vossBase {
		t.Fatalf("Station(%q)=%+v %v, Station(%q)=%+v %v", vossPOI, byPOI, ok1, vossBase, byBase, ok2)
	}
	cj, _ := r.Station(cjPOI)
	if cj.Name != "Conjunction Junction" || cj.SystemName != "Cloverfield" || cj.Condition != "thriving" || cj.SatisfactionPct != 100 {
		t.Errorf("CJ = %+v", cj)
	}
	if _, ok := r.Station("nowhere"); ok {
		t.Error("unknown id resolved")
	}
}

func TestLoadRefDataRecipesAndCategories(t *testing.T) {
	r := loadSeededRef(t)
	g := r.Recipes["grow_sunspindle"]
	if g.Inputs["purified_water"] != 300 || g.Inputs["liquid_nitrogen"] != 30 || g.Outputs["sunspindle"] != 200 || g.CraftingTime != 360 {
		t.Errorf("grow_sunspindle = %+v", g)
	}
	if r.ItemCategory["steel_plate"] != "refined" || r.ItemName["steel_plate"] != "Steel Plate" {
		t.Errorf("steel_plate category %q name %q", r.ItemCategory["steel_plate"], r.ItemName["steel_plate"])
	}
}

func TestLoadRefDataModulesCategorisedAsModule(t *testing.T) {
	if got := loadSeededRef(t).ItemCategory["pulse_laser_i"]; got != "module" {
		t.Errorf("pulse_laser_i category = %q, want module", got)
	}
}

func TestLoadRefDataFacilitiesForOurFactionOnly(t *testing.T) {
	fs := loadSeededRef(t).Facilities[cjBase]
	if len(fs) != 2 {
		t.Fatalf("CJ facilities = %+v, want CRFT's 2", fs)
	}
	var refinery Facility
	for _, f := range fs {
		if f.Type == "iron_refinery" {
			refinery = f
		}
	}
	if refinery.RecipeID != "refine_steel" || refinery.Level != 1 || refinery.CapturedAt.IsZero() {
		t.Errorf("refinery = %+v", refinery)
	}
}

func TestLoadRefDataUpkeep(t *testing.T) {
	u := loadSeededRef(t).Upkeep["atmospheric_recycler"]
	want := map[string]int{"sunspindle": 1, "water_ice": 3, "purified_water": 1, "oxygen_gas": 2, "catalytic_scrubber_cartridge": 1}
	if len(u) != len(want) {
		t.Fatalf("upkeep = %v, want %v", u, want)
	}
	for k, v := range want {
		if u[k] != v {
			t.Errorf("upkeep[%s] = %d, want %d", k, u[k], v)
		}
	}
}

func TestLoadRefDataMissingKBErrors(t *testing.T) {
	if _, err := LoadRefData(context.Background(), filepath.Join(t.TempDir(), "nope.db"), writeFacilityCatalog(t), crftID); err == nil {
		t.Error("missing KB loaded without error")
	}
}
