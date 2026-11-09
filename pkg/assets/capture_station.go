package assets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// minTourismSpan is the shortest gap between two base snapshots that yields a
// tourism rate; closer readings are mostly rounding noise on a lifetime counter.
const minTourismSpan = 30 * time.Minute

// FacilityRow is one facility from a station's `facility list`. Recipe and the
// queue fields are filled for production lines only; MaintenanceSatisfied is
// nil for facilities with no upkeep.
type FacilityRow struct {
	FacilityID, Type, Name, Category, Recipe string
	Level                                    int64
	MaintenanceSatisfied                     *bool
	MaintenanceLevel                         int64
	QueuedRuns, QueuedItems, BacklogTicks    int64
	ItemsPerHour                             float64
}

// FacilityRowsFrom decodes a raw `facility list` reply. ok is false when the
// reply is unreadable or names no base_id.
func FacilityRowsFrom(raw []byte) (baseID string, rows []FacilityRow, ok bool) {
	var r struct {
		BaseID     string `json:"base_id"`
		Facilities []struct {
			FacilityID           string `json:"facility_id"`
			Type                 string `json:"type"`
			Name                 string `json:"name"`
			Category             string `json:"category"`
			Level                int64  `json:"level"`
			MaintenanceSatisfied *bool  `json:"maintenance_satisfied"`
			MaintenanceLevel     int64  `json:"maintenance_level"`
			Production           *struct {
				Recipe       string  `json:"recipe"`
				QueuedRuns   int64   `json:"queued_runs"`
				QueuedItems  int64   `json:"queued_items"`
				BacklogTicks int64   `json:"backlog_ticks"`
				ItemsPerHour float64 `json:"items_per_hour"`
			} `json:"production"`
		} `json:"faction_facilities"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &r) != nil || r.BaseID == "" {
		return "", nil, false
	}
	rows = make([]FacilityRow, 0, len(r.Facilities))
	for _, f := range r.Facilities {
		row := FacilityRow{
			FacilityID: f.FacilityID, Type: f.Type, Name: f.Name, Category: f.Category,
			Level: f.Level, MaintenanceSatisfied: f.MaintenanceSatisfied, MaintenanceLevel: f.MaintenanceLevel,
		}
		if p := f.Production; p != nil {
			row.Recipe, row.QueuedRuns, row.QueuedItems = p.Recipe, p.QueuedRuns, p.QueuedItems
			row.BacklogTicks, row.ItemsPerHour = p.BacklogTicks, p.ItemsPerHour
		}
		rows = append(rows, row)
	}
	return r.BaseID, rows, true
}

// PassengerSnapshot is one reading of the docked station's passenger board.
type PassengerSnapshot struct {
	Waiting, LoungeOccupancy, LoungeCapacity int64
	DemandLevel                              string
	FareSurge                                float64
	MarketConditions                         string
}

// PassengerSnapshotFrom decodes a raw list_station_passengers reply. ok is
// false when the reply is unreadable or names no station.
func PassengerSnapshotFrom(raw []byte) (PassengerSnapshot, bool) {
	var r struct {
		Station string            `json:"station"`
		Count   int64             `json:"count"`
		Waiting []json.RawMessage `json:"waiting"`
		Lounge  struct {
			Occupancy int64 `json:"occupancy"`
			Capacity  int64 `json:"capacity"`
		} `json:"transit_lounge"`
		DemandLevel      string  `json:"demand_level"`
		FareSurge        float64 `json:"fare_surge"`
		MarketConditions string  `json:"market_conditions"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &r) != nil || r.Station == "" {
		return PassengerSnapshot{}, false
	}
	waiting := max(r.Count, int64(len(r.Waiting)))
	return PassengerSnapshot{
		Waiting: waiting, LoungeOccupancy: r.Lounge.Occupancy, LoungeCapacity: r.Lounge.Capacity,
		DemandLevel: r.DemandLevel, FareSurge: r.FareSurge, MarketConditions: r.MarketConditions,
	}, true
}

