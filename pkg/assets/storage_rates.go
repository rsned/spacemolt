package assets

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// BaseItem keys a holding by station (wire base id) and item.
type BaseItem struct {
	BaseID string
	ItemID string
}

// StorageSnapshot is one capture_storage pass for one agent: every holding it
// saw. An item absent from Items was not held (0) at At.
type StorageSnapshot struct {
	PlayerID string
	At       time.Time
	Items    map[BaseItem]float64
}

// StorageFlow is a known movement out of (Out > 0) or into (Out < 0) an
// agent's station storage that is not mining: a market sale or purchase, or a
// station-storage craft consuming inputs and producing outputs.
type StorageFlow struct {
	PlayerID string
	BaseID   string
	ItemID   string
	At       time.Time
	Out      float64
}

// StorageRate is the estimated mining (or other unexplained inflow) rate of
// one item into one agent's storage at one station over [From, To].
type StorageRate struct {
	PlayerID string
	BaseID   string
	ItemID   string
	From, To time.Time
	Delta    float64 // last - first holding
	Outflow  float64 // net known outflow inside the window
	PerDay   float64 // (Delta + Outflow) per day
}

// RecipeIO is a recipe's per-run inputs and outputs, by item id.
type RecipeIO struct {
	Inputs, Outputs map[string]float64
}

