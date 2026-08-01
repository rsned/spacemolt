package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// UpsertIdentity records the player_id -> (agent_id, username) mapping.
// A rename updates in place against the stable player_id; first_seen is set
// once and never overwritten.
func (s *Store) UpsertIdentity(ctx context.Context, id Identity, now time.Time) error {
	if s == nil || s.db == nil || id.PlayerID == "" {
		return nil
	}
	ts := rfc3339(now)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents (player_id, agent_id, username, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(player_id) DO UPDATE SET
			agent_id  = excluded.agent_id,
			username  = excluded.username,
			last_seen = excluded.last_seen`,
		id.PlayerID, id.AgentID, id.Username, ts, ts)
	if err != nil {
		return fmt.Errorf("assets: upsert identity %s: %w", id.PlayerID, err)
	}

	return nil
}

// LookupIdentity returns the mapping for one player id. ok is false when the
// agent has never been captured.
func (s *Store) LookupIdentity(ctx context.Context, playerID string) (Identity, bool, error) {
	if s == nil || s.db == nil {
		return Identity{}, false, nil
	}
	id := Identity{PlayerID: playerID}
	err := s.db.QueryRowContext(ctx,
		`SELECT agent_id, username FROM agents WHERE player_id = ?`, playerID).
		Scan(&id.AgentID, &id.Username)
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, false, nil
	}
	if err != nil {
		return Identity{}, false, fmt.Errorf("assets: lookup identity %s: %w", playerID, err)
	}

	return id, true, nil
}
