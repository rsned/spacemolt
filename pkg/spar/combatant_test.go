package spar

import (
	"reflect"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestNeededModules(t *testing.T) {
	tests := []struct {
		name      string
		installed []string
		weapon    string
		shield    string
		want      []string
	}{
		{"none installed", nil, "pulse_laser_i", "shield_booster_i", []string{"pulse_laser_i", "shield_booster_i"}},
		{"has weapon only", []string{"pulse_laser_i"}, "pulse_laser_i", "shield_booster_i", []string{"shield_booster_i"}},
		{"has shield only", []string{"shield_booster_ii"}, "pulse_laser_i", "shield_booster_i", []string{"pulse_laser_i"}},
		{"fully equipped", []string{"autocannon_i", "shield_booster_i"}, "pulse_laser_i", "shield_booster_i", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := neededModules(tt.installed, tt.weapon, tt.shield)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("neededModules = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFirstPOIOfType(t *testing.T) {
	tests := []struct {
		name    string
		system  game.SystemData
		poiType string
		want    string
	}{
		{
			name: "exact type match",
			system: game.SystemData{POIs: []game.POI{
				{ID: "stn-1", Type: "station"},
				{ID: "belt-1", Type: "asteroid_belt"},
			}},
			poiType: "asteroid_belt",
			want:    "belt-1",
		},
		{
			name: "no match falls back to non-station even when station is first",
			system: game.SystemData{POIs: []game.POI{
				{ID: "stn-1", Type: "station"},
				{ID: "belt-1", Type: "asteroid_belt"},
			}},
			poiType: "gas_cloud",
			want:    "belt-1",
		},
		{
			name:    "empty POIs",
			system:  game.SystemData{POIs: nil},
			poiType: "asteroid_belt",
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstPOIOfType(tt.system, tt.poiType)
			if got != tt.want {
				t.Fatalf("firstPOIOfType = %q, want %q", got, tt.want)
			}
		})
	}
}
