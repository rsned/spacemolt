package fitting

import "testing"

func TestEffUsage(t *testing.T) {
	tests := []struct {
		name        string
		base        int
		level       int
		effPerLevel float64
		want        int
	}{
		{"level zero no reduction", 25, 0, 1, 25},
		{"ceil rounds up", 25, 4, 1, 24},        // 25*0.96 = 24.0 -> 24
		{"ceil rounds fractional up", 13, 4, 1, 13}, // 13*0.96 = 12.48 -> 13
		{"high level large reduction", 100, 50, 1, 50}, // 100*0.5 = 50 -> 50
		{"never below zero", 10, 100, 1, 0},     // 10*0 = 0
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effUsage(tt.base, tt.level, tt.effPerLevel); got != tt.want {
				t.Errorf("effUsage(%d,%d,%g) = %d, want %d", tt.base, tt.level, tt.effPerLevel, got, tt.want)
			}
		})
	}
}

func TestSlotCapacity(t *testing.T) {
	s := Ship{WeaponSlots: 1, DefenseSlots: 2, UtilitySlots: 3}
	cases := map[string]int{"weapon": 1, "defense": 2, "utility": 3, "bogus": 0}
	for slot, want := range cases {
		if got := slotCapacity(s, slot); got != want {
			t.Errorf("slotCapacity(%q) = %d, want %d", slot, got, want)
		}
	}
}
