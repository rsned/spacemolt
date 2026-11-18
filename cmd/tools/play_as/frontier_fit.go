package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/explore"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/sweep"
)

// frontier_fit brings an explorer to the frontier-run fit: the pathfinder
// drive it already has, two Survey Scanner IIs (survey power 90) and ten
// repair kits, from the faction storage of the station it is docked at.
// Without "apply" it only prints what it would remove and fit.
//
// Spec: docs/superpowers/specs/2026-11-18-frontier-full-explore-design.md

// frontierFitKits is the repair kits each ship carries on the run.
const frontierFitKits = 10

// frontierFitPlan plans the fit from a get_ship reply. slotOf names a module
// type's slot from the catalog ("" when unknown); the reply's own slot wins.
// scannerCPU and scannerPower are one scanner's catalog cost: Engineering
// only lowers it, so the plan errs toward removing too much, never too little.
func frontierFitPlan(resp serverapi.GetShipResponse, slotOf func(typeID string) string, scannerCPU, scannerPower int) sweep.FitPlan {
	fitted := make([]sweep.FitModule, 0, len(resp.Modules))
	for _, m := range resp.Modules {
		slot := m.Slot
		if slot == "" {
			slot = slotOf(m.TypeID)
		}
		if slot == "" {
			slot = m.Type
			if slot == "mining" {
				slot = sweep.SlotUtility
			}
		}
		fitted = append(fitted, sweep.FitModule{
			ID: m.ID, TypeID: m.TypeID, Name: m.Name, Slot: slot, CPU: m.CPUUsage, Power: m.PowerUsage,
		})
	}
	utility, cpu, power := resp.Ship.UtilitySlots, int(resp.Ship.CPUCapacity), int(resp.Ship.PowerCapacity)
	if c := resp.Class; c != nil {
		if utility == 0 {
			utility = c.UtilitySlots
		}
		if cpu == 0 {
			cpu = c.CPUCapacity
		}
		if power == 0 {
			power = c.PowerCapacity
		}
	}
	return sweep.PlanFit(fitted, utility, cpu, power, scannerCPU, scannerPower)
}

// formatFrontierFit renders a plan for the operator to approve.
func formatFrontierFit(agent string, p sweep.FitPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Frontier fit for %s:\n", agent)
	if !p.OK {
		fmt.Fprintf(&b, "  CANNOT FIT: %s\n", p.Why)
		return b.String()
	}
	if len(p.Remove) == 0 && len(p.Add) == 0 {
		b.WriteString("  already fitted: pathfinder drive + 2 survey scanners\n")
	}
	for _, m := range p.Remove {
		fmt.Fprintf(&b, "  remove  %s (%s, %d CPU, %d power)\n", m.Name, m.Slot, m.CPU, m.Power)
	}
	if len(p.Add) > 0 {
		fmt.Fprintf(&b, "  fit     %s x%d\n", p.Add[0], len(p.Add))
	}
	fmt.Fprintf(&b, "  after:  CPU %d/%d, power %d/%d\n", p.CPUAfter, p.CPUCap, p.PowerAfter, p.PowerCap)
	return b.String()
}

// readShip refreshes get_ship and decodes the reply.
func readShip(ctx context.Context, client game.GameClient) (serverapi.GetShipResponse, error) {
	var resp serverapi.GetShipResponse
	if err := client.GetShip(ctx); err != nil {
		return resp, fmt.Errorf("get_ship: %w", err)
	}
	raw := client.GetRawJSON("ship")
	if len(raw) == 0 {
		return resp, fmt.Errorf("get_ship returned nothing")
	}
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return resp, fmt.Errorf("parse get_ship: %w", err)
	}
	return resp, nil
}

// cargoCount is how many of item the ship carries.
func cargoCount(st *game.State, item string) int {
	if st == nil {
		return 0
	}
	n := 0.0
	for _, it := range st.Ship.Cargo {
		if it.ItemID == item {
			n += it.Quantity
		}
	}
	return int(n)
}

