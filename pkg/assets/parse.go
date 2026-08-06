package assets

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// ParseCurrentMax splits the server's "current/max" strings (OwnedShip.Hull,
// OwnedShip.Fuel) into two ints. ok is false for anything that is not exactly
// two integers separated by one slash — callers keep the raw string in that
// case rather than recording a misleading zero.
func ParseCurrentMax(s string) (cur, max int, ok bool) {
	before, after, found := strings.Cut(s, "/")
	if !found || strings.Contains(after, "/") {
		return 0, 0, false
	}
	c, err := strconv.Atoi(strings.TrimSpace(before))
	if err != nil {
		return 0, 0, false
	}
	m, err := strconv.Atoi(strings.TrimSpace(after))
	if err != nil {
		return 0, 0, false
	}

	return c, m, true
}

// HullsFrom decodes a raw list_ships body (cache key "owned_ships") into hull
// rows. The bool reports whether a body was actually decoded, mirroring
// CarrierFrom: an empty body yields no hulls, no error and ok=false, because a
// missing cache entry means "nothing captured this pass", which must never
// fail the pass.
//
// The flag exists because the caller cannot otherwise distinguish an empty
// cache from a fleet of zero, and the two demand opposite writes. In this game
// an agent can never own zero ships — you must always hold at least one, and a
// destroyed last hull respawns you in a Tier 0 starter — so an empty slice
// with ok=false is always a stale cache. Treating it as a real result would
// wipe agent_hulls on reconnect churn.
func HullsFrom(raw []byte) ([]Hull, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	var resp serverapi.ListShipsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, false, fmt.Errorf("assets: decode list_ships: %w", err)
	}
	out := make([]Hull, 0, len(resp.Ships))
	for _, s := range resp.Ships {
		h := Hull{
			ShipID:         s.ShipID,
			ClassID:        s.ClassID,
			ClassName:      s.ClassName,
			IsActive:       s.IsActive,
			HullRaw:        s.Hull,
			FuelRaw:        s.Fuel,
			CargoUsed:      s.CargoUsed,
			Location:       s.Location,
			LocationBaseID: s.LocationBaseID,
			Modules:        s.Modules,
			ListingID:      s.ListingID,
			ListingPrice:   s.ListingPrice,
			ListingBaseID:  s.ListingBaseID,
		}
		h.HullCurrent, h.HullMax, _ = ParseCurrentMax(s.Hull)
		h.FuelCurrent, h.FuelMax, _ = ParseCurrentMax(s.Fuel)
		out = append(out, h)
	}

	return out, true, nil
}

// CarrierFrom decodes a raw shipping action=profile body (cache key
// "shipping_profile"). ok is false for an empty body — "not captured this
// pass", not an error.
func CarrierFrom(raw []byte) (Carrier, bool, error) {
	if len(raw) == 0 {
		return Carrier{}, false, nil
	}
	var resp serverapi.ShippingProfileResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Carrier{}, false, fmt.Errorf("assets: decode shipping profile: %w", err)
	}
	// NOTE the field is Progression (json "progression"), NOT TierProgress.
	// Verified against pkg/game/serverapi/responses_shipping.go:184. Decoding
	// against the wrong key succeeds with every field zero, which would read as
	// "no progress toward the next tier" rather than as an error.
	p, capy, tp := resp.Profile, resp.Capacity, resp.Progression

	return Carrier{
		Tier:                          p.Tier,
		SuccessfulDeliveries:          p.SuccessfulDeliveries,
		DeliveredValue:                p.DeliveredValue,
		PriorityDeliveries:            p.PriorityDeliveries,
		Returns:                       p.Returns,
		Breaches:                      p.Breaches,
		Defaults:                      p.Defaults,
		ActiveContracts:               p.ActiveContracts,
		ActiveLiability:               p.ActiveLiability,
		OutstandingDebt:               p.OutstandingDebt,
		DebtBlocksAcceptance:          resp.DebtBlocksAcceptance,
		NextTier:                      tp.NextTier,
		AtMaximumTier:                 tp.AtMaximumTier,
		RequiredSuccessfulDeliveries:  tp.RequiredSuccessfulDeliveries,
		RemainingSuccessfulDeliveries: tp.RemainingSuccessfulDeliveries,
		RequiredDeliveredValue:        tp.RequiredDeliveredValue,
		RemainingDeliveredValue:       tp.RemainingDeliveredValue,
		ActiveContractLimit:           capy.ActiveContractLimit,
		ActiveContractsUnlimited:      capy.ActiveContractsUnlimited,
		AggregateLiabilityLimit:       capy.AggregateLiabilityLimit,
		RemainingAggregateLiability:   capy.RemainingAggregateLiability,
		SinglePackageLiabilityLimit:   capy.SinglePackageLiabilityLimit,
		LiabilityUnlimited:            capy.LiabilityUnlimited,
	}, true, nil
}

