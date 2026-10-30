package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
)

// ConsignRule is a standing instruction to hand one item to one agent.
//
// It is an explicit ALLOW-LIST entry, never a pattern: gifting is irreversible
// and outward-facing, and a wildcard would sit one bug away from handing over
// the drone project's reserved energy crystals or a ship's fuel cells, which
// live in the same storage.
type ConsignRule struct {
	// Item is the exact item id to consign.
	Item string
	// To is the recipient AGENT ID, resolved to an in-game username at send
	// time. Agent ids are stable; usernames in config rot.
	To string
	// Min is the smallest amount worth sending, applied to the sendable
	// quantity AFTER Keep. It exists so a collection point batches rather than
	// dribbling a unit per idle pass.
	Min int
	// Keep is held back for the agent's own use. Needed wherever the collector
	// also consumes the item -- gifting the entire stock of something an agent
	// works with would be wrong, and the right reserve is per item, not global.
	Keep int
}

// Consign hands accumulated goods to haulers as part of the ordinary idle
// pass, so a collection point needs no manual control and no interruption to
// its routine.
//
// WHY THIS WORKS WITHOUT A RENDEZVOUS -- send_gift lands in the RECIPIENT's
// storage at the sending station and waits there for them to arrive. Only the
// sender has to be docked where the goods are. So a pinned marketbot can
// consign to a hauler that is days away, and the hauler collects on whatever
// pass suits it.
//
// Reads personal storage at the docked station only, never cargo: a ship's
// hold is its own work. Returns the number of gifts sent.
//
// Failure policy: consignment is strictly less important than any paying
// activity and must never become a new way for a pass to fail, so a refused
// gift does not stop the other rules. It IS reported, though: the failures come
// back joined in err, which the caller only logs. Dropping them silently hid a
// day of refusals on 2026-10-30 -- marketbot_015 had never earned the 1,000
// credits an account needs before it can gift, and the log showed nothing but
// "send_gift pending". An unresolvable recipient still returns at once, since
// it is a config error.
//
// The post-gift state is never re-read to confirm. Worker heartbeat credits
// read a stale 0 after a transfer, which makes a landed gift look failed; the
// command's own reply is the only trustworthy signal.
func Consign(ctx context.Context, client game.GameClient, rules []ConsignRule,
	usernameFor func(agentID string) (string, error),
) (sent int, err error) {
	if client == nil || len(rules) == 0 {
		return 0, nil
	}
	state := client.GetState()
	if state == nil || state.Player.DockedAtBase == "" {
		// Storage reads and gifts are both docked-station-local, so there is
		// nothing to read and nothing to send.
		return 0, nil
	}

	if serr := client.ViewStorage(ctx); serr != nil {
		return 0, nil //nolint:nilerr // a source failure must never fail the pass
	}
	held, ok := consignHeld(client.GetRawJSON("storage"))
	if !ok {
		return 0, nil
	}

	var failed []error
	for _, r := range rules {
		if r.Item == "" || r.To == "" {
			continue
		}
		sendable := held[r.Item] - r.Keep
		if sendable <= 0 || sendable < r.Min {
			continue
		}
		name, nerr := usernameFor(r.To)
		if nerr != nil || name == "" {
			return sent, fmt.Errorf("consign %s: cannot resolve recipient %q: %w", r.Item, r.To, nerr)
		}
		if gerr := client.SendGift(ctx, map[string]any{
			"recipient": name,
			"item_id":   r.Item,
			"quantity":  sendable,
			"source":    "storage",
		}); gerr != nil {
			failed = append(failed, fmt.Errorf("consign %s x%d to %s: %w", r.Item, sendable, r.To, gerr))
			continue
		}
		sent++
	}

	return sent, errors.Join(failed...)
}

// consignHeld reads item quantities out of a view_storage frame. A frame that
// does not decode or names no station is treated as no data rather than as an
// empty storage, so a mis-routed reply cannot read as "nothing held".
func consignHeld(raw []byte) (map[string]int, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var resp struct {
		BaseID string `json:"base_id"`
		Items  []struct {
			ItemID   string  `json:"item_id"`
			Quantity float64 `json:"quantity"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.BaseID == "" {
		return nil, false
	}
	out := make(map[string]int, len(resp.Items))
	for _, it := range resp.Items {
		if it.ItemID != "" {
			out[it.ItemID] += int(it.Quantity)
		}
	}

	return out, true
}

// ParseConsignRules decodes the compact flag form the supervisor forwards:
// item:recipient:min[:keep], comma-separated.
//
// The fleet yaml holds these as structs; this is only the wire between
// supervisor and worker, which is a single CLI flag like every other
// per-worker option.
//
// A malformed rule is REJECTED rather than skipped. A typo'd flag that parsed
// to zero rules would leave a collection point quietly accumulating goods
// forever with nothing to show why, which is the failure mode this whole
// capability exists to avoid.
func ParseConsignRules(s string) ([]ConsignRule, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []ConsignRule
	for field := range strings.SplitSeq(s, ",") {
		spec := strings.TrimSpace(field)
		if spec == "" {
			continue
		}
		parts := strings.Split(spec, ":")
		if len(parts) < 3 || len(parts) > 4 {
			return nil, fmt.Errorf("consign rule %q: want item:recipient:min[:keep]", spec)
		}
		r := ConsignRule{Item: strings.TrimSpace(parts[0]), To: strings.TrimSpace(parts[1])}
		if r.Item == "" || r.To == "" {
			return nil, fmt.Errorf("consign rule %q: item and recipient are both required", spec)
		}
		min, err := strconv.Atoi(strings.TrimSpace(parts[2]))
		if err != nil {
			return nil, fmt.Errorf("consign rule %q: min: %w", spec, err)
		}
		// min 0 would send on every idle pass as soon as a single unit landed,
		// which is the dribbling this batches to avoid.
		if min < 1 {
			return nil, fmt.Errorf("consign rule %q: min must be at least 1", spec)
		}
		r.Min = min
		if len(parts) == 4 {
			keep, kerr := strconv.Atoi(strings.TrimSpace(parts[3]))
			if kerr != nil {
				return nil, fmt.Errorf("consign rule %q: keep: %w", spec, kerr)
			}
			if keep < 0 {
				return nil, fmt.Errorf("consign rule %q: keep cannot be negative", spec)
			}
			r.Keep = keep
		}
		out = append(out, r)
	}

	return out, nil
}
