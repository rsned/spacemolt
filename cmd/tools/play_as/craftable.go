package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/rsned/spacemolt/pkg/craftplan"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"

	_ "modernc.org/sqlite"
)

// playAsSource adapts game.GameClient + the crafting DB to craftplan.Source.
// One instance is created per `craftable` / `plan` REPL invocation; the
// Engine on top caches the recipe catalog across calls within the session.
type playAsSource struct {
	client     game.GameClient
	craftingDB *sql.DB // may be nil → BOM and IllegalAt return empty/errors
}

func newPlayAsSource(client game.GameClient, craftingDB *sql.DB) *playAsSource {
	return &playAsSource{client: client, craftingDB: craftingDB}
}

func (s *playAsSource) Recipes(ctx context.Context, refresh bool) (map[string]serverapi.Recipe, error) {
	// Server replaced get_recipes with catalog(type="recipes"). The catalog
	// response paginates (max page_size=50) and stores raw JSON under the
	// "catalog" key; the recipes field is a RawMessage holding []Recipe
	// (array, not the old map).
	const pageSize = 50
	out := map[string]serverapi.Recipe{}
	for page := 1; ; page++ {
		if err := s.client.Catalog(ctx, "recipes", page, pageSize); err != nil {
			return nil, fmt.Errorf("catalog recipes page %d: %w", page, err)
		}
		raw := s.client.GetRawJSON("catalog")
		if len(raw) == 0 {
			return nil, fmt.Errorf("catalog recipes returned empty payload")
		}
		var resp serverapi.CatalogResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, fmt.Errorf("decode catalog recipes page %d: %w", page, err)
		}
		if len(resp.Recipes) > 0 {
			var recipes []serverapi.Recipe
			if err := json.Unmarshal(resp.Recipes, &recipes); err != nil {
				return nil, fmt.Errorf("decode recipes array page %d: %w", page, err)
			}
			for _, r := range recipes {
				out[r.ID] = r
			}
		}
		if resp.TotalPages <= 0 || page >= resp.TotalPages {
			break
		}
	}
	return out, nil
}

func (s *playAsSource) Inventory(ctx context.Context, includeFaction bool) (craftplan.Inventory, error) {
	inv := craftplan.Inventory{
		Cargo:   map[string]int{},
		Storage: map[string]int{},
	}

	// Cargo from current state. Ship.Cargo is []CargoItem with Quantity float64.
	if state := s.client.GetState(); state != nil {
		for _, c := range state.Ship.Cargo {
			inv.Cargo[c.ItemID] += int(c.Quantity)
		}
	}

	// Personal storage at current station.
	if err := s.client.ViewStorage(ctx); err != nil {
		return inv, fmt.Errorf("view_storage: %w", err)
	}
	if raw := s.client.GetRawJSON("storage"); len(raw) > 0 {
		var st serverapi.ViewStorageResponse
		if err := json.Unmarshal(raw, &st); err == nil {
			for _, item := range st.Items {
				inv.Storage[item.ItemID] += int(item.Quantity)
			}
		}
	}

	if includeFaction {
		inv.Faction = map[string]int{}
		if err := s.client.ViewFactionStorage(ctx); err == nil {
			if raw := s.client.GetRawJSON("faction_storage"); len(raw) > 0 {
				var fs serverapi.ViewStorageResponse
				if err := json.Unmarshal(raw, &fs); err == nil {
					for _, item := range fs.Items {
						inv.Faction[item.ItemID] += int(item.Quantity)
					}
				}
			}
		}
		// Errors (e.g. not in a faction) are silently ignored; faction stays empty.
	}
	return inv, nil
}

func (s *playAsSource) Skills(ctx context.Context) (map[string]int, error) {
	state := s.client.GetState()
	if state == nil {
		return map[string]int{}, nil
	}
	out := make(map[string]int, len(state.Player.Skills))
	for id, skill := range state.Player.Skills {
		out[id] = skill.Level
	}
	return out, nil
}

func (s *playAsSource) CurrentStationID(ctx context.Context) (string, error) {
	state := s.client.GetState()
	if state == nil {
		return "", nil
	}
	if !state.Doc {
		return "", nil
	}
	return state.CurrentPOI, nil
}

