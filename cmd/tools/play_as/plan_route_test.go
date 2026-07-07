package main

import (
	"testing"
)

func TestParseFuelPerJump(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
		ok   bool
	}{
		{"authoritative value", `{"fuel_per_jump":9,"estimated_fuel":162}`, 9, true},
		{"other fields present", `{"route":["a","b"],"fuel_per_jump":12}`, 12, true},
		{"missing field", `{"estimated_fuel":100}`, 0, false},
		{"zero fuel", `{"fuel_per_jump":0}`, 0, false},
		{"negative fuel", `{"fuel_per_jump":-3}`, 0, false},
		{"empty body", ``, 0, false},
		{"malformed json", `{not json`, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseFuelPerJump([]byte(c.raw))
			if got != c.want || ok != c.ok {
				t.Errorf("parseFuelPerJump(%q) = (%d, %v), want (%d, %v)", c.raw, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestResolveSystemToken(t *testing.T) {
	byID := map[string]string{"market_prime": "market_prime", "sol": "sol"}
	byName := map[string]string{"market prime": "market_prime", "sol": "sol"}

	cases := []struct {
		tok  string
		want string
		ok   bool
	}{
		{"market_prime", "market_prime", true},
		{"Market Prime", "market_prime", true}, // name with space + case
		{"SOL", "sol", true},
		{"nowhere", "", false},
	}
	for _, c := range cases {
		got, ok := resolveSystemToken(c.tok, byID, byName)
		if ok != c.ok || got != c.want {
			t.Errorf("resolveSystemToken(%q) = (%q, %v), want (%q, %v)", c.tok, got, ok, c.want, c.ok)
		}
	}
}
