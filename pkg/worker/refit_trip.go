package worker

import (
	"context"
	"fmt"
)

const (
	refitBayItem   = "advanced_drone_bay"
	refitDroneItem = "mining_drone"
	refitBays      = 1
	refitDrones    = 5
	// refitCargoNeeded is one bay (size 10) and five drones (size 5 each).
	refitCargoNeeded = 35
)

// RefitTrip collects a bay and drones from this agent's own storage at hub,
// flies home and fits them there. Drone scripts bind to the system they are
// fitted in, so the fit cannot happen at the hub. Every step reads what is
// already done, so a schedule can repeat the task until it reports fitted.
func (d *WorkerDispatch) RefitTrip(ctx context.Context, hub, home, script string, stripTypes []string) error {
	bays, drones, err := d.refitProgress(ctx)
	if err != nil {
		return err
	}
	if bays >= refitBays && drones >= refitDrones {
		fmt.Fprintln(d.Out, "refit_trip: already fitted") //nolint:errcheck
		return nil
	}
	if err = refuseRoleHarvester(d.Role, stripTypes); err != nil {
		return fmt.Errorf("refit_trip: %w", err)
	}

	if err = d.Client.GetCargo(ctx); err != nil {
		return fmt.Errorf("refit_trip: get_cargo: %w", err)
	}
	st := d.Client.GetState()
	wantBays := max(refitBays-bays-cargoCount(st, refitBayItem), 0)
	wantDrones := max(refitDrones-drones-cargoCount(st, refitDroneItem), 0)
	var collectErr error
	if wantBays+wantDrones > 0 {
		if cargoFreeSpace(st) < refitCargoNeeded {
			return fmt.Errorf("refit_trip: need %d free cargo, have %d", refitCargoNeeded, cargoFreeSpace(st))
		}
		collectErr = d.refitCollect(ctx, hub, wantBays, wantDrones)
	}

	homeSys, homePOI, err := resolveBase(ctx, d.KB, home)
	if err != nil {
		return fmt.Errorf("refit_trip: resolve home %q: %w", home, err)
	}
	if err = d.autopilotAndDock(ctx, homeSys, homePOI); err != nil {
		return fmt.Errorf("refit_trip: travel home: %w", err)
	}
	if collectErr != nil {
		return collectErr
	}

	return d.FitDrones(ctx, script, refitBays, refitDrones, refitBayItem, refitDroneItem,
		FitDronesOpts{Deploy: true, Resume: true, StripTypes: stripTypes})
}

// refitProgress reads the bays fitted and drones loaded.
func (d *WorkerDispatch) refitProgress(ctx context.Context) (bays, drones int, err error) {
	if err = d.Client.GetShip(ctx); err != nil {
		return 0, 0, fmt.Errorf("refit_trip: get_ship: %w", err)
	}
	if bays, err = installedBays(d.Client.GetRawJSON("ship"), refitBayItem); err != nil {
		return 0, 0, fmt.Errorf("refit_trip: %w", err)
	}
	if err = d.Client.GetDrones(ctx); err != nil {
		return 0, 0, fmt.Errorf("refit_trip: get_drones: %w", err)
	}
	ids, err := droneIDsFromRoster(d.Client.GetRawJSON("drones"), refitDroneItem)
	if err != nil {
		// An empty bay reads as no match, which is not a failure here.
		return bays, 0, nil //nolint:nilerr
	}

	return bays, len(ids), nil
}

func (d *WorkerDispatch) refitCollect(ctx context.Context, hub string, wantBays, wantDrones int) error {
	hubSys, hubPOI, err := resolveBase(ctx, d.KB, hub)
	if err != nil {
		return fmt.Errorf("refit_trip: resolve hub %q: %w", hub, err)
	}
	if err = d.autopilotAndDock(ctx, hubSys, hubPOI); err != nil {
		return fmt.Errorf("refit_trip: travel to hub: %w", err)
	}
	for _, part := range []struct {
		item string
		qty  int
	}{{refitBayItem, wantBays}, {refitDroneItem, wantDrones}} {
		if part.qty == 0 {
			continue
		}
		if err = d.Client.WithdrawItems(ctx, part.item, float64(part.qty)); err != nil {
			return fmt.Errorf("refit_trip: withdraw %d %s at %s: %w", part.qty, part.item, hub, err)
		}
	}
	// A withdraw lands a tick later; the resumed fit counts what is aboard, and
	// a stale hold would send it back to storage for parts already taken.
	if err = d.Client.GetCargo(ctx); err != nil {
		return fmt.Errorf("refit_trip: get_cargo: %w", err)
	}

	return nil
}
