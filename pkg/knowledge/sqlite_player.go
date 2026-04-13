package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// StorePlayer stores or updates a player record and their stats.
func (kb *SQLiteKB) StorePlayer(ctx context.Context, player PlayerRecord) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO players (id, username, empire, credits, current_system, current_poi,
			current_ship_id, home_base, docked_at_base, faction_id, faction_rank,
			experience, last_updated_tick)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			username = excluded.username,
			empire = excluded.empire,
			credits = excluded.credits,
			current_system = excluded.current_system,
			current_poi = excluded.current_poi,
			current_ship_id = excluded.current_ship_id,
			home_base = excluded.home_base,
			docked_at_base = excluded.docked_at_base,
			faction_id = excluded.faction_id,
			faction_rank = excluded.faction_rank,
			experience = excluded.experience,
			last_updated_tick = excluded.last_updated_tick
	`, player.ID, player.Username, player.Empire, player.Credits, player.CurrentSystem,
		player.CurrentPOI, player.CurrentShipID, player.HomeBase, player.DockedAtBase,
		player.FactionID, player.FactionRank, player.Experience, player.LastUpdatedTick)
	if err != nil {
		return fmt.Errorf("failed to upsert player: %w", err)
	}

	s := player.Stats
	_, err = tx.ExecContext(ctx, `
		INSERT INTO player_stats (player_id, ships_destroyed, times_destroyed, ore_mined,
			credits_earned, credits_spent, trades_completed, systems_discovered,
			items_crafted, missions_completed, bases_destroyed, distance_traveled,
			pirates_destroyed, ships_lost, time_played, last_updated_tick)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(player_id) DO UPDATE SET
			ships_destroyed = excluded.ships_destroyed,
			times_destroyed = excluded.times_destroyed,
			ore_mined = excluded.ore_mined,
			credits_earned = excluded.credits_earned,
			credits_spent = excluded.credits_spent,
			trades_completed = excluded.trades_completed,
			systems_discovered = excluded.systems_discovered,
			items_crafted = excluded.items_crafted,
			missions_completed = excluded.missions_completed,
			bases_destroyed = excluded.bases_destroyed,
			distance_traveled = excluded.distance_traveled,
			pirates_destroyed = excluded.pirates_destroyed,
			ships_lost = excluded.ships_lost,
			time_played = excluded.time_played,
			last_updated_tick = excluded.last_updated_tick
	`, player.ID, s.ShipsDestroyed, s.TimesDestroyed, s.OreMined,
		s.CreditsEarned, s.CreditsSpent, s.TradesCompleted, s.SystemsDiscovered,
		s.ItemsCrafted, s.MissionsCompleted, s.BasesDestroyed, s.DistanceTraveled,
		s.PiratesDestroyed, s.ShipsLost, s.TimePlayed, s.LastUpdatedTick)
	if err != nil {
		return fmt.Errorf("failed to upsert player stats: %w", err)
	}

	return tx.Commit()
}

// GetPlayer retrieves a player record by ID.
func (kb *SQLiteKB) GetPlayer(ctx context.Context, playerID string) (*PlayerRecord, error) {
	var p PlayerRecord
	err := kb.db.QueryRowContext(ctx, `
		SELECT id, username, COALESCE(empire, ''), credits, COALESCE(current_system, ''),
			COALESCE(current_poi, ''), COALESCE(current_ship_id, ''), COALESCE(home_base, ''),
			COALESCE(docked_at_base, ''), COALESCE(faction_id, ''), COALESCE(faction_rank, ''),
			experience, last_updated_tick
		FROM players WHERE id = ?
	`, playerID).Scan(&p.ID, &p.Username, &p.Empire, &p.Credits, &p.CurrentSystem,
		&p.CurrentPOI, &p.CurrentShipID, &p.HomeBase, &p.DockedAtBase,
		&p.FactionID, &p.FactionRank, &p.Experience, &p.LastUpdatedTick)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query player: %w", err)
	}

	// Load stats
	var s PlayerStatsRecord
	err = kb.db.QueryRowContext(ctx, `
		SELECT ships_destroyed, times_destroyed, ore_mined, credits_earned, credits_spent,
			trades_completed, systems_discovered, items_crafted, missions_completed,
			bases_destroyed, distance_traveled, pirates_destroyed, ships_lost, time_played,
			last_updated_tick
		FROM player_stats WHERE player_id = ?
	`, playerID).Scan(&s.ShipsDestroyed, &s.TimesDestroyed, &s.OreMined, &s.CreditsEarned,
		&s.CreditsSpent, &s.TradesCompleted, &s.SystemsDiscovered, &s.ItemsCrafted,
		&s.MissionsCompleted, &s.BasesDestroyed, &s.DistanceTraveled, &s.PiratesDestroyed,
		&s.ShipsLost, &s.TimePlayed, &s.LastUpdatedTick)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to query player stats: %w", err)
	}
	p.Stats = s

	return &p, nil
}

// StorePlayerSkills stores a player's skill progress.
func (kb *SQLiteKB) StorePlayerSkills(ctx context.Context, playerID string, skills []PlayerSkillRecord) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "DELETE FROM player_skills WHERE player_id = ?", playerID); err != nil {
		return fmt.Errorf("failed to clear player skills: %w", err)
	}

	for _, s := range skills {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO player_skills (player_id, skill_id, level, current_xp)
			VALUES (?, ?, ?, ?)
		`, playerID, s.SkillID, s.Level, s.CurrentXP)
		if err != nil {
			return fmt.Errorf("failed to insert player skill %s: %w", s.SkillID, err)
		}
	}

	return tx.Commit()
}