// cmdFrontierFit runs `frontier_fit [apply] [fuel_cells=N]`.
func cmdFrontierFit(ctx context.Context, client game.GameClient, args []string) error {
	apply, fuelCells := false, 0
	for _, a := range args {
		switch {
		case a == "apply":
			apply = true
		case strings.HasPrefix(a, "fuel_cells="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "fuel_cells="))
			if err != nil || n < 0 {
				return fmt.Errorf("fuel_cells wants a count, got %q", a)
			}
			fuelCells = n
		default:
			return fmt.Errorf("usage: frontier_fit [apply] [fuel_cells=N]")
		}
	}

	slotOf, scannerCPU, scannerPower := func(string) string { return "" }, 5, 7
	if globalKB != nil {
		slotOf = func(typeID string) string {
			if it, gerr := globalKB.GetItem(ctx, typeID); gerr == nil && it != nil && it.Module != nil {
				return it.Module.Slot
			}
			return ""
		}
		if it, gerr := globalKB.GetItem(ctx, sweep.FitScanner); gerr == nil && it != nil && it.Module != nil && it.Module.CPUUsage > 0 {
			scannerCPU, scannerPower = it.Module.CPUUsage, it.Module.PowerUsage
		}
	}
	ship, err := readShip(ctx, client)
	if err != nil {
		return err
	}
	plan := frontierFitPlan(ship, slotOf, scannerCPU, scannerPower)
	fmt.Print(formatFrontierFit(globalAgentID, plan))
	_ = client.GetCargo(ctx)
	kits := cargoCount(client.GetState(), explore.KitItem)
	fmt.Printf("  repair kits aboard: %d (the run carries %d)\n", kits, frontierFitKits)
	if !plan.OK {
		return fmt.Errorf("frontier_fit: %s", plan.Why)
	}
	if !apply {
		fmt.Println("  dry run — `frontier_fit apply` to do it (docked, at a station with faction storage)")
		return nil
	}
	if st := client.GetState(); st == nil || !st.IsDocked() {
		return fmt.Errorf("frontier_fit apply: dock at the station holding the scanners first")
	}
	supplyErr := applyFrontierFit(ctx, client, func() { time.Sleep(game.SleepTick) }, plan, kits, fuelCells)
	if supplyErr != nil && !strings.Contains(supplyErr.Error(), "supplies are short") {
		return supplyErr
	}

	invalidateSurveyScannerCache()
	after, err := readShip(ctx, client)
	if err != nil {
		return err
	}
	check := frontierFitPlan(after, slotOf, scannerCPU, scannerPower)
	_ = client.GetCargo(ctx)
	fmt.Printf("  now: %d module(s), %d repair kits, %d fuel cells aboard\n",
		len(after.Modules), cargoCount(client.GetState(), explore.KitItem), cargoCount(client.GetState(), "fuel_cell"))
	if !check.OK || len(check.Add) > 0 || len(check.Remove) > 0 {
		return fmt.Errorf("frontier_fit: the ship is not at the frontier fit after applying:\n%s", formatFrontierFit(globalAgentID, check))
	}
	if supplyErr != nil {
		return supplyErr
	}
	fmt.Println("  fit verified: pathfinder drive + 2 survey scanners. Undock and run `survey_system` to read the power (want 90).")
	return nil
}

// fitClient is what applying a fit needs from the game client.
type fitClient interface {
	UninstallMod(ctx context.Context, moduleID string) error
	InstallMod(ctx context.Context, moduleID string) error
	FactionDepositItems(ctx context.Context, itemID string, quantity int) error
	FactionWithdrawItems(ctx context.Context, itemID string, quantity int) error
	WithdrawItems(ctx context.Context, itemID string, quantity float64) error
}

// drawGear takes n of item from faction storage, or, for a ship without the
// permission, from its own storage at this station (where an officer's gift
// of the gear lands).
func drawGear(ctx context.Context, client fitClient, item string, n int) error {
	err := client.FactionWithdrawItems(ctx, item, n)
	if err == nil || !strings.Contains(err.Error(), "no_permission") {
		return err
	}
	fmt.Printf("  (no faction-storage permission; drawing %d %s from this ship's own storage)\n", n, item)
	if oerr := client.WithdrawItems(ctx, item, float64(n)); oerr != nil {
		return fmt.Errorf("faction storage: %w; own storage: %w", err, oerr)
	}
	return nil
}

// applyFrontierFit performs a plan at the docked station: removed modules go
// to faction storage, scanners, repair kits and fuel cells come out of it.
// The scanners are drawn first, so a station that cannot supply them leaves
// the ship as it was. A kit or fuel-cell shortfall is returned as an error
// after the modules are fitted. pause waits one tick between commands.
func applyFrontierFit(ctx context.Context, client fitClient, pause func(), plan sweep.FitPlan, kits, fuelCells int) error {
	n := len(plan.Add)
	if n > 0 {
		if err := drawGear(ctx, client, sweep.FitScanner, n); err != nil {
			return fmt.Errorf("withdraw %d %s (nothing was removed from the ship): %w", n, sweep.FitScanner, err)
		}
		pause()
	}
	for _, m := range plan.Remove {
		if err := client.UninstallMod(ctx, m.ID); err != nil {
			return fmt.Errorf("uninstall %s: %w", m.Name, err)
		}
		pause()
		fmt.Printf("  removed %s\n", m.Name)
		if err := client.FactionDepositItems(ctx, m.TypeID, 1); err != nil {
			fmt.Printf("  (could not deposit %s to faction storage, left in cargo: %v)\n", m.TypeID, err)
		} else {
			pause()
		}
	}
	for range n {
		if err := client.InstallMod(ctx, sweep.FitScanner); err != nil {
			return fmt.Errorf("install %s: %w", sweep.FitScanner, err)
		}
		pause()
		fmt.Printf("  fitted %s\n", sweep.FitScanner)
	}
	var short []error
	withdraw := func(item string, n int) {
		if n <= 0 {
			return
		}
		if err := drawGear(ctx, client, item, n); err != nil {
			short = append(short, fmt.Errorf("could not withdraw %d %s: %w", n, item, err))
			return
		}
		pause()
		fmt.Printf("  loaded %d %s\n", n, item)
	}
	withdraw(explore.KitItem, frontierFitKits-kits)
	withdraw("fuel_cell", fuelCells)
	if err := errors.Join(short...); err != nil {
		return fmt.Errorf("frontier_fit: modules are fitted but supplies are short: %w", err)
	}
	return nil
}
