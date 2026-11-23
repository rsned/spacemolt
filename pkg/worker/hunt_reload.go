package worker

import (
	"context"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/reload"
)

// huntReload fills the hunter's magazines before an engagement and reports
// whether the ship can shoot. Observed live 2026-11-18: pirate-5 and pirate-13
// opened fight after fight with dry magazines — the server answered every tick
// "Your weapons cannot fire — magazine empty!", the engagement timed out, and
// the pass ended at 0 kills. Nothing in the worker ever sent `reload`.
//
// It runs between engagements, where a reload costs a tick and nothing else.
// A dry magazine takes the best-ranked fitting stack; a partial one is only
// topped up with the ammo already loaded, because loading a different type
// discards the rounds still in it. Each module's reload is its own tick, so
// they are issued in sequence.
//
// armed is false only when the ship has ammo-fed weapons and every one of them
// is still dry afterwards: nothing aboard fits, and a fight would be ten ticks
// of taking damage for nothing. Any failure to read the ship fails open.
func huntReload(ctx context.Context, deps HuntDeps, out io.Writer, who string) (armed bool) {
	plan, err := reload.Fetch(ctx, deps.Client, deps.KB)
	if err != nil {
		fmt.Fprintf(out, "hunt[%s]: reload check skipped: %v\n", who, err) //nolint:errcheck
		return true
	}
	return huntReloadPlan(ctx, deps, out, who, plan)
}

// huntWorthReloading reports whether e should be reloaded now. A dry magazine
// takes the best fitting stack. A partial one is topped up only with the ammo
// already loaded, and only once it is below half: one reload spends a whole
// ammo item whatever the magazine still holds. Observed live 2026-11-22,
// pirate-5 spent both spare boxes topping two autocannons up from 648/650 and
// then flew home to rearm.
func huntWorthReloading(e reload.Entry) bool {
	if e.Full() || len(e.Candidates) == 0 {
		return false
	}
	if e.Empty() {
		return true
	}
	return e.Candidates[0].ItemID == e.LoadedAmmoID && e.CurrentAmmo*2 < e.MagazineSize
}

// huntReloadPlan is huntReload on a plan already read.
func huntReloadPlan(ctx context.Context, deps HuntDeps, out io.Writer, who string, plan []reload.Entry) (armed bool) {
	if len(plan) == 0 {
		return true // energy weapons only: nothing to reload
	}
	loaded := 0
	for _, e := range plan {
		rounds := e.CurrentAmmo
		if huntWorthReloading(e) {
			ammo := e.Candidates[0]
			if rerr := deps.Client.Reload(ctx, e.WeaponID, ammo.ItemID); rerr != nil {
				fmt.Fprintf(out, "hunt[%s]: reload %s with %s failed: %v\n", who, e.WeaponName, ammo.ItemID, rerr) //nolint:errcheck
			} else {
				fmt.Fprintf(out, "hunt[%s]: reloaded %s (%d/%d) with %s (%.0f aboard)\n", //nolint:errcheck
					who, e.WeaponName, e.CurrentAmmo, e.MagazineSize, ammo.ItemID, ammo.Quantity)
				rounds = max(rounds, 1)
			}
		}
		if rounds > 0 {
			loaded++
		}
	}
	if loaded > 0 {
		return true
	}
	for _, e := range plan {
		fmt.Fprintf(out, "hunt[%s]: NO AMMO: %s (%s) is empty and no %s ammo is aboard\n", who, e.WeaponName, e.WeaponTypeID, e.AmmoType) //nolint:errcheck
	}
	return false
}
