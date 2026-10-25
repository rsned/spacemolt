package linedash

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // database/sql driver for assets.db
)

// Action-log event types the dashboard reads.
const (
	EventCrafted      = "crafting.completed"
	EventExchangeFill = "trading.exchange_fill"
	EventGiftSent     = "trading.gift_sent"
	EventGiftReceived = "trading.gift_received"
	EventWithdraw     = "faction.withdraw"
	EventDeposit      = "storage.deposit_items"
)

// Event is one action-log row for a line agent.
type Event struct {
	AgentID string // from agents.agent_id via player_id, never from payload names
	Type    string // event_type
	At      time.Time
	Item    string
	Units   int    // payload quantity; runs*unitsPerRun is applied by the evaluator
	Runs    int    // crafting.completed only
	Recipe  string // crafting.completed only
	Station string // payload base_id when present
	Role    string // exchange_fill only
	Price   int    // exchange_fill only
	// Sender is set for gift_received only: the payload sender resolved to an
	// agent_id through agents.username, then agents.agent_id. The raw payload
	// value is kept when neither matches.
	Sender string
}

// Events reads the action log from assets.db, read-only.
type Events struct{ db *sql.DB }

// OpenEvents opens assets.db read-only. A missing file is an error.
func OpenEvents(_ context.Context, assetsPath string) (*Events, error) {
	if _, err := os.Stat(assetsPath); err != nil {
		return nil, fmt.Errorf("stat assets db: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+assetsPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open assets db: %w", err)
	}

	return &Events{db: db}, nil
}

// Close closes the database handle.
func (e *Events) Close() error { return e.db.Close() }

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// senderResolver maps a gift sender string to an agent_id.
type senderResolver struct {
	byUsername map[string]string
}

func (e *Events) loadSenders(ctx context.Context) (*senderResolver, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT agent_id, username FROM agents WHERE agent_id <> '' AND username <> '' ORDER BY last_seen`)
	if err != nil {
		return nil, fmt.Errorf("agents: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	r := &senderResolver{byUsername: map[string]string{}}
	for rows.Next() {
		var agent, user string
		if err := rows.Scan(&agent, &user); err != nil {
			return nil, fmt.Errorf("scan agents: %w", err)
		}
		r.byUsername[user] = agent // later last_seen wins
	}

	return r, rows.Err()
}

func (r *senderResolver) resolve(sender string) string {
	if id, ok := r.byUsername[sender]; ok {
		return id
	}

	// A bare agent id (miningbots, whose username equals their id) or an
	// unknown name stays as the raw string.
	return sender
}

// Window returns the six event types above for the given agents with
// created_at >= since, ordered created_at then event_id ascending. Credit gifts
// (no item_id) are skipped.
func (e *Events) Window(ctx context.Context, agents []string, since time.Time) ([]Event, error) {
	if len(agents) == 0 {
		return nil, nil
	}
	senders, err := e.loadSenders(ctx)
	if err != nil {
		return nil, err
	}
	types := []string{EventCrafted, EventExchangeFill, EventGiftSent, EventGiftReceived, EventWithdraw, EventDeposit}
	args := make([]any, 0, len(types)+len(agents)+1)
	for _, t := range types {
		args = append(args, t)
	}
	for _, a := range agents {
		args = append(args, a)
	}
	args = append(args, since.UTC().Format(time.RFC3339))
	query := `SELECT a.agent_id, ev.event_type, ev.created_at, ev.data_json
FROM action_log_events ev JOIN agents a ON a.player_id = ev.player_id
WHERE ev.event_type IN (` + placeholders(len(types)) + `)
  AND a.agent_id IN (` + placeholders(len(agents)) + `)
  AND ev.created_at >= ?
ORDER BY ev.created_at ASC, ev.event_id ASC`
	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("action_log_events: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []Event
	for rows.Next() {
		var agent, typ, created, data string
		if err := rows.Scan(&agent, &typ, &created, &data); err != nil {
			return nil, fmt.Errorf("scan action_log_events: %w", err)
		}
		p := map[string]string{}
		_ = json.Unmarshal([]byte(data), &p) // tolerant: a bad payload yields an empty map
		if (typ == EventGiftSent || typ == EventGiftReceived) && p["item_id"] == "" {
			continue
		}
		ev := Event{
			AgentID: agent,
			Type:    typ,
			Item:    p["item_id"],
			Units:   atoi(p["quantity"]),
			Runs:    atoi(p["runs"]),
			Recipe:  p["recipe_id"],
			Station: p["base_id"],
			Role:    p["role"],
			Price:   atoi(p["price"]),
		}
		if ts, perr := time.Parse(time.RFC3339Nano, created); perr == nil {
			ev.At = ts
		}
		if typ == EventGiftReceived {
			ev.Sender = senders.resolve(p["sender"])
		}
		out = append(out, ev)
	}

	return out, rows.Err()
}

// atoi parses an integer string, returning 0 when missing or malformed.
func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}

	return n
}

// CursorAges returns when each agent's action log was last walked
// (action_log_cursor.captured_at). Every requested agent is present in the
// map; the time is zero when the agent has no cursor row or an unknown id.
func (e *Events) CursorAges(ctx context.Context, agents []string) (map[string]time.Time, error) {
	out := make(map[string]time.Time, len(agents))
	if len(agents) == 0 {
		return out, nil
	}
	args := make([]any, len(agents))
	for i, a := range agents {
		out[a] = time.Time{}
		args[i] = a
	}
	rows, err := e.db.QueryContext(ctx,
		`SELECT a.agent_id, c.captured_at FROM action_log_cursor c JOIN agents a ON a.player_id = c.player_id
WHERE a.agent_id IN (`+placeholders(len(agents))+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("action_log_cursor: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var agent, captured string
		if err := rows.Scan(&agent, &captured); err != nil {
			return nil, fmt.Errorf("scan action_log_cursor: %w", err)
		}
		if ts, perr := time.Parse(time.RFC3339Nano, captured); perr == nil && ts.After(out[agent]) {
			out[agent] = ts
		}
	}

	return out, rows.Err()
}
