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
