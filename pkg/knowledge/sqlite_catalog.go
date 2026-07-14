package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// mapToJSON serializes a map to a JSON string, returning nil for empty/nil maps.
func mapToJSON(m map[string]float64) any {
	if len(m) == 0 {
		return nil
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// StoreItems stores or replaces all items in the catalog.
func (kb *SQLiteKB) StoreItems(ctx context.Context, items []CatalogItem) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Clear detail tables first (FK order), then base tables.
	for _, table := range []string{
		"item_ammo", "item_consumable_effects",
		"item_weapons", "item_defenses", "item_mining", "item_utilities",
		"item_modules", "items",
	} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("failed to clear %s: %w", table, err)
		}
	}

	for _, item := range items {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO items (id, name, description, category, rarity, size, base_value, stackable, tradeable,
				hazardous, power_bonus, quest_item, extracted_by, required_skills, region_lock,
				passenger_economy_berths, passenger_business_berths, passenger_first_berths)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.ID, item.Name, item.Description, item.Category, item.Rarity,
			item.Size, item.BaseValue, item.Stackable, item.Tradeable, item.Hazardous, item.PowerBonus,
			item.QuestItem, nilIfEmpty(item.ExtractedBy), mapIntToJSON(item.RequiredSkills), sliceToJSON(item.RegionLock),
			item.PassengerEconomyBerths, item.PassengerBusinessBerths, item.PassengerFirstBerths)
		if err != nil {
			return fmt.Errorf("failed to insert item %s: %w", item.ID, err)
		}

		if err := storeItemDetails(ctx, tx, item); err != nil {
			return fmt.Errorf("failed to insert details for item %s: %w", item.ID, err)
		}
	}

	return tx.Commit()
}

// storeItemDetails inserts module/consumable/ammo detail rows for an item.
func storeItemDetails(ctx context.Context, tx *sql.Tx, item CatalogItem) error {
	if m := item.Module; m != nil {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO item_modules (item_id, type, type_id, slot, cpu_usage, power_usage, hidden, special)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, item.ID, m.Type, m.TypeID, nilIfEmpty(m.Slot), m.CPUUsage, m.PowerUsage, m.Hidden, nilIfEmpty(m.Special)); err != nil {
			return err
		}

		switch m.Type {
		case "weapon":
			if w := m.Weapon; w != nil {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO item_weapons (item_id, damage, damage_type, range, reach, cooldown, ammo_type,
						magazine_size, armor_bypass_bonus, shield_bypass_bonus)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				`, item.ID, w.Damage, w.DamageType, w.Range, w.Reach, w.Cooldown,
					nilIfEmpty(w.AmmoType), w.MagazineSize, w.ArmorBypassBonus, w.ShieldBypassBonus); err != nil {
					return err
				}
			}
		case "defense":
			if d := m.Defense; d != nil {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO item_defenses (item_id, armor_bonus, hull_bonus, shield_bonus,
						shield_recharge_bonus, armor_repair_rate, resistance_bonus, damage_reduction,
						cloak_strength, cooldown, damage, damage_type, range)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				`, item.ID, d.ArmorBonus, d.HullBonus, d.ShieldBonus,
					d.ShieldRechargeBonus, d.ArmorRepairRate, mapToJSON(d.ResistanceBonus), d.DamageReduction,
					d.CloakStrength, d.Cooldown, d.Damage, nilIfEmpty(d.DamageType), d.Range); err != nil {
					return err
				}
			}
		case "mining":
			if mi := m.Mining; mi != nil {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO item_mining (item_id, mining_power, mining_range)
					VALUES (?, ?, ?)
				`, item.ID, mi.MiningPower, mi.MiningRange); err != nil {
					return err
				}
			}
		case "utility":
			if u := m.Utility; u != nil {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO item_utilities (item_id, speed_bonus, cargo_bonus, cloak_strength,
						scanner_power, accuracy_bonus, tracking_bonus, signature_bonus, fuel_efficiency,
						drone_bandwidth, drone_capacity, harvest_power, harvest_range,
						survey_power, survey_range, tow_speed_penalty, cpu_bonus, max_fuel_bonus,
						hull_penalty, speed_penalty, cooldown)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				`, item.ID, u.SpeedBonus, u.CargoBonus, u.CloakStrength,
					u.ScannerPower, u.AccuracyBonus, u.TrackingBonus, u.SignatureBonus, u.FuelEfficiency,
					u.DroneBandwidth, u.DroneCapacity, u.HarvestPower, u.HarvestRange,
					u.SurveyPower, u.SurveyRange, u.TowSpeedPenalty, u.CPUBonus, u.MaxFuelBonus,
					u.HullPenalty, u.SpeedPenalty, u.Cooldown); err != nil {
					return err
				}
			}
		}
	}

	if ce := item.ConsumableEffect; ce != nil {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO item_consumable_effects (item_id, effect_type, subtype, amount, duration, stat)
			VALUES (?, ?, ?, ?, ?, ?)
		`, item.ID, ce.EffectType, nilIfEmpty(ce.Subtype), ce.Amount, ce.Duration, nilIfEmpty(ce.Stat)); err != nil {
			return err
		}
	}

	if a := item.Ammo; a != nil {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO item_ammo (item_id, ammo_type, modifiers)
			VALUES (?, ?, ?)
		`, item.ID, a.AmmoType, anyMapToJSON(a.Modifiers)); err != nil {
			return err
		}
	}

	return nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// mapIntToJSON serializes a string→int map to JSON, returning nil when empty.
