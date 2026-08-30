package knowledge

import (
	"context"
	"fmt"
	"time"
)

// ShipCapture is one witnessed boarding outcome: a hull changing hands intact
// (server v0.572.0 ship_captured push). Mirrors serverapi.ShipCaptured plus
// the observer stamp, kept here so this package does not import pkg/game.
type ShipCapture struct {
	BattleID            string
	Tick                int64
	BoardingOperationID string
	CaptorID            string
	CaptorUsername      string
	FormerOwnerID       string
	FormerOwnerUsername string
	ShipID              string
	ShipClass           string

	ObserverID string // our agent that received the push
	SeenAt     time.Time
}

// SeenPrize is one observation of an intact captured ship (a "prize") from a
// get_nearby-style read. ActorID is whoever currently holds it — captor or
// the prize crew's principal — and Status/WaitReason are the server's
// recovery state words, stored verbatim.
type SeenPrize struct {
	PrizeID    string
	ShipID     string
	ShipClass  string
	ShipName   string
	ActorID    string
	Status     string
	WaitReason string
	Hull       int
	MaxHull    int
	Shield     int
	MaxShield  int
	InCombat   bool

	SystemID   string
	POIID      string
	Source     string
	Tick       int64
	ObserverID string
	SeenAt     time.Time
}

// BoardingRecorder is the subset of the KB that stores boarding outcomes and
// prize sightings. SQLiteKB implements it; the in-memory KB and mocks do not,
// so wiring code narrows a Base to this and no-ops when it cannot.
type BoardingRecorder interface {
	RecordShipCaptures(ctx context.Context, rows []ShipCapture) error
	RecordPrizeSightings(ctx context.Context, rows []SeenPrize) error
}

// RecordShipCaptures stores each capture keyed on its boarding operation. The
// first observer to report an operation wins; later reports of the same
// operation (other agents in the same battle) are ignored.
func (kb *SQLiteKB) RecordShipCaptures(ctx context.Context, rows []ShipCapture) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("knowledge: begin ship_captures tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, r := range rows {
		if r.BoardingOperationID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO ship_captures
	(boarding_operation_id, battle_id, tick, captor_id, captor_username,
	 former_owner_id, former_owner_username, ship_id, ship_class,
	 observer_id, seen_at_utc)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.BoardingOperationID, r.BattleID, r.Tick, r.CaptorID, r.CaptorUsername,
			r.FormerOwnerID, r.FormerOwnerUsername, r.ShipID, r.ShipClass,
			r.ObserverID, r.SeenAt.UTC().Format(time.RFC3339),
		); err != nil {
			return fmt.Errorf("knowledge: insert ship_captures: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("knowledge: commit ship_captures: %w", err)
	}
	return nil
}

// RecordPrizeSightings appends one timeline row per observation. The same
// observer seeing the same prize in the same system at the same tick is one
// observation however many calls reported it; the later report refreshes
// the mutable state (hull, status, POI).
func (kb *SQLiteKB) RecordPrizeSightings(ctx context.Context, rows []SeenPrize) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("knowledge: begin seen_prize_events tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, p := range rows {
		if p.PrizeID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO seen_prize_events
	(prize_id, ship_id, ship_class, ship_name, actor_id, status, wait_reason,
	 hull, max_hull, shield, max_shield, in_combat,
	 system_id, poi_id, source, tick, observer_id, seen_at_utc)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(observer_id, prize_id, system_id, tick) WHERE tick > 0 DO UPDATE SET
	poi_id      = CASE WHEN excluded.poi_id <> '' THEN excluded.poi_id ELSE poi_id END,
	status      = CASE WHEN excluded.status <> '' THEN excluded.status ELSE status END,
	wait_reason = excluded.wait_reason,
	actor_id    = CASE WHEN excluded.actor_id <> '' THEN excluded.actor_id ELSE actor_id END,
	hull        = excluded.hull,
	shield      = excluded.shield,
	in_combat   = excluded.in_combat,
	seen_at_utc = excluded.seen_at_utc`,
			p.PrizeID, p.ShipID, p.ShipClass, p.ShipName, p.ActorID, p.Status, p.WaitReason,
			p.Hull, p.MaxHull, p.Shield, p.MaxShield, boolToInt(p.InCombat),
			p.SystemID, p.POIID, p.Source, p.Tick, p.ObserverID, p.SeenAt.UTC().Format(time.RFC3339),
		); err != nil {
			return fmt.Errorf("knowledge: insert seen_prize_events: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("knowledge: commit seen_prize_events: %w", err)
	}
	return nil
}
