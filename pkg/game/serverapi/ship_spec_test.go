package serverapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// openAPIProperties returns the property names of one schema in the current
// server_docs/openapi.json, or skips the test when the spec is not checked
// out (it is a symlink to a dated, git-ignored snapshot).
func openAPIProperties(t *testing.T, schema string) map[string]bool {
	t.Helper()
	path := filepath.Join("..", "..", "..", "server_docs", "openapi.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("openapi.json not available: %v", err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse openapi.json: %v", err)
	}
	s, ok := doc.Components.Schemas[schema]
	if !ok {
		t.Fatalf("schema %q not in openapi.json", schema)
	}
	out := map[string]bool{}
	for n := range s.Properties {
		out[n] = true
	}
	return out
}

// jsonTags returns the json field names a struct type decodes.
func jsonTags(typ reflect.Type) map[string]bool {
	out := map[string]bool{}
	for i := range typ.NumField() {
		tag := typ.Field(i).Tag.Get("json")
		if name, _, _ := strings.Cut(tag, ","); name != "" && name != "-" {
			out[name] = true
		}
	}
	return out
}

// Ship is the one struct every state-bearing reply carries, so it drifts the
// fastest: burn and armor-melt state, garage custody and the self-destruct
// countdown were all in the spec and absent here on 2026-08-30, and the
// capacitor fields absorbed that day were removed again by v0.572.4.
// This pins the struct to the spec in BOTH directions.
func TestShipMatchesOpenAPISpec(t *testing.T) {
	spec := openAPIProperties(t, "Ship")
	ours := jsonTags(reflect.TypeOf(Ship{}))
	for n := range spec {
		if !ours[n] {
			t.Errorf("openapi Ship.%s is not decoded by serverapi.Ship", n)
		}
	}
	for n := range ours {
		if !spec[n] {
			t.Errorf("serverapi.Ship decodes %q, which the openapi Ship schema no longer has", n)
		}
	}
}

// Combat status effects and custody fields from the Ship schema, as carried
// by the live Eviction Notice reply of 2026-08-30 (minus capacitor, which
// v0.572.4 removed from the game).
func TestShip_DecodesCombatAndCustodyFields(t *testing.T) {
	raw := `{"id":"s1","class_id":"eviction_notice",
		"burn_ticks_remaining":3,"burn_damage_per_tick":7,"burn_source_id":"molten",
		"armor_melt_pct":0.25,"armor_melt_ticks_remaining":2,
		"in_faction_garage":"fac1","loadout_version":3,"singleton_instance_key":"named_hull_7",
		"battle_self_destruct":{"battle_id":"b1","started_tick":100,"detonate_tick":105}}`
	var s Ship
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	if s.BurnTicksRemaining != 3 || s.BurnDamagePerTick != 7 ||
		s.BurnSourceID != "molten" || s.ArmorMeltPct != 0.25 || s.ArmorMeltTicksRemaining != 2 ||
		s.InFactionGarage != "fac1" || s.LoadoutVersion != 3 || s.SingletonInstanceKey != "named_hull_7" {
		t.Errorf("ship = %+v", s)
	}
	if s.BattleSelfDestruct == nil || s.BattleSelfDestruct.DetonateTick != 105 || s.BattleSelfDestruct.BattleID != "b1" {
		t.Errorf("battle_self_destruct = %+v", s.BattleSelfDestruct)
	}
}
