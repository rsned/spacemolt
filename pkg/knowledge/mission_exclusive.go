package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// MissionExclusiveBase is an authoritative statement that a mission is offered
// at exactly one station.
//
// This is NOT the same fact as a row in mission_template_locations. That table
// records where a mission has been observed on a board, and single-station is
// the default there (11,365 of 11,389 templates have one row), so a row count
// says nothing about exclusivity. Worse, the strongest evidence of a binding
// produces no row at all: a mission nobody has seen listed is never captured,
// yet the server will still name its giver when you try to accept it elsewhere.
type MissionExclusiveBinding struct {
	MissionID string
	// BaseID is a RESOLVED base id. The refusal that carries this fact quotes
	// a display name ("Alpha Centauri Colonial Station"); callers resolve it
	// before storing, so nothing downstream has to guess which naming scheme
	// a value uses.
	BaseID   string
	SystemID string
	// Source names what asserted the exclusivity, e.g. accept_mission_refusal.
	Source string
	Tick   int64
	SeenAt string
}

// ErrMissionUnknown is returned when a binding names a mission with no
// template row. Silently inserting nothing would leave the caller believing
// the giver had been recorded.
var ErrMissionUnknown = errors.New("mission template not found")

// SetMissionExclusiveBase records that missionID is offered only at baseID.
// A later statement wins: the server can move a mission, and the newest
// assertion is the one worth routing on.
func (kb *SQLiteKB) SetMissionExclusiveBase(ctx context.Context, missionID, baseID, source string, tick int64) error {
	return kb.setMissionExclusiveBase(ctx, missionID, baseID, "", source, tick)
}

// SetMissionExclusiveBaseInSystem is SetMissionExclusiveBase with the station's
// system recorded alongside, so routing does not need a second lookup.
func (kb *SQLiteKB) SetMissionExclusiveBaseInSystem(ctx context.Context, missionID, baseID, systemID, source string, tick int64) error {
	return kb.setMissionExclusiveBase(ctx, missionID, baseID, systemID, source, tick)
}

func (kb *SQLiteKB) setMissionExclusiveBase(ctx context.Context, missionID, baseID, systemID, source string, tick int64) error {
	if missionID == "" || baseID == "" {
		return fmt.Errorf("mission id and base id are both required")
	}
	res, err := kb.db.ExecContext(ctx, `
		UPDATE mission_templates
		SET exclusive_base_id   = ?,
		    exclusive_system_id = NULLIF(?, ''),
		    exclusive_source    = ?,
		    exclusive_seen_tick = ?,
		    exclusive_seen_at   = ?
		WHERE id = ?
	`, baseID, systemID, source, tick, time.Now().UTC().Format(time.RFC3339), missionID)
	if err != nil {
		return fmt.Errorf("set exclusive base: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set exclusive base: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrMissionUnknown, missionID)
	}
	return nil
}

// MissionExclusiveBase returns the recorded binding, or nil when none has been
// asserted. Nil rather than a zero value on purpose: an empty base id is not
// something a caller should ever route on.
func (kb *SQLiteKB) MissionExclusiveBase(ctx context.Context, missionID string) (*MissionExclusiveBinding, error) {
	var (
		baseID, systemID, source, seenAt sql.NullString
		tick                             sql.NullInt64
	)
	err := kb.db.QueryRowContext(ctx, `
		SELECT exclusive_base_id, exclusive_system_id, exclusive_source,
		       exclusive_seen_tick, exclusive_seen_at
		FROM mission_templates WHERE id = ?
	`, missionID).Scan(&baseID, &systemID, &source, &tick, &seenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read exclusive base: %w", err)
	}
	if !baseID.Valid || baseID.String == "" {
		return nil, nil
	}
	return &MissionExclusiveBinding{
		MissionID: missionID,
		BaseID:    baseID.String,
		SystemID:  systemID.String,
		Source:    source.String,
		Tick:      tick.Int64,
		SeenAt:    seenAt.String,
	}, nil
}
