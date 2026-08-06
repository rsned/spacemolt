package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// BaseResolver maps a station's base-id form to its poi-id form. Station ids
// are dual-named: 15 of 43 bases differ between the two (the five empire
// capitals are genuine renames, the seven pirate strongholds carry a mechanical
// "_station" suffix, three player bases are hex pairs). Every base_id column in
// this package stores the WIRE value verbatim, so any reader joining against
// pois.id must pass through a resolver or silently under-report.
//
// A nil BaseResolver is valid and means "leave ids alone" -- degrading to
// unjoinable ids is always better than degrading to wrong ones.
type BaseResolver func(baseID string) string

// resolve applies r if non-nil.
func (r BaseResolver) resolve(baseID string) string {
	if r == nil {
		return baseID
	}

	return r(baseID)
}

// parseCapturedAt reads a captured_at column. An unparseable or empty value
// yields the zero time rather than an error: a bad timestamp must not make an
// otherwise-good row unreadable.
func parseCapturedAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}

	return t
}

// LoadProfile returns the stored profile scalars. ok=false means no row.
func (s *Store) LoadProfile(ctx context.Context, playerID string) (Profile, bool, error) {
	if s == nil || s.db == nil || playerID == "" {
		return Profile{}, false, nil
	}
	var (
		p  Profile
		at string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT player_id, username, empire, credits, home_base, docked_at_base,
		       current_system, current_poi, active_ship_id, faction_id, faction_rank,
		       experience, captured_at
		  FROM agent_profile WHERE player_id = ?`, playerID).Scan(
		&p.PlayerID, &p.Username, &p.Empire, &p.Credits, &p.HomeBase, &p.DockedAtBase,
		&p.CurrentSystem, &p.CurrentPOI, &p.ActiveShipID, &p.FactionID, &p.FactionRank,
		&p.Experience, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, false, nil
	}
	if err != nil {
		return Profile{}, false, fmt.Errorf("assets: load profile %s: %w", playerID, err)
	}
	p.CapturedAt = parseCapturedAt(at)

	return p, true, nil
}

// LoadCarrier returns the stored carrier standing and its capture time.
func (s *Store) LoadCarrier(ctx context.Context, playerID string) (Carrier, time.Time, bool, error) {
	if s == nil || s.db == nil || playerID == "" {
		return Carrier{}, time.Time{}, false, nil
	}
	var (
		c  Carrier
		at string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT tier, successful_deliveries, delivered_value, priority_deliveries,
		       returns, breaches, defaults, active_contracts, active_liability,
		       outstanding_debt, debt_blocks_acceptance, next_tier, at_maximum_tier,
		       required_successful_deliveries, remaining_successful_deliveries,
		       required_delivered_value, remaining_delivered_value,
		       active_contract_limit, active_contracts_unlimited,
		       aggregate_liability_limit, remaining_aggregate_liability,
		       single_package_liability_limit, liability_unlimited, captured_at
		  FROM agent_carrier WHERE player_id = ?`, playerID).Scan(
		&c.Tier, &c.SuccessfulDeliveries, &c.DeliveredValue, &c.PriorityDeliveries,
		&c.Returns, &c.Breaches, &c.Defaults, &c.ActiveContracts, &c.ActiveLiability,
		&c.OutstandingDebt, &c.DebtBlocksAcceptance, &c.NextTier, &c.AtMaximumTier,
		&c.RequiredSuccessfulDeliveries, &c.RemainingSuccessfulDeliveries,
		&c.RequiredDeliveredValue, &c.RemainingDeliveredValue,
		&c.ActiveContractLimit, &c.ActiveContractsUnlimited,
		&c.AggregateLiabilityLimit, &c.RemainingAggregateLiability,
		&c.SinglePackageLiabilityLimit, &c.LiabilityUnlimited, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return Carrier{}, time.Time{}, false, nil
	}
	if err != nil {
		return Carrier{}, time.Time{}, false, fmt.Errorf("assets: load carrier %s: %w", playerID, err)
	}

	return c, parseCapturedAt(at), true, nil
}

// LoadHulls returns the stored hull set, resolving location_base_id through r.
// ok=false means no rows -- which for hulls always means "never captured",
// since an agent can never own zero ships.
func (s *Store) LoadHulls(ctx context.Context, playerID string, r BaseResolver) ([]Hull, time.Time, bool, error) {
	if s == nil || s.db == nil || playerID == "" {
		return nil, time.Time{}, false, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ship_id, class_id, class_name, is_active, hull_current, hull_max, hull_raw,
		       fuel_current, fuel_max, fuel_raw, cargo_used, location, location_base_id,
		       modules, listing_id, listing_price, listing_base_id, captured_at
		  FROM agent_hulls WHERE player_id = ? ORDER BY ship_id`, playerID)
	if err != nil {
		return nil, time.Time{}, false, fmt.Errorf("assets: load hulls %s: %w", playerID, err)
	}
	defer func() { _ = rows.Close() }()

	var (
		out []Hull
		at  time.Time
	)
	for rows.Next() {
		var (
			h  Hull
			ts string
		)
		if err := rows.Scan(&h.ShipID, &h.ClassID, &h.ClassName, &h.IsActive,
			&h.HullCurrent, &h.HullMax, &h.HullRaw, &h.FuelCurrent, &h.FuelMax, &h.FuelRaw,
			&h.CargoUsed, &h.Location, &h.LocationBaseID, &h.Modules,
			&h.ListingID, &h.ListingPrice, &h.ListingBaseID, &ts); err != nil {
			return nil, time.Time{}, false, fmt.Errorf("assets: scan hull %s: %w", playerID, err)
		}
		h.LocationBaseID = r.resolve(h.LocationBaseID)
		h.ListingBaseID = r.resolve(h.ListingBaseID)
		at = parseCapturedAt(ts)
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, false, fmt.Errorf("assets: iterate hulls %s: %w", playerID, err)
	}

	return out, at, len(out) > 0, nil
}
