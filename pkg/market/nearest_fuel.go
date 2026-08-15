package market

import (
	"context"
	"sort"
	"time"

	"github.com/rsned/spacemolt/pkg/navigation"
)

// FuelStop is one candidate re-tanking station, ranked by NearestFuel.
type FuelStop struct {
	StationID     string
	SystemID      string
	Jumps         int
	AllIn         int // per-unit all-in fuel price at the desk
	DeskReserve   int // -1 = never measured
	BunkerReserve int // allied faction bunker units; -1 = none reported
	FactionID     string
	// KnownWet: a reserve reading fresh within the caller's window shows
	// enough fuel (desk, or an allowed faction's bunker) to cover need.
	// False = the station merely sells fuel and has no fresh contrary
	// reading — worth trying only after every KnownWet stop.
	KnownWet bool
}

// NearestFuel ranks stations that can (or might) sell/supply at least `need`
// units of fuel, nearest first. Stations with a FRESH reading showing less
// than need available are excluded outright — a measured-dry desk is not a
// candidate, which is the whole point of measuring. Stations in
// excludeSystems (e.g. pirate strongholds the agent cannot dock at) are
// skipped. allowedFactions names factions whose fuel bunkers count as supply
// for this agent (own faction + allies).
//
// The station→system join uses market.db's own stations table, which is
// written by the same capture path and keyed the same way (POI ids), so the
// dual-named base-id/poi-id alias trap does not apply here.
func (c *Collector) NearestFuel(ctx context.Context, fromSystemID string, need int,
	graph navigation.JumpGraph, allowedFactions map[string]bool, excludeSystems map[string]bool,
	freshWithin time.Duration, now time.Time) ([]FuelStop, error) {
	if c == nil {
		return nil, nil
	}
	rows, err := c.db.QueryContext(ctx, `
		SELECT f.station_id, s.system_id, f.fuel_price_all_in,
		       f.fuel_reserve, f.faction_fuel_reserve, f.faction_id, f.reserve_observed_at
		  FROM station_fuel_prices f
		  JOIN stations s ON s.station_id = f.station_id
		 WHERE f.fuel_price_all_in > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var stops []FuelStop
	for rows.Next() {
		var (
			st         FuelStop
			observedAt string
		)
		if err := rows.Scan(&st.StationID, &st.SystemID, &st.AllIn,
			&st.DeskReserve, &st.BunkerReserve, &st.FactionID, &observedAt); err != nil {
			return nil, err
		}
		if excludeSystems[st.SystemID] {
			continue
		}
		fresh := false
		if t, terr := time.Parse(time.RFC3339, observedAt); terr == nil {
			fresh = now.Sub(t) <= freshWithin
		}
		deskWet := fresh && st.DeskReserve >= need
		bunkerWet := fresh && allowedFactions[st.FactionID] && st.BunkerReserve >= need
		if fresh && !deskWet && !bunkerWet && st.DeskReserve >= 0 {
			continue // fresh reading says not enough here — measured, not guessed
		}
		st.KnownWet = deskWet || bunkerWet
		stops = append(stops, st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	systems := make([]string, 0, len(stops))
	for _, s := range stops {
		systems = append(systems, s.SystemID)
	}
	jumps := navigation.BFSJumps(graph, fromSystemID, systems)
	kept := stops[:0]
	for _, s := range stops {
		j, ok := jumps[s.SystemID]
		if !ok || j >= navigation.RouteInf {
			continue // unreachable from here (BFSJumps marks these RouteInf)
		}
		s.Jumps = j
		kept = append(kept, s)
	}
	stops = kept

	sort.Slice(stops, func(i, j int) bool {
		a, b := stops[i], stops[j]
		if a.KnownWet != b.KnownWet {
			return a.KnownWet
		}
		if a.Jumps != b.Jumps {
			return a.Jumps < b.Jumps
		}
		if a.AllIn != b.AllIn {
			return a.AllIn < b.AllIn
		}
		return a.StationID < b.StationID
	})

	return stops, nil
}
