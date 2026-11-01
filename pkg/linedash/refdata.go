package linedash

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Station is one station, reachable by any of its ids. Station ids are
// dual-named: fleet yamls pin agents by POI id (voss_redoubt) while facility
// and storage captures key by base id (voss_redoubt_station), so every lookup
// goes through RefData.Station.
type Station struct {
	BaseID, POIID, Name, SystemID, SystemName, Condition string
	SatisfactionPct                                      int
}

// Recipe is one recipe's per-run inputs and outputs.
type Recipe struct {
	ID, Name        string
	CraftingTime    float64 // the live catalog has fractional times (2.02)
	Inputs, Outputs map[string]int
}

// Facility is one of our faction's facilities at a station.
type Facility struct {
	BaseID, Type     string
	Level            int
	RecipeID, Status string
	CapturedAt       time.Time
}

// RefData is the slow-changing reference the derivation reads.
type RefData struct {
	Stations     map[string]Station // keyed by base id AND poi id
	Recipes      map[string]Recipe
	ItemCategory map[string]string // "module" for ship modules, which carry no items.category
	ItemName     map[string]string
	Facilities   map[string][]Facility     // base id -> our faction's facilities there
	Upkeep       map[string]map[string]int // facility catalog id AND display name -> item -> units per cycle
}

// Station resolves a base id or POI id.
func (r RefData) Station(id string) (Station, bool) {
	s, ok := r.Stations[id]
	return s, ok
}

// LoadRefData reads stations, recipes, item categories and our faction's
// facilities from the knowledge DB (read-only) and upkeep inputs from the
// facility catalog. factionID selects our facility rows.
func LoadRefData(ctx context.Context, kbPath, facilityCatalogPath, factionID string) (RefData, error) {
	db, err := openReadOnly(kbPath)
	if err != nil {
		return RefData{}, fmt.Errorf("knowledge db: %w", err)
	}
	defer func() { _ = db.Close() }()

	r := RefData{
		Stations: map[string]Station{}, Recipes: map[string]Recipe{},
		ItemCategory: map[string]string{}, ItemName: map[string]string{},
		Facilities: map[string][]Facility{}, Upkeep: map[string]map[string]int{},
	}
	steps := []func(context.Context, *sql.DB) error{
		r.loadStations,
		r.loadRecipes,
		r.loadItems,
		func(ctx context.Context, db *sql.DB) error { return r.loadFacilities(ctx, db, factionID) },
	}
	for _, step := range steps {
		if err := step(ctx, db); err != nil {
			return RefData{}, err
		}
	}
	if err := r.loadUpkeep(facilityCatalogPath); err != nil {
		return RefData{}, err
	}

	return r, nil
}

func (r RefData) loadStations(ctx context.Context, db *sql.DB) error {
	const q = `
SELECT b.id, COALESCE(b.poi_id, ''), COALESCE(b.name, ''), COALESCE(p.system_id, ''),
       COALESCE(s.name, ''), COALESCE(b.condition, ''), COALESCE(b.satisfaction_pct, 0)
FROM bases b
LEFT JOIN pois p ON p.id = b.poi_id
LEFT JOIN systems s ON s.id = p.system_id`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return fmt.Errorf("knowledge db stations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var st Station
		if err := rows.Scan(&st.BaseID, &st.POIID, &st.Name, &st.SystemID, &st.SystemName, &st.Condition, &st.SatisfactionPct); err != nil {
			return fmt.Errorf("knowledge db stations: %w", err)
		}
		r.Stations[st.BaseID] = st
		if st.POIID != "" {
			r.Stations[st.POIID] = st
		}
	}

	return rows.Err()
}

