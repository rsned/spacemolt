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
// rows. An empty body yields no hulls and no error: a missing cache entry means
// "nothing captured this pass", which must never fail the pass.
func HullsFrom(raw []byte) ([]Hull, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var resp serverapi.ListShipsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("assets: decode list_ships: %w", err)
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

	return out, nil
}
