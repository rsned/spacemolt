package game

import (
	"testing"
)

func TestCountMiningEquipment(t *testing.T) {
	asteroidCfg := miningTypeConfigs[MiningTypeAsteroid]
	gasCfg := miningTypeConfigs[MiningTypeGas]
	iceCfg := miningTypeConfigs[MiningTypeIce]

	tests := []struct {
		name  string
		state *State
		cfg   miningTypeConfig
		want  int
	}{
		{
			name: "no modules",
			state: &State{
				Ship:              Ship{Modules: []string{}},
				ModuleDefinitions: map[string]ModuleDefinition{},
			},
			cfg:  asteroidCfg,
			want: 0,
		},
		{
			name: "one mining laser",
			state: &State{
				Ship: Ship{Modules: []string{"mod1"}},
				ModuleDefinitions: map[string]ModuleDefinition{
					"mod1": {ID: "mod1", Name: "Mining Laser Mk1", Type: "mining"},
				},
			},
			cfg:  asteroidCfg,
			want: 1,
		},
		{
			name: "multiple mining lasers",
			state: &State{
				Ship: Ship{Modules: []string{"mod1", "mod2", "mod3"}},
				ModuleDefinitions: map[string]ModuleDefinition{
					"mod1": {ID: "mod1", Name: "Mining Laser Mk1", Type: "mining"},
					"mod2": {ID: "mod2", Name: "Shield Generator", Type: "defense"},
					"mod3": {ID: "mod3", Name: "Mining Laser Mk2", Type: "utility"},
				},
			},
			cfg:  asteroidCfg,
			want: 2, // mod1 matches by type "mining", mod3 matches by name prefix "mining laser"
		},
		{
			name: "mining type but no mining laser name",
			state: &State{
				Ship: Ship{Modules: []string{"mod1"}},
				ModuleDefinitions: map[string]ModuleDefinition{
					"mod1": {ID: "mod1", Name: "Ore Scanner", Type: "mining"},
				},
			},
			cfg:  asteroidCfg,
			want: 1, // type starts with "mining"
		},
		{
			name: "module not in definitions",
			state: &State{
				Ship:              Ship{Modules: []string{"unknown_mod"}},
				ModuleDefinitions: map[string]ModuleDefinition{},
			},
			cfg:  asteroidCfg,
			want: 0,
		},
		{
			name: "nil module definitions",
			state: &State{
				Ship:              Ship{Modules: []string{"mod1"}},
				ModuleDefinitions: nil,
			},
			cfg:  asteroidCfg,
			want: 0,
		},
		{
			name:  "nil modules slice",
			state: &State{Ship: Ship{}},
			cfg:   asteroidCfg,
			want:  0,
		},
		{
			name: "gas harvester matched",
			state: &State{
				Ship: Ship{Modules: []string{"mod1"}},
				ModuleDefinitions: map[string]ModuleDefinition{
					"mod1": {ID: "mod1", Name: "Gas Harvester II", Type: "gas_harvester"},
				},
			},
			cfg:  gasCfg,
			want: 1,
		},
		{
			name: "gas harvester not counted as asteroid",
			state: &State{
				Ship: Ship{Modules: []string{"mod1"}},
				ModuleDefinitions: map[string]ModuleDefinition{
					"mod1": {ID: "mod1", Name: "Gas Harvester II", Type: "gas_harvester"},
				},
			},
			cfg:  asteroidCfg,
			want: 0,
		},
		{
			name: "ice harvester matched",
			state: &State{
				Ship: Ship{Modules: []string{"mod1"}},
				ModuleDefinitions: map[string]ModuleDefinition{
					"mod1": {ID: "mod1", Name: "Ice Harvester III", Type: "ice_harvester"},
				},
			},
			cfg:  iceCfg,
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countMiningEquipment(tt.state, tt.cfg); got != tt.want {
				t.Errorf("countMiningEquipment() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPOIFreshnessThreshold(t *testing.T) {
	tests := []struct {
		poiType string
		want    int64
	}{
		{"asteroid_belt", FreshnessResourcePOI},
		{"asteroid", FreshnessResourcePOI},
		{"asteroid_field", FreshnessResourcePOI},
		{"gas_cloud", FreshnessResourcePOI},
		{"ice_field", FreshnessResourcePOI},
		{"nebula", FreshnessResourcePOI},
		{"station", FreshnessStationPOI},
		{"base", FreshnessStationPOI},
		{"planet", FreshnessDefaultPOI},
		{"moon", FreshnessDefaultPOI},
		{"sun", FreshnessDefaultPOI},
		{"relic", FreshnessDefaultPOI},
		{"jump_gate", FreshnessDefaultPOI},
		{"wreck", FreshnessDefaultPOI},
		{"unknown_type", FreshnessDefaultPOI},
		{"", FreshnessDefaultPOI},
	}

	for _, tt := range tests {
		t.Run(tt.poiType, func(t *testing.T) {
			if got := POIFreshnessThreshold(tt.poiType); got != tt.want {
				t.Errorf("POIFreshnessThreshold(%q) = %d, want %d", tt.poiType, got, tt.want)
			}
		})
	}
}
