package linedash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	_ "modernc.org/sqlite" // database/sql driver for assets.db
)

// TankLevel is the stored quantity of one item for one holder.
type TankLevel struct {
	Holder     Holder
	Item       string
	Units      float64
	CapturedAt time.Time // zero when the holder has no row for the item
}

// Tanks reads storage snapshots from assets.db, read-only.
type Tanks struct {
	db         *sql.DB
	factionIDs map[string]string // faction tag -> faction_id

	mu      sync.Mutex
	players map[string]string // agent_id -> player_id
}

// OpenTanks opens assets.db read-only and caches the faction tag and agent
// lookups. A missing file is an error.
func OpenTanks(ctx context.Context, assetsPath string) (*Tanks, error) {
	if _, err := os.Stat(assetsPath); err != nil {
		return nil, fmt.Errorf("stat assets db: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+assetsPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open assets db: %w", err)
	}
	t := &Tanks{db: db, factionIDs: map[string]string{}, players: map[string]string{}}
	if err := t.loadFactions(ctx); err != nil {
		_ = db.Close()

		return nil, err
	}
	if err := t.loadPlayers(ctx); err != nil {
		_ = db.Close()

		return nil, err
	}

	return t, nil
}

func (t *Tanks) loadFactions(ctx context.Context) error {
	rows, err := t.db.QueryContext(ctx, `SELECT tag, faction_id FROM faction_profile WHERE tag <> ''`)
	if err != nil {
		return fmt.Errorf("faction_profile: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var tag, id string
		if err := rows.Scan(&tag, &id); err != nil {
			return fmt.Errorf("scan faction_profile: %w", err)
		}
		t.factionIDs[tag] = id
	}

	return rows.Err()
}

func (t *Tanks) loadPlayers(ctx context.Context) error {
	rows, err := t.db.QueryContext(ctx, `SELECT agent_id, player_id FROM agents WHERE agent_id <> '' ORDER BY last_seen`)
	if err != nil {
		return fmt.Errorf("agents: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	t.mu.Lock()
	defer t.mu.Unlock()
	for rows.Next() {
		var agent, player string
		if err := rows.Scan(&agent, &player); err != nil {
			return fmt.Errorf("scan agents: %w", err)
		}
		t.players[agent] = player // later last_seen wins
	}

	return rows.Err()
}

// player resolves an agent id, re-querying on a cache miss because agents can
// appear after the server started. ok is false when the agent is unknown.
func (t *Tanks) player(ctx context.Context, agentID string) (string, bool, error) {
	t.mu.Lock()
	id, ok := t.players[agentID]
	t.mu.Unlock()
	if ok {
		return id, true, nil
	}
	err := t.db.QueryRowContext(ctx,
		`SELECT player_id FROM agents WHERE agent_id = ? ORDER BY last_seen DESC LIMIT 1`, agentID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("resolve agent %q: %w", agentID, err)
	}
	t.mu.Lock()
	t.players[agentID] = id
	t.mu.Unlock()

	return id, true, nil
}

// Close closes the database handle.
func (t *Tanks) Close() error { return t.db.Close() }

// Levels returns one TankLevel per (holder, item) for the node, in the node's
// holder order then item order. Missing rows yield Units 0 and a zero
// CapturedAt. A holder agent unknown to assets.db yields no levels; an unknown
// faction tag is an error.
func (t *Tanks) Levels(ctx context.Context, n Node) ([]TankLevel, error) {
	var out []TankLevel
	for _, h := range n.Holders {
		var query, owner string
		switch {
		case h.Agent != "":
			player, ok, err := t.player(ctx, h.Agent)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			query = `SELECT quantity, captured_at FROM agent_storage_items WHERE player_id = ? AND base_id = ? AND item_id = ?`
			owner = player
		case h.Faction != "":
			id, ok := t.factionIDs[h.Faction]
			if !ok {
				return nil, fmt.Errorf("unknown faction tag %q", h.Faction)
			}
			query = `SELECT quantity, captured_at FROM faction_storage_items WHERE faction_id = ? AND base_id = ? AND item_id = ?`
			owner = id
		default:
			return nil, fmt.Errorf("node %q: holder has neither agent nor faction", n.ID)
		}
		for _, item := range n.Items {
			lv := TankLevel{Holder: h, Item: item}
			var qty float64
			var captured string
			err := t.db.QueryRowContext(ctx, query, owner, h.Station, item).Scan(&qty, &captured)
			switch {
			case errors.Is(err, sql.ErrNoRows):
			case err != nil:
				return nil, fmt.Errorf("storage %s/%s: %w", h.Station, item, err)
			default:
				lv.Units = qty
				if ts, perr := time.Parse(time.RFC3339Nano, captured); perr == nil {
					lv.CapturedAt = ts
				}
			}
			out = append(out, lv)
		}
	}

	return out, nil
}

// SumUnits totals the units of one item across levels.
func SumUnits(levels []TankLevel, item string) float64 {
	var sum float64
	for _, l := range levels {
		if l.Item == item {
			sum += l.Units
		}
	}

	return sum
}

// Freshest returns the newest CapturedAt among levels, or zero when none.
func Freshest(levels []TankLevel) time.Time {
	var newest time.Time
	for _, l := range levels {
		if l.CapturedAt.After(newest) {
			newest = l.CapturedAt
		}
	}

	return newest
}
