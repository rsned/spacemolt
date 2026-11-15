package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

// kitFake models a station (or open space) with no repair service: a
// repair works only from repair kits in cargo, 50 hull per kit.
type kitFake struct {
	safetyFake
	sellsKits bool
}

func (c *kitFake) kits() float64 {
	for _, it := range c.state.Ship.Cargo {
		if it.ItemID == repairKitItem {
			return it.Quantity
		}
	}
	return 0
}

func (c *kitFake) Repair(context.Context) error {
	c.calls = append(c.calls, "repair")
	k := c.kits()
	if k == 0 {
		return errors.New("no_repair_kits: No repair kits found in cargo. Buy repair kits from the market, or dock at a station with repair service.")
	}
	used := 0.0
	for used < k && c.state.Ship.Hull < c.state.Ship.MaxHull {
		c.state.Ship.Hull = min(c.state.Ship.Hull+repairKitHull, c.state.Ship.MaxHull)
		used++
	}
	c.state.Ship.Cargo = []game.CargoItem{{ItemID: repairKitItem, Quantity: k - used}}
	return nil
}

func (c *kitFake) Buy(_ context.Context, item string, qty float64) error {
	c.calls = append(c.calls, fmt.Sprintf("buy %s %v", item, qty))
	if !c.sellsKits {
		return errors.New("item_not_available")
	}
	c.state.Ship.Cargo = []game.CargoItem{{ItemID: item, Quantity: c.kits() + qty}}
	return nil
}

// explorer-8, 2026-11-14: docked at Winterhold (a player station with no
// repair yard) at 200/480, its repair failed with no_repair_kits. It buys
// the kits the damage needs on that market and repairs.
func TestRefuelDockedBuysRepairKitsWhenStationHasNoRepair(t *testing.T) {
	c := &kitFake{safetyFake: *newSafety("winterhold", 200, 480), sellsKits: true}
	c.state.Doc = true
	if err := exploreRefuelDocked(context.Background(), ExploreDeps{Client: c}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(c.calls, "buy repair_kit 6") || c.state.Ship.Hull != 480 {
		t.Fatalf("calls %v hull %v, want 6 kits bought and hull 480", c.calls, c.state.Ship.Hull)
	}
}

// With no kits on sale either, it says so and carries on.
func TestRefuelDockedNoKitsForSale(t *testing.T) {
	c := &kitFake{safetyFake: *newSafety("winterhold", 200, 480)}
	c.state.Doc = true
	out := &bytes.Buffer{}
	if err := exploreRefuelDocked(context.Background(), ExploreDeps{Client: c}, out); err != nil {
		t.Fatal(err)
	}
	if c.state.Ship.Hull != 200 || !strings.Contains(out.String(), "repair kits") {
		t.Fatalf("hull %v out %q, want unrepaired and a repair-kit note", c.state.Ship.Hull, out.String())
	}
}

// Low hull with no station in reach but spare kits aboard: repair from
// the kits in space before launching.
func TestSweepLowHullNoStationUsesSpareKits(t *testing.T) {
	kb := knowledge.NewMemoryKB()
	placeSystem(t, kb, "far_shelf", -5000, 5600, 1, 1)
	c := &kitFake{safetyFake: *newSafety("far_shelf", 150, 480)}
	c.state.Ship.Cargo = []game.CargoItem{{ItemID: repairKitItem, Quantity: 4}}
	s := sweep.NewState(7500, 148, 45)
	s.Phase, s.LaunchCostMeasured = sweep.PhaseLap, true
	d, _, _, _ := newSafetyDispatch(t, &c.safetyFake, kb, s)
	d.Client = c
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := slices.Index(c.calls, "repair")
	j := slices.IndexFunc(c.calls, func(x string) bool { return strings.HasPrefix(x, "jump ") })
	if r < 0 || j < 0 || r > j || c.state.Ship.Hull != 350 {
		t.Fatalf("calls %v hull %v, want a kit repair to 350 before the launch", c.calls, c.state.Ship.Hull)
	}
}
