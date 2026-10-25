package main

import (
	"slices"
	"testing"
)

// The station command takes its sub-action as `action`; the generic
// passthrough would send a bare positional as arg1 and the server answers
// invalid_payload (seen twice live, 2026-10-25).
func TestStationParts_PositionalBecomesAction(t *testing.T) {
	cases := []struct {
		in, want []string
	}{
		{[]string{"station", "info"}, []string{"station", "--action", "info"}},
		{[]string{"station", "set_market_fee", "--fee", "5"}, []string{"station", "--action", "set_market_fee", "--fee", "5"}},
		{[]string{"station", "--action", "info"}, []string{"station", "--action", "info"}},
		{[]string{"station"}, []string{"station"}},
	}
	for _, c := range cases {
		if got := stationParts(c.in); !slices.Equal(got, c.want) {
			t.Errorf("stationParts(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