// hintSeparator is the fixed phrase between the item total and the base list in
// view_storage's prose hint.
const hintSeparator = " in storage at "

// hintEmptySentinel is what the server sends when an agent holds nothing
// anywhere. It matches the general shape, so it MUST be recognised before the
// generic split: the tail "any station." would otherwise become a base id, get
// queried, and -- far worse -- make the base list non-empty, suppressing the
// deletion that should have cleared the agent's stale holdings.
const hintEmptySentinel = "No items in storage at any station."

// StorageHint is the parsed form of view_storage's hint.
//
// Total is the item count across ALL listed bases, verified against live data:
// databot's "920 items" equals the exact sum of its ten item quantities. That
// makes it a truncation detector -- if a sweep's sum falls short of Total,
// bases were omitted from the hint.
type StorageHint struct {
	Bases    []string
	Total    float64
	HasTotal bool
}

// ParseStorageHint reads the prose hint into a base list.
//
// ok=false means "unparseable" and callers MUST NOT delete anything on it: an
// empty sweep is indistinguishable from "sold everything" and would erase real
// holdings. ok=true with no bases is the genuine "holds nothing" answer.
//
// The hint is agent-global, not per-base: a query against a station where the
// agent holds nothing still returns the full list (verified 2026-08-06 against
// databot at grand_exchange_station). So one call from anywhere is enough.
func ParseStorageHint(hint string) (StorageHint, bool) {
	h := strings.TrimSpace(hint)
	if h == "" {
		return StorageHint{}, false
	}
	if h == hintEmptySentinel {
		return StorageHint{}, true
	}
	// Cut on the separator FIRST. The total is comma-grouped ("2,720,379"), so
	// splitting the whole string on ", " would shred the number into fake bases.
	head, tail, found := strings.Cut(h, hintSeparator)
	if !found || strings.TrimSpace(tail) == "" {
		return StorageHint{}, false
	}

	out := StorageHint{}
	if total, ok := parseGroupedCount(head); ok {
		out.Total, out.HasTotal = total, true
	}

	for _, part := range strings.Split(tail, ",") {
		base := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(part), "."))
		if base == "" {
			continue
		}
		out.Bases = append(out.Bases, base)
	}
	if len(out.Bases) == 0 {
		return StorageHint{}, false
	}

	return out, true
}

// parseGroupedCount reads the leading "2,720,379 items" style count. A missing
// or unreadable count is not fatal -- it only disables the truncation
// cross-check, and the base list is the load-bearing part.
func parseGroupedCount(head string) (float64, bool) {
	fields := strings.Fields(head)
	if len(fields) == 0 {
		return 0, false
	}
	n, err := strconv.ParseFloat(strings.ReplaceAll(fields[0], ",", ""), 64)
	if err != nil {
		return 0, false
	}

	return n, true
}

// StorageFrom decodes a raw view_storage body (cache key "storage" -- NOT
// "view_storage"; see the key table in the plan) into one base's holdings plus
// the agent-global hint.
//
// ok=false for an empty body means "nothing captured this pass" and must never
// be treated as "holds nothing": the caller distinguishes the two, because for
// storage -- unlike hulls -- zero really is reachable.
func StorageFrom(raw []byte) (StorageBase, string, bool, error) {
	if len(raw) == 0 {
		return StorageBase{}, "", false, nil
	}
	var resp serverapi.ViewStorageResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return StorageBase{}, "", false, fmt.Errorf("assets: decode view_storage: %w", err)
	}
	out := StorageBase{BaseID: resp.BaseID, Credits: resp.Credits}
	for _, it := range resp.Items {
		out.Items = append(out.Items, StorageItem{
			ItemID: it.ItemID, Name: it.Name, Quantity: it.Quantity, Size: it.Size,
		})
	}

	return out, resp.Hint, true, nil
}
