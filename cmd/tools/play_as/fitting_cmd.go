package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// fittingBaseURL is the KB's ship fitting viewer. Its query contract is the
// page's own encodeState/decodeState (kb/ships/fitting.html): ship=<class>,
// eng=<engineering level>, stock=0 for a hand-built fit, w<i>/d<i>/u<i> for
// weapon/defense/utility slots (1-based), am_<ammo_type>=<round id>. Ids are
// [a-z0-9_] throughout the catalog, so nothing is URL-encoded — the page does
// the same, and encoding would only make the link unreadable.
const fittingBaseURL = "https://rsned.github.io/spacemolt-kb/ships/fitting.html"

// fittingURL renders a get_ship reply as a fitting-viewer link. Slot numbers
// follow the order get_ship lists the modules in, per slot type; the page only
// cares which slots are filled. Returns "" when the reply names no ship.
func fittingURL(resp serverapi.GetShipResponse, engineering int) string {
	if resp.Ship.ClassID == "" {
		return ""
	}
	parts := []string{"ship=" + resp.Ship.ClassID}
	if engineering > 0 {
		parts = append(parts, fmt.Sprintf("eng=%d", engineering))
	}

	slotPrefix := map[string]string{"weapon": "w", "defense": "d", "utility": "u"}
	var slots []string
	var ammo []string
	counts := map[string]int{}
	for _, m := range resp.Modules {
		pre, ok := slotPrefix[m.Slot]
		if !ok || m.TypeID == "" {
			continue
		}
		counts[m.Slot]++
		slots = append(slots, fmt.Sprintf("%s%d=%s", pre, counts[m.Slot], m.TypeID))
		if m.AmmoType != "" && m.LoadedAmmoID != "" {
			ammo = append(ammo, fmt.Sprintf("am_%s=%s", m.AmmoType, m.LoadedAmmoID))
		}
	}
	if len(slots) > 0 {
		// The page groups slots by type (all w, then d, then u); emit in
		// that order so the link reads the way the page would write it.
		ordered := make([]string, 0, len(slots))
		for _, pre := range []string{"w", "d", "u"} {
			for _, s := range slots {
				if strings.HasPrefix(s, pre) {
					ordered = append(ordered, s)
				}
			}
		}
		parts = append(parts, "stock=0")
		parts = append(parts, ordered...)
		parts = append(parts, ammo...)
	}
	return fittingBaseURL + "?" + strings.Join(parts, "&")
}

// showFitting refreshes get_ship and prints the fitting-viewer link for the
// hull we are flying, with the pilot's Engineering level so the page's
// CPU/power figures match the game's (module cost falls 1%/level).
func showFitting(ctx context.Context, client game.GameClient) error {
	if err := client.GetShip(ctx); err != nil {
		return fmt.Errorf("get_ship: %w", err)
	}
	raw := client.GetRawJSON("ship")
	if len(raw) == 0 {
		return fmt.Errorf("get_ship returned nothing to fit")
	}
	var resp serverapi.GetShipResponse
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return fmt.Errorf("parse get_ship: %w", err)
	}
	eng := 0
	if st := client.GetState(); st != nil {
		eng = st.Player.Skills["engineering"].Level
	}
	url := fittingURL(resp, eng)
	if url == "" {
		return fmt.Errorf("get_ship named no ship class")
	}
	fmt.Println(url)
	return nil
}
