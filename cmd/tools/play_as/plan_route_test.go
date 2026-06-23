package main

import (
	"testing"
)

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
