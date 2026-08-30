package agent

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// boardingSpy is a MemoryKB (a Base that cannot store boarding data) wrapped
// with the BoardingRecorder methods, so the test can see what the wiring
// hands the KB.
type boardingSpy struct {
	knowledge.Base
	captures []knowledge.ShipCapture
	prizes   []knowledge.SeenPrize
}

func (s *boardingSpy) RecordShipCaptures(_ context.Context, rows []knowledge.ShipCapture) error {
	s.captures = append(s.captures, rows...)
	return nil
}

func (s *boardingSpy) RecordPrizeSightings(_ context.Context, rows []knowledge.SeenPrize) error {
	s.prizes = append(s.prizes, rows...)
	return nil
}

// Wiring must translate both observer shapes into KB rows, keeping the
// observer stamp (who saw it, where, at which tick).
func TestWireBoardingObservers_RecordsCapturesAndPrizes(t *testing.T) {
	c := &game.Client{}
	spy := &boardingSpy{Base: knowledge.NewMemoryKB()}
	WireBoardingObservers(c, spy)

	if c.CaptureObserver() == nil || c.PrizeObserver() == nil {
		t.Fatal("observers not registered")
	}
	c.CaptureObserver()(game.ObservedCapture{
		Capture: serverapi.ShipCaptured{BattleID: "b1", Tick: 9, BoardingOperationID: "op1",
			CaptorID: "molten", FormerOwnerID: "h7", ShipID: "s7", ShipClass: "congregation"},
		SystemID: "zaniah", ObserverID: "h7",
	})
	c.PrizeObserver()([]game.ObservedPrize{{PrizeID: "pz1", ActorID: "molten", Status: "in_transit",
		SystemID: "zaniah", POIID: "gate", Tick: 10, ObserverID: "mb", Source: "get_nearby"}})

	if len(spy.captures) != 1 || spy.captures[0].BoardingOperationID != "op1" ||
		spy.captures[0].ObserverID != "h7" || spy.captures[0].Tick != 9 || spy.captures[0].CaptorID != "molten" {
		t.Errorf("captures = %+v", spy.captures)
	}
	if len(spy.prizes) != 1 || spy.prizes[0].PrizeID != "pz1" || spy.prizes[0].ObserverID != "mb" ||
		spy.prizes[0].Tick != 10 || spy.prizes[0].POIID != "gate" {
		t.Errorf("prizes = %+v", spy.prizes)
	}
}

// A KB that cannot store boarding data (in-memory, mocks) leaves the client
// unwired rather than registering observers that would fail on every push.
func TestWireBoardingObservers_NoOpWithoutRecorder(t *testing.T) {
	c := &game.Client{}
	WireBoardingObservers(c, knowledge.NewMemoryKB())
	if c.CaptureObserver() != nil || c.PrizeObserver() != nil {
		t.Error("observers registered on a KB that cannot record boarding")
	}
}
