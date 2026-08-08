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

// hintSeparators are the phrases between the item total and the base list.
// view_storage says "in storage at"; view_faction_storage says "in faction
// storage at" (observed live 2026-08-07 against craftsman-1). Neither string
// contains the other, so the search order does not matter.
var hintSeparators = []string{" in faction storage at ", " in storage at "}

// hintEmptySentinels are what the server sends when nothing is held anywhere.
// They match the general shape, so they MUST be recognised before the generic
// split: the tail "any station." would otherwise become a base id, get queried,
// and -- far worse -- make the base list non-empty, suppressing the deletion
// that should have cleared the stale holdings.
var hintEmptySentinels = []string{
	"No items in storage at any station.",
	"No items in faction storage at any station.",
}

// cutHint splits a hint on whichever separator it carries.
func cutHint(h string) (head, tail string, found bool) {
	for _, sep := range hintSeparators {
		if head, tail, found = strings.Cut(h, sep); found {
			return head, tail, true
		}
	}

	return "", "", false
}

// opaqueBaseIDLen is the length of the unnamed bases' hex ids.
const opaqueBaseIDLen = 32

// looksLikeBaseID reports whether s has the shape of a base id.
//
// This is what stops trailing prose becoming a base, and the bar has to be
// higher than "lowercase word": the hint prose is itself lowercase, so a
// charset check alone would happily record "any" (from the "no items ... at any
// station." sentinel) or "no" as bases. A phantom base is worse than a missed
// one -- it gets queried, and a non-empty list suppresses the deletion that
// should have cleared stale holdings.
//
// Checked against all 43 bases in the knowledge base on 2026-08-07: every id is
// [a-z0-9_]+, every named one contains an underscore, and the only three
// without are bare 32-char hex ids.
func looksLikeBaseID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}

	return strings.Contains(s, "_") || len(s) == opaqueBaseIDLen
}

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
	for _, sentinel := range hintEmptySentinels {
		if h == sentinel {
			return StorageHint{}, true
		}
	}
	// Cut on the separator FIRST. The total is comma-grouped ("2,720,379"), so
	// splitting the whole string on ", " would shred the number into fake bases.
	head, tail, found := cutHint(h)
	if !found || strings.TrimSpace(tail) == "" {
		return StorageHint{}, false
	}

	out := StorageHint{}
	if total, ok := parseGroupedCount(head); ok {
		out.Total, out.HasTotal = total, true
	}

	for _, part := range strings.Split(tail, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// A base id never contains whitespace, and any prose the server appends
		// runs to the end of the hint, so the first non-base-shaped token ends
		// the list.
		base := strings.TrimSuffix(strings.Fields(part)[0], ".")
		if !looksLikeBaseID(base) {
			break
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

// FactionProfileFrom decodes a raw faction_info body (cache key "faction_info").
func FactionProfileFrom(raw []byte) (FactionProfile, bool, error) {
	if len(raw) == 0 {
		return FactionProfile{}, false, nil
	}
	var resp serverapi.FactionInfoResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return FactionProfile{}, false, fmt.Errorf("assets: decode faction_info: %w", err)
	}
	if resp.ID == "" {
		return FactionProfile{}, false, nil
	}

	return FactionProfile{
		FactionID:   resp.ID,
		Name:        resp.Name,
		Tag:         strings.TrimSpace(resp.Tag), // the server pads tags to a fixed width
		LeaderID:    resp.LeaderID,
		Treasury:    resp.Treasury,
		MemberCount: resp.MemberCount,
		OwnedBases:  resp.OwnedBases,
	}, true, nil
}

// FactionStorageFrom decodes a raw view_faction_storage body (cache key
// "faction_storage" -- the classifier routes a storage-shaped payload there
// whenever faction_id is present).
func FactionStorageFrom(raw []byte) (FactionStorageBase, string, bool, error) {
	if len(raw) == 0 {
		return FactionStorageBase{}, "", false, nil
	}
	var resp serverapi.ViewFactionStorageResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return FactionStorageBase{}, "", false, fmt.Errorf("assets: decode view_faction_storage: %w", err)
	}
	out := FactionStorageBase{
		BaseID:       resp.BaseID,
		Credits:      resp.Credits,
		FuelReserve:  resp.FactionFuelReserve,
		FuelCapacity: resp.FactionFuelCapacity,
	}
	for _, it := range resp.Items {
		out.Items = append(out.Items, StorageItem{
			ItemID: it.ItemID, Name: it.Name, Quantity: it.Quantity, Size: it.Size,
		})
	}

	return out, resp.Hint, true, nil
}