// GetPlayerSkills retrieves a player's skill progress.
func (kb *SQLiteKB) GetPlayerSkills(ctx context.Context, playerID string) ([]PlayerSkillRecord, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT skill_id, level, current_xp FROM player_skills WHERE player_id = ?
	`, playerID)
	if err != nil {
		return nil, fmt.Errorf("failed to query player skills: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var skills []PlayerSkillRecord
	for rows.Next() {
		var s PlayerSkillRecord
		if err := rows.Scan(&s.SkillID, &s.Level, &s.CurrentXP); err != nil {
			return nil, fmt.Errorf("failed to scan player skill: %w", err)
		}
		skills = append(skills, s)
	}
	return skills, rows.Err()
}

// StoreShip stores or updates a ship record with its cargo and modules.
func (kb *SQLiteKB) StoreShip(ctx context.Context, ship ShipRecord) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO agent_ships (id, owner_id, class_id, name, hull, max_hull, shield, max_shield,
			shield_recharge, armor, speed, fuel, max_fuel, cargo_used, cargo_capacity,
			cpu_used, cpu_capacity, power_used, power_capacity,
			weapon_slots, defense_slots, utility_slots, docked_at_base, last_updated_tick)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			owner_id = excluded.owner_id,
			class_id = excluded.class_id,
			name = excluded.name,
			hull = excluded.hull,
			max_hull = excluded.max_hull,
			shield = excluded.shield,
			max_shield = excluded.max_shield,
			shield_recharge = excluded.shield_recharge,
			armor = excluded.armor,
			speed = excluded.speed,
			fuel = excluded.fuel,
			max_fuel = excluded.max_fuel,
			cargo_used = excluded.cargo_used,
			cargo_capacity = excluded.cargo_capacity,
			cpu_used = excluded.cpu_used,
			cpu_capacity = excluded.cpu_capacity,
			power_used = excluded.power_used,
			power_capacity = excluded.power_capacity,
			weapon_slots = excluded.weapon_slots,
			defense_slots = excluded.defense_slots,
			utility_slots = excluded.utility_slots,
			docked_at_base = excluded.docked_at_base,
			last_updated_tick = excluded.last_updated_tick
	`, ship.ID, ship.OwnerID, ship.ClassID, ship.Name, ship.Hull, ship.MaxHull,
		ship.Shield, ship.MaxShield, ship.ShieldRecharge, ship.Armor, ship.Speed,
		ship.Fuel, ship.MaxFuel, ship.CargoUsed, ship.CargoCapacity,
		ship.CPUUsed, ship.CPUCapacity, ship.PowerUsed, ship.PowerCapacity,
		ship.WeaponSlots, ship.DefenseSlots, ship.UtilitySlots, ship.DockedAtBase,
		ship.LastUpdatedTick)
	if err != nil {
		return fmt.Errorf("failed to upsert ship: %w", err)
	}

	// Replace cargo
	if _, err := tx.ExecContext(ctx, "DELETE FROM ship_cargo WHERE ship_id = ?", ship.ID); err != nil {
		return fmt.Errorf("failed to clear ship cargo: %w", err)
	}
	for _, c := range ship.Cargo {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO ship_cargo (ship_id, item_id, quantity) VALUES (?, ?, ?)
		`, ship.ID, c.ItemID, c.Quantity)
		if err != nil {
			return fmt.Errorf("failed to insert ship cargo: %w", err)
		}
	}

	// Replace modules
	if _, err := tx.ExecContext(ctx, "DELETE FROM ship_modules WHERE ship_id = ?", ship.ID); err != nil {
		return fmt.Errorf("failed to clear ship modules: %w", err)
	}
	for _, m := range ship.Modules {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO ship_modules (id, ship_id, type_id, name, type, cpu_usage, power_usage,
				quality, quality_grade, wear, wear_status, last_updated_tick)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, m.ID, ship.ID, m.TypeID, m.Name, m.Type, m.CPUUsage, m.PowerUsage,
			m.Quality, m.QualityGrade, m.Wear, m.WearStatus, m.LastUpdatedTick)
		if err != nil {
			return fmt.Errorf("failed to insert ship module: %w", err)
		}
	}

	return tx.Commit()
}

// GetShip retrieves a ship by ID with its cargo and modules.
func (kb *SQLiteKB) GetShip(ctx context.Context, shipID string) (*ShipRecord, error) {
	ship, err := kb.scanShipRow(kb.db.QueryRowContext(ctx, `
		SELECT id, owner_id, class_id, COALESCE(name, ''), hull, max_hull, shield, max_shield,
			shield_recharge, armor, speed, fuel, max_fuel, cargo_used, cargo_capacity,
			cpu_used, cpu_capacity, power_used, power_capacity,
			weapon_slots, defense_slots, utility_slots, COALESCE(docked_at_base, ''),
			last_updated_tick
		FROM agent_ships WHERE id = ?
	`, shipID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query ship: %w", err)
	}

	ship.Cargo, err = kb.loadShipCargo(ctx, shipID)
	if err != nil {
		return nil, err
	}
	ship.Modules, err = kb.loadShipModules(ctx, shipID)
	if err != nil {
		return nil, err
	}

	return ship, nil
}

// GetPlayerShips retrieves all ships owned by a player.
func (kb *SQLiteKB) GetPlayerShips(ctx context.Context, playerID string) ([]ShipRecord, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT id, owner_id, class_id, COALESCE(name, ''), hull, max_hull, shield, max_shield,
			shield_recharge, armor, speed, fuel, max_fuel, cargo_used, cargo_capacity,
			cpu_used, cpu_capacity, power_used, power_capacity,
			weapon_slots, defense_slots, utility_slots, COALESCE(docked_at_base, ''),
			last_updated_tick
		FROM agent_ships WHERE owner_id = ? ORDER BY name
	`, playerID)
	if err != nil {
		return nil, fmt.Errorf("failed to query player ships: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ships []ShipRecord
	for rows.Next() {
		var s ShipRecord
		if err := rows.Scan(&s.ID, &s.OwnerID, &s.ClassID, &s.Name, &s.Hull, &s.MaxHull,
			&s.Shield, &s.MaxShield, &s.ShieldRecharge, &s.Armor, &s.Speed,
			&s.Fuel, &s.MaxFuel, &s.CargoUsed, &s.CargoCapacity,
			&s.CPUUsed, &s.CPUCapacity, &s.PowerUsed, &s.PowerCapacity,
			&s.WeaponSlots, &s.DefenseSlots, &s.UtilitySlots, &s.DockedAtBase,
			&s.LastUpdatedTick); err != nil {
			return nil, fmt.Errorf("failed to scan ship: %w", err)
		}
		ships = append(ships, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating ships: %w", err)
	}

	// Load cargo and modules for each ship
	for i := range ships {
		ships[i].Cargo, err = kb.loadShipCargo(ctx, ships[i].ID)
		if err != nil {
			return nil, err
		}
		ships[i].Modules, err = kb.loadShipModules(ctx, ships[i].ID)
		if err != nil {
			return nil, err
		}
	}

	return ships, nil
}

func (kb *SQLiteKB) scanShipRow(row *sql.Row) (*ShipRecord, error) {
	var s ShipRecord
	err := row.Scan(&s.ID, &s.OwnerID, &s.ClassID, &s.Name, &s.Hull, &s.MaxHull,
		&s.Shield, &s.MaxShield, &s.ShieldRecharge, &s.Armor, &s.Speed,
		&s.Fuel, &s.MaxFuel, &s.CargoUsed, &s.CargoCapacity,
		&s.CPUUsed, &s.CPUCapacity, &s.PowerUsed, &s.PowerCapacity,
		&s.WeaponSlots, &s.DefenseSlots, &s.UtilitySlots, &s.DockedAtBase,
		&s.LastUpdatedTick)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (kb *SQLiteKB) loadShipCargo(ctx context.Context, shipID string) ([]CargoEntry, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT item_id, quantity FROM ship_cargo WHERE ship_id = ?
	`, shipID)
	if err != nil {
		return nil, fmt.Errorf("failed to query ship cargo: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var cargo []CargoEntry
	for rows.Next() {
		var c CargoEntry
		if err := rows.Scan(&c.ItemID, &c.Quantity); err != nil {
			return nil, fmt.Errorf("failed to scan ship cargo: %w", err)
		}
		cargo = append(cargo, c)
	}
	return cargo, rows.Err()
}

func (kb *SQLiteKB) loadShipModules(ctx context.Context, shipID string) ([]ShipModuleRecord, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT id, type_id, COALESCE(name, ''), COALESCE(type, ''), cpu_usage, power_usage,
			quality, COALESCE(quality_grade, ''), wear, COALESCE(wear_status, ''),
			last_updated_tick
		FROM ship_modules WHERE ship_id = ?
	`, shipID)
	if err != nil {
		return nil, fmt.Errorf("failed to query ship modules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var modules []ShipModuleRecord
	for rows.Next() {
		var m ShipModuleRecord
		if err := rows.Scan(&m.ID, &m.TypeID, &m.Name, &m.Type, &m.CPUUsage, &m.PowerUsage,
			&m.Quality, &m.QualityGrade, &m.Wear, &m.WearStatus, &m.LastUpdatedTick); err != nil {
			return nil, fmt.Errorf("failed to scan ship module: %w", err)
		}
		modules = append(modules, m)
	}
	return modules, rows.Err()
}
