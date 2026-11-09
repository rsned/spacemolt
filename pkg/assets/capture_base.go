package assets

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// BaseSnapshot is one reading of the docked station's get_base: defense,
// power, life support, fuel and tourism. Rows append, so the history gives
// rates (tourism revenue per day) and before/after views of builds.
type BaseSnapshot struct {
	BaseID            string
	Condition         string
	Hull              int64
	MaxHull           int64
	Shield            int64
	MaxShield         int64
	WeaponDPS         int64
	WeaponReach       int64
	Armor             int64
	SatisfactionPct   int64
	DiningPoints      int64
	LeisurePoints     int64
	TourismRevenue    int64
	LifeSupportSupply int64
	LifeSupportDemand int64
	PowerSupply       int64
	PowerDraw         int64
	BatteryCapacity   int64
	BatteryStored     int64
	FuelReserve       int64
	FuelCapacity      int64
}

// BaseSnapshotFrom decodes a raw get_base reply. ok is false when the reply
// is unreadable or names no base.
func BaseSnapshotFrom(raw []byte) (BaseSnapshot, bool) {
	var r struct {
		Base struct {
			ID          string `json:"id"`
			Hull        int64  `json:"hull"`
			MaxHull     int64  `json:"max_hull"`
			Shield      int64  `json:"shield"`
			MaxShield   int64  `json:"max_shield"`
			WeaponDPS   int64  `json:"weapon_dps"`
			WeaponReach int64  `json:"weapon_reach"`
			Armor       int64  `json:"armor"`
			Fuel        *int64 `json:"fuel"`
			MaxFuel     *int64 `json:"max_fuel"`
		} `json:"base"`
		Condition struct {
			Condition       string `json:"condition"`
			SatisfactionPct int64  `json:"satisfaction_pct"`
		} `json:"condition"`
		DiningPoints   int64 `json:"dining_points"`
		LeisurePoints  int64 `json:"leisure_points"`
		TourismRevenue int64 `json:"tourism_revenue"`
		LifeSupport    struct {
			Supply int64 `json:"supply"`
			Demand int64 `json:"demand"`
		} `json:"life_support"`
		Power struct {
			Supply          int64 `json:"supply"`
			CurrentDraw     int64 `json:"current_draw"`
			BatteryCapacity int64 `json:"battery_capacity"`
			BatteryStored   int64 `json:"battery_stored"`
		} `json:"power"`
		FuelReserve  int64 `json:"faction_fuel_reserve"`
		FuelCapacity int64 `json:"faction_fuel_capacity"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &r) != nil || r.Base.ID == "" {
		return BaseSnapshot{}, false
	}
	// A player station reports its tank as base.fuel / base.max_fuel; the
	// top-level faction_fuel_* pair is the fallback for other bases.
	fuel, fuelCap := r.FuelReserve, r.FuelCapacity
	if r.Base.Fuel != nil {
		fuel = *r.Base.Fuel
	}
	if r.Base.MaxFuel != nil {
		fuelCap = *r.Base.MaxFuel
	}
	return BaseSnapshot{
		BaseID: r.Base.ID, Armor: r.Base.Armor, Condition: r.Condition.Condition,
		Hull: r.Base.Hull, MaxHull: r.Base.MaxHull, Shield: r.Base.Shield, MaxShield: r.Base.MaxShield,
		WeaponDPS: r.Base.WeaponDPS, WeaponReach: r.Base.WeaponReach,
		SatisfactionPct: r.Condition.SatisfactionPct,
		DiningPoints:    r.DiningPoints, LeisurePoints: r.LeisurePoints, TourismRevenue: r.TourismRevenue,
		LifeSupportSupply: r.LifeSupport.Supply, LifeSupportDemand: r.LifeSupport.Demand,
		PowerSupply: r.Power.Supply, PowerDraw: r.Power.CurrentDraw,
		BatteryCapacity: r.Power.BatteryCapacity, BatteryStored: r.Power.BatteryStored,
		FuelReserve: fuel, FuelCapacity: fuelCap,
	}, true
}

// InsertBaseSnapshot appends one get_base reading.
func (s *Store) InsertBaseSnapshot(ctx context.Context, agentID string, b BaseSnapshot, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO base_snapshots (base_id, agent_id, condition, hull, max_hull, shield, max_shield,
			weapon_dps, weapon_reach, armor, satisfaction_pct, dining_points, leisure_points, tourism_revenue,
			life_support_supply, life_support_demand, power_supply, power_draw, battery_capacity,
			battery_stored, fuel_reserve, fuel_capacity, captured_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.BaseID, agentID, b.Condition, b.Hull, b.MaxHull, b.Shield, b.MaxShield,
		b.WeaponDPS, b.WeaponReach, b.Armor, b.SatisfactionPct, b.DiningPoints, b.LeisurePoints, b.TourismRevenue,
		b.LifeSupportSupply, b.LifeSupportDemand, b.PowerSupply, b.PowerDraw, b.BatteryCapacity,
		b.BatteryStored, b.FuelReserve, b.FuelCapacity, rfc3339(now))
	if err != nil {
		return fmt.Errorf("assets: insert base snapshot %s: %w", b.BaseID, err)
	}
	return nil
}

// CaptureBase records the docked station's get_base. A failed or unparseable
// read leaves the history unchanged and never fails the capture pass.
func CaptureBase(ctx context.Context, client game.GameClient, st *Store, agentID string, now time.Time) error {
	if st == nil || client == nil {
		return nil
	}
	if state := client.GetState(); state == nil || !state.Doc {
		return nil
	}
	if err := client.GetBase(ctx); err != nil {
		return nil //nolint:nilerr // a source failure must never fail the capture pass
	}
	snap, ok := BaseSnapshotFrom(client.GetRawJSON("base"))
	if !ok {
		return nil
	}
	return st.InsertBaseSnapshot(ctx, agentID, snap, now)
}
