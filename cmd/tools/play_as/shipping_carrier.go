package main

import (
	"context"
	"fmt"
	"strings"
)

// carrierCommand handles the CARRIER side of freight: accepting a posted
// contract, settling it at the destination, and the return escape hatch.
//
// play_as shipped the reads (list/get/track/profile), pay_debt, and later the
// shipper side (quote/post/active/cancel) — but never accept/deliver/return.
// The GameClient methods existed the whole time; only the REPL wiring was
// missing, so a contract could be found from a play_as session and then not
// hauled from one.
//
// Accept does NOT put the package in your hold: it lands in the carrier's
// STORAGE AT ORIGIN. Flying off straight after accepting carries nothing and
// arrives to a deliver that cannot settle — withdraw_items first.
//
// parts is the whole command line, so parts[0] is "shipping" and parts[1] is
// the action.
func carrierCommand(ctx context.Context, c shippingSender, parts []string) error {
	if len(parts) < 2 {
		return fmt.Errorf("usage: shipping <accept|deliver|return> <shipment-id>")
	}
	action := strings.ToLower(parts[1])

	if len(parts) < 3 {
		return fmt.Errorf("usage: shipping %s <shipment-id>%s  (`shipping list` shows ids)",
			action, acceptUsageSuffix(action))
	}
	payload := map[string]any{"shipment_id": parts[2]}

	if action == "accept" {
		// Who takes on the liability. The server defaults nothing here, and
		// accepting as a faction when you meant yourself puts the debt on the
		// faction's record, so send the choice explicitly.
		flags, err := shipperFlags(parts[3:], "carrier")
		if err != nil {
			return err
		}
		carrier, _ := flags["carrier"].(string)
		if carrier == "" {
			carrier = "player"
		}
		if carrier != "player" && carrier != "faction" {
			return fmt.Errorf("--carrier must be player or faction, got %q", carrier)
		}
		payload["carrier"] = carrier
	}

	return c.Shipping(ctx, action, payload)
}

// acceptUsageSuffix names the carrier flag only on the action that takes one,
// so deliver's usage line does not advertise a flag it ignores.
func acceptUsageSuffix(action string) string {
	if action == "accept" {
		return " [--carrier player|faction]"
	}

	return ""
}
