package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// StoreItems stores or replaces all items in the catalog.
func (kb *SQLiteKB) StoreItems(ctx context.Context, items []CatalogItem) error {
	tx, err := kb.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "DELETE FROM items"); err != nil {
		return fmt.Errorf("failed to clear items: %w", err)
	}

	for _, item := range items {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO items (id, name, description, category, rarity, size, base_value, stackable, tradeable)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.ID, item.Name, item.Description, item.Category, item.Rarity,
			item.Size, item.BaseValue, item.Stackable, item.Tradeable)
		if err != nil {
			return fmt.Errorf("failed to insert item %s: %w", item.ID, err)
		}
	}

	return tx.Commit()
}

// GetItem retrieves a single item by ID.
func (kb *SQLiteKB) GetItem(ctx context.Context, itemID string) (*CatalogItem, error) {
	var item CatalogItem
	err := kb.db.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(description, ''), COALESCE(category, ''), COALESCE(rarity, ''),
		       size, base_value, stackable, tradeable
		FROM items WHERE id = ?
	`, itemID).Scan(&item.ID, &item.Name, &item.Description, &item.Category, &item.Rarity,
		&item.Size, &item.BaseValue, &item.Stackable, &item.Tradeable)
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
	return kb.queryItems(ctx, "SELECT id, name, COALESCE(description, ''), COALESCE(category, ''), COALESCE(rarity, ''), size, base_value, stackable, tradeable FROM items ORDER BY name")
}

// GetItemsByCategory retrieves items filtered by category.
func (kb *SQLiteKB) GetItemsByCategory(ctx context.Context, category string) ([]CatalogItem, error) {
	return kb.queryItems(ctx, "SELECT id, name, COALESCE(description, ''), COALESCE(category, ''), COALESCE(rarity, ''), size, base_value, stackable, tradeable FROM items WHERE category = ? ORDER BY name", category)
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
			&item.Size, &item.BaseValue, &item.Stackable, &item.Tradeable); err != nil {
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

	if _, err := tx.ExecContext(ctx, "DELETE FROM ship_class_build_materials"); err != nil {
		return fmt.Errorf("failed to clear build materials: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM ship_classes"); err != nil {
		return fmt.Errorf("failed to clear ship classes: %w", err)
	}

	for _, sc := range classes {
		reqSkills, _ := json.Marshal(sc.RequiredSkills)
		defMods, _ := json.Marshal(sc.DefaultModules)
		flavTags, _ := json.Marshal(sc.FlavorTags)

		_, err := tx.ExecContext(ctx, `
			INSERT INTO ship_classes (id, name, class, category, description, lore, faction,
				tier, scale, price, base_hull, base_shield, base_shield_recharge, base_armor,
				base_speed, base_fuel, cargo_capacity, cpu_capacity, power_capacity,
				weapon_slots, defense_slots, utility_slots, build_time, shipyard_tier,
				starter_ship, tow_speed_bonus, required_skills, default_modules, flavor_tags,
				last_updated_tick)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, sc.ID, sc.Name, sc.Class, sc.Category, sc.Description, sc.Lore, sc.Faction,
			sc.Tier, sc.Scale, sc.Price, sc.BaseHull, sc.BaseShield, sc.BaseShieldRecharge, sc.BaseArmor,
			sc.BaseSpeed, sc.BaseFuel, sc.CargoCapacity, sc.CPUCapacity, sc.PowerCapacity,
			sc.WeaponSlots, sc.DefenseSlots, sc.UtilitySlots, sc.BuildTime, sc.ShipyardTier,
			sc.StarterShip, sc.TowSpeedBonus, string(reqSkills), string(defMods), string(flavTags),
			sc.LastUpdatedTick)
		if err != nil {
			return fmt.Errorf("failed to insert ship class %s: %w", sc.ID, err)
		}

		for _, mat := range sc.BuildMaterials {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO ship_class_build_materials (ship_class_id, item_id, quantity)
				VALUES (?, ?, ?)
			`, sc.ID, mat.ItemID, mat.Quantity)
			if err != nil {
				return fmt.Errorf("failed to insert build material for %s: %w", sc.ID, err)
			}
		}
	}

	return tx.Commit()
}

// GetShipClass retrieves a single ship class by ID.
func (kb *SQLiteKB) GetShipClass(ctx context.Context, classID string) (*ShipClassDef, error) {
	sc, err := kb.scanShipClass(kb.db.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(class, ''), COALESCE(category, ''), COALESCE(description, ''),
			COALESCE(lore, ''), COALESCE(faction, ''), tier, scale, price,
			base_hull, base_shield, base_shield_recharge, base_armor, base_speed, base_fuel,
			cargo_capacity, cpu_capacity, power_capacity, weapon_slots, defense_slots, utility_slots,
			build_time, shipyard_tier, starter_ship, tow_speed_bonus,
			required_skills, default_modules, flavor_tags, last_updated_tick
		FROM ship_classes WHERE id = ?
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
		SELECT id, name, COALESCE(class, ''), COALESCE(category, ''), COALESCE(description, ''),
			COALESCE(lore, ''), COALESCE(faction, ''), tier, scale, price,
			base_hull, base_shield, base_shield_recharge, base_armor, base_speed, base_fuel,
			cargo_capacity, cpu_capacity, power_capacity, weapon_slots, defense_slots, utility_slots,
			build_time, shipyard_tier, starter_ship, tow_speed_bonus,
			required_skills, default_modules, flavor_tags, last_updated_tick
		FROM ship_classes ORDER BY name
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
		SELECT ship_class_id, item_id, quantity FROM ship_class_build_materials
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
	rows, err := kb.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(class, ''), COALESCE(category, ''), COALESCE(description, ''),
			COALESCE(lore, ''), COALESCE(faction, ''), tier, scale, price,
			base_hull, base_shield, base_shield_recharge, base_armor, base_speed, base_fuel,
			cargo_capacity, cpu_capacity, power_capacity, weapon_slots, defense_slots, utility_slots,
			build_time, shipyard_tier, starter_ship, tow_speed_bonus,
			required_skills, default_modules, flavor_tags, last_updated_tick
		FROM ship_classes WHERE class = ? ORDER BY price ASC
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
	var reqSkillsJSON, defModsJSON, flavTagsJSON string
	err := row.Scan(&sc.ID, &sc.Name, &sc.Class, &sc.Category, &sc.Description,
		&sc.Lore, &sc.Faction, &sc.Tier, &sc.Scale, &sc.Price,
		&sc.BaseHull, &sc.BaseShield, &sc.BaseShieldRecharge, &sc.BaseArmor, &sc.BaseSpeed, &sc.BaseFuel,
		&sc.CargoCapacity, &sc.CPUCapacity, &sc.PowerCapacity, &sc.WeaponSlots, &sc.DefenseSlots, &sc.UtilitySlots,
		&sc.BuildTime, &sc.ShipyardTier, &sc.StarterShip, &sc.TowSpeedBonus,
		&reqSkillsJSON, &defModsJSON, &flavTagsJSON, &sc.LastUpdatedTick)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(reqSkillsJSON), &sc.RequiredSkills)
	_ = json.Unmarshal([]byte(defModsJSON), &sc.DefaultModules)
	_ = json.Unmarshal([]byte(flavTagsJSON), &sc.FlavorTags)
	return &sc, nil
}

func (kb *SQLiteKB) scanShipClassRow(rows *sql.Rows) (*ShipClassDef, error) {
	var sc ShipClassDef
	var reqSkillsJSON, defModsJSON, flavTagsJSON string
	err := rows.Scan(&sc.ID, &sc.Name, &sc.Class, &sc.Category, &sc.Description,
		&sc.Lore, &sc.Faction, &sc.Tier, &sc.Scale, &sc.Price,
		&sc.BaseHull, &sc.BaseShield, &sc.BaseShieldRecharge, &sc.BaseArmor, &sc.BaseSpeed, &sc.BaseFuel,
		&sc.CargoCapacity, &sc.CPUCapacity, &sc.PowerCapacity, &sc.WeaponSlots, &sc.DefenseSlots, &sc.UtilitySlots,
		&sc.BuildTime, &sc.ShipyardTier, &sc.StarterShip, &sc.TowSpeedBonus,
		&reqSkillsJSON, &defModsJSON, &flavTagsJSON, &sc.LastUpdatedTick)
	if err != nil {
		return nil, fmt.Errorf("failed to scan ship class: %w", err)
	}
	_ = json.Unmarshal([]byte(reqSkillsJSON), &sc.RequiredSkills)
	_ = json.Unmarshal([]byte(defModsJSON), &sc.DefaultModules)
	_ = json.Unmarshal([]byte(flavTagsJSON), &sc.FlavorTags)
	return &sc, nil
}

func (kb *SQLiteKB) loadBuildMaterials(ctx context.Context, shipClassID string) ([]BuildMaterial, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT item_id, quantity FROM ship_class_build_materials WHERE ship_class_id = ?
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
				xp_per_level, bonus_per_level, required_skills)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, s.ID, s.Name, s.Description, s.Category, s.MaxLevel, s.TrainingSource,
			string(xpJSON), string(bonusJSON), string(reqJSON))
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
				skill_quality_mod, required_skills, last_updated_tick)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, r.ID, r.Name, r.Description, r.Category, r.CraftingTime, r.BaseQuality,
			r.SkillQualityMod, string(reqJSON), r.LastUpdatedTick)
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
			base_quality, skill_quality_mod, required_skills, last_updated_tick
		FROM recipes WHERE id = ?
	`, recipeID).Scan(&r.ID, &r.Name, &r.Description, &r.Category, &r.CraftingTime,
		&r.BaseQuality, &r.SkillQualityMod, &reqJSON, &r.LastUpdatedTick)
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
			base_quality, skill_quality_mod, required_skills, last_updated_tick
		FROM recipes ORDER BY name
	`)
}

// GetRecipesByCategory retrieves recipes filtered by category.
func (kb *SQLiteKB) GetRecipesByCategory(ctx context.Context, category string) ([]RecipeDef, error) {
	return kb.queryRecipes(ctx, `
		SELECT id, name, COALESCE(description, ''), COALESCE(category, ''), crafting_time,
			base_quality, skill_quality_mod, required_skills, last_updated_tick
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
			&r.BaseQuality, &r.SkillQualityMod, &reqJSON, &r.LastUpdatedTick); err != nil {
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
