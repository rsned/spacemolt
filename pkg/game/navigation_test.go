package game

import (
	"testing"
)

func TestSameSystem(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"sol", "sol", true},
		{"Sol", "sol", true},
		{"SOL", "sol", true},
		{"sol", "krynn", false},
		{"", "", true},
		{"sol", "", false},
		{"", "sol", false},
		{"Alpha Centauri", "alpha centauri", true},
		{"NEXUS", "Nexus", true},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			if got := sameSystem(tt.a, tt.b); got != tt.want {
				t.Errorf("sameSystem(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
