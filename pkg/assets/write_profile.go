package assets

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// UpsertProfile writes the scalar half of get_status.
func (s *Store) UpsertProfile(ctx context.Context, p Profile) error {
	if s == nil || s.db == nil || p.PlayerID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_profile (player_id, username, empire, credits, home_base,
			docked_at_base, current_system, current_poi, active_ship_id,
			faction_id, faction_rank, experience, captured_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(player_id) DO UPDATE SET
			username=excluded.username, empire=excluded.empire,
			credits=excluded.credits, home_base=excluded.home_base,
			docked_at_base=excluded.docked_at_base,
			current_system=excluded.current_system, current_poi=excluded.current_poi,
			active_ship_id=excluded.active_ship_id, faction_id=excluded.faction_id,
			faction_rank=excluded.faction_rank, experience=excluded.experience,
			captured_at=excluded.captured_at`,
		p.PlayerID, p.Username, p.Empire, p.Credits, p.HomeBase,
		p.DockedAtBase, p.CurrentSystem, p.CurrentPOI, p.ActiveShipID,
		p.FactionID, p.FactionRank, p.Experience, rfc3339(p.CapturedAt))
	if err != nil {
		return fmt.Errorf("assets: upsert profile %s: %w", p.PlayerID, err)
	}

	return nil
}

// replaceSet runs one whole-set replacement inside a single transaction:
// delete every row for this agent, then insert the new set. Upserting row by
// row would leave rows behind for entries the server no longer reports, and
// phantom data is exactly what would make this ledger untrustworthy.
func (s *Store) replaceSet(ctx context.Context, delSQL, playerID string, insert func(*sql.Tx) error) error {
	if s == nil || s.db == nil || playerID == "" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("assets: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, delSQL, playerID); err != nil {
		return fmt.Errorf("assets: clear rows for %s: %w", playerID, err)
	}
	if err := insert(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("assets: commit: %w", err)
	}

	return nil
}

// ReplaceSkills swaps in the agent's full skill set. Skills absent from rows
// are deleted.
func (s *Store) ReplaceSkills(ctx context.Context, playerID string, rows []SkillRow, now time.Time) error {
	ts := rfc3339(now)

	return s.replaceSet(ctx, `DELETE FROM agent_skills WHERE player_id = ?`, playerID, func(tx *sql.Tx) error {
		for _, r := range rows {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO agent_skills (player_id, skill, level, xp, captured_at) VALUES (?,?,?,?,?)`,
				playerID, r.Skill, r.Level, r.XP, ts); err != nil {
				return fmt.Errorf("assets: insert skill %s/%s: %w", playerID, r.Skill, err)
			}
		}

		return nil
	})
}

// ReplaceStandings swaps in the agent's full standings set.
func (s *Store) ReplaceStandings(ctx context.Context, playerID string, rows []StandingRow, now time.Time) error {
	ts := rfc3339(now)

	return s.replaceSet(ctx, `DELETE FROM agent_standings WHERE player_id = ?`, playerID, func(tx *sql.Tx) error {
		for _, r := range rows {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO agent_standings (player_id, faction, reputation, baseline,
					outstanding_bounty, jailed_until, captured_at) VALUES (?,?,?,?,?,?,?)`,
				playerID, r.Faction, r.Reputation, r.Baseline,
				r.OutstandingBounty, r.JailedUntil, ts); err != nil {
				return fmt.Errorf("assets: insert standing %s/%s: %w", playerID, r.Faction, err)
			}
		}

		return nil
	})
}