// InsertFacilityRows appends one facility-list pass; every row shares now.
func (s *Store) InsertFacilityRows(ctx context.Context, baseID, agentID string, rows []FacilityRow, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("assets: insert facility rows %s: %w", baseID, err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	for _, f := range rows {
		var sat any
		if f.MaintenanceSatisfied != nil {
			sat = 0
			if *f.MaintenanceSatisfied {
				sat = 1
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO facility_snapshots (base_id, agent_id, captured_at, facility_id, type, name, category,
				recipe, level, maintenance_satisfied, maintenance_level, queued_runs, queued_items,
				backlog_ticks, items_per_hour)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			baseID, agentID, rfc3339(now), f.FacilityID, f.Type, f.Name, f.Category,
			f.Recipe, f.Level, sat, f.MaintenanceLevel, f.QueuedRuns, f.QueuedItems,
			f.BacklogTicks, f.ItemsPerHour); err != nil {
			return fmt.Errorf("assets: insert facility row %s/%s: %w", baseID, f.FacilityID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("assets: insert facility rows %s: %w", baseID, err)
	}
	return nil
}

// InsertPassengerSnapshot appends one passenger-board reading.
func (s *Store) InsertPassengerSnapshot(ctx context.Context, baseID, agentID string, p PassengerSnapshot, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO station_passenger_snapshots (base_id, agent_id, captured_at, waiting, lounge_occupancy,
			lounge_capacity, demand_level, fare_surge, market_conditions)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		baseID, agentID, rfc3339(now), p.Waiting, p.LoungeOccupancy, p.LoungeCapacity,
		p.DemandLevel, p.FareSurge, p.MarketConditions)
	if err != nil {
		return fmt.Errorf("assets: insert passenger snapshot %s: %w", baseID, err)
	}
	return nil
}

// CaptureFacilities records the docked station's `facility list`. A failed or
// unparseable read leaves the history unchanged and never fails the pass.
func CaptureFacilities(ctx context.Context, client game.GameClient, st *Store, agentID string, now time.Time) error {
	if st == nil || client == nil {
		return nil
	}
	if state := client.GetState(); state == nil || !state.Doc {
		return nil
	}
	if err := client.Facility(ctx, map[string]any{"action": "list"}); err != nil {
		return nil //nolint:nilerr // a source failure must never fail the capture pass
	}
	baseID, rows, ok := FacilityRowsFrom(client.GetRawJSON("facility"))
	if !ok || len(rows) == 0 {
		return nil
	}
	return st.InsertFacilityRows(ctx, baseID, agentID, rows, now)
}

// CapturePassengers records the docked station's passenger board. baseID is the
// docked base from client state: the reply names only the station.
func CapturePassengers(ctx context.Context, client game.GameClient, st *Store, agentID, baseID string, now time.Time) error {
	if st == nil || client == nil || baseID == "" {
		return nil
	}
	if state := client.GetState(); state == nil || !state.Doc {
		return nil
	}
	if err := client.RawCommand(ctx, "list_station_passengers", nil); err != nil {
		return nil //nolint:nilerr // a source failure must never fail the capture pass
	}
	snap, ok := PassengerSnapshotFrom(client.GetRawJSON("list_station_passengers"))
	if !ok {
		return nil
	}
	return st.InsertPassengerSnapshot(ctx, baseID, agentID, snap, now)
}

// StationReading is the latest known state of one station.
type StationReading struct {
	Base           BaseSnapshot
	Facilities     []FacilityRow
	Passengers     *PassengerSnapshot
	CapturedAt     time.Time
	TourismPerHour *float64
}

// baseCols is the base_snapshots column list scanned by scanBase.
const baseCols = `base_id, condition, hull, max_hull, shield, max_shield, weapon_dps, weapon_reach, armor,
	satisfaction_pct, dining_points, leisure_points, tourism_revenue, life_support_supply, life_support_demand,
	power_supply, power_draw, battery_capacity, battery_stored, fuel_reserve, fuel_capacity, captured_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanBase(r rowScanner) (BaseSnapshot, time.Time, error) {
	var b BaseSnapshot
	var at string
	err := r.Scan(&b.BaseID, &b.Condition, &b.Hull, &b.MaxHull, &b.Shield, &b.MaxShield, &b.WeaponDPS,
		&b.WeaponReach, &b.Armor, &b.SatisfactionPct, &b.DiningPoints, &b.LeisurePoints, &b.TourismRevenue,
		&b.LifeSupportSupply, &b.LifeSupportDemand, &b.PowerSupply, &b.PowerDraw, &b.BatteryCapacity,
		&b.BatteryStored, &b.FuelReserve, &b.FuelCapacity, &at)
	return b, parseCapturedAt(at), err
}

// tourismRate is the revenue gained per hour between two snapshots, nil when
// they are under minTourismSpan apart or the lifetime counter went backwards.
func tourismRate(prev, cur BaseSnapshot, prevAt, curAt time.Time) *float64 {
	span := curAt.Sub(prevAt)
	if span < minTourismSpan || cur.TourismRevenue < prev.TourismRevenue {
		return nil
	}
	v := float64(cur.TourismRevenue-prev.TourismRevenue) / span.Hours()
	return &v
}

// LatestStation returns the newest base snapshot with the newest facility pass,
// the newest passenger row and a tourism rate. ok is false when the base has no
// snapshot.
func (s *Store) LatestStation(ctx context.Context, baseID string) (StationReading, bool, error) {
	var out StationReading
	cur, curAt, err := scanBase(s.db.QueryRowContext(ctx,
		`SELECT `+baseCols+` FROM base_snapshots WHERE base_id=? ORDER BY captured_at DESC, rowid DESC LIMIT 1`, baseID))
	if errors.Is(err, sql.ErrNoRows) {
		return out, false, nil
	}
	if err != nil {
		return out, false, fmt.Errorf("assets: latest station %s: %w", baseID, err)
	}
	out.Base, out.CapturedAt = cur, curAt

	// Newest earlier snapshot at least minTourismSpan older than the newest.
	cutoff := rfc3339(curAt.Add(-minTourismSpan))
	prev, prevAt, err := scanBase(s.db.QueryRowContext(ctx,
		`SELECT `+baseCols+` FROM base_snapshots WHERE base_id=? AND captured_at<=? ORDER BY captured_at DESC, rowid DESC LIMIT 1`,
		baseID, cutoff))
	switch {
	case err == nil:
		out.TourismPerHour = tourismRate(prev, cur, prevAt, curAt)
	case !errors.Is(err, sql.ErrNoRows):
		return out, false, fmt.Errorf("assets: latest station %s: %w", baseID, err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT facility_id, type, name, category, recipe, level, maintenance_satisfied, maintenance_level,
			queued_runs, queued_items, backlog_ticks, items_per_hour
		FROM facility_snapshots
		WHERE base_id=? AND captured_at=(SELECT max(captured_at) FROM facility_snapshots WHERE base_id=?)
		ORDER BY rowid`, baseID, baseID)
	if err != nil {
		return out, false, fmt.Errorf("assets: latest facilities %s: %w", baseID, err)
	}
	defer rows.Close() //nolint:errcheck // read-only
	for rows.Next() {
		var f FacilityRow
		var sat sql.NullInt64
		if err = rows.Scan(&f.FacilityID, &f.Type, &f.Name, &f.Category, &f.Recipe, &f.Level, &sat,
			&f.MaintenanceLevel, &f.QueuedRuns, &f.QueuedItems, &f.BacklogTicks, &f.ItemsPerHour); err != nil {
			return out, false, fmt.Errorf("assets: latest facilities %s: %w", baseID, err)
		}
		if sat.Valid {
			v := sat.Int64 != 0
			f.MaintenanceSatisfied = &v
		}
		out.Facilities = append(out.Facilities, f)
	}
	if err = rows.Err(); err != nil {
		return out, false, fmt.Errorf("assets: latest facilities %s: %w", baseID, err)
	}

	var p PassengerSnapshot
	err = s.db.QueryRowContext(ctx, `
		SELECT waiting, lounge_occupancy, lounge_capacity, demand_level, fare_surge, market_conditions
		FROM station_passenger_snapshots WHERE base_id=? ORDER BY captured_at DESC, rowid DESC LIMIT 1`, baseID).
		Scan(&p.Waiting, &p.LoungeOccupancy, &p.LoungeCapacity, &p.DemandLevel, &p.FareSurge, &p.MarketConditions)
	switch {
	case err == nil:
		out.Passengers = &p
	case !errors.Is(err, sql.ErrNoRows):
		return out, false, fmt.Errorf("assets: latest passengers %s: %w", baseID, err)
	}
	return out, true, nil
}

// HistoryPoint is one base snapshot reduced to percentages for charting.
// LifeMarginPct is (supply-demand)/demand, 0 when demand is 0.
type HistoryPoint struct {
	At                                                       time.Time
	ShieldPct, HullPct, FuelPct, PowerDrawPct, LifeMarginPct float64
	TourismPerHour                                           *float64
	BacklogTicks                                             int64
}

func pct(num, den int64) float64 {
	if den <= 0 {
		return 0
	}
	return float64(num) / float64(den) * 100
}

// StationHistory returns one point per base snapshot captured at or after
// since, oldest first. BacklogTicks sums the nearest facility pass at or before
// the snapshot; TourismPerHour compares to the preceding snapshot in range that
// is at least minTourismSpan earlier (nil for the first point).
func (s *Store) StationHistory(ctx context.Context, baseID string, since time.Time) ([]HistoryPoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+baseCols+`, (SELECT COALESCE(SUM(f.backlog_ticks), 0) FROM facility_snapshots f
			WHERE f.base_id=b.base_id AND f.captured_at=(SELECT max(captured_at) FROM facility_snapshots
				WHERE base_id=b.base_id AND captured_at<=b.captured_at))
		FROM base_snapshots b WHERE base_id=? AND captured_at>=? ORDER BY captured_at, rowid`,
		baseID, rfc3339(since))
	if err != nil {
		return nil, fmt.Errorf("assets: station history %s: %w", baseID, err)
	}
	defer rows.Close() //nolint:errcheck // read-only

	var pts []HistoryPoint
	var snaps []BaseSnapshot
	for rows.Next() {
		var b BaseSnapshot
		var at string
		var backlog int64
		if err := rows.Scan(&b.BaseID, &b.Condition, &b.Hull, &b.MaxHull, &b.Shield, &b.MaxShield, &b.WeaponDPS,
			&b.WeaponReach, &b.Armor, &b.SatisfactionPct, &b.DiningPoints, &b.LeisurePoints, &b.TourismRevenue,
			&b.LifeSupportSupply, &b.LifeSupportDemand, &b.PowerSupply, &b.PowerDraw, &b.BatteryCapacity,
			&b.BatteryStored, &b.FuelReserve, &b.FuelCapacity, &at, &backlog); err != nil {
			return nil, fmt.Errorf("assets: station history %s: %w", baseID, err)
		}
		p := HistoryPoint{
			At: parseCapturedAt(at), BacklogTicks: backlog,
			ShieldPct: pct(b.Shield, b.MaxShield), HullPct: pct(b.Hull, b.MaxHull),
			FuelPct: pct(b.FuelReserve, b.FuelCapacity), PowerDrawPct: pct(b.PowerDraw, b.PowerSupply),
		}
		if b.LifeSupportDemand > 0 {
			p.LifeMarginPct = pct(b.LifeSupportSupply-b.LifeSupportDemand, b.LifeSupportDemand)
		}
		// Latest earlier in-range snapshot far enough back to give a stable rate.
		for i := len(pts) - 1; i >= 0; i-- {
			if p.At.Sub(pts[i].At) >= minTourismSpan {
				p.TourismPerHour = tourismRate(snaps[i], b, pts[i].At, p.At)
				break
			}
		}
		pts = append(pts, p)
		snaps = append(snaps, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("assets: station history %s: %w", baseID, err)
	}
	return pts, nil
}
