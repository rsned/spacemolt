package game

import (
	"encoding/json"
	"os"
	"testing"
)

func shipyardFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func shipyardClient(t *testing.T) (*Client, *[]ObservedShipyard) {
	t.Helper()
	c := newTestClient()
	c.state.Player.DockedAtBase = "base_x"
	c.state.Player.FactionID = "fac_y"
	c.state.CurrentTick = 4242
	var got []ObservedShipyard
	c.SetShipyardObserver(func(o ObservedShipyard) { got = append(got, o) })
	return c, &got
}

func TestShipyardObserverQuote(t *testing.T) {
	c, got := shipyardClient(t)
	c.notifyShipyardFromPayload(shipyardFixture(t, "shipyard_quote.json"))
	if len(*got) != 1 {
		t.Fatalf("got %d observations, want 1", len(*got))
	}
	o := (*got)[0]
	if o.Kind != "quote" || o.ShipClass != "congregation" || o.Labor != 1000 || o.MaterialCost != 30906 ||
		o.YardMargin != 9571 || o.ProvideMaterialsTotal != 10571 || o.ShipyardTierHere != 2 {
		t.Errorf("unexpected observation: %+v", o)
	}
	if o.Royalty == nil || *o.Royalty != 723 {
		t.Errorf("royalty = %v, want 723", o.Royalty)
	}
	if o.BaseID != "base_x" || o.FactionID != "fac_y" || o.Tick != 4242 || o.SeenAt.IsZero() || len(o.Raw) == 0 {
		t.Errorf("context fields wrong: %+v", o)
	}
}

func TestShipyardObserverCommissionWrappedInResult(t *testing.T) {
	c, got := shipyardClient(t)
	c.notifyShipyardFromPayload(shipyardFixture(t, "shipyard_commission.json"))
	if len(*got) != 1 {
		t.Fatalf("got %d observations, want 1", len(*got))
	}
	o := (*got)[0]
	if o.Kind != "commission" || o.Labor != 1000 || o.ShipClass != "congregation" {
		t.Errorf("unexpected observation: %+v", o)
	}
	if o.Royalty == nil || *o.Royalty != 723 {
		t.Errorf("royalty = %v, want 723", o.Royalty)
	}
}

func TestShipyardObserverLicense(t *testing.T) {
	c, got := shipyardClient(t)
	c.notifyShipyardFromPayload(shipyardFixture(t, "shipyard_license.json"))
	if len(*got) != 1 {
		t.Fatalf("got %d observations, want 1", len(*got))
	}
	o := (*got)[0]
	if o.Kind != "license" || o.CostPaid != 1000000 || o.Empire != "outerrim" || o.ShipClass != "congregation" {
		t.Errorf("unexpected observation: %+v", o)
	}
}

func TestShipyardObserverNoRoyaltyText(t *testing.T) {
	c, got := shipyardClient(t)
	p := shipyardFixture(t, "shipyard_quote.json")
	p["message"] = "Quote for a hull."
	delete(p, "blockers")
	c.notifyShipyardFromPayload(p)
	if len(*got) != 1 || (*got)[0].Royalty != nil {
		t.Fatalf("want one observation with nil royalty, got %+v", *got)
	}
}

func TestShipyardObserverIgnoresOtherPayloads(t *testing.T) {
	c, got := shipyardClient(t)
	c.notifyShipyardFromPayload(map[string]any{"action": "browse_ships", "listings": []any{}, "ship_class": "x"})
	c.notifyShipyardFromPayload(map[string]any{"result": "ok"})
	if len(*got) != 0 {
		t.Fatalf("unexpected observations: %+v", *got)
	}
}

func TestShipyardObserverNilObserver(t *testing.T) {
	c := newTestClient()
	c.notifyShipyardFromPayload(shipyardFixture(t, "shipyard_quote.json"))
}

func TestParseRoyalty(t *testing.T) {
	if r := parseRoyalty("nothing", "pays 1000 labor + 734 royalty"); r == nil || *r != 734 {
		t.Errorf("got %v", r)
	}
	if r := parseRoyalty("none"); r != nil {
		t.Errorf("got %v", *r)
	}
}

func TestParseRoyaltyThousandsSeparator(t *testing.T) {
	if r := parseRoyalty("pays 1,000 labor + 12,345 royalty"); r == nil || *r != 12345 {
		t.Errorf("got %v", r)
	}
}
