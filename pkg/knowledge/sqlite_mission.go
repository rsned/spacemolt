package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// UpsertMissionTemplate implements Base for the SQLite backend.
func (kb *SQLiteKB) UpsertMissionTemplate(
	ctx context.Context,
	entry serverapi.MissionBoardEntry,
	baseID, systemID string,
	tick int64,
) (*MissionUpsertResult, error) {
	row := missionRowFromEntry(entry)
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	existing, existed, err := loadMissionRow(ctx, tx, row.ID)
	if err != nil {
		return nil, err
	}

	res := &MissionUpsertResult{}
	if existed {
		diffs := diffMissionRows(existing, row)
		if len(diffs) > 0 {
			res.Diffs = diffs
			if err := updateMissionRow(ctx, tx, row, tick, now); err != nil {
				return nil, err
			}
			if err := replaceMissionObjectives(ctx, tx, row); err != nil {
				return nil, err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				UPDATE mission_templates
				SET last_seen_tick = ?, last_seen_at = ?
				WHERE id = ?
			`, tick, now, row.ID); err != nil {
				return nil, fmt.Errorf("touch mission: %w", err)
			}
		}
	} else {
		res.Inserted = true
		if err := insertMissionRow(ctx, tx, row, tick, now); err != nil {
			return nil, err
		}
		if err := replaceMissionObjectives(ctx, tx, row); err != nil {
			return nil, err
		}
	}

	if err := upsertMissionLocation(ctx, tx, row.ID, baseID, systemID, tick, now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return res, nil
}

func loadMissionRow(ctx context.Context, tx *sql.Tx, id string) (missionCatalogRow, bool, error) {
	var r missionCatalogRow
	var repeatableInt int
	err := tx.QueryRowContext(ctx, `
		SELECT id, title, COALESCE(description, ''), COALESCE(type, ''), difficulty,
		       COALESCE(giver_name, ''), COALESCE(giver_title, ''),
		       COALESCE(faction_id, ''), COALESCE(faction_name, ''),
		       COALESCE(dialog_offer, ''), COALESCE(dialog_accept, ''),
		       COALESCE(dialog_decline, ''), COALESCE(dialog_complete, ''),
		       COALESCE(chain_next, ''), repeatable, expires_in_ticks,
		       rewards_credits, rewards_skill_xp, rewards_items,
		       requirements, required_modules, provided_items
		FROM mission_templates WHERE id = ?
	`, id).Scan(
		&r.ID, &r.Title, &r.Description, &r.Type, &r.Difficulty,
		&r.GiverName, &r.GiverTitle,
		&r.FactionID, &r.FactionName,
		&r.DialogOffer, &r.DialogAccept, &r.DialogDecline, &r.DialogComplete,
		&r.ChainNext, &repeatableInt, &r.ExpiresInTicks,
		&r.RewardsCredits, &r.RewardsSkillXP, &r.RewardsItems,
		&r.Requirements, &r.RequiredModules, &r.ProvidedItems,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, fmt.Errorf("select mission: %w", err)
	}
	r.Repeatable = repeatableInt != 0

	rows, err := tx.QueryContext(ctx, `
		SELECT sort_order, type, COALESCE(description, ''),
		       COALESCE(item_id, ''), quantity,
		       COALESCE(system_id, ''), COALESCE(system_name, ''),
		       COALESCE(target_base_id, ''), COALESCE(target_base_name, '')
		FROM mission_objectives WHERE mission_id = ? ORDER BY sort_order
	`, id)
	if err != nil {
		return r, false, fmt.Errorf("query objectives: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var objs []objectiveRow
	for rows.Next() {
		var o objectiveRow
		if err := rows.Scan(
			&o.SortOrder, &o.Type, &o.Description,
			&o.ItemID, &o.Quantity,
			&o.SystemID, &o.SystemName,
			&o.TargetBaseID, &o.TargetBaseName,
		); err != nil {
			return r, false, fmt.Errorf("scan objective: %w", err)
		}
		objs = append(objs, o)
	}
	if err := rows.Err(); err != nil {
		return r, false, fmt.Errorf("iterate objectives: %w", err)
	}
	r.Objectives = jsonMarshalString(objs, "[]")
	return r, true, nil
}

func insertMissionRow(ctx context.Context, tx *sql.Tx, r missionCatalogRow, tick int64, now string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO mission_templates (
			id, title, description, type, difficulty,
			giver_name, giver_title, faction_id, faction_name,
			dialog_offer, dialog_accept, dialog_decline, dialog_complete,
			chain_next, repeatable, expires_in_ticks,
			rewards_credits, rewards_skill_xp, rewards_items,
			requirements, required_modules, provided_items,
			first_seen_tick, last_seen_tick, first_seen_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.ID, r.Title, r.Description, r.Type, r.Difficulty,
		r.GiverName, r.GiverTitle, r.FactionID, r.FactionName,
		r.DialogOffer, r.DialogAccept, r.DialogDecline, r.DialogComplete,
		r.ChainNext, missionBoolToInt(r.Repeatable), r.ExpiresInTicks,
		r.RewardsCredits, r.RewardsSkillXP, r.RewardsItems,
		r.Requirements, r.RequiredModules, r.ProvidedItems,
		tick, tick, now, now,
	)
	if err != nil {
		return fmt.Errorf("insert mission: %w", err)
	}
	return nil
}

func updateMissionRow(ctx context.Context, tx *sql.Tx, r missionCatalogRow, tick int64, now string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE mission_templates SET
			title = ?, description = ?, type = ?, difficulty = ?,
			giver_name = ?, giver_title = ?, faction_id = ?, faction_name = ?,
			dialog_offer = ?, dialog_accept = ?, dialog_decline = ?, dialog_complete = ?,
			chain_next = ?, repeatable = ?, expires_in_ticks = ?,
			rewards_credits = ?, rewards_skill_xp = ?, rewards_items = ?,
			requirements = ?, required_modules = ?, provided_items = ?,
			last_seen_tick = ?, last_seen_at = ?
		WHERE id = ?
	`,
		r.Title, r.Description, r.Type, r.Difficulty,
		r.GiverName, r.GiverTitle, r.FactionID, r.FactionName,
		r.DialogOffer, r.DialogAccept, r.DialogDecline, r.DialogComplete,
		r.ChainNext, missionBoolToInt(r.Repeatable), r.ExpiresInTicks,
		r.RewardsCredits, r.RewardsSkillXP, r.RewardsItems,
		r.Requirements, r.RequiredModules, r.ProvidedItems,
		tick, now, r.ID,
	)
	if err != nil {
		return fmt.Errorf("update mission: %w", err)
	}
	return nil
}

func replaceMissionObjectives(ctx context.Context, tx *sql.Tx, r missionCatalogRow) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM mission_objectives WHERE mission_id = ?`, r.ID); err != nil {
		return fmt.Errorf("delete objectives: %w", err)
	}
	for _, o := range objectivesFromRow(r) {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO mission_objectives (
				mission_id, sort_order, type, description,
				item_id, quantity, system_id, system_name,
				target_base_id, target_base_name
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			r.ID, o.SortOrder, o.Type, o.Description,
			o.ItemID, o.Quantity, o.SystemID, o.SystemName,
			o.TargetBaseID, o.TargetBaseName,
		); err != nil {
			return fmt.Errorf("insert objective: %w", err)
		}
	}
	return nil
}

func upsertMissionLocation(ctx context.Context, tx *sql.Tx, missionID, baseID, systemID string, tick int64, now string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO mission_template_locations (
			mission_id, base_id, system_id,
			first_seen_tick, last_seen_tick, first_seen_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(mission_id, base_id) DO UPDATE SET
			last_seen_tick = excluded.last_seen_tick,
			last_seen_at   = excluded.last_seen_at,
			system_id      = excluded.system_id
	`, missionID, baseID, systemID, tick, tick, now, now)
	if err != nil {
		return fmt.Errorf("upsert location: %w", err)
	}
	return nil
}

func missionBoolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
