package linedash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	db, err := openReadOnly(assetsPath)
	if err != nil {
		return nil, err
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

// storageQueries are the item and snapshot-header queries of one holder kind.
type storageQueries struct{ item, header string }

var (
	agentStorage = storageQueries{
		item:   `SELECT quantity, captured_at FROM agent_storage_items WHERE player_id = ? AND base_id = ? AND item_id = ?`,
		header: `SELECT COALESCE(MAX(captured_at), '') FROM agent_storage WHERE player_id = ?`,
	}
	factionStorage = storageQueries{
		item:   `SELECT quantity, captured_at FROM faction_storage_items WHERE faction_id = ? AND base_id = ? AND item_id = ?`,
		header: `SELECT COALESCE(MAX(captured_at), '') FROM faction_storage WHERE faction_id = ?`,
	}
)

// Levels returns one TankLevel per (holder, item) for the node, in the node's
// holder order then item order. A missing item row yields Units 0 and the
// holder's storage snapshot time (the snapshot replaces the whole set, so a
// missing row means "captured, and empty"). A holder never captured, or an
// agent unknown to assets.db, yields zero levels with a zero CapturedAt. An
// unknown faction tag is an error.
func (t *Tanks) Levels(ctx context.Context, n Node) ([]TankLevel, error) {
	var out []TankLevel
	for _, h := range n.Holders {
		q, owner, known, err := t.holderOwner(ctx, n.ID, h)
		if err != nil {
			return nil, err
		}
		if !known {
			for _, item := range n.Items {
				out = append(out, TankLevel{Holder: h, Item: item})
			}

			continue
		}
		var header string
		if err := t.db.QueryRowContext(ctx, q.header, owner).Scan(&header); err != nil {
			return nil, fmt.Errorf("storage header %s: %w", h.Station, err)
		}
		headerAt := parseTime(header)
		for _, item := range n.Items {
			lv := TankLevel{Holder: h, Item: item, CapturedAt: headerAt}
			var qty float64
			var captured string
			err := t.db.QueryRowContext(ctx, q.item, owner, h.Station, item).Scan(&qty, &captured)
			switch {
			case errors.Is(err, sql.ErrNoRows):
			case err != nil:
				return nil, fmt.Errorf("storage %s/%s: %w", h.Station, item, err)
			default:
				lv.Units = qty
				lv.CapturedAt = parseTime(captured)
			}
			out = append(out, lv)
		}
	}

	return out, nil
}

// holderOwner resolves a holder to its storage queries and owner id. known is
// false for an agent assets.db has never seen.
func (t *Tanks) holderOwner(ctx context.Context, nodeID string, h Holder) (storageQueries, string, bool, error) {
	switch {
	case h.Agent != "":
		player, ok, err := t.player(ctx, h.Agent)

		return agentStorage, player, ok, err
	case h.Faction != "":
		id, ok := t.factionIDs[h.Faction]
		if !ok {
			return storageQueries{}, "", false, fmt.Errorf("unknown faction tag %q", h.Faction)
		}

		return factionStorage, id, true, nil
	default:
		return storageQueries{}, "", false, fmt.Errorf("node %q: holder has neither agent nor faction", nodeID)
	}
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
