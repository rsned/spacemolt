// Package craftbrain plans the full recursive work needed to build a target
// (item, ship, or facility): what to mine, buy, haul, and craft, and where.
//
// The Engine reads every fact through a Source, so the arithmetic is testable
// without a database or a game connection. It is read-only: nothing here
// writes to the KB or talks to the server.
package craftbrain

import (
	"encoding/json"
	"time"
)

// Kind is the action a plan Node represents.
type Kind string

const (
	KindMine    Kind = "mine"
	KindBuy     Kind = "buy"
	KindHaul    Kind = "haul"
	KindCraft   Kind = "craft"
	KindBlocked Kind = "blocked"
)

// Status annotates a Node whose assumptions are weaker than they look.
type Status string

const (
	// StatusOK is a node with fresh data and a resolved site.
	StatusOK Status = "ok"
	// StatusBlocked is facility_only with no known facility and no market seller.
	StatusBlocked Status = "blocked"
	// StatusStale leaned on a holding or facility older than Options.MaxStockAge.
	StatusStale Status = "stale"
	// StatusSlow blew the MaxHandTicks budget but had no facility to escape to.
	StatusSlow Status = "slow"
)

// Holding is stock the fleet already has, attributed to whoever holds it.
// Executor B needs Holder+BaseID to tell an agent what to withdraw and where.
type Holding struct {
	Holder     string // agent_id, or "" for faction storage
	BaseID     string
	Qty        int
	CapturedAt time.Time
}

// Facility is one public production line, as sited by the planner.
//
// TicksPerRun is read per-instance and never derived: measured across the live
// catalog, crafting_time / ticks_per_run / 3^(level-1) takes at least five
// distinct values, so there is no formula.
type Facility struct {
	StationID       string
	FacilityID      string
	Level           int
	RentalFeePerRun int
	TicksPerRun     float64
	OutputPerRun    int
	BacklogTicks    float64
	LastSeenUTC     string
}

// ParseProduction fills the production fields from a captured details_json
// payload. An empty or unparseable payload leaves zero values, except
// OutputPerRun which defaults to 1 so run arithmetic never divides by zero.
func (f *Facility) ParseProduction(detailsJSON string) error {
	f.OutputPerRun = 1
	if detailsJSON == "" {
		return nil
	}
	var envelope struct {
		Production struct {
			TicksPerRun     float64 `json:"ticks_per_run"`
			OutputPerRun    int     `json:"output_per_run"`
			BacklogTicks    float64 `json:"backlog_ticks"`
			RentalFeePerRun int     `json:"rental_fee_per_run"`
		} `json:"production"`
	}
	if err := json.Unmarshal([]byte(detailsJSON), &envelope); err != nil {
		return err
	}
	p := envelope.Production
	f.TicksPerRun = p.TicksPerRun
	f.BacklogTicks = p.BacklogTicks
	if p.OutputPerRun > 0 {
		f.OutputPerRun = p.OutputPerRun
	}
	if f.RentalFeePerRun == 0 {
		f.RentalFeePerRun = p.RentalFeePerRun
	}
	return nil
}

// Node is one step in the plan DAG.
type Node struct {
	ID         string   `json:"id"`
	Kind       Kind     `json:"kind"`
	ItemID     string   `json:"item_id"`
	Qty        int      `json:"qty"`
	Runs       int      `json:"runs,omitempty"`
	RecipeID   string   `json:"recipe_id,omitempty"`
	StationID  string   `json:"station_id,omitempty"`
	FacilityID string   `json:"facility_id,omitempty"`
	FeeTotal   int      `json:"fee_total,omitempty"`
	TicksEst   float64  `json:"ticks_est,omitempty"`
	Holder     string   `json:"holder,omitempty"`
	FromBase   string   `json:"from_base,omitempty"` // haul source
	ToBase     string   `json:"to_base,omitempty"`   // haul destination
	Jumps      int      `json:"jumps,omitempty"`
	Status     Status   `json:"status"`
	Reason     string   `json:"reason,omitempty"` // why blocked/slow/stale
	DependsOn  []string `json:"depends_on,omitempty"`
}

// MineOrBuy carries both options for a raw leaf. The operator decides at
// review; the planner never chooses.
type MineOrBuy struct {
	Mineable    bool    `json:"mineable"`
	MarketPrice float64 `json:"market_price,omitempty"`
	MarketQty   float64 `json:"market_qty,omitempty"`
	BestStation string  `json:"best_station,omitempty"`
}

// Coverage reports how much of the facility catalog the plan could see, so a
// BLOCKED node reads as "unknown" rather than "impossible".
type Coverage struct {
	Stations            int `json:"stations"`
	FacilityOnlyCovered int `json:"facility_only_covered"`
	FacilityOnlyTotal   int `json:"facility_only_total"`
}

// Plan is the full step-DAG plus the honesty footers.
type Plan struct {
	Target         string               `json:"target"`
	Quantity       int                  `json:"quantity"`
	Nodes          []Node               `json:"nodes"`
	Leaves         map[string]MineOrBuy `json:"leaves,omitempty"`
	Surplus        map[string]int       `json:"surplus,omitempty"`
	Diagnostics    []string             `json:"diagnostics,omitempty"`
	TotalFee       int                  `json:"total_fee"`
	TotalTicks     float64              `json:"total_ticks"`
	TotalHaulJumps int                  `json:"total_haul_jumps"`
	Coverage       Coverage             `json:"coverage"`
}

// Options tunes the planner. Zero value is not valid; use DefaultOptions.
type Options struct {
	// MaxHandTicks is the running plan-time budget above which a craft
	// prefers a facility over hand-crafting. A tick is ~10s, so the default
	// 360 is about an hour.
	MaxHandTicks float64
	// MaxStockAge marks a holding stale (still subtracted, but flagged).
	MaxStockAge time.Duration
	// Now is injectable so tests are deterministic. Zero means time.Now().
	Now time.Time
}

// DefaultOptions returns the documented defaults.
func DefaultOptions() Options {
	return Options{MaxHandTicks: 360, MaxStockAge: 24 * time.Hour}
}
