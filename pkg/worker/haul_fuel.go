package worker

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// HaulMaxHaulJumps is the hard backstop on the haul leg (buy->sell) jump count.
// Fuel cost already penalizes long hauls economically; this caps the tail for when
// price/graph data is thin (median ~ 0). The approach leg keeps DefaultHaulMaxJumps.
const HaulMaxHaulJumps = 20

// haulFreeFuelStations are stations where the databot faction refuels for free (its
// ally pump); fuel priced at one of these costs 0. A future refinement can data-drive
// this from a captured ally_fuel signal.
var haulFreeFuelStations = map[string]bool{
	"grand_exchange_station": true,
}

// FuelPriceSource supplies captured station fuel prices for net-of-fuel haul
// economics. Satisfied by *market.Collector. Optional on HaulDeps: a nil source
// makes every fuel cost 0, so ranking/gating fall back to gross-only behavior.
type FuelPriceSource interface {
	GetStationFuelPrice(ctx context.Context, stationID string) (allIn int, capturedAt time.Time, ok bool, err error)
	MedianStationFuelAllIn(ctx context.Context) (median int, ok bool, err error)
}

// haulFuel is the per-pass fuel model: the ship's fuel-per-jump rate, a
// station->price resolver, and the jump graph for leg distances. Built once per
// haul pass. A zero perJump (no probe) makes every cost 0 (gross-only fallback).
type haulFuel struct {
	perJump  int
	priceOf  func(stationID string) float64
	graph    navigation.JumpGraph
	nameToID map[string]string //nolint:unused // wired by a later ranking/gating task
}

// legCost is the fuel credit cost of traveling `jumps` jumps, refueling at
// pickupStation's price. Zero when the rate or jump count is non-positive.
func (hf haulFuel) legCost(jumps int, pickupStation string) float64 {
	if hf.perJump <= 0 || jumps <= 0 {
		return 0
	}
	return float64(jumps*hf.perJump) * hf.priceOf(pickupStation)
}

// haulJumpsBetween returns the jump count between two system ids. ok is false when
// either id is empty or the target is unreachable.
func (hf haulFuel) haulJumpsBetween(fromSys, toSys string) (int, bool) {
	if fromSys == "" || toSys == "" {
		return 0, false
	}
	d := navigation.BFSJumps(hf.graph, fromSys, []string{toSys})
	j, ok := d[toSys]
	if !ok || j >= navigation.RouteInf {
		return 0, false
	}
	return j, true
}

// buildPriceOf returns a station->creditsPerUnit resolver: 0 for free-pump stations,
// the captured all-in when present, else the galaxy median (probed once here). A nil
// source yields a resolver that always returns 0 (gross-only fallback).
func buildPriceOf(ctx context.Context, src FuelPriceSource) func(string) float64 {
	if src == nil {
		return func(string) float64 { return 0 }
	}
	median, medianOK, _ := src.MedianStationFuelAllIn(ctx)
	return func(stationID string) float64 {
		if haulFreeFuelStations[stationID] {
			return 0
		}
		if allIn, _, ok, err := src.GetStationFuelPrice(ctx, stationID); err == nil && ok {
			return float64(allIn)
		}
		if medianOK {
			return float64(median)
		}
		return 0
	}
}

// haulFuelPerJump probes the ship's fuel-per-jump (ship-constant). It prefers the
// server value from a single find_route to probeTarget (cached under "_last", read by
// parseFuelEstimates); on failure it falls back to the ship-class formula
// ceil(scale^1.5 * base_speed * 10 * 0.10). Returns 0 when neither is available (fuel
// cost then degrades to 0 -> gross-only).
func haulFuelPerJump(ctx context.Context, client game.GameClient, probeTarget string) int {
	if probeTarget != "" {
		if _, err := client.FindRoute(ctx, probeTarget); err == nil {
			if fpj, _, _ := parseFuelEstimates(client); fpj > 0 {
				return fpj
			}
		}
	}
	raw := client.GetRawJSON("ship")
	if len(raw) == 0 {
		return 0
	}
	var shipResp struct {
		Class *struct {
			Scale     int `json:"scale"`
			BaseSpeed int `json:"base_speed"`
		} `json:"class"`
	}
	if err := json.Unmarshal(raw, &shipResp); err != nil || shipResp.Class == nil {
		return 0
	}
	scale, spd := float64(shipResp.Class.Scale), float64(shipResp.Class.BaseSpeed)
	if scale <= 0 || spd <= 0 {
		return 0
	}
	return max(1, int(math.Ceil(math.Pow(scale, 1.5)*spd*10.0*0.10)))
}
