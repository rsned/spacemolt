---
name: reference_catalog_items_tradeable_drift
description: "Server catalog_items JSON omits `tradeable` for modules — they import as tradeable=0 by default; bulk tools must compensate."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 73e356b1-e0c3-4d21-8b72-cfe61b9cf707
---

The server's `catalog(items)` JSON only emits `tradeable` for consumable/material/ore-class items. Anything with `type_id` set (every ship module — weapon/defense/mining/utility/drone) ships **without** the field, so JSON decode leaves Go's zero-value `false` and `import-catalog-items` writes `tradeable=0` into `items.tradeable` in the crafting DB.

Live market behavior contradicts that: `create_buy_order` and `create_sell_order` happily accept module IDs, and `view_orders` shows real listings for them. The DB flag is wrong, not the server.

**Fix applied (commit ec6c87c, 2026-05-28):**
- `cmd/data/import-catalog-items/main.go` — `convertItem` now forces `tradeable=true` when `j.TypeID != ""`
- `cmd/tools/bulk-buy-order/main.go` — `loadItemIDs` SQL `WHERE` now permits `category IN ('weapon','defense','mining','utility','drone')` regardless of the tradeable bit, so existing DBs work without re-import

**When this matters:** any tool that filters items by `items.tradeable=1` will silently drop ~175 modules unless it adds the same module-category escape hatch, OR the DB has been re-imported with the fixed converter. Check both paths before assuming the filter is correct.

Related: [[reference_catalog_recipes_shape]] (the analogous shape quirk for `catalog(recipes)`).
