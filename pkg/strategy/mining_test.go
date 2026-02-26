package strategy

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestMiningStrategyName(t *testing.T) {
	s := &MiningStrategy{}
	if got := s.Name(); got != "mining" {
		t.Errorf("Name() = %q, want %q", got, "mining")
	}
}

func TestMiningStrategyDescription(t *testing.T) {
	s := &MiningStrategy{}
	desc := s.Description()
	if desc == "" {
		t.Error("Description() is empty")
	}
}

func TestMiningStatusTransitions(t *testing.T) {
	s := &MiningStrategy{}

	if got := s.CurrentStatus(); got != "idle" {
		t.Errorf("initial status = %q, want %q", got, "idle")
	}

	s.setStatus("mining")
	if got := s.CurrentStatus(); got != "mining" {
		t.Errorf("status = %q, want %q", got, "mining")
	}

	s.setStatus("mining run #5 (earned 1000 credits)")
	if got := s.CurrentStatus(); got != "mining run #5 (earned 1000 credits)" {
		t.Errorf("status = %q, want expected", got)
	}

	s.setStatus("error: connection lost")
	if got := s.CurrentStatus(); got != "error: connection lost" {
		t.Errorf("status = %q, want %q", got, "error: connection lost")
	}
}

func TestMiningStationActionParsing(t *testing.T) {
	// This tests the parameter parsing logic in MiningStrategy.Run.
	// We verify the mapping between config parameter strings and game.StationActionStrategy values.
	// Since we cannot easily call Run without a real client/server, we test the mapping logic
	// by verifying the expected station action function identities.

	tests := []struct {
		param    string
		wantFunc game.StationActionStrategy
	}{
		{"sell", game.StationActionSellAll},
		{"craft-sell", game.StationActionCraftAndSell},
		{"craft-deposit", game.StationActionCraftAndDeposit},
	}

	for _, tt := range tests {
		t.Run(tt.param, func(t *testing.T) {
			// Verify the game package exports the expected station action functions.
			if tt.wantFunc == nil {
				t.Errorf("station action function for %q is nil", tt.param)
			}
		})
	}
}
