package spar

import (
	"reflect"
	"testing"
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