func (s *playAsSource) IllegalAt(ctx context.Context, stationID string) (map[string]bool, error) {
	if s.craftingDB == nil || stationID == "" {
		return map[string]bool{}, nil
	}
	// A recipe is illegal at `stationID` unless its legal_location matches it
	// exactly. Empty/null legal_location = illegal everywhere.
	rows, err := s.craftingDB.QueryContext(ctx,
		`SELECT recipe_id FROM illegal_recipes WHERE legal_location != ? OR legal_location = '' OR legal_location IS NULL`,
		stationID,
	)
	if err != nil {
		return nil, fmt.Errorf("query illegal_recipes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan illegal_recipes: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (s *playAsSource) BOM(ctx context.Context, recipeIDs []string) (map[string][]craftplan.BOMRow, error) {
	if s.craftingDB == nil {
		return nil, fmt.Errorf("crafting DB not configured (set CRAFTING_DB or place DB at default path)")
	}
	if len(recipeIDs) == 0 {
		return map[string][]craftplan.BOMRow{}, nil
	}

	placeholders := make([]string, len(recipeIDs))
	args := make([]any, len(recipeIDs))
	for i, id := range recipeIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	q := fmt.Sprintf(
		`SELECT target_id, base_item_id, quantity, recipe_path
		 FROM bill_of_materials
		 WHERE target_type = 'item' AND target_id IN (%s)`,
		strings.Join(placeholders, ","),
	)

	rows, err := s.craftingDB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query bill_of_materials: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string][]craftplan.BOMRow{}
	for rows.Next() {
		var targetID, baseItem, pathJSON string
		var qty int
		if err := rows.Scan(&targetID, &baseItem, &qty, &pathJSON); err != nil {
			return nil, fmt.Errorf("scan bill_of_materials: %w", err)
		}
		var path []string
		if pathJSON != "" {
			if err := json.Unmarshal([]byte(pathJSON), &path); err != nil {
				// recipe_path malformed → skip just the path; row still counts toward base materials.
				path = nil
			}
		}
		out[targetID] = append(out[targetID], craftplan.BOMRow{
			BaseItemID: baseItem,
			Quantity:   qty,
			RecipePath: path,
		})
	}
	return out, rows.Err()
}

// ensureCraftingDB lazily opens the crafting SQLite DB and returns a cached
// handle. Returns nil if the DB can't be located — callers should treat that
// as "BOM unavailable" and degrade gracefully.
var (
	craftingDBMu sync.Mutex
	craftingDB   *sql.DB
)

func ensureCraftingDB() *sql.DB {
	craftingDBMu.Lock()
	defer craftingDBMu.Unlock()
	if craftingDB != nil {
		return craftingDB
	}
	path := os.Getenv("CRAFTING_DB")
	if path == "" {
		defaultPath := "../../spacemolt-crafting-server/database/crafting.db"
		if _, err := os.Stat(defaultPath); err == nil {
			path = defaultPath
		}
	}
	if path == "" {
		return nil
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil
	}
	craftingDB = db
	return db
}

// Compile-time assertion that *playAsSource satisfies craftplan.Source.
var _ craftplan.Source = (*playAsSource)(nil)

// handleCraftable wires the `craftable` REPL command. parts[0] is the verb,
// parts[1:] are flags / args.
func handleCraftable(client game.GameClient, ctx context.Context, parts []string, craftingDB *sql.DB, format outputFormat) error {
	_ = format
	flags := parseFlagArgs(parts[1:],
		"reachable", "category", "search", "include-faction",
		"detail", "recipe", "refresh", "max",
	)

	opts := craftplan.CraftableOpts{
		Reachable:      flagBool(flags["reachable"]),
		IncludeFaction: flagBool(flags["include-faction"]),
		Refresh:        flagBool(flags["refresh"]),
	}
	if v, ok := flags["category"]; ok {
		opts.CategoryFilter, _ = flagString(v)
	}
	if v, ok := flags["search"]; ok {
		opts.SearchFilter, _ = flagString(v)
	}
	if v, ok := flags["recipe"]; ok {
		opts.OneRecipe, _ = flagString(v)
	}
	if v, ok := flags["max"]; ok {
		if n, ok := flagInt(v); ok {
			opts.Max = n
		}
	}
	detail := flagBool(flags["detail"])

	src := newPlayAsSource(client, craftingDB)
	eng := craftplan.New(src)
	rows, err := eng.Craftable(ctx, opts)
	if err != nil {
		if errors.Is(err, craftplan.ErrBOMUnavailable) {
			fmt.Printf("BOM unavailable: %v\n  Install/update the crafting DB or omit --reachable.\n", err)
			return nil
		}
		return err
	}

	stationID, _ := src.CurrentStationID(ctx)
	if detail {
		fmt.Print(craftplan.FormatCraftableDetail(rows, craftplan.FormatCraftableOpts{
			StationID: stationID,
			Reachable: opts.Reachable,
		}))
	} else {
		fmt.Print(craftplan.FormatCraftableCompact(rows, craftplan.FormatCraftableOpts{
			StationID: stationID,
			Reachable: opts.Reachable,
		}))
	}
	return nil
}

// handlePlan wires the `plan <id> [qty]` REPL command.
func handlePlan(client game.GameClient, ctx context.Context, parts []string, craftingDB *sql.DB, format outputFormat) error {
	_ = format
	positional, flags := partitionFlags(parts[1:])
	if len(positional) < 1 {
		return fmt.Errorf("usage: plan <recipe-id-or-item-id> [qty] [--reachable] [--include-faction] [--detail]")
	}

	opts := craftplan.PlanOpts{
		ID:       positional[0],
		Quantity: 1,
	}
	if _, has := flags["reachable"]; has {
		v := flags["reachable"]
		opts.Reachable = v == "" || strings.EqualFold(v, "true") || v == "1"
	}
	if _, has := flags["include-faction"]; has {
		v := flags["include-faction"]
		opts.IncludeFaction = v == "" || strings.EqualFold(v, "true") || v == "1"
	}
	if _, has := flags["refresh"]; has {
		v := flags["refresh"]
		opts.Refresh = v == "" || strings.EqualFold(v, "true") || v == "1"
	}
	if len(positional) >= 2 {
		qty, err := strconv.Atoi(positional[1])
		if err != nil {
			return fmt.Errorf("invalid qty %q: %w", positional[1], err)
		}
		opts.Quantity = qty
	}

	src := newPlayAsSource(client, craftingDB)
	eng := craftplan.New(src)
	res, err := eng.Plan(ctx, opts)
	if err != nil {
		if errors.Is(err, craftplan.ErrBOMUnavailable) {
			fmt.Printf("BOM unavailable: %v\n  Install/update the crafting DB or omit --reachable.\n", err)
			return nil
		}
		return err
	}
	fmt.Print(craftplan.FormatPlan(res))
	return nil
}
