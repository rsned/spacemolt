package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RecordStats appends one agent_stats row, but ONLY when a counter differs from
// the agent's most recent row. Returns whether a row was written.
//
// Append-on-change rather than upsert-in-place, and deliberately not the
// replaceSet pattern the rest of this ledger uses. Every other asset table
// answers "what does this agent have right now", which is why a destroyed hull
// leaves no trace in agent_hulls and why a ship loss was undetectable from
// stored data. These counters only ratchet upward, so keeping the changes is
// nearly free and is the entire value: the gap between two rows brackets when a
// death happened to within one capture interval.
func (s *Store) RecordStats(ctx context.Context, playerID string, st Stats, now time.Time) (bool, error) {
	if s == nil || s.db == nil || playerID == "" {
		return false, nil
	}

	prev, err := s.LatestStats(ctx, playerID)
	switch {
	case err != nil:
		return false, err
	case prev != nil && prev.sameCounters(st):
		return false, nil
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_stats (player_id, captured_at, ships_lost, ships_destroyed,
			deaths_by_pirate, deaths_by_player, deaths_by_self_destruct,
			deaths_by_wildlife, insurance_claims_made, insurance_payouts_received)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(player_id, captured_at) DO NOTHING`,
		playerID, rfc3339(now), st.ShipsLost, st.ShipsDestroyed,
		st.DeathsByPirate, st.DeathsByPlayer, st.DeathsBySelfDestruct,
		st.DeathsByWildlife, st.InsuranceClaimsMade, st.InsurancePayoutsReceived,
	); err != nil {
		return false, fmt.Errorf("assets: record stats %s: %w", playerID, err)
	}

	return true, nil
}

// LatestStats returns the agent's most recent stats row, or nil when it has
// never been captured. Nil is meaningful and distinct from a zeroed row: it
// means "never observed", not "has never died".
func (s *Store) LatestStats(ctx context.Context, playerID string) (*Stats, error) {
	if s == nil || s.db == nil || playerID == "" {
		return nil, nil
	}
	var st Stats
	err := s.db.QueryRowContext(ctx, `
		SELECT captured_at, ships_lost, ships_destroyed, deaths_by_pirate,
		       deaths_by_player, deaths_by_self_destruct, deaths_by_wildlife,
		       insurance_claims_made, insurance_payouts_received
		FROM agent_stats WHERE player_id = ?
		ORDER BY captured_at DESC LIMIT 1`, playerID).Scan(
		&st.CapturedAt, &st.ShipsLost, &st.ShipsDestroyed, &st.DeathsByPirate,
		&st.DeathsByPlayer, &st.DeathsBySelfDestruct, &st.DeathsByWildlife,
		&st.InsuranceClaimsMade, &st.InsurancePayoutsReceived)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("assets: latest stats %s: %w", playerID, err)
	}

	return &st, nil
}

// StatsHistory returns an agent's stats rows newest-first. Each row is a change,
// so consecutive rows bracket when a counter moved.
func (s *Store) StatsHistory(ctx context.Context, playerID string, limit int) ([]Stats, error) {
	if s == nil || s.db == nil || playerID == "" {
		return nil, nil
	}
	if limit < 1 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT captured_at, ships_lost, ships_destroyed, deaths_by_pirate,
		       deaths_by_player, deaths_by_self_destruct, deaths_by_wildlife,
		       insurance_claims_made, insurance_payouts_received
		FROM agent_stats WHERE player_id = ?
		ORDER BY captured_at DESC LIMIT ?`, playerID, limit)
	if err != nil {
		return nil, fmt.Errorf("assets: stats history %s: %w", playerID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Stats
	for rows.Next() {
		var st Stats
		if err := rows.Scan(&st.CapturedAt, &st.ShipsLost, &st.ShipsDestroyed,
			&st.DeathsByPirate, &st.DeathsByPlayer, &st.DeathsBySelfDestruct,
			&st.DeathsByWildlife, &st.InsuranceClaimsMade,
			&st.InsurancePayoutsReceived); err != nil {
			return nil, fmt.Errorf("assets: scan stats history: %w", err)
		}
		out = append(out, st)
	}

	return out, rows.Err()
}

// FleetDeaths returns the latest stats row for every captured agent, keyed by
// agent id where one is known, ordered by total deaths descending. This is the
// fleet-wide answer to how often each agent has died.
func (s *Store) FleetDeaths(ctx context.Context) ([]AgentStats, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH latest AS (
			SELECT player_id, MAX(captured_at) AS captured_at
			FROM agent_stats GROUP BY player_id
		)
		SELECT COALESCE(ag.agent_id, ''), COALESCE(ag.username, ''), st.player_id,
		       st.captured_at, st.ships_lost, st.ships_destroyed,
		       st.deaths_by_pirate, st.deaths_by_player, st.deaths_by_self_destruct,
		       st.deaths_by_wildlife, st.insurance_claims_made,
		       st.insurance_payouts_received
		FROM agent_stats st
		JOIN latest l ON l.player_id = st.player_id AND l.captured_at = st.captured_at
		LEFT JOIN agents ag ON ag.player_id = st.player_id
		ORDER BY (st.deaths_by_pirate + st.deaths_by_player +
		          st.deaths_by_self_destruct + st.deaths_by_wildlife) DESC,
		         st.ships_lost DESC, ag.agent_id`)
	if err != nil {
		return nil, fmt.Errorf("assets: fleet deaths: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []AgentStats
	for rows.Next() {
		var a AgentStats
		if err := rows.Scan(&a.AgentID, &a.Username, &a.PlayerID, &a.Stats.CapturedAt,
			&a.Stats.ShipsLost, &a.Stats.ShipsDestroyed, &a.Stats.DeathsByPirate,
			&a.Stats.DeathsByPlayer, &a.Stats.DeathsBySelfDestruct,
			&a.Stats.DeathsByWildlife, &a.Stats.InsuranceClaimsMade,
			&a.Stats.InsurancePayoutsReceived); err != nil {
			return nil, fmt.Errorf("assets: scan fleet deaths: %w", err)
		}
		out = append(out, a)
	}

	return out, rows.Err()
}
