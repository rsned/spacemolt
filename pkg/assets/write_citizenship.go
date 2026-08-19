package assets

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ReplaceCitizenships swaps in the memberships an agent currently holds.
//
// Absent rows are deleted: a renounced citizenship must stop appearing, or the
// table would keep reporting a tax liability the agent no longer has. An
// exclusive grant renounces the others server-side without telling us which, so
// replace-wholesale is the only shape that stays correct.
func (s *Store) ReplaceCitizenships(ctx context.Context, playerID string, held []CitizenshipGrant, now time.Time) error {
	if playerID == "" {
		return nil
	}
	stamp := rfc3339(now)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("assets: begin citizenships %s: %w", playerID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM agent_citizenship WHERE player_id = ?`, playerID); err != nil {
		return fmt.Errorf("assets: clear citizenships %s: %w", playerID, err)
	}
	for _, g := range held {
		if g.EmpireID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO agent_citizenship (player_id, empire_id, granted_at, granted_by, captured_at)
			VALUES (?,?,?,?,?)
			ON CONFLICT(player_id, empire_id) DO UPDATE SET
				granted_at  = excluded.granted_at,
				granted_by  = excluded.granted_by,
				captured_at = excluded.captured_at`,
			playerID, g.EmpireID, g.GrantedAt, g.GrantedBy, stamp); err != nil {
			return fmt.Errorf("assets: insert citizenship %s/%s: %w", playerID, g.EmpireID, err)
		}
	}

	return tx.Commit()
}

// UpsertCitizenshipPetitions records applications and their outcomes.
//
// Unlike the citizenships this is NOT a replace: a decided petition drops out of
// both the pending list and, eventually, the recent-decisions window, and losing
// the row would destroy the only record of how long a review actually took.
// first_seen is set once and never overwritten so latency stays measurable even
// if we miss the moment of decision.
func (s *Store) UpsertCitizenshipPetitions(ctx context.Context, playerID string, petitions []CitizenshipPetition, now time.Time) error {
	if playerID == "" {
		return nil
	}
	stamp := rfc3339(now)
	for _, p := range petitions {
		if p.ID == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO agent_citizenship_petitions (petition_id, player_id, empire_id,
				status, decision, fee_paid, reputation, credits,
				created_at, decided_at, decided_by, first_seen, captured_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(petition_id) DO UPDATE SET
				status      = excluded.status,
				decision    = excluded.decision,
				fee_paid    = excluded.fee_paid,
				reputation  = excluded.reputation,
				credits     = excluded.credits,
				decided_at  = excluded.decided_at,
				decided_by  = excluded.decided_by,
				captured_at = excluded.captured_at`,
			p.ID, playerID, p.EmpireID, p.Status, p.Decision, p.FeePaid,
			p.Reputation, p.Credits, p.CreatedAt, p.DecidedAt, p.DecidedBy,
			stamp, stamp); err != nil {
			return fmt.Errorf("assets: upsert petition %s: %w", p.ID, err)
		}
	}

	return nil
}

// ReplaceCitizenshipPolicies swaps in the per-empire policy as reported to this
// agent. Replace rather than accumulate: the gates are current policy, and a
// stale row would be read as a live constraint.
func (s *Store) ReplaceCitizenshipPolicies(ctx context.Context, playerID string, policies []CitizenshipPolicy, now time.Time) error {
	if playerID == "" {
		return nil
	}
	stamp := rfc3339(now)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("assets: begin citizenship policy %s: %w", playerID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM agent_citizenship_policy WHERE player_id = ?`, playerID); err != nil {
		return fmt.Errorf("assets: clear citizenship policy %s: %w", playerID, err)
	}
	for _, c := range policies {
		if c.EmpireID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO agent_citizenship_policy (player_id, empire_id, empire_name,
				is_citizen, has_pending, open, exclusive, auto_approve,
				fee, min_balance, min_reputation, your_reputation,
				eligible, ineligible_reason, captured_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			playerID, c.EmpireID, c.EmpireName, boolToInt(c.IsCitizen),
			boolToInt(c.HasPending), boolToInt(c.Open), boolToInt(c.Exclusive),
			boolToInt(c.AutoApprove), c.Fee, c.MinBalance, c.MinReputation,
			c.YourReputation, boolToInt(c.Eligible), c.IneligibleReason, stamp); err != nil {
			return fmt.Errorf("assets: insert citizenship policy %s/%s: %w", playerID, c.EmpireID, err)
		}
	}

	return tx.Commit()
}

// PendingPetition is one unresolved application, with how long it has been
// waiting. This is the operator view: a manual-review queue has no SLA, so the
// only way to tell "under review" from "unattended" is the age.
type PendingPetition struct {
	PetitionID string
	AgentID    string
	EmpireID   string
	Status     string
	CreatedAt  string
	FirstSeen  string
}

// PendingPetitions lists applications that have not been decided.
func (s *Store) PendingPetitions(ctx context.Context) ([]PendingPetition, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.petition_id, COALESCE(a.agent_id, ''), p.empire_id,
		       p.status, p.created_at, p.first_seen
		FROM agent_citizenship_petitions p
		LEFT JOIN agents a ON a.player_id = p.player_id
		WHERE p.decided_at = '' AND p.status = 'pending'
		ORDER BY p.created_at`)
	if err != nil {
		return nil, fmt.Errorf("assets: pending petitions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []PendingPetition
	for rows.Next() {
		var p PendingPetition
		if err := rows.Scan(&p.PetitionID, &p.AgentID, &p.EmpireID,
			&p.Status, &p.CreatedAt, &p.FirstSeen); err != nil {
			return nil, fmt.Errorf("assets: scan pending petition: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("assets: pending petitions rows: %w", err)
	}

	return out, nil
}
