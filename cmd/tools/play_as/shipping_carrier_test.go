package main

import (
	"context"
	"strings"
	"testing"
)

// accept/deliver/return are the carrier side of freight. play_as could find a
// contract (`shipping list`) but not haul it, so the whole carrier workflow was
// unreachable from a play_as session despite the client methods existing.
func TestShippingAcceptSendsShipmentAndCarrier(t *testing.T) {
	c := &shippingRecorder{}
	err := carrierCommand(context.Background(), c, []string{"shipping", "accept", "shp_1"})
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if c.action != "accept" {
		t.Errorf("action = %q, want accept", c.action)
	}
	if c.payload["shipment_id"] != "shp_1" {
		t.Errorf("shipment_id = %v, want shp_1", c.payload["shipment_id"])
	}
	// The server defaults nothing: accepting as a faction when you meant
	// yourself puts the liability on the faction's record, so the choice is
	// always sent explicitly and defaults to the individual.
	if c.payload["carrier"] != "player" {
		t.Errorf("carrier = %v, want player by default", c.payload["carrier"])
	}
}

func TestShippingAcceptAsFaction(t *testing.T) {
	c := &shippingRecorder{}
	err := carrierCommand(context.Background(), c,
		[]string{"shipping", "accept", "shp_1", "--carrier", "faction"})
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if c.payload["carrier"] != "faction" {
		t.Errorf("carrier = %v, want faction", c.payload["carrier"])
	}
}

// A typo'd carrier must not reach the wire: the server would either reject it a
// tick later or, worse, read it as a kind we did not mean.
func TestShippingAcceptRejectsUnknownCarrier(t *testing.T) {
	c := &shippingRecorder{}
	err := carrierCommand(context.Background(), c,
		[]string{"shipping", "accept", "shp_1", "--carrier", "corporation"})
	if err == nil {
		t.Fatal("an unknown carrier kind must be refused locally")
	}
	if !strings.Contains(err.Error(), "corporation") {
		t.Errorf("error %q should quote the offending value", err)
	}
	if c.calls != 0 {
		t.Error("nothing should reach the wire")
	}
}

func TestShippingDeliverAndReturnSendOnlyTheID(t *testing.T) {
	for _, action := range []string{"deliver", "return"} {
		c := &shippingRecorder{}
		if err := carrierCommand(context.Background(), c, []string{"shipping", action, "shp_9"}); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if c.action != action {
			t.Errorf("action = %q, want %s", c.action, action)
		}
		if c.payload["shipment_id"] != "shp_9" {
			t.Errorf("%s: shipment_id = %v, want shp_9", action, c.payload["shipment_id"])
		}
		// deliver/return take no carrier: sending one would be a key the
		// server reads as a choice nobody made.
		if _, ok := c.payload["carrier"]; ok {
			t.Errorf("%s must not send a carrier", action)
		}
	}
}

func TestShippingCarrierUsageRequiresID(t *testing.T) {
	for _, action := range []string{"accept", "deliver", "return"} {
		c := &shippingRecorder{}
		err := carrierCommand(context.Background(), c, []string{"shipping", action})
		if err == nil {
			t.Fatalf("%s without an id must be a usage error", action)
		}
		if !strings.Contains(err.Error(), "usage") {
			t.Errorf("%s: error %q should be a usage message", action, err)
		}
		if c.calls != 0 {
			t.Errorf("%s: nothing should reach the wire", action)
		}
	}
}

// Only accept takes --carrier, so only accept's usage line should mention it.
func TestCarrierUsageSuffixOnlyOnAccept(t *testing.T) {
	if s := acceptUsageSuffix("accept"); !strings.Contains(s, "--carrier") {
		t.Errorf("accept usage suffix = %q, want it to name --carrier", s)
	}
	for _, action := range []string{"deliver", "return"} {
		if s := acceptUsageSuffix(action); s != "" {
			t.Errorf("%s usage suffix = %q, want empty", action, s)
		}
	}
}
