package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/craftplan"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// parseDemandOptions converts `demand` flags into demandOptions.
func parseDemandOptions(args []string) (demandOptions, error) {
	opts := demandOptions{sort: sortByProceeds, only: onlyAll}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, val, hasEq := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		next := func() (string, error) {
			if hasEq {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("demand: %s requires a value", arg)
			}
			i++
			return args[i], nil
		}
		switch key {
		case "item":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.item = strings.ToLower(v)
		case "station":
			v, err := next()
			if err != nil {
				return opts, err
			}
			opts.station = v
		case "min-price":
			v, err := next()
			if err != nil {
				return opts, err
			}
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return opts, fmt.Errorf("demand: --min-price: %w", err)
			}
			opts.minPrice = n
		case "max-age":
			v, err := next()
			if err != nil {
				return opts, err
			}
			d, err := time.ParseDuration(v)
			if err != nil {
				return opts, fmt.Errorf("demand: --max-age: %w", err)
			}
			opts.maxAge = d
		case "limit":
			v, err := next()
			if err != nil {
				return opts, err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return opts, fmt.Errorf("demand: --limit: %w", err)
			}
			opts.limit = n
		case "station-only":
			opts.stationOnly = true
		case "hide-player-only":
			opts.hidePlayerOnly = true
		case "include-mine":
			opts.includeMine = true
		case "only":
			v, err := next()
			if err != nil {
				return opts, err
			}
			switch strings.ToLower(v) {
			case "fulfillable":
				opts.only = onlyFulfillable
			case "craftable":
				opts.only = onlyCraftable
			case "all":
				opts.only = onlyAll
			default:
				return opts, fmt.Errorf("demand: --only must be fulfillable|craftable|all")
			}
		case "sort":
			v, err := next()
			if err != nil {
				return opts, err
			}
			switch strings.ToLower(v) {
			case "price":
				opts.sort = sortByPrice
			case "age":
				opts.sort = sortByAge
			case "proceeds":
				opts.sort = sortByProceeds
			default:
				return opts, fmt.Errorf("demand: --sort must be price|proceeds|age")
			}
		default:
			return opts, fmt.Errorf("demand: unknown flag %q", arg)
		}
	}
	return opts, nil
}

// liveOnHand returns item_id -> (ship cargo + current-station storage) quantity.
func liveOnHand(client game.GameClient, ctx context.Context) map[string]float64 {
	out := map[string]float64{}
	if err := client.GetCargo(ctx); err == nil {
		var resp struct {
			Cargo []storageItem `json:"cargo"`
		}
		if raw := client.GetRawJSON("cargo"); len(raw) > 0 {
			if json.Unmarshal(raw, &resp) == nil {
				for _, c := range resp.Cargo {
					out[c.ItemID] += c.Quantity
				}
			}
		}
	}
	if err := client.ViewStorage(ctx); err == nil {
		var resp struct {
			Items []storageItem `json:"items"`
		}
		if raw := client.GetRawJSON("storage"); len(raw) > 0 {
			if json.Unmarshal(raw, &resp) == nil {
				for _, s := range resp.Items {
					out[s.ItemID] += s.Quantity
				}
			}
		}
	}
	return out
}

// liveCanCraft returns output item_id -> craftable batch count from the
// current inventory/skills (direct recipes only; no BOM/crafting DB needed).
func liveCanCraft(client game.GameClient, ctx context.Context) map[string]int {
	out := map[string]int{}
	src := newPlayAsSource(client, ensureCraftingDB())
	eng := craftplan.New(src)
	rows, _, err := eng.Craftable(ctx, craftplan.CraftableOpts{})
	if err != nil {
		return out
	}
	for _, r := range rows {
		if r.CanMake > out[r.OutputItemID] {
			out[r.OutputItemID] = r.CanMake
		}
	}
	return out
}

// runDemand loads the ledger, scores it against live inventory/craftability,
// and renders the report. Works offline from the ledger; inventory/craft
// signals degrade gracefully to empty when not connected/docked.
func runDemand(client game.GameClient, ctx context.Context, opts demandOptions, format outputFormat) error {
	if globalKB == nil {
		return fmt.Errorf("demand: no knowledge DB configured (start play_as with --db)")
	}
	sqlite, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return fmt.Errorf("demand: knowledge DB is not SQLite-backed")
	}
	deep, err := sqlite.LoadMarketBuyOrders(ctx)
	if err != nil {
		return fmt.Errorf("demand: load ledger: %w", err)
	}

	onHand := liveOnHand(client, ctx)
	canCraft := liveCanCraft(client, ctx)

	rep := buildDemandReport(deep, onHand, canCraft, time.Now(), opts)

	switch format {
	case formatStyled:
		fmt.Print(renderDemandStyled(rep))
	default:
		fmt.Print(renderDemandJSON(rep))
	}
	return nil
}
