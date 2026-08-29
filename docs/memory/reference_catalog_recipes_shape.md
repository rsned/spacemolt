---
name: reference-catalog-recipes-shape
description: "catalog(type=recipes) returns recipes inside items[] with full Recipe shape, not the abbreviated CatalogItem"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 73e356b1-e0c3-4d21-8b72-cfe61b9cf707
---

`catalog` replaced `get_recipes` (server retired the latter — `unknown_command` error). The catalog command paginates (max `page_size=50`).

**Gotcha:** For `type="recipes"`, the per-entry objects come back inside the `items` array but their shape is the full `serverapi.Recipe` (id, name, category, inputs[{item_id,quantity}], outputs[{item_id,quantity}], crafting_time, description, facility_only, hidden, fuel_output). The shared `serverapi.CatalogResponse.Items` is typed `[]CatalogItem` (id/name/category/description only) — decoding through that struct **silently drops inputs/outputs/crafting_time**.

Workaround: decode through a local envelope that types `items` as `[]serverapi.Recipe`:

```go
type env struct {
    Items      []serverapi.Recipe `json:"items"`
    TotalPages int                `json:"total_pages"`
}
```

Raw JSON storage: the client classifier (`pkg/game/client.go:4025`) stores under `"catalog"` when payload has both `page` and `items`; otherwise under `"recipes"` if a `recipes` field is present. Read both keys defensively.

Also note: the catalog recipe schema does NOT include `required_skills` — server may have eliminated recipe skill requirements (see [[project_skills_removed_TBD]] when confirmed). The engine's skill-gate is effectively a no-op for recipes from this endpoint.

See `cmd/tools/play_as/craftable.go:Recipes()` for working adapter (commit `a6404dc`).
