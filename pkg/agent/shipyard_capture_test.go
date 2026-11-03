package agent

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

type shipyardSpy struct {
	knowledge.Base
	quotes   []knowledge.ShipyardQuote
	licenses []knowledge.ShipLicense
}

func (s *shipyardSpy) RecordShipyardQuote(_ context.Context, q knowledge.ShipyardQuote) error {
	s.quotes = append(s.quotes, q)
	return nil
}

func (s *shipyardSpy) RecordShipLicense(_ context.Context, l knowledge.ShipLicense) error {
	s.licenses = append(s.licenses, l)
	return nil
}

func TestWireShipyardObserverRecordsQuote(t *testing.T) {
	c := &game.Client{}
	spy := &shipyardSpy{Base: knowledge.NewMemoryKB()}
	WireShipyardObserver(c, spy, "agent-1")

	obs := c.ShipyardObserver()
	if obs == nil {
		t.Fatal("observer not registered")
	}
	royalty := 5
	seen := time.Unix(1000, 0).UTC()
	obs(game.ObservedShipyard{
		Kind: "quote", ShipClass: "cutter", Labor: 1000, MaterialCost: 200,
		ProvideMaterialsTotal: 300, YardMargin: 7, ShipyardTierHere: 2,
		Royalty: &royalty, BaseID: "base-1", FactionID: "f1", Tick: 42, SeenAt: seen, Raw: []byte(`{}`),
	})
	obs(game.ObservedShipyard{
		Kind: "license", ShipClass: "cutter", Empire: "solarian", CostPaid: 900,
		FactionID: "f1", Tick: 43, SeenAt: seen,
	})

	if len(spy.quotes) != 1 {
		t.Fatalf("quotes = %d, want 1", len(spy.quotes))
	}
	q := spy.quotes[0]
	if q.Labor != 1000 || q.Mode != "quote" || q.StationID != "base-1" || q.AgentID != "agent-1" ||
		q.FactionID != "f1" || q.MaterialCost != 200 || q.Royalty == nil || *q.Royalty != 5 || q.Tick != 42 {
		t.Errorf("unexpected quote: %+v", q)
	}
	if len(spy.licenses) != 1 {
		t.Fatalf("licenses = %d, want 1", len(spy.licenses))
	}
	l := spy.licenses[0]
	if l.CostPaid != 900 || l.Empire != "solarian" || l.AgentID != "agent-1" || l.FactionID != "f1" {
		t.Errorf("unexpected license: %+v", l)
	}
}

func TestWireShipyardObserverSkipsNonRecorder(t *testing.T) {
	c := &game.Client{}
	WireShipyardObserver(c, knowledge.NewMemoryKB(), "agent-1")
	if c.ShipyardObserver() != nil {
		t.Fatal("observer registered for KB without ShipyardRecorder")
	}
	WireShipyardObserver(nil, knowledge.NewMemoryKB(), "a")
	WireShipyardObserver(c, nil, "a")
}