// StorageRates estimates per-day inflow for every (agent, station, item) seen
// in snaps: (last - first + net known outflow) / days. The window for an agent
// runs from its first snapshot to its last snapshot not after covered[player]
// (the action-log capture time; flows after it are not known yet, so a later
// snapshot would read sales as lost mining). A nil or missing covered entry
// means no limit. Agents with fewer than two usable snapshots are skipped.
// Results are sorted by PerDay, highest first.
func StorageRates(snaps []StorageSnapshot, flows []StorageFlow, covered map[string]time.Time) []StorageRate {
	byPlayer := map[string][]StorageSnapshot{}
	for _, s := range snaps {
		if c, ok := covered[s.PlayerID]; ok && s.At.After(c) {
			continue
		}
		byPlayer[s.PlayerID] = append(byPlayer[s.PlayerID], s)
	}
	var out []StorageRate
	for pid, ps := range byPlayer {
		if len(ps) < 2 {
			continue
		}
		sort.Slice(ps, func(i, j int) bool { return ps[i].At.Before(ps[j].At) })
		first, last := ps[0], ps[len(ps)-1]
		days := last.At.Sub(first.At).Hours() / 24
		if days <= 0 {
			continue
		}
		home := mainBase(last)
		net := map[BaseItem]float64{}
		for _, f := range flows {
			if f.PlayerID != pid || !f.At.After(first.At) || f.At.After(last.At) {
				continue
			}
			base := f.BaseID
			if base == "" {
				base = home
			}
			net[BaseItem{base, f.ItemID}] += f.Out
		}
		keys := map[BaseItem]bool{}
		for k := range first.Items {
			keys[k] = true
		}
		for k := range last.Items {
			keys[k] = true
		}
		for k := range net {
			keys[k] = true
		}
		for k := range keys {
			delta := last.Items[k] - first.Items[k]
			out = append(out, StorageRate{
				PlayerID: pid, BaseID: k.BaseID, ItemID: k.ItemID,
				From: first.At, To: last.At,
				Delta: delta, Outflow: net[k],
				PerDay: (delta + net[k]) / days,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PerDay != out[j].PerDay {
			return out[i].PerDay > out[j].PerDay
		}
		if out[i].BaseID != out[j].BaseID {
			return out[i].BaseID < out[j].BaseID
		}
		return out[i].ItemID < out[j].ItemID
	})

	return out
}

// mainBase is the station where the snapshot holds the most units: where a
// station-bound agent lives. Flows whose event names no station land there.
func mainBase(s StorageSnapshot) string {
	total := map[string]float64{}
	for k, q := range s.Items {
		total[k.BaseID] += q
	}
	best := ""
	for b, q := range total {
		if best == "" || q > total[best] || (q == total[best] && b < best) {
			best = b
		}
	}

	return best
}

// FlowsFromEvent turns one action-log event into storage flows:
//   - trading.exchange_fill: seller = out of storage, buyer = into it, at base_id.
//     (A fill of the agent's OWN sell order moves nothing: the units left
//     storage when the order was created. LoadStorageFlows drops those.)
//   - trading.sell_order_created: the whole order leaves storage into escrow.
//   - trading.order_cancelled: the unsold remainder returns to storage.
//     (Only true of sell orders; LoadStorageFlows drops the others.)
//   - trading.gift_sent with source "storage": out of storage.
//   - crafting.completed with storage "station": runs x recipe inputs out,
//     runs x outputs in, at the base named by the facility id's last segment.
//
// An event that names no station (order created, gift, a facility id that is a
// bare id) yields flows with an empty BaseID; StorageRates books those at the
// agent's main base. Anything else (and any payload it cannot read) yields
// nothing.
func FlowsFromEvent(playerID string, at time.Time, eventType, dataJSON string, recipes map[string]RecipeIO) []StorageFlow {
	var d map[string]string
	if err := json.Unmarshal([]byte(dataJSON), &d); err != nil {
		return nil
	}
	num := func(k string) float64 {
		v, _ := strconv.ParseFloat(d[k], 64)
		return v
	}
	switch eventType {
	case "trading.exchange_fill":
		q := num("quantity")
		if q == 0 || d["base_id"] == "" || d["item_id"] == "" {
			return nil
		}
		switch d["role"] {
		case "seller":
		case "buyer":
			q = -q
		default:
			return nil
		}
		return []StorageFlow{{PlayerID: playerID, BaseID: d["base_id"], ItemID: d["item_id"], At: at, Out: q}}
	case "trading.sell_order_created":
		q := num("quantity")
		if q == 0 || d["item_id"] == "" {
			return nil
		}
		return []StorageFlow{{PlayerID: playerID, ItemID: d["item_id"], At: at, Out: q}}
	case "trading.order_cancelled":
		q := num("quantity_remaining")
		if q == 0 || d["item_id"] == "" {
			return nil
		}
		return []StorageFlow{{PlayerID: playerID, BaseID: d["base_id"], ItemID: d["item_id"], At: at, Out: -q}}
	case "trading.gift_sent":
		q := num("quantity")
		if d["source"] != "storage" || q == 0 || d["item_id"] == "" {
			return nil
		}
		return []StorageFlow{{PlayerID: playerID, ItemID: d["item_id"], At: at, Out: q}}
	case "crafting.completed":
		if d["storage"] != "station" {
			return nil
		}
		r, ok := recipes[d["recipe_id"]]
		runs := num("runs")
		fac := d["facility_id"]
		base := ""
		if i := strings.LastIndex(fac, ":"); i >= 0 {
			base = fac[i+1:]
		}
		if !ok || runs == 0 || fac == "" {
			return nil
		}
		var out []StorageFlow
		for item, q := range r.Inputs {
			out = append(out, StorageFlow{PlayerID: playerID, BaseID: base, ItemID: item, At: at, Out: runs * q})
		}
		for item, q := range r.Outputs {
			out = append(out, StorageFlow{PlayerID: playerID, BaseID: base, ItemID: item, At: at, Out: -runs * q})
		}
		return out
	}

	return nil
}

// LoadStorageHistory returns every history snapshot at or after since for
// the given players, oldest first per player.
func (s *Store) LoadStorageHistory(ctx context.Context, since time.Time, playerIDs []string) ([]StorageSnapshot, error) {
	var out []StorageSnapshot
	for _, pid := range playerIDs {
		snaps, err := s.loadPlayerHistory(ctx, pid, rfc3339(since))
		if err != nil {
			return nil, err
		}
		out = append(out, snaps...)
	}

	return out, nil
}

func (s *Store) loadPlayerHistory(ctx context.Context, pid, since string) ([]StorageSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT h.captured_at, i.base_id, i.item_id, i.quantity
		  FROM storage_history_snapshots h
		  LEFT JOIN storage_history i ON i.player_id = h.player_id AND i.captured_at = h.captured_at
		 WHERE h.player_id = ? AND h.captured_at >= ?
		 ORDER BY h.captured_at`, pid, since)
	if err != nil {
		return nil, fmt.Errorf("assets: load storage history %s: %w", pid, err)
	}
	defer func() { _ = rows.Close() }()
	var out []StorageSnapshot
	for rows.Next() {
		var (
			ts         string
			base, item sql.NullString
			qty        sql.NullFloat64
		)
		if err := rows.Scan(&ts, &base, &item, &qty); err != nil {
			return nil, fmt.Errorf("assets: scan storage history %s: %w", pid, err)
		}
		at := parseCapturedAt(ts)
		if len(out) == 0 || !out[len(out)-1].At.Equal(at) {
			out = append(out, StorageSnapshot{PlayerID: pid, At: at, Items: map[BaseItem]float64{}})
		}
		if base.Valid && item.Valid {
			out[len(out)-1].Items[BaseItem{base.String, item.String}] = qty.Float64
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("assets: iterate storage history %s: %w", pid, err)
	}

	return out, nil
}

// LoadStorageFlows reads the action-log events for the given players since
// `since` and converts them with FlowsFromEvent. It also returns each
// player's action-log coverage limit for StorageRates: the capture time, or
// the newest stored event while the cursor is still catching up.
func (s *Store) LoadStorageFlows(ctx context.Context, since time.Time, playerIDs []string, recipes map[string]RecipeIO) ([]StorageFlow, map[string]time.Time, error) {
	var flows []StorageFlow
	covered := map[string]time.Time{}
	for _, pid := range playerIDs {
		// A cursor that has not caught up is still paging through old events:
		// the log is only known up to the newest event stored, not up to the
		// capture time. (marketbot_010 on 2026-11-18: captured that day, newest
		// event 11-06, so twelve days of crafts read as "no mining".)
		var ts string
		err := s.db.QueryRowContext(ctx, `
			SELECT CASE WHEN c.caught_up = 0
			            THEN COALESCE((SELECT MAX(e.created_at) FROM action_log_events e WHERE e.player_id = c.player_id), '')
			            ELSE c.captured_at END
			  FROM action_log_cursor c WHERE c.player_id = ?`, pid).Scan(&ts)
		switch {
		case err == sql.ErrNoRows:
			covered[pid] = time.Time{} // no action log at all: nothing is covered
		case err != nil:
			return nil, nil, fmt.Errorf("assets: action log cursor %s: %w", pid, err)
		default:
			covered[pid] = parseCapturedAt(ts)
		}
		ownSell, err := s.ownSellOrders(ctx, pid)
		if err != nil {
			return nil, nil, err
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT event_type, created_at, data_json FROM action_log_events
			 WHERE player_id = ? AND created_at >= ?
			   AND event_type IN ('trading.exchange_fill', 'crafting.completed',
			       'trading.sell_order_created', 'trading.order_cancelled', 'trading.gift_sent')`, pid, rfc3339(since))
		if err != nil {
			return nil, nil, fmt.Errorf("assets: load flows %s: %w", pid, err)
		}
		for rows.Next() {
			var typ, created, data string
			if serr := rows.Scan(&typ, &created, &data); serr != nil {
				_ = rows.Close()
				return nil, nil, fmt.Errorf("assets: scan flow %s: %w", pid, serr)
			}
			if !movesStorage(typ, data, ownSell) {
				continue
			}
			fs := FlowsFromEvent(pid, parseCapturedAt(created), typ, data, recipes)
			if typ == "trading.sell_order_created" {
				for i := range fs {
					fs[i].BaseID = ownSell[eventOrderID(data)]
				}
			}
			flows = append(flows, fs...)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("assets: iterate flows %s: %w", pid, err)
		}
	}

	return flows, covered, nil
}