func (r RefData) loadRecipes(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(name, ''), COALESCE(crafting_time, 0) FROM recipes`)
	if err != nil {
		return fmt.Errorf("knowledge db recipes: %w", err)
	}
	for rows.Next() {
		rc := Recipe{Inputs: map[string]int{}, Outputs: map[string]int{}}
		if err := rows.Scan(&rc.ID, &rc.Name, &rc.CraftingTime); err != nil {
			_ = rows.Close()
			return fmt.Errorf("knowledge db recipes: %w", err)
		}
		r.Recipes[rc.ID] = rc
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, side := range []struct {
		table string
		pick  func(Recipe) map[string]int
	}{
		{"recipe_inputs", func(rc Recipe) map[string]int { return rc.Inputs }},
		{"recipe_outputs", func(rc Recipe) map[string]int { return rc.Outputs }},
	} {
		rows, err := db.QueryContext(ctx, `SELECT recipe_id, item_id, quantity FROM `+side.table) // #nosec G202 -- fixed table names
		if err != nil {
			return fmt.Errorf("knowledge db %s: %w", side.table, err)
		}
		for rows.Next() {
			var id, item string
			var qty float64
			if err := rows.Scan(&id, &item, &qty); err != nil {
				_ = rows.Close()
				return fmt.Errorf("knowledge db %s: %w", side.table, err)
			}
			if rc, ok := r.Recipes[id]; ok {
				side.pick(rc)[item] += int(qty)
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}

	return nil
}

func (r RefData) loadItems(ctx context.Context, db *sql.DB) error {
	const q = `
SELECT i.id, COALESCE(i.name, ''),
       CASE WHEN COALESCE(i.category, '') = '' AND EXISTS (SELECT 1 FROM item_modules m WHERE m.item_id = i.id)
            THEN 'module' ELSE COALESCE(i.category, '') END
FROM items i`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return fmt.Errorf("knowledge db items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, name, cat string
		if err := rows.Scan(&id, &name, &cat); err != nil {
			return fmt.Errorf("knowledge db items: %w", err)
		}
		r.ItemName[id], r.ItemCategory[id] = name, cat
	}

	return rows.Err()
}

func (r RefData) loadFacilities(ctx context.Context, db *sql.DB, factionID string) error {
	const q = `
SELECT base_id, facility_type, COALESCE(level, 0), COALESCE(recipe_id, ''), COALESCE(status, ''), COALESCE(captured_utc, '')
FROM faction_facilities WHERE faction_id = ?`
	rows, err := db.QueryContext(ctx, q, factionID)
	if err != nil {
		return fmt.Errorf("knowledge db faction_facilities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var f Facility
		var at string
		if err := rows.Scan(&f.BaseID, &f.Type, &f.Level, &f.RecipeID, &f.Status, &at); err != nil {
			return fmt.Errorf("knowledge db faction_facilities: %w", err)
		}
		f.CapturedAt, _ = time.Parse(time.RFC3339Nano, at) // zero = never, which reads stale
		r.Facilities[f.BaseID] = append(r.Facilities[f.BaseID], f)
	}

	return rows.Err()
}

func (r RefData) loadUpkeep(path string) error {
	raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied catalog path
	if err != nil {
		return fmt.Errorf("facility catalog: %w", err)
	}
	var cat struct {
		Items []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Maintenance []struct {
				ItemID   string `json:"item_id"`
				Quantity int    `json:"quantity"`
			} `json:"maintenance_inputs"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &cat); err != nil {
		return fmt.Errorf("facility catalog %s: %w", path, err)
	}
	for _, d := range cat.Items {
		if len(d.Maintenance) == 0 {
			continue
		}
		m := map[string]int{}
		for _, in := range d.Maintenance {
			m[in.ItemID] += in.Quantity
		}
		r.Upkeep[d.ID] = m
		if d.Name != "" {
			r.Upkeep[d.Name] = m // faction_facilities.facility_type carries the display name
		}
	}

	return nil
}

// LookupFactionID resolves a faction tag (CRFT) to its id from assets.db's
// faction_profile, which capture_faction keeps current.
func LookupFactionID(ctx context.Context, assetsPath, tag string) (string, error) {
	db, err := openReadOnly(assetsPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()
	var id string
	if err := db.QueryRowContext(ctx, `SELECT faction_id FROM faction_profile WHERE tag = ?`, tag).Scan(&id); err != nil {
		return "", fmt.Errorf("faction %s: %w", tag, err)
	}
	return id, nil
}
