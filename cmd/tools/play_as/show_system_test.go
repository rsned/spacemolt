package main

import (
	"reflect"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestSuggestSystems(t *testing.T) {
	systems := []knowledge.System{
		{ID: "nexus_prime", Name: "Nexus Prime"},
		{ID: "nova_terra", Name: "Nova Terra"},
		{ID: "sol", Name: "Sol"},
	}
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"typo within distance 2", "nexis_prime", []string{"nexus_prime"}},
		{"substring on id", "nova", []string{"nova_terra"}},
		{"no match", "zzzzzz", nil},
		{"caps insensitive substring", "SOL", []string{"sol"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := suggestSystems(tt.query, systems)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("suggestSystems(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestSuggestSystemsLimitsToThree(t *testing.T) {
	systems := []knowledge.System{
		{ID: "node_a"}, {ID: "node_b"}, {ID: "node_c"}, {ID: "node_d"},
	}
	if got := suggestSystems("node", systems); len(got) != 3 {
		t.Errorf("len(suggestSystems) = %d, want 3 (capped)", len(got))
	}
}
