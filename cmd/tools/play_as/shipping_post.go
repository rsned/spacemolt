package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// shippingSender is the one call the shipper-side actions need. Narrow on
// purpose: it makes quote/post testable without standing up a 150-method
// GameClient, and it is the only thing this file asks of a client.
type shippingSender interface {
	Shipping(ctx context.Context, action string, payload map[string]any) error
}

// shipperOptionalFlags are the post/quote knobs beyond the two positional
// arguments. Names are copied verbatim from the server's shipping schema —
// they are the wire keys, not our own vocabulary.
var shipperOptionalFlags = []string{
	"base_reward",    // int  — carrier's flat pay; required to post
	"speed_bonus",    // int  — extra for fast delivery, decays to the deadline
	"max_total_cost", // int  — reject the post if fees+escrow+premium exceed this
	"service_level",  // str  — standard | priority (timing only, not payment)
	"shipper",        // str  — player (default) | faction
	"visibility",     // str  — who may accept
	"recipient_type", // str  — delivery beneficiary kind
	"recipient_id",   // str
	"source",         // str  — where the sealed package sits (cargo by default)
	"insured",        // bool — request cargo insurance
}

// shipperIntFlags are sent as integers. The server types these numerically, and
// a quoted "2500" is the kind of drift that decodes to zero and reads as
// "omitted" — which for base_reward means the post is rejected as rewardless.
var shipperIntFlags = map[string]bool{
	"base_reward":    true,
	"speed_bonus":    true,
	"max_total_cost": true,
}

// shipperCommand handles the SHIPPER side of freight: `shipping quote` and
// `shipping post`. The carrier side (list/accept/deliver/return/cancel) and the
// account actions (profile/track/pay_debt) live in the main dispatch switch.
//
// Both take a SEALED PACKAGE, not loose cargo: seal first with
// `craft pack_package`, then quote to learn what similar runs paid, then post
// with a reward you choose. There is no automatic distance-based rate — the
// server pays whatever base_reward you set, and refuses a post without one.
//
// parts is the whole command line, so parts[0] is "shipping" and parts[1] is
// the action.
func shipperCommand(ctx context.Context, c shippingSender, parts []string) error {
	if len(parts) < 2 {
		return fmt.Errorf("usage: shipping <quote|post> <package-id> <destination-base-id> [flags]")
	}
	action := strings.ToLower(parts[1])
	if len(parts) < 4 {
		return fmt.Errorf("usage: shipping %s <package-id> <destination-base-id> [--base_reward N] "+
			"[--service_level standard|priority] [--insured true] [--speed_bonus N] [--shipper player|faction]\n"+
			"  the package must already be sealed — see `craft pack_package`", action)
	}

	payload := map[string]any{
		"package_id":          parts[2],
		"destination_base_id": parts[3],
	}

	flags, err := parseFlagArgs(parts[4:], shipperOptionalFlags...)
	if err != nil {
		return err
	}
	for k, v := range flags {
		switch {
		case shipperIntFlags[k]:
			n, err := strconv.ParseInt(fmt.Sprintf("%v", v), 10, 64)
			if err != nil {
				return fmt.Errorf("--%s must be a whole number: %w", k, err)
			}
			payload[k] = n
		case k == "insured":
			payload[k] = flagBool(v)
		default:
			payload[k] = v
		}
	}

	// quote is the call you make to FIND OUT what to offer, so a reward on it
	// would report an estimate as if it were a decision.
	if action == "quote" {
		delete(payload, "base_reward")
	}

	// Fail here rather than at the server: post without a positive reward comes
	// back as reward_required, and a round trip to be told that is a round trip
	// wasted.
	if action == "post" {
		n, ok := payload["base_reward"].(int64)
		if !ok || n <= 0 {
			return fmt.Errorf("shipping post needs a carrier reward: pass --base_reward N " +
				"(run `shipping quote <package-id> <destination>` first — it returns " +
				"estimated_reward from recently-completed runs of similar distance)")
		}
	}

	return c.Shipping(ctx, action, payload)
}
