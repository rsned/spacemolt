package worker

import (
	"context"
	"encoding/json"
	"fmt"
)

// ownedShipsReply is the part of a list_ships reply switch_hull reads. The
// faction_garage entries are this station's garage (live reply at
// Conjunction Junction, 2026-11-21).
type ownedShipsReply struct {
	ActiveShipClass string `json:"active_ship_class"`
	FactionGarage   []struct {
		ClassID string `json:"class_id"`
		ShipID  string `json:"ship_id"`
	} `json:"faction_garage"`
	Ships []struct {
		ClassID  string `json:"class_id"`
		ShipID   string `json:"ship_id"`
		IsActive bool   `json:"is_active"`
	} `json:"ships"`
}

// SwitchHull boards a ship of class at the docked station: one of the agent's
// own stored hulls first (a hull gifted to it), then one from the faction
// garage. It moves route-fleet truckers out of starter Cobbles into hauling
// hulls as they are built (2026-11-21). Already flying the class, undocked, or
// no such hull here is a no-op, so it can run on a schedule. A refused switch
// (another member claimed that hull first) moves on to the next candidate.
func (d *WorkerDispatch) SwitchHull(ctx context.Context, class string) error {
	st := d.Client.GetState()
	if st == nil || !st.IsDocked() {
		return nil
	}
	if err := d.Client.ListShips(ctx); err != nil {
		return fmt.Errorf("switch_hull: list_ships: %w", err)
	}
	var r ownedShipsReply
	if err := json.Unmarshal(d.Client.GetRawJSON("owned_ships"), &r); err != nil {
		return fmt.Errorf("switch_hull: decode list_ships: %w", err)
	}
	if r.ActiveShipClass == class {
		return nil
	}
	var candidates []string
	for _, s := range r.Ships {
		if s.ClassID == class && !s.IsActive {
			candidates = append(candidates, s.ShipID)
		}
	}
	for _, s := range r.FactionGarage {
		if s.ClassID == class {
			candidates = append(candidates, s.ShipID)
		}
	}
	var lastErr error
	for _, id := range candidates {
		if lastErr = d.Client.SwitchShip(ctx, id); lastErr == nil {
			fmt.Fprintf(d.Out, "switch_hull: boarded %s %s\n", class, id) //nolint:errcheck
			// switch_ship leaves the old hull's capacity cached; re-read the
			// hold so the next command sizes loads from the new ship.
			if err := d.Client.GetCargo(ctx); err != nil {
				return fmt.Errorf("switch_hull: refresh cargo after boarding %s: %w", id, err)
			}
			return nil
		}
		fmt.Fprintf(d.Out, "switch_hull: %s %s refused: %v\n", class, id, lastErr) //nolint:errcheck
	}
	if lastErr != nil {
		return fmt.Errorf("switch_hull: every %s here was refused: %w", class, lastErr)
	}
	return nil
}