func mapIntToJSON(m map[string]int) any {
	if len(m) == 0 {
		return nil
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// anyMapToJSON serializes a generic string→any map to JSON, returning nil when empty.
func anyMapToJSON(m map[string]any) any {
	if len(m) == 0 {
		return nil
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// sliceToJSON serializes a string slice to JSON, returning nil when empty.
func sliceToJSON(s []string) any {
	if len(s) == 0 {
		return nil
	}
	b, _ := json.Marshal(s)
	return string(b)
}

// GetItem retrieves a single item by ID.
func (kb *SQLiteKB) GetItem(ctx context.Context, itemID string) (*CatalogItem, error) {
	var item CatalogItem
	err := kb.db.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(description, ''), COALESCE(category, ''), COALESCE(rarity, ''),
		       size, base_value, stackable, tradeable, hazardous, power_bonus
		FROM items WHERE id = ?
	`, itemID).Scan(&item.ID, &item.Name, &item.Description, &item.Category, &item.Rarity,
		&item.Size, &item.BaseValue, &item.Stackable, &item.Tradeable, &item.Hazardous, &item.PowerBonus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query item: %w", err)
	}
	return &item, nil
}

// GetItems retrieves all catalog items.
func (kb *SQLiteKB) GetItems(ctx context.Context) ([]CatalogItem, error) {
	return kb.queryItems(ctx, "SELECT id, name, COALESCE(description, ''), COALESCE(category, ''), COALESCE(rarity, ''), size, base_value, stackable, tradeable, hazardous, power_bonus FROM items ORDER BY name")
}

// GetItemsByCategory retrieves items filtered by category.
func (kb *SQLiteKB) GetItemsByCategory(ctx context.Context, category string) ([]CatalogItem, error) {
	return kb.queryItems(ctx, "SELECT id, name, COALESCE(description, ''), COALESCE(category, ''), COALESCE(rarity, ''), size, base_value, stackable, tradeable, hazardous, power_bonus FROM items WHERE category = ? ORDER BY name", category)
}

func (kb *SQLiteKB) queryItems(ctx context.Context, query string, args ...any) ([]CatalogItem, error) {
	rows, err := kb.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []CatalogItem
	for rows.Next() {
		var item CatalogItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Category, &item.Rarity,
			&item.Size, &item.BaseValue, &item.Stackable, &item.Tradeable, &item.Hazardous, &item.PowerBonus); err != nil {
			return nil, fmt.Errorf("failed to scan item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// StoreShipClasses stores or replaces all ship classes in the catalog.
func (kb *SQLiteKB) StoreShipClasses(ctx context.Context, classes []ShipClassDef) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "DELETE FROM ship_build_materials"); err != nil {
		return fmt.Errorf("failed to clear build materials: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM ships"); err != nil {
		return fmt.Errorf("failed to clear ship classes: %w", err)
	}

	for _, sc := range classes {
		defMods, _ := json.Marshal(sc.DefaultModules)
		flavTags, _ := json.Marshal(sc.FlavorTags)
		passRecipes, _ := json.Marshal(sc.PassiveRecipes)
		capJSON, _ := json.Marshal(sc.InherentCapabilities)

		_, err := tx.ExecContext(ctx, `
			INSERT INTO ships (id, name, class, category, description, lore, faction,
				tier, scale, base_hull, base_shield, base_shield_recharge, base_armor,
				base_speed, base_fuel, cargo_capacity, cpu_capacity, power_capacity,
				weapon_slots, defense_slots, utility_slots, build_time, shipyard_tier,
				starter_ship, tow_speed_bonus, based_on, npc_role, special, required_reputation,
				piloting_required, inherent_capabilities, default_modules, flavor_tags,
				passive_recipes, required_achievement, required_faction_achievement,
				required_faction_leader, prestige_lock, default_loadout_version,
				last_updated_tick)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, sc.ID, sc.Name, sc.Class, sc.Category, sc.Description, sc.Lore, sc.Faction,
			sc.Tier, sc.Scale, sc.BaseHull, sc.BaseShield, sc.BaseShieldRecharge, sc.BaseArmor,
			sc.BaseSpeed, sc.BaseFuel, sc.CargoCapacity, sc.CPUCapacity, sc.PowerCapacity,
			sc.WeaponSlots, sc.DefenseSlots, sc.UtilitySlots, sc.BuildTime, sc.ShipyardTier,
			sc.StarterShip, sc.TowSpeedBonus, nilIfEmpty(sc.BasedOn), nilIfEmpty(sc.NPCRole), nilIfEmpty(sc.Special), sc.RequiredReputation,
			sc.PilotingRequired, string(capJSON), string(defMods), string(flavTags),
			string(passRecipes), sc.RequiredAchievement, sc.RequiredFactionAchievement,
			sc.RequiredFactionLeader, sc.PrestigeLock, sc.DefaultLoadoutVersion,
			sc.LastUpdatedTick)
		if err != nil {
			return fmt.Errorf("failed to insert ship class %s: %w", sc.ID, err)
		}

		for _, mat := range sc.BuildMaterials {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO ship_build_materials (ship_class_id, item_id, quantity)
				VALUES (?, ?, ?)
			`, sc.ID, mat.ItemID, mat.Quantity)
			if err != nil {
				return fmt.Errorf("failed to insert build material for %s: %w", sc.ID, err)
			}
		}
	}

	return tx.Commit()
}

// shipClassColumns is the canonical ship-class projection. It is shared by
// every ship-class SELECT so the column order can never drift out of step with
// scanShipClass/scanShipClassRow, which scan positionally.
//
// Note the absence of `price` and `required_skills`: the server stopped
// emitting both for ship classes in v0.495.1, so the columns survive in the
// table (defaulted) but are no longer read or written.
const shipClassColumns = `
	id, name, COALESCE(class, ''), COALESCE(category, ''), COALESCE(description, ''),
	COALESCE(lore, ''), COALESCE(faction, ''), tier, scale,
	base_hull, base_shield, base_shield_recharge, base_armor, base_speed, base_fuel,
	cargo_capacity, cpu_capacity, power_capacity, weapon_slots, defense_slots, utility_slots,
	build_time, shipyard_tier, starter_ship, tow_speed_bonus,
	COALESCE(based_on, ''), COALESCE(npc_role, ''), COALESCE(special, ''), required_reputation, piloting_required,
	COALESCE(inherent_capabilities, ''),
	default_modules, flavor_tags, passive_recipes,
	COALESCE(required_achievement, ''), COALESCE(required_faction_achievement, ''),
	required_faction_leader, COALESCE(prestige_lock, ''), default_loadout_version,
	last_updated_tick`

// GetShipClass retrieves a single ship class by ID.
func (kb *SQLiteKB) GetShipClass(ctx context.Context, classID string) (*ShipClassDef, error) {
	sc, err := kb.scanShipClass(kb.db.QueryRowContext(ctx, `
		SELECT `+shipClassColumns+`
		FROM ships WHERE id = ?
	`, classID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query ship class: %w", err)
	}

	mats, err := kb.loadBuildMaterials(ctx, classID)
	if err != nil {
		return nil, err
	}
	sc.BuildMaterials = mats

	return sc, nil
}

// GetShipClasses retrieves all ship classes.
func (kb *SQLiteKB) GetShipClasses(ctx context.Context) ([]ShipClassDef, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT `+shipClassColumns+`
		FROM ships ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query ship classes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var classes []ShipClassDef
	for rows.Next() {
		sc, err := kb.scanShipClassRow(rows)
		if err != nil {
			return nil, err
		}
		classes = append(classes, *sc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating ship classes: %w", err)
	}

	// Bulk load build materials
	matRows, err := kb.db.QueryContext(ctx, `
		SELECT ship_class_id, item_id, quantity FROM ship_build_materials
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query build materials: %w", err)
	}
	defer func() { _ = matRows.Close() }()

	matMap := make(map[string][]BuildMaterial)
	for matRows.Next() {
		var scID string
		var mat BuildMaterial
		if err := matRows.Scan(&scID, &mat.ItemID, &mat.Quantity); err != nil {
			return nil, fmt.Errorf("failed to scan build material: %w", err)
		}
		matMap[scID] = append(matMap[scID], mat)
	}
	if err := matRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating build materials: %w", err)
	}

	for i := range classes {
		classes[i].BuildMaterials = matMap[classes[i].ID]
	}

	return classes, nil
}

// GetShipClassesByCategory retrieves ship classes filtered by class (e.g., "Mining", "Fighter").
func (kb *SQLiteKB) GetShipClassesByCategory(ctx context.Context, category string) ([]ShipClassDef, error) {
	// Ordered by tier (then name), not price: the server dropped ship-class
	// price in v0.495.1, so the old ORDER BY price ASC silently degenerated to
	// an arbitrary order once every row imported as 0. Tier preserves the
	// original "simplest/cheapest first" intent now that hulls are built
	// rather than bought.
	rows, err := kb.db.QueryContext(ctx, `
		SELECT `+shipClassColumns+`
		FROM ships WHERE class = ? ORDER BY tier ASC, name ASC
	`, category)
	if err != nil {
		return nil, fmt.Errorf("failed to query ship classes by category: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var classes []ShipClassDef
	for rows.Next() {
		sc, err := kb.scanShipClassRow(rows)
		if err != nil {
			return nil, err
		}
		classes = append(classes, *sc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating ship classes: %w", err)
	}

	return classes, nil
}

func (kb *SQLiteKB) scanShipClass(row *sql.Row) (*ShipClassDef, error) {
	var sc ShipClassDef
	var defModsJSON, flavTagsJSON, passRecipesJSON, capJSON string
	err := row.Scan(shipClassScanTargets(&sc, &capJSON, &defModsJSON, &flavTagsJSON, &passRecipesJSON)...)
	if err != nil {
		return nil, err
	}
	decodeShipClassJSON(&sc, capJSON, defModsJSON, flavTagsJSON, passRecipesJSON)
	return &sc, nil
}

// shipClassScanTargets returns scan destinations in the exact order of
// shipClassColumns. Both scanners share it so a column change only has to be
// made in two places that sit next to each other.
func shipClassScanTargets(sc *ShipClassDef, capJSON, defModsJSON, flavTagsJSON, passRecipesJSON *string) []any {
	return []any{
		&sc.ID, &sc.Name, &sc.Class, &sc.Category, &sc.Description,
		&sc.Lore, &sc.Faction, &sc.Tier, &sc.Scale,
		&sc.BaseHull, &sc.BaseShield, &sc.BaseShieldRecharge, &sc.BaseArmor, &sc.BaseSpeed, &sc.BaseFuel,
		&sc.CargoCapacity, &sc.CPUCapacity, &sc.PowerCapacity, &sc.WeaponSlots, &sc.DefenseSlots, &sc.UtilitySlots,
		&sc.BuildTime, &sc.ShipyardTier, &sc.StarterShip, &sc.TowSpeedBonus,
		&sc.BasedOn, &sc.NPCRole, &sc.Special, &sc.RequiredReputation, &sc.PilotingRequired,
		capJSON,
		defModsJSON, flavTagsJSON, passRecipesJSON,
		&sc.RequiredAchievement, &sc.RequiredFactionAchievement,
		&sc.RequiredFactionLeader, &sc.PrestigeLock, &sc.DefaultLoadoutVersion,
		&sc.LastUpdatedTick,
	}
}

func decodeShipClassJSON(sc *ShipClassDef, capJSON, defModsJSON, flavTagsJSON, passRecipesJSON string) {
	_ = json.Unmarshal([]byte(defModsJSON), &sc.DefaultModules)
	_ = json.Unmarshal([]byte(flavTagsJSON), &sc.FlavorTags)
	_ = json.Unmarshal([]byte(passRecipesJSON), &sc.PassiveRecipes)
	if capJSON != "" {
		_ = json.Unmarshal([]byte(capJSON), &sc.InherentCapabilities)
	}
}

func (kb *SQLiteKB) scanShipClassRow(rows *sql.Rows) (*ShipClassDef, error) {
	var sc ShipClassDef
	var defModsJSON, flavTagsJSON, passRecipesJSON, capJSON string
	err := rows.Scan(shipClassScanTargets(&sc, &capJSON, &defModsJSON, &flavTagsJSON, &passRecipesJSON)...)
	if err != nil {
		return nil, fmt.Errorf("failed to scan ship class: %w", err)
	}
	decodeShipClassJSON(&sc, capJSON, defModsJSON, flavTagsJSON, passRecipesJSON)
	return &sc, nil
}

func (kb *SQLiteKB) loadBuildMaterials(ctx context.Context, shipClassID string) ([]BuildMaterial, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT item_id, quantity FROM ship_build_materials WHERE ship_class_id = ?
	`, shipClassID)
	if err != nil {
		return nil, fmt.Errorf("failed to query build materials: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var mats []BuildMaterial
	for rows.Next() {
		var mat BuildMaterial
		if err := rows.Scan(&mat.ItemID, &mat.Quantity); err != nil {
			return nil, fmt.Errorf("failed to scan build material: %w", err)
		}
		mats = append(mats, mat)
	}
	return mats, rows.Err()
}

// StoreSkills stores or replaces all skills in the catalog.
func (kb *SQLiteKB) StoreSkills(ctx context.Context, skills []Skill) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "DELETE FROM skills"); err != nil {
		return fmt.Errorf("failed to clear skills: %w", err)
	}

	for _, s := range skills {
		xpJSON, _ := json.Marshal(s.XPPerLevel)
		bonusJSON, _ := json.Marshal(s.BonusPerLevel)
		reqJSON, _ := json.Marshal(s.RequiredSkills)

		_, err := tx.ExecContext(ctx, `
			INSERT INTO skills (id, name, description, category, max_level, training_source,
				xp_per_level, bonus_per_level, required_skills, empire_restriction)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, s.ID, s.Name, s.Description, s.Category, s.MaxLevel, s.TrainingSource,
			string(xpJSON), string(bonusJSON), string(reqJSON), s.EmpireRestriction)
		if err != nil {
			return fmt.Errorf("failed to insert skill %s: %w", s.ID, err)
		}
	}

	return tx.Commit()
}

// StoreRecipes stores or replaces all recipes in the catalog.
func (kb *SQLiteKB) StoreRecipes(ctx context.Context, recipes []RecipeDef) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "DELETE FROM recipe_inputs"); err != nil {
		return fmt.Errorf("failed to clear recipe inputs: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM recipe_outputs"); err != nil {
		return fmt.Errorf("failed to clear recipe outputs: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM recipes"); err != nil {
		return fmt.Errorf("failed to clear recipes: %w", err)
	}

	for _, r := range recipes {
		reqJSON, _ := json.Marshal(r.RequiredSkills)

		_, err := tx.ExecContext(ctx, `
			INSERT INTO recipes (id, name, description, category, crafting_time, base_quality,
				skill_quality_mod, required_skills, hidden, facility_only, no_recycle, fuel_output,
				last_updated_tick)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, r.ID, r.Name, r.Description, r.Category, r.CraftingTime, r.BaseQuality,
			r.SkillQualityMod, string(reqJSON), r.Hidden, r.FacilityOnly, r.NoRecycle, r.FuelOutput,
			r.LastUpdatedTick)
		if err != nil {
			return fmt.Errorf("failed to insert recipe %s: %w", r.ID, err)
		}

		for _, inp := range r.Inputs {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO recipe_inputs (recipe_id, item_id, quantity)
				VALUES (?, ?, ?)
			`, r.ID, inp.ItemID, inp.Quantity)
			if err != nil {
				return fmt.Errorf("failed to insert recipe input for %s: %w", r.ID, err)
			}
		}

		for _, out := range r.Outputs {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO recipe_outputs (recipe_id, item_id, quantity, quality_mod)
				VALUES (?, ?, ?, ?)
			`, r.ID, out.ItemID, out.Quantity, out.QualityMod)
			if err != nil {
				return fmt.Errorf("failed to insert recipe output for %s: %w", r.ID, err)
			}
		}
	}

	return tx.Commit()
}

// GetRecipe retrieves a single recipe by ID.
func (kb *SQLiteKB) GetRecipe(ctx context.Context, recipeID string) (*RecipeDef, error) {
	var r RecipeDef
	var reqJSON string
	err := kb.db.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(description, ''), COALESCE(category, ''), crafting_time,
			base_quality, skill_quality_mod, required_skills, hidden, facility_only, no_recycle,
			fuel_output, last_updated_tick
		FROM recipes WHERE id = ?
	`, recipeID).Scan(&r.ID, &r.Name, &r.Description, &r.Category, &r.CraftingTime,
		&r.BaseQuality, &r.SkillQualityMod, &reqJSON, &r.Hidden, &r.FacilityOnly, &r.NoRecycle,
		&r.FuelOutput, &r.LastUpdatedTick)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query recipe: %w", err)
	}
	_ = json.Unmarshal([]byte(reqJSON), &r.RequiredSkills)

	inputs, err := kb.loadRecipeInputs(ctx, recipeID)
	if err != nil {
		return nil, err
	}
	r.Inputs = inputs

	outputs, err := kb.loadRecipeOutputs(ctx, recipeID)
	if err != nil {
		return nil, err
	}
	r.Outputs = outputs

	return &r, nil
}

// GetRecipes retrieves all recipes.
func (kb *SQLiteKB) GetRecipes(ctx context.Context) ([]RecipeDef, error) {
	return kb.queryRecipes(ctx, `
		SELECT id, name, COALESCE(description, ''), COALESCE(category, ''), crafting_time,
			base_quality, skill_quality_mod, required_skills, hidden, facility_only, no_recycle,
			fuel_output, last_updated_tick
		FROM recipes ORDER BY name
	`)
}

// GetRecipesByCategory retrieves recipes filtered by category.
func (kb *SQLiteKB) GetRecipesByCategory(ctx context.Context, category string) ([]RecipeDef, error) {
	return kb.queryRecipes(ctx, `
		SELECT id, name, COALESCE(description, ''), COALESCE(category, ''), crafting_time,
			base_quality, skill_quality_mod, required_skills, hidden, facility_only, no_recycle,
			fuel_output, last_updated_tick
		FROM recipes WHERE category = ? ORDER BY name
	`, category)
}

func (kb *SQLiteKB) queryRecipes(ctx context.Context, query string, args ...any) ([]RecipeDef, error) {
	rows, err := kb.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query recipes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var recipes []RecipeDef
	for rows.Next() {
		var r RecipeDef
		var reqJSON string
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Category, &r.CraftingTime,
			&r.BaseQuality, &r.SkillQualityMod, &reqJSON, &r.Hidden, &r.FacilityOnly, &r.NoRecycle,
			&r.FuelOutput, &r.LastUpdatedTick); err != nil {
			return nil, fmt.Errorf("failed to scan recipe: %w", err)
		}
		_ = json.Unmarshal([]byte(reqJSON), &r.RequiredSkills)
		recipes = append(recipes, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating recipes: %w", err)
	}

	// Bulk load inputs and outputs
	inputMap, err := kb.loadAllRecipeInputs(ctx)
	if err != nil {
		return nil, err
	}
	outputMap, err := kb.loadAllRecipeOutputs(ctx)
	if err != nil {
		return nil, err
	}

	for i := range recipes {
		recipes[i].Inputs = inputMap[recipes[i].ID]
		recipes[i].Outputs = outputMap[recipes[i].ID]
	}

	return recipes, nil
}

func (kb *SQLiteKB) loadRecipeInputs(ctx context.Context, recipeID string) ([]RecipeIngredient, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT item_id, quantity FROM recipe_inputs WHERE recipe_id = ?
	`, recipeID)
	if err != nil {
		return nil, fmt.Errorf("failed to query recipe inputs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var inputs []RecipeIngredient
	for rows.Next() {
		var inp RecipeIngredient
		if err := rows.Scan(&inp.ItemID, &inp.Quantity); err != nil {
			return nil, fmt.Errorf("failed to scan recipe input: %w", err)
		}
		inputs = append(inputs, inp)
	}
	return inputs, rows.Err()
}

func (kb *SQLiteKB) loadRecipeOutputs(ctx context.Context, recipeID string) ([]RecipeProduct, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT item_id, quantity, quality_mod FROM recipe_outputs WHERE recipe_id = ?
	`, recipeID)
	if err != nil {
		return nil, fmt.Errorf("failed to query recipe outputs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var outputs []RecipeProduct
	for rows.Next() {
		var out RecipeProduct
		if err := rows.Scan(&out.ItemID, &out.Quantity, &out.QualityMod); err != nil {
			return nil, fmt.Errorf("failed to scan recipe output: %w", err)
		}
		outputs = append(outputs, out)
	}
	return outputs, rows.Err()
}

func (kb *SQLiteKB) loadAllRecipeInputs(ctx context.Context) (map[string][]RecipeIngredient, error) {
	rows, err := kb.db.QueryContext(ctx, `SELECT recipe_id, item_id, quantity FROM recipe_inputs`)
	if err != nil {
		return nil, fmt.Errorf("failed to query all recipe inputs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string][]RecipeIngredient)
	for rows.Next() {
		var recipeID string
		var inp RecipeIngredient
		if err := rows.Scan(&recipeID, &inp.ItemID, &inp.Quantity); err != nil {
			return nil, fmt.Errorf("failed to scan recipe input: %w", err)
		}
		result[recipeID] = append(result[recipeID], inp)
	}
	return result, rows.Err()
}

func (kb *SQLiteKB) loadAllRecipeOutputs(ctx context.Context) (map[string][]RecipeProduct, error) {
	rows, err := kb.db.QueryContext(ctx, `SELECT recipe_id, item_id, quantity, quality_mod FROM recipe_outputs`)
	if err != nil {
		return nil, fmt.Errorf("failed to query all recipe outputs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string][]RecipeProduct)
	for rows.Next() {
		var recipeID string
		var out RecipeProduct
		if err := rows.Scan(&recipeID, &out.ItemID, &out.Quantity, &out.QualityMod); err != nil {
			return nil, fmt.Errorf("failed to scan recipe output: %w", err)
		}
		result[recipeID] = append(result[recipeID], out)
	}
	return result, rows.Err()
}