// eventOrderID is the order_id of a trading event payload ("" if none).
func eventOrderID(dataJSON string) string {
	var d struct {
		OrderID string `json:"order_id"`
	}
	_ = json.Unmarshal([]byte(dataJSON), &d)

	return d.OrderID
}

// movesStorage reports whether a trading event changed station storage, given
// the agent's own sell orders: a seller fill of an own order did not (the
// units were escrowed at creation), and a cancel returns units only for a
// sell order.
func movesStorage(eventType, dataJSON string, ownSell map[string]string) bool {
	_, own := ownSell[eventOrderID(dataJSON)]
	switch eventType {
	case "trading.exchange_fill":
		var d struct {
			Role string `json:"role"`
		}
		_ = json.Unmarshal([]byte(dataJSON), &d)
		return d.Role != "seller" || !own
	case "trading.order_cancelled":
		return own
	}

	return true
}

// ownSellOrders maps every sell order the player ever created (any age: an
// old order still fills) to the station it sits at, read from its fills or
// its cancel; "" when the log never named the station.
func (s *Store) ownSellOrders(ctx context.Context, pid string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT json_extract(c.data_json, '$.order_id'),
		       COALESCE((SELECT json_extract(f.data_json, '$.base_id') FROM action_log_events f
		                  WHERE f.player_id = c.player_id
		                    AND f.event_type IN ('trading.exchange_fill', 'trading.order_cancelled')
		                    AND json_extract(f.data_json, '$.order_id') = json_extract(c.data_json, '$.order_id')
		                  LIMIT 1), '')
		  FROM action_log_events c
		 WHERE c.player_id = ? AND c.event_type = 'trading.sell_order_created'`, pid)
	if err != nil {
		return nil, fmt.Errorf("assets: own sell orders %s: %w", pid, err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var id, base sql.NullString
		if err := rows.Scan(&id, &base); err != nil {
			return nil, fmt.Errorf("assets: scan sell order %s: %w", pid, err)
		}
		if id.Valid && id.String != "" {
			out[id.String] = base.String
		}
	}

	return out, rows.Err()
}

// PlayersByAgentPrefix maps player id -> agent id for every known agent whose
// agent id starts with prefix ("" = all).
func (s *Store) PlayersByAgentPrefix(ctx context.Context, prefix string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT player_id, agent_id FROM agents WHERE agent_id LIKE ? || '%'`, prefix)
	if err != nil {
		return nil, fmt.Errorf("assets: agents by prefix %q: %w", prefix, err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var pid, aid string
		if err := rows.Scan(&pid, &aid); err != nil {
			return nil, fmt.Errorf("assets: scan agent: %w", err)
		}
		out[pid] = aid
	}

	return out, rows.Err()
}
