package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ActionLogCursor records how far an agent's since_id walk has advanced.
type ActionLogCursor struct {
	// NextSinceID is what to pass as since_id on the next poll. Zero means the
	// walk has not started, which the server reads as "newest-first paging" —
	// so a fresh walk must send 1, not 0. See CaptureActionLog.
	NextSinceID int64
	// EventsStored counts rows this walk has inserted, for observability. It is
	// not decremented by pruning, so it stays a lifetime ingest count rather
	// than a table size.
	EventsStored int64
	// CaughtUp reports that the last poll returned fewer entries than it asked
	// for, i.e. the walk has reached the present. Until then the agent is still
	// backfilling history and should be polled as often as the budget allows.
	CaughtUp   bool
	CapturedAt string
}

// InsertActionLogEvents appends events for one agent, ignoring any event_id
// already stored. Returns the number of rows actually inserted.
//
// INSERT OR IGNORE rather than upsert: an action-log entry is immutable once
// written by the server, so a re-fetched id needs no update, and ignoring makes
// an overlapping re-walk (after a cursor reset, or a --full rescan) idempotent.
func (s *Store) InsertActionLogEvents(ctx context.Context, playerID string, evs []ActionLogEvent) (int, error) {
	if s == nil || s.db == nil || playerID == "" || len(evs) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("assets: begin action_log insert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO action_log_events (player_id, event_id, event_type, category, created_at, data_json)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(player_id, event_id) DO NOTHING`)
	if err != nil {
		return 0, fmt.Errorf("assets: prepare action_log insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	inserted := 0
	for _, e := range evs {
		data, err := MarshalActionLogData(e.Data)
		if err != nil {
			return 0, err
		}
		res, err := stmt.ExecContext(ctx, playerID, e.EventID, e.EventType, e.Category, e.CreatedAt, data)
		if err != nil {
			return 0, fmt.Errorf("assets: insert action_log event %d: %w", e.EventID, err)
		}
		if n, err := res.RowsAffected(); err == nil {
			inserted += int(n)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("assets: commit action_log insert: %w", err)
	}

	return inserted, nil
}

// LoadActionLogCursor returns an agent's walk position. ok is false when the
// agent has never been polled, which is what starts a full-history backfill.
func (s *Store) LoadActionLogCursor(ctx context.Context, playerID string) (ActionLogCursor, bool, error) {
	if s == nil || s.db == nil || playerID == "" {
		return ActionLogCursor{}, false, nil
	}
	var (
		c        ActionLogCursor
		caughtUp int
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT next_since_id, events_stored, caught_up, captured_at
		FROM action_log_cursor WHERE player_id = ?`, playerID).Scan(
		&c.NextSinceID, &c.EventsStored, &caughtUp, &c.CapturedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ActionLogCursor{}, false, nil
	}
	if err != nil {
		return ActionLogCursor{}, false, fmt.Errorf("assets: load action_log cursor %s: %w", playerID, err)
	}
	c.CaughtUp = caughtUp != 0

	return c, true, nil
}

// SaveActionLogCursor writes an agent's walk position.
func (s *Store) SaveActionLogCursor(ctx context.Context, playerID string, c ActionLogCursor, now time.Time) error {
	if s == nil || s.db == nil || playerID == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO action_log_cursor (player_id, next_since_id, events_stored, caught_up, captured_at)
		VALUES (?,?,?,?,?)
		ON CONFLICT(player_id) DO UPDATE SET
			next_since_id = excluded.next_since_id,
			events_stored = excluded.events_stored,
			caught_up     = excluded.caught_up,
			captured_at   = excluded.captured_at`,
		playerID, c.NextSinceID, c.EventsStored, boolToInt(c.CaughtUp), rfc3339(now),
	); err != nil {
		return fmt.Errorf("assets: save action_log cursor %s: %w", playerID, err)
	}

	return nil
}

// ActionLogShortTTL is how long the bulk event types are kept.
//
// Three days rather than the server's ~85 because these types answer "what is
// happening now", not "what happened before the loss": a jump route only matters
// while reconstructing a recent death, and after that it is the single largest
// thing in the table.
const ActionLogShortTTL = 72 * time.Hour

// actionLogShortLived are the event types pruned to ActionLogShortTTL.
//
// Chosen from the measured distribution of craftsman-1's 45,016 entries
// (2026-08-17), not guessed: trading.buy_order_created alone was 19.1% of them,
// navigation.jumped 5.2%, session.login 0.7% and ship.refuel 0.5%.
//
// Deliberately NOT here: trading.exchange_fill (32.6%). It is the largest single
// type after rent, and it is also the only record of what an agent was carrying
// and at what price — the cargo manifest behind a ship loss is reconstructed
// from exactly these rows, so it is kept in full.
var actionLogShortLived = []string{
	"navigation.jumped",
	"ship.refuel",
	"session.login",
	"trading.buy_order_created",
}

// actionLogDownsampled are event types thinned to one row per calendar day once
// past ActionLogShortTTL rather than deleted.
//
// other.rent_paid is 35.3% of craftsman-1's log — one row per facility per rent
// cycle, forever. The daily total is the only part anyone would query, so a
// day's worth collapses to its newest row and the series survives.
var actionLogDownsampled = []string{
	"other.rent_paid",
}

// PruneActionLog enforces retention for one agent and returns rows removed.
//
// Called inline on every capture pass rather than by a separate daemon. The
// market.db lesson: an unsupervised pruner that dies takes months to notice, and
// that database reached 62GB before anyone did.
func (s *Store) PruneActionLog(ctx context.Context, playerID string, now time.Time) (int64, error) {
	if s == nil || s.db == nil || playerID == "" {
		return 0, nil
	}
	cutoff := rfc3339(now.Add(-ActionLogShortTTL))
	var removed int64

	// created_at <> '' guards the comparison: an entry that arrived without a
	// timestamp would sort below every cutoff and be deleted on the first pass.
	res, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM action_log_events
		WHERE player_id = ? AND created_at <> '' AND created_at < ?
		  AND event_type IN (%s)`, placeholders(len(actionLogShortLived))),
		append([]any{playerID, cutoff}, toAny(actionLogShortLived)...)...)
	if err != nil {
		return removed, fmt.Errorf("assets: prune action_log %s: %w", playerID, err)
	}
	if n, err := res.RowsAffected(); err == nil {
		removed += n
	}

	for _, et := range actionLogDownsampled {
		res, err := s.db.ExecContext(ctx, `
			DELETE FROM action_log_events
			WHERE player_id = ? AND event_type = ?
			  AND created_at <> '' AND created_at < ?
			  AND event_id NOT IN (
				SELECT MAX(event_id) FROM action_log_events
				WHERE player_id = ? AND event_type = ? AND created_at <> ''
				GROUP BY substr(created_at, 1, 10)
			  )`, playerID, et, cutoff, playerID, et)
		if err != nil {
			return removed, fmt.Errorf("assets: downsample action_log %s/%s: %w", playerID, et, err)
		}
		if n, err := res.RowsAffected(); err == nil {
			removed += n
		}
	}

	return removed, nil
}

// CountActionLogEvents returns how many rows are stored for an agent.
func (s *Store) CountActionLogEvents(ctx context.Context, playerID string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	var n int64
	q := "SELECT COUNT(*) FROM action_log_events"
	args := []any{}
	if playerID != "" {
		q += " WHERE player_id = ?"
		args = append(args, playerID)
	}
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("assets: count action_log events: %w", err)
	}

	return n, nil
}

// ActionLogEventsByType returns an agent's stored events of the given types,
// newest first. Passing no types returns every type.
//
// This is the death-forensics read: combat.ship_destroyed for the loss itself,
// then the trading.* rows around its timestamp for what went down with it.
func (s *Store) ActionLogEventsByType(ctx context.Context, playerID string, types []string, limit int) ([]ActionLogEvent, error) {
	if s == nil || s.db == nil || playerID == "" {
		return nil, nil
	}
	if limit < 1 {
		limit = 100
	}
	q := `SELECT event_id, event_type, category, created_at, data_json
	      FROM action_log_events WHERE player_id = ?`
	args := []any{playerID}
	if len(types) > 0 {
		q += fmt.Sprintf(" AND event_type IN (%s)", placeholders(len(types)))
		args = append(args, toAny(types)...)
	}
	q += " ORDER BY event_id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("assets: read action_log events %s: %w", playerID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []ActionLogEvent
	for rows.Next() {
		var (
			e   ActionLogEvent
			raw string
		)
		if err := rows.Scan(&e.EventID, &e.EventType, &e.Category, &e.CreatedAt, &raw); err != nil {
			return nil, fmt.Errorf("assets: scan action_log event: %w", err)
		}
		e.Data = unmarshalActionLogData(raw)
		out = append(out, e)
	}

	return out, rows.Err()
}

// placeholders renders "?,?,?" for an IN clause of n values.
func placeholders(n int) string {
	if n < 1 {
		return "NULL"
	}

	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// toAny widens a string slice for variadic query args.
func toAny(ss []string) []any {
	out := make([]any, 0, len(ss))
	for _, s := range ss {
		out = append(out, s)
	}

	return out
}
