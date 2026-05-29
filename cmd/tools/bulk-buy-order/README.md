# bulk-buy-order

Place buy orders for every item in the crafting database to seed market data. Orders are sent in bulk batches (up to 50 per API call).

## Usage

```sh
# Place 1-credit buy orders for all items
bulk-buy-order --agent=trader-1 --db=path/to/crafting.db

# Preview what would be sent
bulk-buy-order --agent=trader-1 --dry-run

# Only order ship modules and weapons
bulk-buy-order --agent=trader-1 --categories=defense,weapon,drone,utility,mining

# Retry a failed range (e.g. items 450+)
bulk-buy-order --agent=trader-1 --offset=450

# Send a single test order
bulk-buy-order --agent=trader-1 --offset=450 --limit=1

# Top-up mode: only re-order items that have been filled (skip items that
# still have an open buy order from this agent at the current station).
bulk-buy-order --agent=trader-1 --skip-open

# Top-up preview: connect, query view_orders, print what would be sent.
bulk-buy-order --agent=trader-1 --skip-open --dry-run

# Skip contraband (needs smuggling skill / pirate station to actually list).
bulk-buy-order --agent=trader-1 --exclude-categories=contraband
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--agent` | *(required)* | Agent ID for authentication |
| `--db` | auto-detect | Path to crafting SQLite DB (env: `CRAFTING_DB`) |
| `--price` | `1` | Price per unit in credits |
| `--quantity` | `1` | Quantity per item |
| `--categories` | *(all)* | Comma-separated item categories to **include** (whitelist) |
| `--exclude-categories` | *(none)* | Comma-separated item categories to **exclude** (blacklist; applied after `--categories`). Useful for dropping `contraband` (sellable only at pirate strongholds with smuggling skill) on a normal run. |
| `--batch-size` | `50` | Orders per API call (max 50) |
| `--offset` | `0` | Skip the first N items |
| `--limit` | `0` | Only send orders for N items (0 = all) |
| `--dry-run` | `false` | Print batches without sending |
| `--skip-open` | `false` | Query `view_orders` first and drop items that already have an open buy order from this agent at the current station. Top-up mode; forces a connect even with `--dry-run`. |
| `--include-untradeable` | `false` | Include items with `tradeable=0` (quest items, artifacts). Default filters them out at the SQL layer since `create_buy_order` rejects each one with `quest_item`/`not_tradeable`. Ship-module categories (`weapon`/`defense`/`mining`/`utility`/`drone`) are always allowed in the default run — the server's catalog omits `tradeable` for modules so the DB stores `0`, but the live market clearly accepts orders for them. Ignored with `--items-file`. |
| `--debug` | `false` | Enable debug logging |

## Notes

- Items are queried from the `items` table sorted by `id`, so `--offset` and `--limit` operate on that stable ordering.
- A 10-second delay (one game tick) is inserted between batches to avoid "action already pending" errors.
- The `--dry-run --debug` combination prints the full JSON payload for each batch.
- `--skip-open` runs the filter **before** `--offset`/`--limit`, so `--skip-open --limit 50` means "submit up to 50 *new* orders." Matching is by `item_id` only — any open buy order at any price suppresses a re-order, regardless of the `--price` you pass.
