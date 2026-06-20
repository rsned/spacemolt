package worker

import (
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func testState() *game.State {
	return &game.State{
		Credits: 12345.67,
		Ship:    game.Ship{ID: "ship-xyz"},
		System: game.SystemData{
			ID:   "sys-001",
			Name: "Sol",
			POIs: []game.POI{
				{ID: "belt-b", Type: "asteroid_belt"},
				{ID: "belt-a", Type: "asteroid_belt"},
				{ID: "station-1", Type: "station"},
				{ID: "gas-1", Type: "gas_cloud"},
			},
		},
	}
}

func TestResolveTokens(t *testing.T) {
	st := testState()
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"no tokens", []string{"travel", "belt-a"}, []string{"travel", "belt-a"}},
		{"station", []string{"travel", "$STATION$"}, []string{"travel", "station-1"}},
		{"belt id tiebreak", []string{"travel", "$ASTEROID_BELT$"}, []string{"travel", "belt-a"}},
		{"gas cloud", []string{"travel", "$GAS_CLOUD$"}, []string{"travel", "gas-1"}},
		{"lowercase token name", []string{"travel", "$station$"}, []string{"travel", "station-1"}},
		{"state system", []string{"jump", "$SYSTEM$"}, []string{"jump", "sys-001"}},
		{"state ship", []string{"switch_ship", "$SHIP$"}, []string{"switch_ship", "ship-xyz"}},
		{"state credits", []string{"deposit_credits", "$CREDITS$"}, []string{"deposit_credits", "12345"}},
		{"token inside quoted arg", []string{"chat", "go to $STATION$ now"}, []string{"chat", "go to station-1 now"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveTokens(tc.in, st)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("arg %d: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestResolveTokensErrors(t *testing.T) {
	st := testState()
	cases := []struct {
		name string
		in   []string
	}{
		{"no matching poi", []string{"travel", "$ICE_FIELD$"}},
		{"unknown token", []string{"travel", "$STATON$"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveTokens(tc.in, st)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			var te *TokenError
			if !errors.As(err, &te) {
				t.Fatalf("expected *TokenError, got %T: %v", err, err)
			}
		})
	}
}
